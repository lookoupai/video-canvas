package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// FindRemakeClip 按用户、原视频、裁片配方查派生视频，原片或裁片缺失及归属不符时返回 ErrNotFound。
func (r *MarketingRepository) FindRemakeClip(ctx context.Context, userID, sourceID uint64, recipe string) (uint64, error) {
	var clip model.MarketingRemakeClip
	err := r.db.WithContext(ctx).Model(&model.MarketingRemakeClip{}).
		Joins("JOIN assets AS derived ON derived.id = marketing_remake_clips.asset_id AND derived.user_id = marketing_remake_clips.user_id AND derived.kind = 'video'").
		Joins("JOIN assets AS source ON source.id = marketing_remake_clips.source_id AND source.user_id = marketing_remake_clips.user_id AND source.kind = 'video'").
		Where("marketing_remake_clips.user_id = ? AND source_id = ? AND recipe = ?", userID, sourceID, recipe).Take(&clip).Error
	return clip.AssetID, translate(err)
}

// KeepRemakeClip 按用户、原素材和配方在短事务中登记裁片。同一配方并发完成时统一返回先登记的素材 ID。
// 原素材或待登记素材不属于该用户的视频时返回 ErrNotFound。
func (r *MarketingRepository) KeepRemakeClip(ctx context.Context, userID, sourceID uint64, recipe string, asset *model.Asset) (uint64, error) {
	if asset.UserID != userID || asset.Kind != "video" {
		return 0, ErrNotFound
	}
	var id uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 相同配方在多实例间串行登记，不把转码、上传或网络 I/O 放进事务。
		lockKey := fmt.Sprintf("mkt-remake-clip/%d/%d/%s", userID, sourceID, recipe)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return err
		}
		// 原片的归属与存在性必须在登记时再次确认，行锁避免在登记事务提交前被删除或修改。
		var source model.Asset
		if err := forUpdate(tx, true).Where("id = ? AND user_id = ? AND kind = ?", sourceID, userID, "video").Take(&source).Error; err != nil {
			return translate(err)
		}
		existing, err := (&MarketingRepository{db: tx}).FindRemakeClip(ctx, userID, sourceID, recipe)
		if err == nil {
			id = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := tx.Create(asset).Error; err != nil {
			return err
		}
		clip := &model.MarketingRemakeClip{UserID: userID, SourceID: sourceID, Recipe: recipe}
		// 派生素材曾被删除时更新旧的引用。
		if err := tx.Where(clip).Assign(map[string]any{"asset_id": asset.ID}).FirstOrCreate(clip).Error; err != nil {
			return err
		}
		id = asset.ID
		return nil
	})
	return id, err
}
