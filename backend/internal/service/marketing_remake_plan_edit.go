package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// GetRemakePlan 返回按当前镜序排列的编辑稿、单镜报价和真实生成版本。
func (s *MarketingService) GetRemakePlan(ctx context.Context, userID, id uint64) (*model.MarketingRemakePlanView, error) {
	// 1. 在本人可见项目内取出快照，只查询本计划生成意图关联的任务。
	row, data, err := s.loadRemakePlan(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.RemakeTakeTasks(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	byNode := make(map[string][]model.MarketingTaskLite)
	for i := range tasks {
		t := &tasks[i]
		byNode[t.NodeID] = append(byNode[t.NodeID], taskLite(t))
	}
	// 2. 没有可用生成模型时仍允许看旧版、选版和合成；界面显示就地原因。
	out := &model.MarketingRemakePlanView{MarketingRemakePlanSummary: remakePlanSummary(row), Revision: row.Revision, RefVideoAssetID: data.RefVideoAssetID, VideoModel: data.Settings.VideoModel, SourceCatalog: data.SourceCatalog, Shots: make([]model.MarketingRemakePlanShotView, 0, len(data.Shots))}
	for i := range data.Shots {
		shot := &data.Shots[i]
		takes := byNode[remakeShotNode(id, shot.ID)]
		if takes == nil {
			takes = []model.MarketingTaskLite{}
		}
		v := model.MarketingRemakePlanShotView{RemakePlanShot: *shot, Takes: takes}
		v.GenerateSeconds, v.RenderCredits, err = s.remakeShotQuote(ctx, data, shot)
		if err != nil {
			var e *errcode.Error
			if !errors.As(err, &e) {
				return nil, err
			}
			v.GenerateError = e.Msg
		}
		out.Shots = append(out.Shots, v)
	}
	return out, nil
}

// UpdateRemakeShot 按计划版本保存当前镜的动作、时长、来源或采用版本。
func (s *MarketingService) UpdateRemakeShot(ctx context.Context, userID, id uint64, shotID int, req *model.UpdateRemakeShotReq) (*model.MarketingRemakePlanView, error) {
	// 1. 持锁检查计划修订后定位稳定镜号，并检查来源区间和选版归属。
	return s.editRemakePlan(ctx, userID, id, req.Revision, func(tx repository.MarketingTx, data *model.RemakePlanData) error {
		shot, err := findRemakeShot(data, shotID)
		if err != nil {
			return err
		}
		if err := editRemakeDraft(data, shot, req); err != nil {
			return err
		}
		if req.ChosenTaskID != nil {
			if err := chooseRemakeTask(ctx, tx, userID, id, shot, *req.ChosenTaskID); err != nil {
				return err
			}
		}
		// 2. 镜头修订固定生成输入，重新排序只修改计划修订。
		shot.Revision++
		return nil
	})
}

// editRemakeDraft 校验当前编辑字段；不替换操作未涉及的来源、选版或镜序。
func editRemakeDraft(data *model.RemakePlanData, shot *model.RemakePlanShot, req *model.UpdateRemakeShotReq) error {
	if req.Scene == nil && req.Narration == nil && req.DurationMs == nil && req.TrimStartMs == nil && req.Sources == nil && req.ChosenTaskID == nil {
		return errcode.ErrInvalidParams.WithMsg("请提供要修改的镜头字段")
	}
	if req.Scene != nil {
		value := strings.TrimSpace(*req.Scene)
		if value == "" || utf8.RuneCountInString(value) > 4000 {
			return errcode.ErrInvalidParams.WithMsg("画面描述须为 1–4000 字")
		}
		shot.Scene = value
	}
	if req.Narration != nil {
		if utf8.RuneCountInString(*req.Narration) > 4000 {
			return errcode.ErrInvalidParams.WithMsg("旁白超长")
		}
		shot.Narration = strings.TrimSpace(*req.Narration)
	}
	if req.DurationMs != nil {
		shot.DurationMs = *req.DurationMs
	}
	if req.TrimStartMs != nil {
		shot.TrimStartMs = *req.TrimStartMs
	}
	if shot.DurationMs <= 0 || shot.DurationMs > 600000 || shot.TrimStartMs < 0 || shot.TrimStartMs > 600000 {
		return errcode.ErrInvalidParams.WithMsg("裁剪时间窗超出支持范围")
	}
	if req.Sources != nil {
		sources := *req.Sources
		if len(sources) > 128 || !validRemakeSources(data.SourceCatalog, sources) {
			return errcode.ErrInvalidParams.WithMsg("来源区间必须位于本计划对应的原镜范围内")
		}
		shot.Sources = append([]model.MarketingRemakeSource{}, sources...)
		shot.Unresolved = false
	}
	return nil
}

// validRemakeSources 按每个引用所指原镜检查左右边界，允许分开裁取不连续区间。
func validRemakeSources(catalog, sources []model.MarketingRemakeSource) bool {
	byID := make(map[int]model.MarketingRemakeSource, len(catalog))
	for _, ref := range catalog {
		byID[ref.RefShot] = ref
	}
	for _, ref := range sources {
		allowed, ok := byID[ref.RefShot]
		if !ok || ref.StartMs < allowed.StartMs || ref.StartMs >= ref.EndMs || ref.EndMs > allowed.EndMs {
			return false
		}
	}
	return true
}

// chooseRemakeTask 要求选中的版本是本人对本镜发起的正式成功任务，且包含已转存的视频产物。
func chooseRemakeTask(ctx context.Context, tx repository.MarketingTx, userID, planID uint64, shot *model.RemakePlanShot, taskID uint64) error {
	if taskID == 0 {
		shot.ChosenTaskID = nil
		return nil
	}
	tasks, err := tx.RemakeTakeTasks(ctx, userID, planID)
	if err != nil {
		return err
	}
	node := remakeShotNode(planID, shot.ID)
	for i := range tasks {
		t := &tasks[i]
		if t.ID != taskID || t.UserID != userID || t.IsTest || t.NodeID != node || t.Status != model.TaskSucceeded || t.Kind != model.KindVideo {
			continue
		}
		for _, v := range taskLite(t).Outputs {
			if v.AssetID > 0 && v.MediaType == model.KindVideo {
				shot.ChosenTaskID = &taskID
				return nil
			}
		}
	}
	return errcode.ErrInvalidParams.WithMsg("请选择本镜已成功的生成视频")
}

// OrderRemakeShots 校验完整排列后保存目标镜展示与合成顺序。
func (s *MarketingService) OrderRemakeShots(ctx context.Context, userID, id uint64, req *model.OrderRemakeShotsReq) (*model.MarketingRemakePlanView, error) {
	// 1. 在持锁的计划版本上检查镜头无缺失、重复或越界。
	return s.editRemakePlan(ctx, userID, id, req.Revision, func(_ repository.MarketingTx, data *model.RemakePlanData) error {
		if len(data.Shots) != len(req.ShotIDs) {
			return errcode.ErrInvalidParams.WithMsg("重排须包含全部镜头")
		}
		available := make(map[int]model.RemakePlanShot, len(data.Shots))
		for _, shot := range data.Shots {
			available[shot.ID] = shot
		}
		ordered := make([]model.RemakePlanShot, 0, len(data.Shots))
		for _, shotID := range req.ShotIDs {
			shot, ok := available[shotID]
			if !ok {
				return errcode.ErrInvalidParams.WithMsg("重排含有重复或不存在的镜头")
			}
			delete(available, shotID)
			ordered = append(ordered, shot)
		}
		// 2. 仅替换顺序，保留镜号、修订、旧版本及采用结果。
		data.Shots = ordered
		return nil
	})
}

// editRemakePlan 将读改写串行化，过期修订返回冲突供调用方重新拉取视图。
func (s *MarketingService) editRemakePlan(ctx context.Context, userID, id uint64, revision int64, edit func(repository.MarketingTx, *model.RemakePlanData) error) (*model.MarketingRemakePlanView, error) {
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		row, err := tx.RemakePlan(ctx, userID, id, true)
		if err != nil {
			return err
		}
		if row.Revision != revision {
			return errcode.ErrMarketingConflict
		}
		data, err := decodeRemakePlan(row)
		if err != nil {
			return err
		}
		if err := edit(tx, data); err != nil {
			return err
		}
		row.Data, err = json.Marshal(data)
		if err != nil {
			return err
		}
		return tx.SaveRemakePlan(ctx, row)
	})
	if err != nil {
		return nil, remakeReadError(err)
	}
	return s.GetRemakePlan(ctx, userID, id)
}
