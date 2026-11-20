package repository

import (
	"context"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// MarketingPlanTx 是营销逐镜计划的仓储操作，写操作均由调用者开启事务。
type MarketingPlanTx interface {
	// RemakePlan 查询用户可见的存活项目内计划，可锁定用于更新。
	RemakePlan(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingRemakePlan, error)
	// RemakePlans 列出脚本历次修订的计划，仅选摘要列。
	RemakePlans(ctx context.Context, userID, scriptID uint64) ([]model.MarketingRemakePlan, error)
	// InsertRemakePlan 新增已持锁脚本的计划，回填 ID。
	InsertRemakePlan(ctx context.Context, row *model.MarketingRemakePlan) error
	// SaveRemakePlan 在持锁的用户计划上更新编辑稿和修订。
	SaveRemakePlan(ctx context.Context, row *model.MarketingRemakePlan) error
	// RemakeTakeTasks 按生成意图读取当前用户在这份计划的真实任务。
	RemakeTakeTasks(ctx context.Context, userID, planID uint64) ([]model.GenerationTask, error)
}

// visibleRemakePlans 限定计划及其项目均属于用户，且项目仍可访问。
func (r *MarketingRepository) visibleRemakePlans(ctx context.Context, userID uint64) *gorm.DB {
	return r.db.WithContext(ctx).Table("marketing_remake_plans AS m").
		Joins("JOIN marketing_projects AS p ON p.id = m.project_id AND p.user_id = m.user_id AND p.deleted_at IS NULL").
		Where("m.user_id = ?", userID)
}

// RemakePlan 按用户和计划主键查存活项目，可加行锁；未命中返回 ErrNotFound。
func (r *MarketingRepository) RemakePlan(ctx context.Context, userID, id uint64, lock bool) (*model.MarketingRemakePlan, error) {
	var row model.MarketingRemakePlan
	q := r.visibleRemakePlans(ctx, userID).Select("m.*").Where("m.id = ?", id)
	if err := forUpdate(q, lock).Take(&row).Error; err != nil {
		return nil, translate(err)
	}
	return &row, nil
}

// RemakePlans 按所属用户和脚本查询计划摘要，修订从新到旧，未命中返回空列表。
func (r *MarketingRepository) RemakePlans(ctx context.Context, userID, scriptID uint64) ([]model.MarketingRemakePlan, error) {
	var rows []model.MarketingRemakePlan
	err := r.visibleRemakePlans(ctx, userID).Select("m.id,m.script_id,m.script_revision").Where("m.script_id = ?", scriptID).Order("m.script_revision DESC").Find(&rows).Error
	return rows, err
}

// InsertRemakePlan 为已持锁的脚本插入一份修订快照，重复修订受唯一索引保护。
func (r *MarketingRepository) InsertRemakePlan(ctx context.Context, row *model.MarketingRemakePlan) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// SaveRemakePlan 按用户、计划和旧修订更新编辑稿，冲突返回 ErrRevisionConflict。
func (r *MarketingRepository) SaveRemakePlan(ctx context.Context, row *model.MarketingRemakePlan) error {
	res := r.db.WithContext(ctx).Model(&model.MarketingRemakePlan{}).Where("id = ? AND user_id = ? AND revision = ?", row.ID, row.UserID, row.Revision).
		Updates(map[string]any{"data": row.Data, "revision": row.Revision + 1})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrRevisionConflict
	}
	return nil
}
