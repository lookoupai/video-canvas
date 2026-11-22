package model

import (
	"time"

	"gorm.io/datatypes"
)

// DramaReferencePreparation 固定原素材、参考修订和处理版本，媒体进度独立于正式稿。
type DramaReferencePreparation struct {
	ID               uint64         `gorm:"primaryKey"`
	UserID           uint64         `gorm:"not null"`
	DramaID          uint64         `gorm:"not null"`
	ReferenceID      uint64         `gorm:"not null;uniqueIndex:uk_reference_preparation_recipe,priority:1;index:idx_reference_preparation_page"`
	BaseRevision     int64          `gorm:"not null"`
	SourceAssetID    uint64         `gorm:"not null"`
	DurationMs       int64          `gorm:"not null"`
	RecipeHash       string         `gorm:"size:64;not null;uniqueIndex:uk_reference_preparation_recipe,priority:2"`
	ProcessorVersion string         `gorm:"size:32;not null"`
	Document         datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	ShotIDs          datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Status           string         `gorm:"size:16;not null;default:queued"`
	Attempt          int            `gorm:"not null;default:1"`
	TotalFrames      int            `gorm:"not null;default:0"`
	CompletedFrames  int            `gorm:"not null;default:0"`
	HasAudio         *bool
	Error            string `gorm:"type:text;not null;default:''"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DramaReferencePreparationRequest 将每个外部请求键与规范化意图持久绑定。
type DramaReferencePreparationRequest struct {
	ID            uint64 `gorm:"primaryKey"`
	UserID        uint64 `gorm:"not null"`
	DramaID       uint64 `gorm:"not null"`
	ReferenceID   uint64 `gorm:"not null;uniqueIndex:uk_reference_preparation_request,priority:1"`
	PreparationID uint64 `gorm:"not null;index"`
	RequestKey    string `gorm:"size:128;not null;uniqueIndex:uk_reference_preparation_request,priority:2"`
	Kind          string `gorm:"size:16;not null"`
	Fingerprint   string `gorm:"size:64;not null"`
	CreatedAt     time.Time
}

// DramaReferencePreparationFrame 保存系统提帧的不可变时间依据，图片损坏时可替换素材 ID。
type DramaReferencePreparationFrame struct {
	ID            uint64 `gorm:"primaryKey"`
	UserID        uint64 `gorm:"not null"`
	PreparationID uint64 `gorm:"not null;uniqueIndex:uk_reference_preparation_frame,priority:1;index:idx_reference_preparation_frame_page"`
	ShotID        string `gorm:"size:128;not null;uniqueIndex:uk_reference_preparation_frame,priority:2"`
	StartMs       int64  `gorm:"not null"`
	EndMs         int64  `gorm:"not null"`
	AtMs          int64  `gorm:"not null;uniqueIndex:uk_reference_preparation_frame,priority:3"`
	AssetID       uint64 `gorm:"not null;default:0"`
	CacheID       uint64 `gorm:"not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DramaReferenceFrameCache 按用户、原素材、镜窗、时刻和处理版本复用有效图片，不混用分析修订。
type DramaReferenceFrameCache struct {
	ID               uint64 `gorm:"primaryKey"`
	UserID           uint64 `gorm:"not null;uniqueIndex:uk_reference_frame_cache,priority:1"`
	StorageToken     string `gorm:"size:48;not null"`
	CacheKey         string `gorm:"size:64;not null;uniqueIndex:uk_reference_frame_cache,priority:2"`
	SourceAssetID    uint64 `gorm:"not null"`
	StartMs          int64  `gorm:"not null"`
	EndMs            int64  `gorm:"not null"`
	AtMs             int64  `gorm:"not null"`
	ProcessorVersion string `gorm:"size:32;not null"`
	AssetID          uint64 `gorm:"not null;default:0"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CreateDramaReferencePreparationReq 明确本次处理的参考基准和可选镜头范围。
type CreateDramaReferencePreparationReq struct {
	RequestKey        string   `json:"request_key" binding:"required,max=128"`
	ReferenceRevision int64    `json:"reference_revision" binding:"required,min=1"`
	ShotIDs           []string `json:"shot_ids" binding:"max=200"`
}

// ResumeDramaReferencePreparationReq 用稳定请求键恢复失败或取消的准备。
type ResumeDramaReferencePreparationReq struct {
	RequestKey string `json:"request_key" binding:"required,max=128"`
}

// DramaReferenceFramePoint 指定一个原镜内的补充采样点。
type DramaReferenceFramePoint struct {
	ShotID string `json:"shot_id"`
	AtMs   int64  `json:"at_ms"`
}

// AddDramaReferenceFramesReq 合并有界补帧点，已有帧复用，缺失素材允许重建。
type AddDramaReferenceFramesReq struct {
	RequestKey string                     `json:"request_key" binding:"required,max=128"`
	Frames     []DramaReferenceFramePoint `json:"frames" binding:"required,min=1,max=24"`
}

// ListDramaReferenceFramesReq 按单镜过滤和帧 ID 游标分页。
type ListDramaReferenceFramesReq struct {
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit,default=20" binding:"min=1,max=100"`
	ShotID string `form:"shot_id" binding:"max=128"`
}

// DramaReferencePreparationView 只返回状态摘要，图片与镜头通过独立分页读取。
type DramaReferencePreparationView struct {
	ID               string    `json:"id"`
	ReferenceID      string    `json:"reference_id"`
	BaseRevision     int64     `json:"base_revision"`
	SourceAssetID    uint64    `json:"source_asset_id"`
	DurationMs       int64     `json:"duration_ms"`
	ProcessorVersion string    `json:"processor_version"`
	Status           string    `json:"status"`
	Attempt          int       `json:"attempt"`
	ShotCount        int       `json:"shot_count"`
	TotalFrames      int       `json:"total_frames"`
	CompletedFrames  int       `json:"completed_frames"`
	HasAudio         *bool     `json:"has_audio"`
	AudioStatus      string    `json:"audio_status"`
	Error            string    `json:"error"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// DramaReferencePreparationList 返回准备列表及下一页游标。
type DramaReferencePreparationList struct {
	Items      []DramaReferencePreparationView `json:"items"`
	NextCursor string                          `json:"next_cursor"`
}

// DramaReferencePreparedFrameView 把原片依据与实际持久帧图一起返回，缺失素材不作为可用证据。
type DramaReferencePreparedFrameView struct {
	ID               string     `json:"id"`
	PreparationID    string     `json:"preparation_id"`
	SourceAssetID    uint64     `json:"source_asset_id"`
	ShotID           string     `json:"shot_id"`
	StartMs          int64      `json:"start_ms"`
	EndMs            int64      `json:"end_ms"`
	AtMs             int64      `json:"at_ms"`
	ProcessorVersion string     `json:"processor_version"`
	Status           string     `json:"status"`
	Asset            *AssetView `json:"asset"`
}

// DramaReferenceFrameList 返回有限数量的帧图引用，不返回图片二进制。
type DramaReferenceFrameList struct {
	Items      []DramaReferencePreparedFrameView `json:"items"`
	NextCursor string                            `json:"next_cursor"`
}

// DramaReferencePreparedShotList 分页返回被冻结的选镜内容。
type DramaReferencePreparedShotList struct {
	Items      []DramaReferenceShot `json:"items"`
	NextCursor string               `json:"next_cursor"`
}
