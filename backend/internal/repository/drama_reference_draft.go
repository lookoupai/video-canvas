package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// DramaReferenceDraftTx 提供同一项目锁内的草稿读写，正式参考文档只读。
type DramaReferenceDraftTx interface {
	// Reference 检查用户、存活项目和参考集归属，按需锁定项目。
	Reference(context.Context, uint64, uint64, uint64, bool) (*model.DramaReferenceEpisode, error)
	// ByKey 按当前参考集的稳定创建键读取草稿。
	ByKey(context.Context, uint64, uint64, uint64, string) (*model.DramaReferenceDraft, error)
	// Draft 按用户、项目、参考集、草稿读取，按需加行锁。
	Draft(context.Context, uint64, uint64, uint64, uint64, bool) (*model.DramaReferenceDraft, error)
	// Drafts 在用户参考集内按 ID 倒序返回游标后的指定条数。
	Drafts(context.Context, uint64, uint64, uint64, uint64, int) ([]model.DramaReferenceDraft, error)
	// Save 插入新草稿或在已锁定行上更新草稿。
	Save(context.Context, *model.DramaReferenceDraft) error
}

// DramaReferenceDraftRepository 使用项目锁串行化创建键和草稿修订。
type DramaReferenceDraftRepository struct{ db *gorm.DB }

// NewDramaReferenceDraftRepository 创建草稿仓储。
func NewDramaReferenceDraftRepository(db *gorm.DB) *DramaReferenceDraftRepository {
	return &DramaReferenceDraftRepository{db: db}
}

// WithTx 在单一事务内读取基准并提交草稿；回调失败会回滚。
func (r *DramaReferenceDraftRepository) WithTx(ctx context.Context, fn func(DramaReferenceDraftTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&DramaReferenceDraftRepository{db: tx})
	})
}

// Reference 按用户和存活项目检查参考集，缺失或越权均返回 ErrNotFound。
func (r *DramaReferenceDraftRepository) Reference(ctx context.Context, u, p, id uint64, lock bool) (*model.DramaReferenceEpisode, error) {
	if _, err := NewDramaRepository(r.db).Project(ctx, u, p, lock); err != nil {
		return nil, err
	}
	return NewDramaReferenceRepository(r.db).Episode(ctx, u, p, id)
}

// ByKey 按用户、项目、参考集和创建键查找，缺失返回 ErrNotFound。
func (r *DramaReferenceDraftRepository) ByKey(ctx context.Context, u, p, ref uint64, key string) (*model.DramaReferenceDraft, error) {
	var row model.DramaReferenceDraft
	err := r.scope(ctx, u, p, ref).Where("request_key = ?", key).Take(&row).Error
	return &row, translate(err)
}

// Draft 按用户、项目、参考集和 ID 查询并按需加锁，缺失返回 ErrNotFound。
func (r *DramaReferenceDraftRepository) Draft(ctx context.Context, u, p, ref, id uint64, lock bool) (*model.DramaReferenceDraft, error) {
	var row model.DramaReferenceDraft
	q := r.scope(ctx, u, p, ref).Where("id = ?", id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&row).Error
	return &row, translate(err)
}

// Drafts 按用户参考集、排他 ID 游标倒序查询；无数据返回空列表。
func (r *DramaReferenceDraftRepository) Drafts(ctx context.Context, u, p, ref, cursor uint64, limit int) ([]model.DramaReferenceDraft, error) {
	rows := []model.DramaReferenceDraft{}
	q := r.scope(ctx, u, p, ref)
	if cursor != 0 {
		q = q.Where("id < ?", cursor)
	}
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// Save 在已锁定的用户项目中插入或更新本人草稿，更新缺失返回 ErrNotFound。
func (r *DramaReferenceDraftRepository) Save(ctx context.Context, row *model.DramaReferenceDraft) error {
	if row.ID == 0 {
		return r.db.WithContext(ctx).Create(row).Error
	}
	result := r.scope(ctx, row.UserID, row.DramaID, row.ReferenceID).Where("id = ?", row.ID).
		Select("revision", "status", "source", "session_id", "run_id", "content", "updated_at").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// scope 把用户、项目和参考集条件固定在每一条草稿查询中。
func (r *DramaReferenceDraftRepository) scope(ctx context.Context, u, p, ref uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.DramaReferenceDraft{}).
		Where("user_id = ? AND drama_id = ? AND reference_id = ?", u, p, ref)
}
