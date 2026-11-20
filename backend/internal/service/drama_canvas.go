package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

// DramaCanvasRepo 是短剧对公共画布读写的依赖，事务中原子写入集锁和画布。
type DramaCanvasRepo interface {
	CanvasReader
	// WithTx 在同一事务执行 fn，任何错误都回滚关联写入。
	WithTx(ctx context.Context, fn func(repository.DramaCanvasTx) error) error
}

// DramaCanvasAsset 是本次新增节点与模型原始资产，供后续 asset_prompts 精确处理新增范围。
type DramaCanvasAsset struct {
	NodeID string      `json:"node_id"`
	Asset  drama.Asset `json:"asset"`
}

// DramaBreakdownBatch 是一批拆解结果：这批集号、模型给出的资产、这次调用的费用、
// 各类资产新建节点的默认图片模型 key 和默认生成参数（都可为 nil）。
type DramaBreakdownBatch struct {
	Episodes []int
	Assets   []drama.Asset
	Cost     int
	Models   map[string]string
	Params   map[string]map[string]string
}

// DramaCanvasService 提供不依赖 Agent run 的短剧画布写入口，复用公共图差异、CAS 重放和 patch 协议。
type DramaCanvasService struct {
	repo DramaCanvasRepo
	bc   ws.Broadcaster
}

// NewDramaCanvasService 创建短剧画布服务，bc 为 nil 时仅持久化而不广播。
func NewDramaCanvasService(repo DramaCanvasRepo, bc ws.Broadcaster) *DramaCanvasService {
	return &DramaCanvasService{repo: repo, bc: bc}
}

// breakdownAttempt 是一次 CAS 尝试的产物，仅事务提交后才对外可见。
type breakdownAttempt struct {
	created  []DramaCanvasAsset
	changes  []canvasgraph.Change
	before   uint64
	revision uint64
}

// dramaScope 是一次拆解事务的归属与范围。
type dramaScope struct {
	userID, dramaID, canvasID, jobID uint64
	episodes                         []int
	selected                         map[int]bool
}

// ApplyBreakdown 把一批资产合并进画布并原子锁定这批集，同一事务把集号、新节点、资产和费用累加进任务检查点；
// 新增角色直接写成带 character 元数据的节点，只返回本批新增节点。这批已写入过（重放）时什么都不做。
func (s *DramaCanvasService) ApplyBreakdown(ctx context.Context, userID, dramaID, canvasID, jobID uint64, batch DramaBreakdownBatch) ([]DramaCanvasAsset, error) {
	// 1. 先校验真实模型结果与选择范围，避免非法输出造成部分写画布或锁集。
	if err := (drama.BreakdownOutput{Assets: batch.Assets}).Validate(); err != nil {
		return nil, errcode.ErrDramaOutputInvalid
	}
	selected, err := dramaEpisodeSet(batch.Episodes)
	if err != nil {
		return nil, err
	}
	sc := dramaScope{userID: userID, dramaID: dramaID, canvasID: canvasID, jobID: jobID, episodes: batch.Episodes, selected: selected}
	// 2. CAS 冲突重读重放整个事务；失败尝试里的集锁一起回滚。角色数据内嵌在画布 payload 里，
	//    随画布 CAS 一起提交或丢弃，不再有需要单独回滚的角色卡。
	out, err := WriteCanvas(ctx, s.repo, userID, canvasID, func(g *canvasgraph.Graph, revision uint64) (breakdownAttempt, error) {
		attempt := breakdownAttempt{created: []DramaCanvasAsset{}, before: revision, revision: revision}
		err := s.repo.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
			return breakdownTx(ctx, tx, g, revision, &attempt, sc, batch)
		})
		return attempt, err
	})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return nil, errcode.ErrCanvasConflict
	}
	if err != nil {
		return nil, err
	}
	// 3. 只在事务完整提交后广播，失败和纯集锁事务均不产生画布事件。
	PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{CanvasID: canvasID, Kind: "breakdown", RevisionBefore: out.before, RevisionAfter: out.revision, Changes: out.changes})
	return out.created, nil
}

// dramaEpisodeSet 校验集号非空、为正且不重复。
func dramaEpisodeSet(episodes []int) (map[int]bool, error) {
	if len(episodes) == 0 {
		return nil, errcode.ErrDramaStepNotReady
	}
	selected := make(map[int]bool, len(episodes))
	for _, no := range episodes {
		if no < 1 || selected[no] {
			return nil, errcode.ErrDramaStepNotReady
		}
		selected[no] = true
	}
	return selected, nil
}

