package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// MarketingGenerateTx 是单镜重拍的持久化接口。
type MarketingGenerateTx interface {
	// RemakeIntent 根据用户和提交键读取意图，不存在返回 ErrNotFound。
	RemakeIntent(ctx context.Context, userID uint64, key string) (*model.MarketingRemakeIntent, error)
	// InsertRemakeIntent 按用户和请求键插入，重复键返回已落库的输入。
	InsertRemakeIntent(ctx context.Context, in *model.MarketingRemakeIntent) (*model.MarketingRemakeIntent, error)
	// ActiveRemakeTasks 统计本人项目的在途单镜任务，用于锁项目后判断可否删除。
	ActiveRemakeTasks(ctx context.Context, userID, projectID uint64) (int64, error)
	// RemakeIntentTask 读取意图对应的已提交任务，不存在返回 ErrNotFound。
	RemakeIntentTask(ctx context.Context, in *model.MarketingRemakeIntent) (*model.GenerationTask, error)
}

// RemakeIntent 按用户与提交键查询，未匹配返回 ErrNotFound。
func (r *MarketingRepository) RemakeIntent(ctx context.Context, userID uint64, key string) (*model.MarketingRemakeIntent, error) {
	var in model.MarketingRemakeIntent
	if err := r.db.WithContext(ctx).Where("user_id = ? AND request_key = ?", userID, key).Take(&in).Error; err != nil {
		return nil, translate(err)
	}
	return &in, nil
}

// InsertRemakeIntent 按用户和提交键固定意图；并发重试复用首次写入的任务键与快照。
func (r *MarketingRepository) InsertRemakeIntent(ctx context.Context, in *model.MarketingRemakeIntent) (*model.MarketingRemakeIntent, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "request_key"}}, DoNothing: true}).Create(in)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return r.RemakeIntent(ctx, in.UserID, in.RequestKey)
	}
	return in, nil
}

// remakeTasks 仅正式生成意图绑定的用户、节点及内部任务键同时相符时才能加入选版清单。
func (r *MarketingRepository) remakeTasks(ctx context.Context, userID uint64) *gorm.DB {
	return r.db.WithContext(ctx).Table("generation_tasks AS t").Joins("JOIN marketing_remake_intents AS i ON i.user_id = t.user_id AND i.task_key = t.idempotency_key AND i.node_id = t.node_id").
		Where("i.user_id = ? AND NOT t.is_test AND t.kind = ?", userID, model.KindVideo)
}

// RemakeTakeTasks 查询本人计划内与提交意图吻合的任务，按创建时间排序，无记录则为空。
func (r *MarketingRepository) RemakeTakeTasks(ctx context.Context, userID, planID uint64) ([]model.GenerationTask, error) {
	var tasks []model.GenerationTask
	err := r.remakeTasks(ctx, userID).Select("t.*").Where("i.plan_id = ?", planID).Order("t.created_at, t.id").Find(&tasks).Error
	return tasks, err
}

// ActiveRemakeTasks 统计用户项目内已提交且仍在执行的逐镜任务。
func (r *MarketingRepository) ActiveRemakeTasks(ctx context.Context, userID, projectID uint64) (int64, error) {
	var n int64
	err := r.remakeTasks(ctx, userID).Joins("JOIN marketing_remake_plans AS p ON i.plan_id = p.id AND p.user_id = i.user_id").Where("p.project_id = ? AND t.status IN ?", projectID, model.ActiveTaskStatuses).Count(&n).Error
	return n, err
}

// RemakeIntentTask 根据意图查询对应的用户任务，不存在返回 ErrNotFound。
func (r *MarketingRepository) RemakeIntentTask(ctx context.Context, in *model.MarketingRemakeIntent) (*model.GenerationTask, error) {
	var t model.GenerationTask
	err := r.remakeTasks(ctx, in.UserID).Select("t.*").Where("i.id = ?", in.ID).Take(&t).Error
	if err != nil {
		return nil, translate(err)
	}
	return &t, nil
}
