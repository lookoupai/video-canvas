package repository

import (
	"context"

	"gorm.io/datatypes"
)

// MarketingPreviewRow 同时读取脚本和轮次快照，避免重写跟拍时读到两次修订的数据。
type MarketingPreviewRow struct {
	Kind            string
	Revision        int64
	Shots           datatypes.JSON
	FollowMap       datatypes.JSON
	RefVideoAssetID *uint64
	FollowBasis     datatypes.JSON
	Reverse         datatypes.JSON
	ReverseRevision int
}

// RemakePreviewRow 按脚本 ID、所属轮次及用户的存活项目读取映射，缺失或不属于本人返回 ErrNotFound。
func (r *MarketingRepository) RemakePreviewRow(ctx context.Context, userID, id uint64) (*MarketingPreviewRow, error) {
	var row MarketingPreviewRow
	err := r.db.WithContext(ctx).Table("marketing_scripts AS s").
		Select("s.kind, s.revision, s.shots, s.follow_map, r.ref_video_asset_id, r.follow_basis, r.reverse, r.reverse_revision").
		Joins("JOIN marketing_rounds AS r ON r.id = s.round_id AND r.project_id = s.project_id AND r.user_id = s.user_id").
		Joins("JOIN marketing_projects AS p ON p.id = s.project_id AND p.user_id = s.user_id AND p.deleted_at IS NULL").
		Where("s.id = ? AND s.user_id = ?", id, userID).Take(&row).Error
	if err != nil {
		return nil, translate(err)
	}
	return &row, nil
}
