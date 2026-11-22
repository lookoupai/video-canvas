package repository

import (
	"context"

	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// FrameCache 按用户和缓存 ID 查询，缺失或越权返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) FrameCache(ctx context.Context, u, id uint64) (*model.DramaReferenceFrameCache, error) {
	var row model.DramaReferenceFrameCache
	err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", u, id).Take(&row).Error
	return &row, translate(err)
}

// EnsureFrameCache 在用户与媒体键唯一约束内插入或复用，并返回持久化缓存 ID。
func (r *DramaReferencePreparationRepository) EnsureFrameCache(ctx context.Context, row *model.DramaReferenceFrameCache) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "cache_key"}}, DoNothing: true}).Create(row).Error
	if err != nil {
		return err
	}
	if row.ID != 0 {
		return nil
	}
	return translate(r.db.WithContext(ctx).Where("user_id = ? AND cache_key = ?", row.UserID, row.CacheKey).Take(row).Error)
}

// SaveFrameCache 按用户及缓存 ID 更新图片关联，缺失返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) SaveFrameCache(ctx context.Context, row *model.DramaReferenceFrameCache) error {
	result := r.db.WithContext(ctx).Model(&model.DramaReferenceFrameCache{}).Where("user_id = ? AND id = ?", row.UserID, row.ID).
		Select("asset_id", "updated_at").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