// lockedJobCheckpoint 读取并校验仍在运行的任务及其检查点。
func lockedJobCheckpoint(ctx context.Context, tx repository.DramaCanvasTx, userID, canvasID, jobID uint64, step string) (*model.DramaJob, dramaCheckpoint, error) {
	job, err := tx.JobForUpdate(ctx, userID, canvasID, jobID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, dramaCheckpoint{}, errcode.ErrDramaStepNotReady
	}
	if err != nil {
		return nil, dramaCheckpoint{}, err
	}
	if job.Status != "running" || job.Step != step {
		return nil, dramaCheckpoint{}, errcode.ErrDramaStepNotReady
	}
	return job, readDramaCheckpoint(job.Output), nil
}

// saveCheckpoint 把累加后的检查点合并进任务 output，与画布写入同一事务。
func saveCheckpoint(ctx context.Context, tx repository.DramaCanvasTx, userID, jobID uint64, cp dramaCheckpoint) error {
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	return tx.MarkCanvasCommitted(ctx, userID, jobID, raw)
}

// breakdownTx 在单个事务里完成任务校验、集锁校验、合并、画布 CAS、锁集与检查点累加。
func breakdownTx(ctx context.Context, tx repository.DramaCanvasTx, g *canvasgraph.Graph, revision uint64, attempt *breakdownAttempt, sc dramaScope, batch DramaBreakdownBatch) error {
	job, checkpoint, err := lockedJobCheckpoint(ctx, tx, sc.userID, sc.canvasID, sc.jobID, "breakdown")
	if err != nil {
		return err
	}
	if job.DramaID != sc.dramaID {
		return errcode.ErrDramaNotFound
	}
	if containsAll(checkpoint.Done, sc.episodes) {
		return nil
	}
	if err := checkEpisodesLockable(ctx, tx, sc); err != nil {
		return err
	}
	if err := tx.CheckCanvasRevision(ctx, sc.userID, sc.canvasID, revision); err != nil {
		return err
	}
	assets := batch.Assets
	graphAssets := make([]canvasgraph.DramaAsset, len(assets))
	for i, a := range assets {
		graphAssets[i] = canvasgraph.DramaAsset{Type: a.Type, Name: a.Name, Look: a.Look, Aliases: a.Aliases, Episodes: a.Episodes, Description: a.Description, VoicePrompt: a.VoicePrompt}
	}
	merged, err := canvasgraph.MergeDrama(g, graphAssets, canvasgraph.Options{DramaModels: batch.Models, DramaParams: batch.Params})
	if err != nil {
		return errcode.ErrDramaOutputInvalid
	}
	attempt.created = createdAssets(merged, assets)
	attempt.changes = canvasgraph.Diff(g, merged.Graph)
	if attempt.revision, err = commitCanvas(ctx, tx, sc.userID, sc.canvasID, revision, merged.Graph, attempt.changes); err != nil {
		return err
	}
	if err := tx.LockEpisodes(ctx, sc.userID, sc.dramaID, sc.episodes); err != nil {
		return err
	}
	var result drama.BreakdownOutput
	_ = json.Unmarshal(checkpoint.Result, &result)
	result.Assets = append(result.Assets, assets...)
	if checkpoint.Result, err = json.Marshal(result); err != nil {
		return err
	}
	checkpoint.Done = append(checkpoint.Done, sc.episodes...)
	checkpoint.NewAssets = append(checkpoint.NewAssets, attempt.created...)
	checkpoint.Cost += batch.Cost
	return saveCheckpoint(ctx, tx, sc.userID, sc.jobID, checkpoint)
}

// commitCanvas 有差异时 CAS 写回画布并返回新 revision；无差异保持原 revision。
func commitCanvas(ctx context.Context, tx repository.DramaCanvasTx, userID, canvasID, revision uint64, g *canvasgraph.Graph, changes []canvasgraph.Change) (uint64, error) {
	if len(changes) == 0 {
		return revision, nil
	}
	payload, err := g.Marshal()
	if err != nil {
		return revision, err
	}
	if err := tx.UpdateCanvas(ctx, userID, canvasID, revision, payload); err != nil {
		return revision, err
	}
	return revision + 1, nil
}

