package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// CreateRemakePlan 在同一事务固定所见的跟拍修订、参考素材和目标镜；同一修订重复打开复用已有计划。
func (s *MarketingService) CreateRemakePlan(ctx context.Context, userID, scriptID uint64, req *model.CreateRemakePlanReq) (*model.MarketingRemakePlanView, error) {
	// 1. 先取归属，随后按项目、轮次、脚本顺序持锁，避免与跟拍重写交错。
	sc, err := s.repo.Script(ctx, userID, scriptID, false)
	if err != nil {
		return nil, remakeReadError(err)
	}
	var planID uint64
	err = s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		planID, err = createRemakePlan(ctx, tx, sc, req.Revision)
		return err
	})
	if err != nil {
		return nil, remakeReadError(err)
	}
	// 2. 返回完整工作台视图；已有计划带回当前编辑和采用结果。
	return s.GetRemakePlan(ctx, userID, planID)
}

// createRemakePlan 先锁所属项目复用已有修订，缺少计划时再锁定原稿并固定快照。
func createRemakePlan(ctx context.Context, tx repository.MarketingTx, sc *model.MarketingScript, revision int64) (uint64, error) {
	p, err := tx.Project(ctx, sc.UserID, sc.ProjectID, true)
	if err != nil {
		return 0, err
	}
	plans, err := tx.RemakePlans(ctx, sc.UserID, sc.ID)
	if err != nil {
		return 0, err
	}
	for _, row := range plans {
		if row.ScriptRevision == revision {
			return row.ID, nil
		}
	}
	r, err := tx.Round(ctx, sc.UserID, sc.RoundID, true)
	if err != nil {
		return 0, err
	}
	current, err := tx.Script(ctx, sc.UserID, sc.ID, true)
	if err != nil {
		return 0, err
	}
	if current.Kind != marketing.KindFollow {
		return 0, errcode.ErrInvalidParams.WithMsg("只有跟拍脚本可以建逐镜计划")
	}
	if current.Revision != revision {
		return 0, errcode.ErrMarketingConflict
	}
	snapshot, err := newRemakeSnapshot(p, r, current)
	if err != nil {
		return 0, err
	}
	row := &model.MarketingRemakePlan{UserID: sc.UserID, ProjectID: p.ID, ScriptID: sc.ID, ScriptRevision: revision, Revision: 1, Original: snapshot, Data: snapshot}
	if err := tx.InsertRemakePlan(ctx, row); err != nil {
		return 0, err
	}
	return row.ID, nil
}

// newRemakeSnapshot 在已锁定的项目、轮次与脚本上组装唯一的原稿快照。
func newRemakeSnapshot(p *model.MarketingProject, r *model.MarketingRound, sc *model.MarketingScript) ([]byte, error) {
	st, err := settingsOf(r)
	if err != nil {
		return nil, err
	}
	basis, _, err := remakeBasis(r.FollowBasis, r.Reverse, r.ReverseRevision)
	if err != nil {
		return nil, err
	}
	script, err := scriptOf(sc)
	if err != nil {
		return nil, err
	}
	var mapping []marketing.FollowMapItem
	if len(sc.FollowMap) > 0 {
		if err := json.Unmarshal(sc.FollowMap, &mapping); err != nil {
			return nil, err
		}
	}
	data := model.RemakePlanData{
		Product: json.RawMessage(p.Product), Brands: basis.Brands, Claims: basis.Claims,
		Settings: st, RefVideoAssetID: r.RefVideoAssetID, ModelAssetID: r.ModelAssetID,
		Shots: []model.RemakePlanShot{}, SourceCatalog: remakeCatalog(basis),
	}
	if err := json.Unmarshal(p.ProductAssetIDs, &data.ProductAssets); err != nil {
		return nil, err
	}
	for i, shot := range marketing.RemakeShots(script.Shots, mapping, basis) {
		data.Shots = append(data.Shots, model.RemakePlanShot{ID: i + 1, Revision: 1, Scene: shot.Scene, Narration: script.Shots[i].Narration,
			DurationMs: shot.DurationMs, Sources: shot.Sources, Unresolved: shot.Unresolved})
	}
	return json.Marshal(data)
}

