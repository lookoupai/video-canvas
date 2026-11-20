package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// ComposeRemakePlan 按用户看到的计划修订固定选版清单，再交给后台队列合成。
func (s *MarketingService) ComposeRemakePlan(ctx context.Context, userID, planID uint64, key string, req *model.ComposeRemakePlanReq) (*model.RemakeCompositionView, error) {
	// 1. 网络重传必须能指回原来的提交。同一键只对应同一修订和镜头集合。
	if strings.TrimSpace(key) == "" || len(key) > maxIdempotencyKeyLen || req == nil || req.Revision < 1 {
		return nil, errcode.ErrInvalidParams
	}
	var result *model.MarketingRemakeComposition
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		// 2. 计划锁串行化选版、重排、快照和重试，避免组合出用户从没见过的版本。
		plan, err := tx.RemakePlan(ctx, userID, planID, true)
		if err != nil {
			return err
		}
		result, err = queueRemakeComposition(ctx, tx, plan, key, req)
		return err
	})
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	// 3. 素材访问地址通过现有存储服务解析，放在事务外。
	return s.remakeCompositionView(ctx, result)
}

// queueRemakeComposition 在计划锁内复用同一次提交或创建新的快照。
func queueRemakeComposition(ctx context.Context, tx repository.MarketingTx, plan *model.MarketingRemakePlan, key string, req *model.ComposeRemakePlanReq) (*model.MarketingRemakeComposition, error) {
	row, err := tx.RemakeCompositionByKey(ctx, plan.UserID, plan.ID, key)
	if err == nil {
		return row, retryRemakeComposition(ctx, tx, row, req)
	}
	if !mktNotFound(err) {
		return nil, err
	}
	if plan.Revision != req.Revision {
		return nil, errcode.ErrMarketingConflict
	}
	var data model.RemakePlanData
	if err := json.Unmarshal(plan.Data, &data); err != nil {
		return nil, err
	}
	tasks, err := tx.RemakeTakeTasks(ctx, plan.UserID, plan.ID)
	if err != nil {
		return nil, err
	}
	parts, err := remakeCompositionParts(plan, data.Shots, tasks, req.ShotIDs)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		return nil, err
	}
	row = &model.MarketingRemakeComposition{UserID: plan.UserID, PlanID: plan.ID, IdempotencyKey: key,
		Revision: plan.Revision, AllShots: req.ShotIDs == nil, Parts: raw, Status: "queued", Attempt: 1}
	if err := tx.InsertRemakeComposition(ctx, row); err != nil {
		return nil, err
	}
	return row, tx.EnqueueRemakeComposition(ctx, row.UserID, row.PlanID, row.ID, row.Attempt)
}

// retryRemakeComposition 仅重排失败记录，任务已经成功或在队列中就直接返回。
func retryRemakeComposition(ctx context.Context, tx repository.MarketingTx, row *model.MarketingRemakeComposition, req *model.ComposeRemakePlanReq) error {
	ids, err := compositionShotIDs(row)
	if err != nil {
		return err
	}
	sameSet := row.AllShots && req.ShotIDs == nil
	if req.ShotIDs != nil {
		requested := slices.Clone(req.ShotIDs)
		slices.Sort(requested)
		slices.Sort(ids)
		sameSet = slices.Equal(requested, ids)
	}
	if row.Revision != req.Revision || !sameSet {
		return errcode.ErrInvalidParams.WithMsg("同一个合成键已用于其他修订或镜头集合")
	}
	if row.Status != "failed" {
		return nil
	}
	row.Attempt++
	row.Status, row.Error = "queued", ""
	if err := tx.RetryRemakeComposition(ctx, row); err != nil {
		return err
	}
	return tx.EnqueueRemakeComposition(ctx, row.UserID, row.PlanID, row.ID, row.Attempt)
}