// checkEpisodesLockable 要求所选集全部存在、已有剧本且未锁定。
func checkEpisodesLockable(ctx context.Context, tx repository.DramaCanvasTx, sc dramaScope) error {
	rows, err := tx.EpisodesForUpdate(ctx, sc.userID, sc.dramaID, sc.canvasID, sc.episodes)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrDramaNotFound
	}
	if err != nil {
		return err
	}
	if len(rows) != len(sc.selected) {
		return errcode.ErrDramaStepNotReady
	}
	for _, ep := range rows {
		if !sc.selected[ep.No] || ep.LockedAt != nil || ep.ScriptStatus != "ready" {
			return errcode.ErrDramaStepNotReady
		}
	}
	return nil
}

// createdAssets 把合并结果里本次新增的节点与模型原始资产配对返回，供后续 asset_prompts 精确处理新增范围。
func createdAssets(merged *canvasgraph.DramaResult, assets []drama.Asset) []DramaCanvasAsset {
	created := make([]DramaCanvasAsset, 0, len(merged.Created))
	for _, c := range merged.Created {
		created = append(created, DramaCanvasAsset{NodeID: c.NodeID, Asset: assets[c.AssetIndex]})
	}
	return created
}

// SyncBoards 让一集的故事板节点和片段对上（见 canvasgraph.SyncBoards）：每个片段一个节点，没有就建空的，
// 片段已经没了的节点和连线清掉；有改动才写，同样推 canvas.patch。
func (s *DramaCanvasService) SyncBoards(ctx context.Context, userID, canvasID uint64, episodeNo int, segments []DramaBoardSegment) error {
	out, err := WriteCanvas(ctx, s.repo, userID, canvasID, func(g *canvasgraph.Graph, revision uint64) (breakdownAttempt, error) {
		out := breakdownAttempt{before: revision, revision: revision}
		res := canvasgraph.SyncBoards(g, episodeNo, segments)
		if res == nil {
			return out, nil
		}
		out.changes = res.Changes
		err := s.repo.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
			if err := tx.CheckCanvasRevision(ctx, userID, canvasID, revision); err != nil {
				return err
			}
			var err error
			out.revision, err = commitCanvas(ctx, tx, userID, canvasID, revision, res.Graph, res.Changes)
			return err
		})
		return out, err
	})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return errcode.ErrCanvasConflict
	}
	if err != nil {
		return err
	}
	if len(out.changes) > 0 {
		PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{CanvasID: canvasID, Kind: "board_sync", RevisionBefore: out.before, RevisionAfter: out.revision, Changes: out.changes})
	}
	return nil
}

// ApplyPrompts 写入一组节点提示词，同一事务把已写节点、被安全过滤的资产（filtered）和费用累加进 step 任务的检查点；
// 这组已写入过（重放）时什么都不做。
func (s *DramaCanvasService) ApplyPrompts(ctx context.Context, userID, canvasID, jobID uint64, step string, prompts []drama.AssetPrompt, filtered []DramaCanvasAsset, cost int) error {
	// 1. 只接受资源层校验通过的节点提示词，不把非法模型输出透传成画布编辑。
	if len(prompts) > 0 {
		if err := (drama.AssetPromptsOutput{Prompts: prompts}).Validate(); err != nil {
			return errcode.ErrDramaOutputInvalid
		}
	}
	pw := promptWrite{step: step, byID: make(map[string]string, len(prompts)), filtered: filtered, cost: cost}
	for _, prompt := range prompts {
		pw.byID[prompt.NodeID] = prompt.Prompt
		pw.ids = append(pw.ids, prompt.NodeID)
	}
	// 2. 用同一公共入口计算差异并事务 CAS，前端同时编辑的字段不会被旧快照覆盖。
	out, err := WriteCanvas(ctx, s.repo, userID, canvasID, func(g *canvasgraph.Graph, revision uint64) (breakdownAttempt, error) {
		out := breakdownAttempt{before: revision, revision: revision}
		err := s.repo.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
			return promptsTx(ctx, tx, g, revision, &out, userID, canvasID, jobID, pw)
		})
		return out, err
	})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return errcode.ErrCanvasConflict
	}
	if err != nil {
		return err
	}
	// 3. 提示词写入成功后发布同格式 patch，不依赖任何 Agent 运行或改动日志。
	if len(out.changes) > 0 {
		PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{CanvasID: canvasID, Kind: "asset_prompts", RevisionBefore: out.before, RevisionAfter: out.revision, Changes: out.changes})
	}
	return nil
}

// promptWrite 是一次提示词写入：所属步骤、节点提示词（ids 保持模型输出顺序）、被安全过滤的资产和这次调用的费用。
type promptWrite struct {
	step     string
	byID     map[string]string
	ids      []string
	filtered []DramaCanvasAsset
	cost     int
}