// remakeCatalog 保留每个唯一原镜的可裁范围，拒绝歧义镜号和空区间。
func remakeCatalog(basis marketing.Reverse) []model.MarketingRemakeSource {
	counts := make(map[int]int, len(basis.Shots))
	for _, shot := range basis.Shots {
		counts[shot.ID]++
	}
	refs := make([]model.MarketingRemakeSource, 0, len(basis.Shots))
	for _, shot := range basis.Shots {
		start, end := int(math.Round(shot.Start*1000)), int(math.Round(shot.End*1000))
		if counts[shot.ID] == 1 && shot.ID > 0 && start >= 0 && end > start {
			refs = append(refs, model.MarketingRemakeSource{RefShot: shot.ID, StartMs: start, EndMs: end})
		}
	}
	return refs
}

// ListRemakePlans 按脚本修订倒序列出本人仍可访问的工作稿。
func (s *MarketingService) ListRemakePlans(ctx context.Context, userID, scriptID uint64) ([]model.MarketingRemakePlanSummary, error) {
	// 1. 校验归属，已删项目与不属于本人统一返回不存在。
	sc, err := s.repo.Script(ctx, userID, scriptID, false)
	if err != nil {
		return nil, remakeReadError(err)
	}
	if _, err := s.repo.Project(ctx, userID, sc.ProjectID, false); err != nil {
		return nil, remakeReadError(err)
	}
	// 2. 查摘要，避免加载每份历史计划的镜头与素材。
	rows, err := s.repo.RemakePlans(ctx, userID, scriptID)
	if err != nil {
		return nil, err
	}
	out := make([]model.MarketingRemakePlanSummary, 0, len(rows))
	for i := range rows {
		out = append(out, remakePlanSummary(&rows[i]))
	}
	return out, nil
}

// remakePlanSummary 将主键转为营销接口通用的十六进制 ID。
func remakePlanSummary(row *model.MarketingRemakePlan) model.MarketingRemakePlanSummary {
	return model.MarketingRemakePlanSummary{ID: hexID(row.ID), ScriptID: hexID(row.ScriptID), ScriptRevision: row.ScriptRevision}
}

// remakeReadError 将仓储不存在和乐观锁冲突按营销的业务错误返回。
func remakeReadError(err error) error {
	if mktNotFound(err) {
		return errcode.ErrMarketingNotFound
	}
	if errors.Is(err, repository.ErrRevisionConflict) {
		return errcode.ErrMarketingConflict
	}
	return err
}

// remakeShotNode 将计划和稳定镜号组合作为本镜独立生成的节点。
func remakeShotNode(planID uint64, shotID int) string {
	return fmt.Sprintf("%s%s:%d", remakeNodePrefix, idcodec.Encode(planID), shotID)
}

// decodeRemakePlan 读取当前编辑稿，原稿不随选版或编辑改变。
func decodeRemakePlan(row *model.MarketingRemakePlan) (*model.RemakePlanData, error) {
	var data model.RemakePlanData
	if err := json.Unmarshal(row.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// loadRemakePlan 按所有者取出计划及其当前有序镜头。
func (s *MarketingService) loadRemakePlan(ctx context.Context, userID, id uint64) (*model.MarketingRemakePlan, *model.RemakePlanData, error) {
	row, err := s.repo.RemakePlan(ctx, userID, id, false)
	if err != nil {
		return nil, nil, remakeReadError(err)
	}
	data, err := decodeRemakePlan(row)
	return row, data, err
}

// findRemakeShot 按稳定镜号匹配；重排不会改变它的身份。
func findRemakeShot(data *model.RemakePlanData, id int) (*model.RemakePlanShot, error) {
	for i := range data.Shots {
		if data.Shots[i].ID == id {
			return &data.Shots[i], nil
		}
	}
	return nil, errcode.ErrMarketingNotFound
}
