package repository

import (
	"context"
	"database/sql"
	"errors"

	"video-canvas/internal/model"
)

// MarketingComposeTx 提供固定选版清单及与成片同事务的队列、素材操作。
type MarketingComposeTx interface {
	// ActiveRemakeCompositions 统计用户项目尚在排队或转码的合成，锁项目后用于删除保护。
	ActiveRemakeCompositions(context.Context, uint64, uint64) (int64, error)
	// RemakeCompositionByKey 按计划和用户查询已提交的同键合成。
	RemakeCompositionByKey(context.Context, uint64, uint64, string) (*model.MarketingRemakeComposition, error)
	// RemakeComposition 按用户、计划和记录 ID 读取单次合成。
	RemakeComposition(context.Context, uint64, uint64, uint64) (*model.MarketingRemakeComposition, error)
	// ListRemakeCompositions 按时间倒序读取本人的计划合成结果。
	ListRemakeCompositions(context.Context, uint64, uint64) ([]model.MarketingRemakeComposition, error)
	// InsertRemakeComposition 保存原始选版输入。
	InsertRemakeComposition(context.Context, *model.MarketingRemakeComposition) error
	// RetryRemakeComposition 将失败条目恢复为排队并更新尝试次数。
	RetryRemakeComposition(context.Context, *model.MarketingRemakeComposition) error
	// EnqueueRemakeComposition 在业务事务中投递本次尝试。
	EnqueueRemakeComposition(context.Context, uint64, uint64, uint64, int) error
	// FinishRemakeComposition 同事务登记成片素材并更新合成状态。
	FinishRemakeComposition(context.Context, *model.MarketingRemakeComposition, *model.Asset, string) error
}

// ActiveRemakeCompositions 按用户及项目统计未完成的合成，没有记录返回 0。
func (r *MarketingRepository) ActiveRemakeCompositions(ctx context.Context, userID, projectID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("marketing_remake_compositions AS c").
		Joins("JOIN marketing_remake_plans AS p ON p.id = c.plan_id AND p.user_id = c.user_id").
		Where("c.user_id = ? AND p.project_id = ? AND c.status = ?", userID, projectID, "queued").Count(&count).Error
	return count, err
}

// RemakeCompositionByKey 按用户、计划、幂等键查询，未命中返回 ErrNotFound。
func (r *MarketingRepository) RemakeCompositionByKey(ctx context.Context, u, p uint64, key string) (*model.MarketingRemakeComposition, error) {
	var row model.MarketingRemakeComposition
	err := r.db.WithContext(ctx).Where("user_id = ? AND plan_id = ? AND idempotency_key = ?", u, p, key).Take(&row).Error
	return &row, translate(err)
}

// RemakeComposition 按用户、计划及记录 ID 查询，未命中返回 ErrNotFound。
func (r *MarketingRepository) RemakeComposition(ctx context.Context, u, p, id uint64) (*model.MarketingRemakeComposition, error) {
	var row model.MarketingRemakeComposition
	err := r.db.WithContext(ctx).Where("user_id = ? AND plan_id = ? AND id = ?", u, p, id).Take(&row).Error
	return &row, translate(err)
}

// ListRemakeCompositions 按用户和计划查最新提交在前的合成列表，无数据返回空表。
func (r *MarketingRepository) ListRemakeCompositions(ctx context.Context, u, p uint64) ([]model.MarketingRemakeComposition, error) {
	var rows []model.MarketingRemakeComposition
	err := r.db.WithContext(ctx).Where("user_id = ? AND plan_id = ?", u, p).Order("id DESC").Find(&rows).Error
	return rows, err
}

// InsertRemakeComposition 在已持有计划锁的事务中保存合成快照，键按计划唯一。
func (r *MarketingRepository) InsertRemakeComposition(ctx context.Context, row *model.MarketingRemakeComposition) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// RetryRemakeComposition 将该用户计划里的失败记录重排队，须持有计划锁。
func (r *MarketingRepository) RetryRemakeComposition(ctx context.Context, row *model.MarketingRemakeComposition) error {
	return r.db.WithContext(ctx).Model(row).Where("user_id = ? AND plan_id = ?", row.UserID, row.PlanID).Updates(map[string]any{"status": row.Status, "attempt": row.Attempt, "error": ""}).Error
}

// EnqueueRemakeComposition 在创建或重试合成快照的事务中写入 River job。
func (r *MarketingRepository) EnqueueRemakeComposition(ctx context.Context, u, p, id uint64, attempt int) error {
	if r.enq == nil {
		return errors.New("营销合成队列未配置")
	}
	tx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("营销合成入队必须处于业务事务")
	}
	return r.enq.EnqueueRemakeCompositionTx(ctx, tx, u, p, id, attempt)
}

// FinishRemakeComposition 在持有计划锁的事务中登记结果素材并完成指定用户的合成尝试。
func (r *MarketingRepository) FinishRemakeComposition(ctx context.Context, row *model.MarketingRemakeComposition, asset *model.Asset, failure string) error {
	fields := map[string]any{"status": "failed", "error": failure}
	if asset != nil {
		if err := r.db.WithContext(ctx).Create(asset).Error; err != nil {
			return err
		}
		fields = map[string]any{"status": "succeeded", "asset_id": asset.ID, "error": ""}
	}
	return r.db.WithContext(ctx).Model(row).Where("user_id = ? AND plan_id = ?", row.UserID, row.PlanID).Updates(fields).Error
}
