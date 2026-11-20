package repository

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// VirtualHumanRepository 真人库（virtual_humans、virtual_human_images）的数据访问。所有查询都带 user_id 条件。
type VirtualHumanRepository struct {
	db *gorm.DB
}

// NewVirtualHumanRepository 创建真人库仓储。
func NewVirtualHumanRepository(db *gorm.DB) *VirtualHumanRepository {
	return &VirtualHumanRepository{db: db}
}

// ListHumans 返回用户全部真人，新建的在前（created_at、id 倒序）。
func (r *VirtualHumanRepository) ListHumans(ctx context.Context, userID uint64) ([]model.VirtualHuman, error) {
	var rows []model.VirtualHuman
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

// CountHumans 统计用户的真人数。
func (r *VirtualHumanRepository) CountHumans(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.VirtualHuman{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// GetHuman 按 id + user_id 查询真人，不存在或不属于该用户返回 ErrNotFound。
func (r *VirtualHumanRepository) GetHuman(ctx context.Context, userID, id uint64) (*model.VirtualHuman, error) {
	var h model.VirtualHuman
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&h).Error; err != nil {
		return nil, translate(err)
	}
	return &h, nil
}

// RenameHuman 修改真人名称，不存在或不属于该用户返回 ErrNotFound。
func (r *VirtualHumanRepository) RenameHuman(ctx context.Context, userID, id uint64, name string) error {
	res := r.db.WithContext(ctx).Model(&model.VirtualHuman{}).Where("id = ? AND user_id = ?", id, userID).Update("name", name)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateHumanWithImage 在一个事务里新建真人和它的第一张图片，成功后两者的 ID 已回填（img.HumanID 指向新真人）。
// 这张素材已在该用户的真人库里返回 ErrDuplicate。
func (r *VirtualHumanRepository) CreateHumanWithImage(ctx context.Context, h *model.VirtualHuman, img *model.VirtualHumanImage) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(h).Error; err != nil {
			return err
		}
		img.HumanID = h.ID
		if err := tx.Create(img).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrDuplicate
			}
			return err
		}
		return nil
	})
}

// DeleteHuman 在一个事务里删除真人和它的全部图片行，不动 assets；不存在或不属于该用户返回 ErrNotFound。
func (r *VirtualHumanRepository) DeleteHuman(ctx context.Context, userID, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("human_id = ? AND user_id = ?", id, userID).Delete(&model.VirtualHumanImage{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&model.VirtualHuman{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ListImages 返回用户全部真人图片，按真人、sort、id 升序（每个真人的第一张是封面）。
func (r *VirtualHumanRepository) ListImages(ctx context.Context, userID uint64) ([]model.VirtualHumanImage, error) {
	var rows []model.VirtualHumanImage
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("human_id ASC, sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// ListImagesByHuman 返回一个真人的图片，按 sort、id 升序；真人不存在时返回空切片。
func (r *VirtualHumanRepository) ListImagesByHuman(ctx context.Context, userID, humanID uint64) ([]model.VirtualHumanImage, error) {
	var rows []model.VirtualHumanImage
	err := r.db.WithContext(ctx).Where("user_id = ? AND human_id = ?", userID, humanID).Order("sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// GetImage 按 id + user_id 查询图片，不存在或不属于该用户返回 ErrNotFound。
func (r *VirtualHumanRepository) GetImage(ctx context.Context, userID, id uint64) (*model.VirtualHumanImage, error) {
	var img model.VirtualHumanImage
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&img).Error; err != nil {
		return nil, translate(err)
	}
	return &img, nil
}

// GetImageByAsset 按 user_id + asset_id 查询图片，不在真人库里返回 ErrNotFound。
func (r *VirtualHumanRepository) GetImageByAsset(ctx context.Context, userID, assetID uint64) (*model.VirtualHumanImage, error) {
	var img model.VirtualHumanImage
	if err := r.db.WithContext(ctx).Where("user_id = ? AND asset_id = ?", userID, assetID).First(&img).Error; err != nil {
		return nil, translate(err)
	}
	return &img, nil
}

// UpdateImageFiling 写回一张图片的备案结果（渠道、方舟素材 ID、状态、失败原因），不存在返回 ErrNotFound。
func (r *VirtualHumanRepository) UpdateImageFiling(ctx context.Context, img *model.VirtualHumanImage) error {
	res := r.db.WithContext(ctx).Model(&model.VirtualHumanImage{}).Where("id = ? AND user_id = ?", img.ID, img.UserID).
		Updates(map[string]any{"channel_key": img.ChannelKey, "ark_asset_id": img.ArkAssetID, "status": img.Status, "error": img.Error})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetImageSort 修改图片顺序，不存在返回 ErrNotFound。
func (r *VirtualHumanRepository) SetImageSort(ctx context.Context, userID, id uint64, sort int) error {
	res := r.db.WithContext(ctx).Model(&model.VirtualHumanImage{}).Where("id = ? AND user_id = ?", id, userID).Update("sort", sort)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteImage 删除一张图片行，不动 assets；不存在返回 ErrNotFound。
func (r *VirtualHumanRepository) DeleteImage(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.VirtualHumanImage{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// characterAssetPath 找出主图或补充图引用了 $ids 里任一素材的角色卡节点（lax 模式下 == 两边是序列时任一相等即为真）。
const characterAssetPath = `$.nodes[*] ? (@.data.kind == "character" && ` +
	`(@.data.assetId == $ids[*] || @.data.character.extras[*].assetId == $ids[*]))`

// CanvasesUsingAssets 返回该用户未删除的画布里，角色卡主图或补充图用到这些素材（十进制字符串）的画布内容。
// SQL 只做粗筛，具体是哪个节点用了哪张图由调用方解析画布内容得到。
func (r *CanvasProjectRepository) CanvasesUsingAssets(ctx context.Context, userID uint64, assetIDs []string) ([]model.CanvasPayloadRow, error) {
	if len(assetIDs) == 0 {
		return nil, nil
	}
	vars, err := json.Marshal(map[string]any{"ids": assetIDs})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID          uint64
		Title       string
		PayloadJSON []byte
	}
	err = r.db.WithContext(ctx).Model(&model.CanvasProject{}).Select("id, title, payload_json").
		Where("user_id = ? AND jsonb_path_exists(payload_json, ?::jsonpath, ?::jsonb)", userID, characterAssetPath, string(vars)).
		Order("id ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]model.CanvasPayloadRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.CanvasPayloadRow{ID: row.ID, Title: row.Title, Payload: row.PayloadJSON})
	}
	return out, nil
}

// errHumanGone 只在本文件内部使用：事务中途发现真人已被删除。
var errHumanGone = errors.New("virtual human gone")

// CreateImageInHuman 在事务里确认真人仍存在后新建图片：真人不存在返回 ErrNotFound，素材重复返回 ErrDuplicate。
// 和 GetHuman + CreateImage 分开调用相比，避免真人刚被删掉时留下没有主人的图片行。
func (r *VirtualHumanRepository) CreateImageInHuman(ctx context.Context, img *model.VirtualHumanImage) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.VirtualHuman{}).Where("id = ? AND user_id = ?", img.HumanID, img.UserID).
			Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return errHumanGone
		}
		return tx.Create(img).Error
	})
	switch {
	case errors.Is(err, errHumanGone):
		return ErrNotFound
	case err != nil && isUniqueViolation(err):
		return ErrDuplicate
	}
	return err
}
