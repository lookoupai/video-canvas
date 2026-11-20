package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// DramaBoardCanvasRepo 故事板读写短剧画布要的原语：读画布、事务里 CAS 写回，以及认领任务时判断画布属于短剧。
type DramaBoardCanvasRepo interface {
	CanvasReader
	// WithTx 原子执行画布写入。
	WithTx(ctx context.Context, fn func(repository.DramaCanvasTx) error) error
	// IsDramaCanvas 画布是不是该用户某部短剧的专属画布。
	IsDramaCanvas(ctx context.Context, userID, canvasID uint64) (bool, error)
}

// boardWrite 是一次故事板画布写入的结果。
type boardWrite struct {
	before, revision uint64
	changes          []canvasgraph.Change
}

// GenerateBoard 片场生成故事板：先建图片任务，再把故事板节点写进短剧画布并绑定任务（同一次 CAS 写入），
// 推 canvas.patch 让已打开的编辑器合并。任务结束后由 OnTaskChanged 把结果写回节点。
// 归属检查事务先结束、再提交任务，避免同步观察者再次锁项目造成死锁；任务没建成时画布不动。
func (s *DramaSegmentService) GenerateBoard(ctx context.Context, userID, sid uint64, key string, req *model.GenerateDramaBoardReq) (*model.GenerationTaskView, error) {
	if s.canvas == nil {
		return nil, errors.New("故事板未配置画布仓储")
	}
	// 1. 片段要属于当前用户、所在集已锁定，分镜在重新生成时不能出图（片段会被清空）。
	var project *model.DramaProject
	var episodeNo, seq int
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, ep, row, err := lockSegment(ctx, tx, userID, sid)
		if err != nil {
			return err
		}
		if ep.LockedAt == nil {
			return errcode.ErrDramaStepNotReady.WithMsg("媒体任务只允许已锁定的集")
		}
		jobs, err := tx.Jobs(ctx, p.UserID, p.ID, true)
		if err != nil {
			return err
		}
		if dramaJobsConflict(jobs, &model.DramaJobReq{Step: "segment", Episodes: []int{ep.No}}) {
			return errcode.ErrDramaJobBusy
		}
		project, episodeNo, seq = p, ep.No, row.Seq
		return nil
	})
	if err != nil {
		return nil, segmentError(err)
	}
	segmentID := idcodec.Encode(sid)

	// 2. 任务挂在故事板节点上：已有节点沿用它的 id，没有就先定好新 id。
	nodeID, err := s.boardNodeID(ctx, userID, project.CanvasID, segmentID)
	if err != nil {
		return nil, err
	}
	task, err := s.createBoardTask(ctx, userID, key, project.CanvasID, nodeID, req)
	if err != nil {
		return nil, err
	}

	// 3. 写节点、连资产、绑定任务，一次 CAS 提交。
	spec := canvasgraph.BoardSpec{
		SegmentID: segmentID, EpisodeNo: episodeNo, Seq: seq, Label: req.Label,
		Prompt: req.Prompt, Params: req.Params, Model: req.ModelID, Refs: req.Refs, NewID: nodeID,
		Board: map[string]any{"segmentId": segmentID, "sourceHash": req.SourceHash, "layout": req.Layout, "shotCount": float64(req.ShotCount)},
	}
	taskID := strconv.FormatUint(task.ID, 10)
	out, err := s.writeBoardCanvas(ctx, userID, project.CanvasID, func(g *canvasgraph.Graph) *canvasgraph.Result {
		res, _ := canvasgraph.UpsertBoard(g, spec, taskID)
		return res
	})
	if err != nil {
		return nil, err
	}
	s.publishBoardPatch(ctx, userID, project.CanvasID, out)
	return task, nil
}

