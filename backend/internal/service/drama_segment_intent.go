package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

const dramaRemakeTaskKeyPrefix = "drama-remake:"

// dramaSegmentIntentContext 仅能由已核对原镜与修订的内部入口注入。
type dramaSegmentIntentContext struct{}

// segmentRemakeOf 解出跟拍依据，普通片段与空历史字段返回 nil。
func segmentRemakeOf(row *model.DramaSegment) (*model.DramaSegmentRemake, error) {
	var remake *model.DramaSegmentRemake
	if len(row.Remake) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(row.Remake, &remake); err != nil {
		return nil, err
	}
	if remake == nil || remake.ReferenceID == "" {
		return nil, nil
	}
	return remake, nil
}

// generateRemake 固定一次用户意图，重传先找原输入，避免裁片或编辑变化产生第二笔任务。
func (s *DramaSegmentService) generateRemake(ctx context.Context, row *model.DramaSegment, key string, req *model.GenerateDramaSegmentReq) (*model.GenerationTaskView, error) {
	key = strings.TrimSpace(key)
	if req.Revision < 1 || key == "" || len(key) > 128 {
		return nil, errcode.ErrInvalidParams.WithMsg("仿拍生成须提供片段修订与幂等键")
	}
	if s.intents == nil {
		return nil, errcode.ErrInternal.WithMsg("仿拍生成服务未配置")
	}
	fingerprint, err := dramaSegmentFingerprint(req)
	if err != nil {
		return nil, errcode.ErrInvalidParams
	}
	in, err := s.intents.Intent(ctx, row.UserID, key)
	if errors.Is(err, repository.ErrNotFound) {
		in, err = s.prepareSegmentIntent(ctx, row, key, fingerprint, req)
	}
	if err != nil {
		return nil, err
	}
	if in.SegmentID != row.ID || in.Fingerprint != fingerprint {
		return nil, errcode.ErrInvalidParams.WithMsg("幂等键已用于其他片段或生成参数")
	}
	return s.submitSegmentIntent(ctx, in)
}

// dramaSegmentFingerprint 使用规范 JSON 比较请求，字段顺序变化不产生新的生成意图。
func dramaSegmentFingerprint(req *model.GenerateDramaSegmentReq) (string, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// prepareSegmentIntent 在事务外裁片，随后加项目锁复核版本，只有完整准备成功才保存输入。
func (s *DramaSegmentService) prepareSegmentIntent(ctx context.Context, row *model.DramaSegment, key, fingerprint string, req *model.GenerateDramaSegmentReq) (*model.DramaSegmentGenerationIntent, error) {
	if row.Revision != req.Revision {
		return nil, errcode.ErrCanvasConflict
	}
	input, err := s.remakeSegmentInput(ctx, row, req)
	if err != nil {
		return nil, err
	}
	remake, err := segmentRemakeOf(row)
	if err != nil {
		return nil, err
	}
	if remake != nil && !modelInputCoversWindow(input, remake) {
		return nil, errcode.ErrInvalidParams.WithMsg("模型生成时长不足以覆盖成片裁剪区间")
	}
	in := &model.DramaSegmentGenerationIntent{UserID: row.UserID, RequestKey: key, TaskKey: dramaRemakeTaskKeyPrefix + uuid.NewString(), SegmentID: row.ID,
		SegmentRevision: row.Revision, Fingerprint: fingerprint, ModelID: req.ModelID, Input: input, RemakeSnapshot: append([]byte{}, row.Remake...)}
	err = s.intents.WithTx(ctx, func(tx repository.DramaSegmentIntentTx) error {
		p, ep, current, err := lockSegment(ctx, tx, row.UserID, row.ID)
		if err != nil {
			return segmentError(err)
		}
		if current.Revision != row.Revision {
			return errcode.ErrCanvasConflict
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
		in.DramaID, in.CanvasID, in.EpisodeID = p.ID, p.CanvasID, ep.ID
		in, err = tx.InsertIntent(ctx, in)
		return err
	})
	return in, err
}

// modelInputCoversWindow 确保申请的模型视频能覆盖采用素材裁剪的终点，避免生成后才发现无法合成。
func modelInputCoversWindow(input []byte, remake *model.DramaSegmentRemake) bool {
	var raw map[string]any
	if json.Unmarshal(input, &raw) != nil {
		return false
	}
	value, ok := raw["duration"]
	if !ok {
		return true
	}
	var seconds float64
	switch number := value.(type) {
	case float64:
		seconds = number
	case json.Number:
		seconds, _ = number.Float64()
	case string:
		seconds, _ = strconv.ParseFloat(number, 64)
	}
	return seconds*1000 >= float64(remake.TrimStartMs+remake.TargetDurationMs)
}

// guardSegmentIntent 只接受内部意图的固定依据，并以模型规范化后的时长再校验一次。
func guardSegmentIntent(ctx context.Context, task *model.GenerationTask, row *model.DramaSegment) error {
	remake, err := segmentRemakeOf(row)
	if err != nil || remake == nil {
		return err
	}
	in, _ := ctx.Value(dramaSegmentIntentContext{}).(*model.DramaSegmentGenerationIntent)
	if in == nil || in.SegmentID != row.ID || in.UserID != task.UserID || in.TaskKey != task.IdempotencyKey {
		return errcode.ErrInvalidParams.WithMsg("请通过片场提交仿拍片段")
	}
	// 重传可以晚于编辑，申请时长必须覆盖原意图的窗口而非后来改大的窗口。
	var frozen model.DramaSegmentRemake
	if err := json.Unmarshal(in.RemakeSnapshot, &frozen); err != nil {
		return err
	}
	if !modelInputCoversWindow(task.InputJSON, &frozen) {
		return errcode.ErrInvalidParams.WithMsg("模型生成时长不足以覆盖成片裁剪区间")
	}
	return nil
}

// remakeSegmentInput 深拷贝客户端输入，原片参考始终替换为服务器依据已采用时码裁出的无声视频。
func (s *DramaSegmentService) remakeSegmentInput(ctx context.Context, row *model.DramaSegment, req *model.GenerateDramaSegmentReq) ([]byte, error) {
	raw, err := json.Marshal(req.Input)
	if err != nil {
		return nil, errcode.ErrInvalidParams
	}
	if !req.UseReferenceVideo {
		return raw, nil
	}
	remake, err := segmentRemakeOf(row)
	if err != nil || remake == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("片段没有可用的原片依据")
	}
	if s.referenceClips == nil || remake.AssetID == 0 || len(remake.Sources) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("原片参考不可用，请关闭视频参考后重试")
	}
	parts := make([]RemakeMediaPart, 0, len(remake.Sources))
	for _, source := range remake.Sources {
		if err := validateRemakeWindow(source.StartMs, source.EndMs-source.StartMs); err != nil {
			return nil, err
		}
		parts = append(parts, RemakeMediaPart{AssetID: remake.AssetID, StartMs: int(source.StartMs), EndMs: int(source.EndMs)})
	}
	id, err := s.referenceClips.Clip(ctx, row.UserID, remake.AssetID, parts)
	if err != nil {
		return nil, err
	}
	input, err := decodeSegmentInput(raw)
	if err != nil {
		return nil, err
	}
	input["videos"] = []string{assetIDString(id)}
	return json.Marshal(input)
}