// remakeCompositionParts 依照计划顺序固定所有已选择镜头，列出缺镜而不隐式跳过。
func remakeCompositionParts(plan *model.MarketingRemakePlan, shots []model.RemakePlanShot, tasks []model.GenerationTask, ids []int) ([]model.RemakeCompositionPart, error) {
	selected := make(map[int]bool, len(ids))
	for _, id := range ids {
		if selected[id] {
			return nil, errcode.ErrInvalidParams.WithMsg("合成镜头重复")
		}
		selected[id] = true
	}
	byID := make(map[uint64]*model.GenerationTask, len(tasks))
	for i := range tasks {
		byID[tasks[i].ID] = &tasks[i]
	}
	var parts []model.RemakeCompositionPart
	var missing []string
	for i, shot := range shots {
		if ids != nil && !selected[shot.ID] {
			continue
		}
		delete(selected, shot.ID)
		var task *model.GenerationTask
		if shot.ChosenTaskID != nil {
			task = byID[*shot.ChosenTaskID]
		}
		part, ok, err := remakeCompositionPart(plan, shot, task)
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, fmt.Sprint(i+1))
			continue
		}
		parts = append(parts, part)
	}
	if len(selected) > 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("镜头不属于当前计划")
	}
	if len(missing) > 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("第 " + strings.Join(missing, "、") + " 镜缺少已采用的视频")
	}
	if len(parts) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("请选择要合成的镜头")
	}
	return parts, nil
}

// remakeCompositionPart 仅接受该用户本镜真实生成意图关联的成功任务；未选版则记为缺镜。
func remakeCompositionPart(plan *model.MarketingRemakePlan, shot model.RemakePlanShot, task *model.GenerationTask) (model.RemakeCompositionPart, bool, error) {
	var part model.RemakeCompositionPart
	if task == nil || task.Status != model.TaskSucceeded || task.UserID != plan.UserID || task.IsTest || task.Kind != model.KindVideo || task.NodeID != remakeShotNode(plan.ID, shot.ID) {
		return part, false, nil
	}
	assetID, err := segmentOutputAsset(task, model.KindVideo)
	if err != nil || assetID == 0 {
		return part, false, err
	}
	part = model.RemakeCompositionPart{ShotID: shot.ID, Revision: shot.Revision, TaskID: task.ID, AssetID: assetID, StartMs: shot.TrimStartMs, EndMs: shot.TrimStartMs + shot.DurationMs}
	return part, true, nil
}

// RemakeCompositions 列出本人的存活计划已提交的合成，最新在前。
func (s *MarketingService) RemakeCompositions(ctx context.Context, userID, planID uint64) ([]model.RemakeCompositionView, error) {
	// 1. 验证计划和项目归属，包括父项目的软删除状态。
	if _, err := s.repo.RemakePlan(ctx, userID, planID, false); err != nil {
		if mktNotFound(err) {
			return nil, errcode.ErrMarketingNotFound
		}
		return nil, err
	}
	// 2. 有成片时返回素材地址。旧清单在计划后续修改后仍能查看。
	rows, err := s.repo.ListRemakeCompositions(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	out := make([]model.RemakeCompositionView, 0, len(rows))
	for i := range rows {
		view, err := s.remakeCompositionView(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return out, nil
}

// compositionShotIDs 提取快照中按成片顺序排列的镜头 ID。
func compositionShotIDs(row *model.MarketingRemakeComposition) ([]int, error) {
	var parts []model.RemakeCompositionPart
	if err := json.Unmarshal(row.Parts, &parts); err != nil {
		return nil, err
	}
	ids := make([]int, len(parts))
	for i, p := range parts {
		ids[i] = p.ShotID
	}
	return ids, nil
}

// remakeCompositionView 转为公开视图，以素材服务校验并还原成片地址。
func (s *MarketingService) remakeCompositionView(ctx context.Context, row *model.MarketingRemakeComposition) (*model.RemakeCompositionView, error) {
	ids, err := compositionShotIDs(row)
	if err != nil {
		return nil, err
	}
	view := &model.RemakeCompositionView{ID: idcodec.Encode(row.ID), Revision: row.Revision, IdempotencyKey: row.IdempotencyKey,
		ShotIDs: ids, Status: row.Status, Error: row.Error, CreatedAt: row.CreatedAt}
	if row.AssetID != nil {
		view.Asset, err = s.assetViewOrNil(ctx, row.UserID, *row.AssetID)
	}
	return view, err
}