// SelectBoardOutput 把片段故事板的当前版本切到 outputID 这一版（生视频时带的就是它）。
// 片场没有挂画布编辑器，所以由后端写节点：校验片段归属，一次 CAS 改节点的当前版本，推 canvas.patch。
// 没有这个版本返回参数错误；已经是当前版本时什么都不改。
func (s *DramaSegmentService) SelectBoardOutput(ctx context.Context, userID, sid uint64, outputID string) error {
	if s.canvas == nil {
		return errors.New("故事板未配置画布仓储")
	}
	var canvasID uint64
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, _, _, err := lockSegment(ctx, tx, userID, sid)
		if err != nil {
			return err
		}
		canvasID = p.CanvasID
		return nil
	})
	if err != nil {
		return segmentError(err)
	}
	segmentID := idcodec.Encode(sid)
	exists := false
	out, err := s.writeBoardCanvas(ctx, userID, canvasID, func(g *canvasgraph.Graph) *canvasgraph.Result {
		exists = hasBoardOutput(g, segmentID, outputID)
		return canvasgraph.SelectBoardOutput(g, segmentID, outputID)
	})
	if err != nil {
		return err
	}
	if !exists {
		return errcode.ErrInvalidParams.WithMsg("没有这一版故事板")
	}
	s.publishBoardPatch(ctx, userID, canvasID, out)
	return nil
}

// hasBoardOutput 片段的故事板节点历史里有没有这一版。
func hasBoardOutput(g *canvasgraph.Graph, segmentID, outputID string) bool {
	n := canvasgraph.FindBoard(g, segmentID)
	if n == nil {
		return false
	}
	return canvasgraph.HasBoardOutput(n, outputID)
}

// boardNodeID 片段已有故事板节点时返回它的 id，没有返回新生成的 id。
func (s *DramaSegmentService) boardNodeID(ctx context.Context, userID, canvasID uint64, segmentID string) (string, error) {
	cv, err := s.canvas.GetCanvas(ctx, userID, canvasID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", errcode.ErrCanvasNotFound
	}
	if err != nil {
		return "", err
	}
	g, err := canvasgraph.Parse(cv.PayloadJSON)
	if err != nil {
		return "", errcode.ErrCanvasPayload
	}
	if n := canvasgraph.FindBoard(g, segmentID); n != nil {
		return n.ID(), nil
	}
	return uuid.NewString(), nil
}

// createBoardTask 用统一的生成任务服务建图片任务，逐项错误恢复成业务错误；幂等键用在别处时拒绝。
func (s *DramaSegmentService) createBoardTask(ctx context.Context, userID uint64, key string, canvasID uint64, nodeID string, req *model.GenerateDramaBoardReq) (*model.GenerationTaskView, error) {
	result, err := s.tasks.Create(ctx, userID, key, &model.CreateGenerationTaskReq{Kind: model.KindImage, ModelID: req.ModelID, CanvasID: idcodec.ID(canvasID), NodeID: nodeID, Input: req.Input})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Items) != 1 {
		return nil, errors.New("故事板任务创建未返回唯一结果")
	}
	item := result.Items[0]
	if item.Error != nil {
		return nil, errcode.New(item.Error.Code, item.Error.Message, item.Error.Status)
	}
	if item.Task == nil {
		return nil, errors.New("故事板任务创建结果缺少任务")
	}
	if item.Task.CanvasProjectID == nil || uint64(*item.Task.CanvasProjectID) != canvasID || item.Task.Kind != model.KindImage {
		return nil, errcode.ErrInvalidParams.WithMsg("Idempotency-Key 已用于其他生成请求")
	}
	return item.Task, nil
}