// decodeSegmentInput 保留素材 ID 的整数精度，不把大整数中转为 float64。
func decodeSegmentInput(raw []byte) (map[string]any, error) {
	var input map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&input)
	return input, err
}

// submitSegmentIntent 使用固定输入恢复任务，模型后来下线也能返回已经创建的结果。
func (s *DramaSegmentService) submitSegmentIntent(ctx context.Context, in *model.DramaSegmentGenerationIntent) (*model.GenerationTaskView, error) {
	if task, err := s.intents.IntentTask(ctx, in); err == nil {
		return taskView(task), nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	input, err := decodeSegmentInput(in.Input)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, dramaSegmentIntentContext{}, in)
	req := &model.CreateGenerationTaskReq{Kind: model.KindVideo, ModelID: in.ModelID, CanvasID: idcodec.ID(in.CanvasID), NodeID: segmentNode(in.SegmentID), Input: input}
	result, err := s.tasks.Create(ctx, in.UserID, in.TaskKey, req)
	if err != nil {
		return nil, err
	}
	return segmentCreatedTask(result, in.CanvasID, in.SegmentID)
}

// segmentCreatedTask 复用正式任务的错误码，并拒绝把其他节点的任务当作当前片段结果。
func segmentCreatedTask(result *model.CreateGenerationTaskResp, canvasID, sid uint64) (*model.GenerationTaskView, error) {
	if result == nil || len(result.Items) != 1 {
		return nil, errors.New("片段任务创建未返回唯一结果")
	}
	item := result.Items[0]
	if item.Error != nil {
		return nil, errcode.New(item.Error.Code, item.Error.Message, item.Error.Status)
	}
	if item.Task == nil || item.Task.NodeID != segmentNode(sid) || item.Task.CanvasProjectID == nil || uint64(*item.Task.CanvasProjectID) != canvasID || item.Task.Kind != model.KindVideo {
		return nil, errcode.ErrInvalidParams.WithMsg("Idempotency-Key 已用于其他生成请求")
	}
	return item.Task, nil
}

// checkDramaRemakeSubmission 禁止公有生成入口复用内部幂等键，服务内部必须带匹配意图。
func checkDramaRemakeSubmission(ctx context.Context, userID uint64, key string, nodes []string) error {
	if !strings.HasPrefix(key, dramaRemakeTaskKeyPrefix) {
		return nil
	}
	in, _ := ctx.Value(dramaSegmentIntentContext{}).(*model.DramaSegmentGenerationIntent)
	if in == nil || in.UserID != userID || in.TaskKey != key || len(nodes) != 1 || nodes[0] != segmentNode(in.SegmentID) {
		return errcode.ErrInvalidParams.WithMsg("请通过片场提交仿拍片段")
	}
	return nil
}
