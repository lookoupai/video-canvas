package repository

import (
	"context"

	"video-canvas/internal/model"
)

// DramaTx 复用当前参考集事务，不开启嵌套事务。
func (r *DramaReferenceRepository) DramaTx() DramaTx { return &DramaRepository{db: r.db} }

// Adoptions 按用户项目读取全部采用记录，最新记录在前。
func (r *DramaReferenceRepository) Adoptions(ctx context.Context, u, p uint64) ([]model.DramaReferenceAdoption, error) {
	var rows []model.DramaReferenceAdoption
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ?", u, p).Order("id DESC").Find(&rows).Error
	return rows, err
}

// AdoptionByKey 按用户提交键读取原始采用，不存在返回 ErrNotFound。
func (r *DramaReferenceRepository) AdoptionByKey(ctx context.Context, u uint64, key string) (*model.DramaReferenceAdoption, error) {
	var row model.DramaReferenceAdoption
	err := r.db.WithContext(ctx).Where("user_id = ? AND (request_key = ? OR jsonb_exists(request_keys, ?))", u, key, key).Take(&row).Error
	return &row, translate(err)
}

// InsertAdoption 在已锁项目内建立采用记录。
func (r *DramaReferenceRepository) InsertAdoption(ctx context.Context, row *model.DramaReferenceAdoption) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// FinishAdoption 只在创建事务里按用户项目补齐快照和片段列表，不提供更新已提交采用的接口。
func (r *DramaReferenceRepository) FinishAdoption(ctx context.Context, row *model.DramaReferenceAdoption) error {
	return r.db.WithContext(ctx).Model(&model.DramaReferenceAdoption{}).Where("user_id = ? AND drama_id = ? AND id = ?", row.UserID, row.DramaID, row.ID).Updates(map[string]any{"snapshot": row.Snapshot, "segment_ids": row.SegmentIDs}).Error
}

// RememberAdoptionKey 只更新本人采用的键别名，不改已采用的制作依据。
func (r *DramaReferenceRepository) RememberAdoptionKey(ctx context.Context, row *model.DramaReferenceAdoption) error {
	return r.db.WithContext(ctx).Model(&model.DramaReferenceAdoption{}).Where("user_id = ? AND drama_id = ? AND id = ?", row.UserID, row.DramaID, row.ID).Update("request_keys", row.RequestKeys).Error
}