// writeBoardCanvas 在最新画布上执行 edit 并 CAS 写回；edit 返回 nil 或没有差异时不写。
func (s *DramaSegmentService) writeBoardCanvas(ctx context.Context, userID, canvasID uint64, edit func(*canvasgraph.Graph) *canvasgraph.Result) (boardWrite, error) {
	out, err := WriteCanvas(ctx, s.canvas, userID, canvasID, func(g *canvasgraph.Graph, revision uint64) (boardWrite, error) {
		out := boardWrite{before: revision, revision: revision}
		res := edit(g)
		if res == nil || len(res.Changes) == 0 {
			return out, nil
		}
		out.changes = res.Changes
		err := s.canvas.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
			if err := tx.CheckCanvasRevision(ctx, userID, canvasID, revision); err != nil {
				return err
			}
			var err error
			out.revision, err = commitCanvas(ctx, tx, userID, canvasID, revision, res.Graph, out.changes)
			return err
		})
		return out, err
	})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return out, errcode.ErrCanvasConflict
	}
	return out, err
}

// publishBoardPatch 写入成功后推同格式的 canvas.patch：已打开的编辑器合并，片场据此重读故事板。
func (s *DramaSegmentService) publishBoardPatch(ctx context.Context, userID, canvasID uint64, out boardWrite) {
	if len(out.changes) > 0 {
		PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{CanvasID: canvasID, Kind: "board", RevisionBefore: out.before, RevisionAfter: out.revision, Changes: out.changes})
	}
}

// onBoardTaskChanged 图片任务结束时，把结果写回短剧画布上绑着这个任务的故事板节点；
// 不是短剧画布、或画布上没有绑着它的故事板节点（普通图片节点由编辑器自己回写）时什么都不做。
func (s *DramaSegmentService) onBoardTaskChanged(ctx context.Context, task *model.GenerationTask) error {
	if s.canvas == nil || task.Kind != model.KindImage || !model.IsTerminalStatus(task.Status) {
		return nil
	}
	canvasID := *task.CanvasProjectID
	ok, err := s.canvas.IsDramaCanvas(ctx, task.UserID, canvasID)
	if err != nil || !ok {
		return err
	}
	outcome, err := boardOutcome(task)
	if err != nil {
		return err
	}
	out, err := s.writeBoardCanvas(ctx, task.UserID, canvasID, func(g *canvasgraph.Graph) *canvasgraph.Result {
		return canvasgraph.BackfillBoard(g, boardNodeOf(g, task), outcome)
	})
	if errors.Is(err, errcode.ErrCanvasNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s.publishBoardPatch(ctx, task.UserID, canvasID, out)
	return nil
}

// boardNodeOf 找绑着这个任务的故事板节点：先看任务挂的节点，不是就按 taskId 在故事板节点里找。
func boardNodeOf(g *canvasgraph.Graph, task *model.GenerationTask) string {
	taskID := strconv.FormatUint(task.ID, 10)
	if n := g.Node(task.NodeID); n != nil && canvasgraph.IsBoardNode(n) {
		return task.NodeID
	}
	for _, n := range g.Nodes {
		if canvasgraph.IsBoardNode(n) && n.Data()["taskId"] == taskID {
			return n.ID()
		}
	}
	return ""
}

// boardOutcome 把任务终态转成写回节点要的结果。
func boardOutcome(task *model.GenerationTask) (canvasgraph.BoardOutcome, error) {
	o := canvasgraph.BoardOutcome{
		TaskID: strconv.FormatUint(task.ID, 10), Status: task.Status, Error: task.ErrorMessage,
		TaskRef: task.TaskRef, Now: time.Now().UnixMilli(),
	}
	if task.Status != model.TaskSucceeded || len(task.OutputJSON) == 0 {
		return o, nil
	}
	var outputs []model.TaskOutput
	if err := json.Unmarshal(task.OutputJSON, &outputs); err != nil {
		return o, fmt.Errorf("解析故事板任务产物失败：%w", err)
	}
	for _, out := range outputs {
		if out.AssetID == 0 || out.URL == "" {
			continue
		}
		o.Outputs = append(o.Outputs, canvasgraph.BoardOutput{AssetID: strconv.FormatUint(out.AssetID, 10), URL: out.URL, MediaType: out.MediaType})
	}
	return o, nil
}
