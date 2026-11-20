package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"

	"go.uber.org/zap"
)

// dramaRun 汇集一个步骤的持久化输入，不持有数据库事务跨网络请求。
type dramaRun struct {
	project  *model.DramaProject
	job      *model.DramaJob
	snapshot provider.Snapshot
	episodes []model.DramaEpisode
	resumed  bool // 之前开始过（崩溃或队列重投后再次执行）
}

// dramaResult 保存步骤输出与实际费用；failure 非空表示部分写入后以它失败，按 cost 结算已写入的部分。
type dramaResult struct {
	output    any
	cost      int
	next      string
	newAssets []DramaCanvasAsset
	failure   error
}

// dramaCheckpoint 是画布写入与后续业务结算之间的持久化交接。拆解和资产提示词每写一次画布就在同一事务里累加
// Done / NewAssets / Prompted / Filtered / Cost，重跑时跳过已写入的部分，失败或取消时按 Cost 结算已写入的部分。
type dramaCheckpoint struct {
	Result          json.RawMessage    `json:"result,omitempty"`
	Cost            int                `json:"cost"`
	CanvasCommitted bool               `json:"canvas_committed"`
	NewAssets       []DramaCanvasAsset `json:"new_assets"`
	Done            []int              `json:"done,omitempty"`     // 已写入画布的拆解集号
	Prompted        []string           `json:"prompted,omitempty"` // 已写入提示词的节点
	Filtered        []DramaCanvasAsset `json:"filtered,omitempty"` // 触发安全过滤、没写成提示词的资产
}

// readDramaCheckpoint 读取任务检查点；空或无法解析时当作没有进度。
func readDramaCheckpoint(raw []byte) dramaCheckpoint {
	var cp dramaCheckpoint
	_ = json.Unmarshal(raw, &cp)
	return cp
}

// committedCost 是已写入画布的部分对应的费用，任务失败或取消时照此结算，其余退回。
func committedCost(j *model.DramaJob) int {
	cp := readDramaCheckpoint(j.Output)
	if len(cp.Done) == 0 && len(cp.Prompted) == 0 {
		return 0
	}
	return cp.Cost
}

// pendingPrompts 是已建节点里还没写提示词的资产（含被安全过滤的），按建节点顺序。
func (cp dramaCheckpoint) pendingPrompts() []DramaCanvasAsset {
	out := []DramaCanvasAsset{}
	for _, a := range cp.NewAssets {
		if !slices.Contains(cp.Prompted, a.NodeID) {
			out = append(out, a)
		}
	}
	return out
}

// unattemptedPrompts 是已建节点里既没写提示词、也没被安全过滤过的资产，重跑时先补上。
func (cp dramaCheckpoint) unattemptedPrompts() []DramaCanvasAsset {
	out := []DramaCanvasAsset{}
	for _, a := range cp.pendingPrompts() {
		if !slices.ContainsFunc(cp.Filtered, func(f DramaCanvasAsset) bool { return f.NodeID == a.NodeID }) {
			out = append(out, a)
		}
	}
	return out
}

// currentCheckpoint 重新读取任务检查点，汇总各批写入的进度。
func (s *DramaService) currentCheckpoint(ctx context.Context, j *model.DramaJob) (dramaCheckpoint, error) {
	current, err := s.repo.Job(ctx, j.UserID, j.DramaID, j.ID, false)
	if err != nil {
		return dramaCheckpoint{}, err
	}
	return readDramaCheckpoint(current.Output), nil
}

// HandleDrama 执行一个短剧步骤；模型业务失败退款，数据库错误交给River恢复。
func (s *DramaService) HandleDrama(ctx context.Context, userID, dramaID, jobID uint64) error {
	// 1. 在项目锁内领取任务；终态重复投递不会再调用模型。
	run, err := s.startDrama(ctx, userID, dramaID, jobID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if run == nil {
		return nil
	}
	ctx, logRun := dramaJobRun(ctx, run.job)
	startFields := append(dramaFields(run.job), zap.Bool("resumed", run.resumed))
	if !run.resumed && run.job.StartedAt != nil {
		startFields = append(startFields, zap.Int64("queue_wait_ms", run.job.StartedAt.Sub(run.job.CreatedAt).Milliseconds()))
	}
	logRun.Start(startFields...)
	s.publish(ctx, run.job)
	if run.job.Step == "compose" {
		return s.handleCompose(ctx, run)
	}
	// 2. 网络请求在事务外执行；有效输出写入持久checkpoint以支持崩溃恢复。模型 deadline 只限单次调用（见 dramaAttempt），
	// 多批 / 多集任务的总时长由队列的任务超时兜底。
	result, err := s.executeDrama(ctx, run)
	if err != nil {
		return s.failDrama(ctx, run.job, err)
	}
	// 3. 输出落库、结算和自动续步一起提交。
	err = s.commitDrama(ctx, run, result)
	var business *errcode.Error
	if run.job.Step == "storyboard" && errors.As(err, &business) {
		return s.failDrama(ctx, run.job, err)
	}
	return err
}

// startDrama 按project→job顺序加锁，保留首次开始时间。
func (s *DramaService) startDrama(ctx context.Context, userID, dramaID, jobID uint64) (*dramaRun, error) {
	run := &dramaRun{}
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var e error
		run.project, e = tx.Project(ctx, userID, dramaID, true)
		if e != nil {
			return e
		}
		run.job, e = tx.Job(ctx, userID, dramaID, jobID, true)
		if e != nil {
			return e
		}
		if !dramaActive(run.job.Status) {
			return nil
		}
		if e := json.Unmarshal(run.job.ConfigSnapshot, &run.snapshot); e != nil {
			return e
		}
		run.episodes, e = tx.Episodes(ctx, userID, dramaID)
		if e != nil {
			return e
		}
		run.resumed = run.job.StartedAt != nil
		if run.job.StartedAt == nil {
			now := time.Now()
			run.job.StartedAt = &now
		}
		run.job.Status = "running"
		return tx.UpdateJob(ctx, userID, jobID, map[string]any{"status": "running", "started_at": *run.job.StartedAt})
	})
	if err != nil {
		return nil, err
	}
	if !dramaActive(run.job.Status) {
		return nil, nil
	}
	return run, nil
}