// promptsTx 在单个事务里校验任务、写入提示词并累加检查点。
func promptsTx(ctx context.Context, tx repository.DramaCanvasTx, g *canvasgraph.Graph, revision uint64, out *breakdownAttempt, userID, canvasID, jobID uint64, pw promptWrite) error {
	_, checkpoint, err := lockedJobCheckpoint(ctx, tx, userID, canvasID, jobID, pw.step)
	if err != nil {
		return err
	}
	if promptsRecorded(checkpoint, pw) {
		return nil
	}
	if len(pw.byID) > 0 {
		if err := tx.CheckCanvasRevision(ctx, userID, canvasID, revision); err != nil {
			return err
		}
		ed, err := canvasgraph.ApplyDramaPrompts(g, pw.byID)
		if err != nil {
			return errcode.ErrDramaOutputInvalid
		}
		out.changes = ed.Changes
		if out.revision, err = commitCanvas(ctx, tx, userID, canvasID, revision, ed.Graph, ed.Changes); err != nil {
			return err
		}
	}
	checkpoint.Prompted = append(checkpoint.Prompted, pw.ids...)
	checkpoint.Filtered = append(checkpoint.Filtered, pw.filtered...)
	checkpoint.Cost += pw.cost
	return saveCheckpoint(ctx, tx, userID, jobID, checkpoint)
}

// promptsRecorded 判断这组提示词与被过滤资产是否都已记进检查点（重放）。
func promptsRecorded(cp dramaCheckpoint, pw promptWrite) bool {
	if len(pw.ids) == 0 && len(pw.filtered) == 0 {
		return true
	}
	for _, id := range pw.ids {
		if !slices.Contains(cp.Prompted, id) {
			return false
		}
	}
	for _, f := range pw.filtered {
		if !slices.ContainsFunc(cp.Filtered, func(a DramaCanvasAsset) bool { return a.NodeID == f.NodeID }) {
			return false
		}
	}
	return true
}

// DeleteEpisode 删第 no 集：同一事务里删集和片段、后面的集号补位，并改写资产画布上的出现集数
// （后面的集号减 1，只出现在这一集的资产改为未分类）；有任务在跑时拒绝，免得任务写回错位的集。
// CAS 冲突时在最新画布上重放，成功后推 canvas.patch，开着的资产画布同步更新。
func (s *DramaCanvasService) DeleteEpisode(ctx context.Context, userID, dramaID, canvasID uint64, no int) error {
	out, err := WriteCanvas(ctx, s.repo, userID, canvasID, func(g *canvasgraph.Graph, revision uint64) (breakdownAttempt, error) {
		out := breakdownAttempt{before: revision, revision: revision}
		err := s.repo.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
			return deleteEpisodeTx(ctx, tx, g, revision, &out, userID, dramaID, canvasID, no)
		})
		return out, err
	})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return errcode.ErrCanvasConflict
	}
	if err != nil {
		return err
	}
	PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{CanvasID: canvasID, Kind: "delete_episode", RevisionBefore: out.before, RevisionAfter: out.revision, Changes: out.changes})
	return nil
}

// deleteEpisodeTx 锁顺序与拆解一致：项目 → 分集 → 画布。
func deleteEpisodeTx(ctx context.Context, tx repository.DramaCanvasTx, g *canvasgraph.Graph, revision uint64, out *breakdownAttempt, userID, dramaID, canvasID uint64, no int) error {
	if err := tx.LockProject(ctx, userID, dramaID); err != nil {
		return dramaError(err)
	}
	phase, err := tx.ProjectPhase(ctx, userID, dramaID)
	if err != nil {
		return dramaError(err)
	}
	if phase == "running" {
		return errcode.ErrDramaPhase
	}
	busy, err := tx.HasActiveJob(ctx, userID, dramaID)
	if err != nil {
		return err
	}
	if busy {
		return errcode.ErrDramaJobBusy
	}
	if err := tx.DeleteEpisode(ctx, userID, dramaID, no); err != nil {
		return dramaError(err)
	}
	if err := tx.CheckCanvasRevision(ctx, userID, canvasID, revision); err != nil {
		return err
	}
	ed := canvasgraph.RemoveDramaEpisode(g, no)
	out.changes = ed.Changes
	out.revision, err = commitCanvas(ctx, tx, userID, canvasID, revision, ed.Graph, ed.Changes)
	return err
}