// executeDrama 按步骤运行固定流水线；拆解和资产提示词从检查点跳过已写入画布的部分。
func (s *DramaService) executeDrama(ctx context.Context, run *dramaRun) (*dramaResult, error) {
	vars := s.dramaVariables(run.project)
	selected := selectedEpisodes(run.episodes, run.job.Episodes)
	result := &dramaResult{}
	var err error
	switch run.job.Step {
	case "analyze":
		result.output, result.cost, err = s.runAnalyze(ctx, run.project, run.job, &run.snapshot)

	case "plan", "outline":
		if err = json.Unmarshal(run.job.InputJSON, &vars); err != nil {
			return nil, err
		}
		// 创作段写分集目录，格式段整理成 episodes；单元覆盖只在格式段校验，校验失败只重试格式段
		draft, draftCost, planErr := s.callDrama(ctx, run.job, &run.snapshot, "plan", "", vars, nil)
		if planErr != nil {
			return nil, planErr
		}
		// 创作段是整批里最耗时的部分，写完先上报进度，格式段很快
		if err = s.dramaProgress(ctx, run.job, 50); err != nil {
			return nil, err
		}
		var formatCost int
		result.output, formatCost, err = s.callDrama(ctx, run.job, &run.snapshot, "plan_format", "", map[string]any{"draft_md": draft, "batch_from": run.job.BatchFrom, "batch_to": run.job.BatchTo}, func(v any) error { return v.(drama.PlanOutput).ValidateCoverage(run.job.BatchFrom, run.job.BatchTo) })
		result.cost = draftCost + formatCost
	case "scripts":
		result.output, result.cost, err = s.runScripts(ctx, run.project, run.job, &run.snapshot, run.episodes, selected)
	case "breakdown":
		return s.runBreakdown(ctx, run, vars, selected)
	case "asset_prompts":
		return s.runAssetPrompts(ctx, run, vars)
	case "storyboard":
		return s.runStoryboard(ctx, run, vars, selected)
	default:
		err = errcode.ErrInvalidParams.WithMsg("不支持的流水线步骤")
	}
	return result, err
}

// runBreakdown 并行拆解各批并边拆边写画布和提示词；还有没写成提示词的资产（被安全过滤等）时接一个提示词任务。
func (s *DramaService) runBreakdown(ctx context.Context, run *dramaRun, vars map[string]any, selected []model.DramaEpisode) (*dramaResult, error) {
	if s.canvas == nil {
		return nil, errcode.ErrInternal
	}
	if cp := readDramaCheckpoint(run.job.Output); len(cp.Done) > 0 {
		logger.Ctx(ctx).Info("短剧任务从检查点恢复", logger.Event(logger.EventRunStep), zap.Ints("done", cp.Done))
	}
	if err := s.breakdownBatches(ctx, run, vars, selected); err != nil {
		return nil, err
	}
	cp, err := s.currentCheckpoint(ctx, run.job)
	if err != nil {
		return nil, err
	}
	out, err := drama.DecodeOutput("breakdown", cp.Result)
	if err != nil {
		out = drama.BreakdownOutput{Assets: []drama.Asset{}}
	}
	result := &dramaResult{output: out, cost: cp.Cost, newAssets: cp.NewAssets}
	if len(cp.pendingPrompts()) > 0 {
		result.next = "asset_prompts"
	}
	return result, nil
}

// runAssetPrompts 为输入里还没写提示词的新资产写提示词；有资产被安全过滤时，写入其余资产后以安全过滤失败，
// 只扣已写入的部分，重试时只处理仍没有提示词的资产。
func (s *DramaService) runAssetPrompts(ctx context.Context, run *dramaRun, vars map[string]any) (*dramaResult, error) {
	var assets []DramaCanvasAsset
	if e := json.Unmarshal(run.job.InputJSON, &assets); e != nil || len(assets) == 0 {
		return nil, errcode.ErrDramaStepNotReady.WithMsg("没有待生成提示词的新资产")
	}
	if s.canvas == nil {
		return nil, errcode.ErrInternal
	}
	cp := readDramaCheckpoint(run.job.Output)
	cp.NewAssets = assets
	if err := s.promptAssets(ctx, run, vars, cp.unattemptedPrompts()); err != nil {
		return nil, err
	}
	cp, err := s.currentCheckpoint(ctx, run.job)
	if err != nil {
		return nil, err
	}
	result := &dramaResult{cost: cp.Cost}
	if len(cp.Filtered) > 0 {
		result.failure = errcode.ErrDramaContentFiltered.WithMsg(filteredMessage(cp.Filtered))
	}
	return result, nil
}

// commitDrama 输出、结算和自动后继任务在同一事务提交。
func (s *DramaService) commitDrama(ctx context.Context, run *dramaRun, result *dramaResult) error {
	var completed, following *model.DramaJob
	finished := false
	failedCode, failedMessage := 0, ""
	nextSnap := &run.snapshot
	var nextModelError error
	if pipelineJob(*run.job) {
		p, e := s.repo.Project(ctx, run.job.UserID, run.job.DramaID, false)
		if e != nil {
			return e
		}
		resolved, e := s.resolveModel(ctx, p.TextModel)
		if e == nil {
			nextSnap = resolved
		} else {
			nextModelError = e
		}
	}
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, e := tx.Project(ctx, run.job.UserID, run.job.DramaID, true)
		if e != nil {
			return e
		}
		current, e := tx.Job(ctx, run.job.UserID, run.job.DramaID, run.job.ID, true)
		if e != nil {
			return e
		}
		completed = current
		if !dramaActive(current.Status) {
			return nil
		}
		if e = s.applyDramaOutput(ctx, tx, p, current, result.output); e != nil {
			return e
		}
		audit := result.output
		if current.Step == "breakdown" || current.Step == "asset_prompts" {
			cp := readDramaCheckpoint(current.Output) // 保留各批进度，重试提示词时据此找出还没写的资产
			cp.CanvasCommitted = true
			audit = cp
		}
		if result.failure != nil {
			code, message := dramaFailure(result.failure)
			failedCode, failedMessage = code, message
			if e = s.finishTx(ctx, tx, current, "failed", code, message, result.cost, audit); e != nil {
				return e
			}
			return tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, map[string]any{"status": "failed"})
		}
		if e = s.finishTx(ctx, tx, current, "succeeded", 0, "", result.cost, audit); e != nil {
			return e
		}
		finished = true
		if nextModelError != nil && p.Phase == "running" && p.SourceKind != "script" && p.PlanCursor < p.RangeEnd {
			code, msg := dramaFailure(nextModelError)
			return setPlanningState(ctx, tx, p, "paused", planningPauseCode(code), msg)
		}
		following, e = s.continueDrama(ctx, tx, p, current, result, nextSnap)
		return e
	})
	if err != nil {
		return err
	}
	s.publish(ctx, completed)
	s.publish(ctx, following)
	s.publishDrama(ctx, run.job.UserID, run.job.DramaID)
	if finished && run.job.Step == "storyboard" {
		s.syncBoardNodes(ctx, run.job)
	}
	if failedCode != 0 && completed.Status == "failed" {
		logger.RunOf(ctx).End(logger.StatusFailed, zap.Int("error_code", failedCode), zap.String("error_message", failedMessage),
			zap.String("error_type", dramaErrorType(result.failure)), zap.Int("credits", completed.Credits), zap.Int("refunded", completed.FrozenCredits-completed.Credits))
	}
	if finished {
		fields := []zap.Field{zap.Int("credits", completed.Credits), zap.Int("frozen_credits", completed.FrozenCredits)}
		if following != nil {
			fields = append(fields, zap.String("next_job_id", idcodec.Encode(following.ID)))
		}
		logger.RunOf(ctx).End(logger.StatusSucceeded, fields...)
		logDramaCreated(ctx, following, completed)
	}
	if pipelineJob(*run.job) {
		eps, e := s.repo.Episodes(ctx, run.job.UserID, run.job.DramaID)
		if e != nil {
			return e
		}
		for _, ep := range eps {
			if ep.UnitTo >= run.job.BatchFrom {
				s.publishEpisode(ctx, run.job, ep.No)
			}
		}
	}
	return nil
}

// continueDrama 自动接续大纲或新资产提示词，复用本步骤模型快照。
func (s *DramaService) continueDrama(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, current *model.DramaJob, result *dramaResult, snap *provider.Snapshot) (*model.DramaJob, error) {
	if pipelineJob(*current) {
		return s.advancePlanningTx(ctx, tx, p, snap)
	}
	if result.next == "" {
		return nil, nil
	}
	req := &model.DramaJobReq{Step: result.next}
	var selected []model.DramaEpisode
	if result.next == "asset_prompts" {
		eps, e := tx.Episodes(ctx, p.UserID, p.ID)
		if e != nil {
			return nil, e
		}
		selected = selectedEpisodes(eps, current.Episodes)
	}
	following, e := s.enqueue(ctx, tx, p, req, snap, selected, 0, nil)
	if e != nil {
		return nil, e
	}
	return following, nil
}
