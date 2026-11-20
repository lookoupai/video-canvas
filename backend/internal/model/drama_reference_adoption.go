package model

import (
	"time"

	"gorm.io/datatypes"
)

// DramaReferenceAdoption 固定已采用的原片依据与实际分集；后续校订不会改写它。
type DramaReferenceAdoption struct {
	ID                uint64         `gorm:"primaryKey"`
	UserID            uint64         `gorm:"not null;uniqueIndex:uk_drama_adoption_key,priority:1"`
	DramaID           uint64         `gorm:"not null;index"`
	ReferenceID       uint64         `gorm:"not null;index"`
	ReferenceRevision int64          `gorm:"not null"`
	RequestKey        string         `gorm:"size:128;not null;uniqueIndex:uk_drama_adoption_key,priority:2"`
	RequestKeys       datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	Fingerprint       string         `gorm:"size:64;not null"`
	Mode              string         `gorm:"size:16;not null"`
	EpisodeID         uint64         `gorm:"not null;index"`
	Snapshot          datatypes.JSON `gorm:"type:jsonb;not null"`
	SegmentIDs        datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt         time.Time
}

// DramaRemakeSource 是采用原镜的左闭右开毫秒区间。
type DramaRemakeSource struct {
	ShotID  string `json:"shot_id"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
}

// DramaRemakeRole 固定当时确认的原角色到目标角色身份对应。
type DramaRemakeRole struct {
	CharacterID  string `json:"character_id"`
	TargetNodeID string `json:"target_node_id"`
	TargetName   string `json:"target_name"`
}

// DramaSegmentRemake 保存单镜生成与合成的原片依据及独立目标裁剪窗口。
type DramaSegmentRemake struct {
	AdoptionID        string              `json:"adoption_id"`
	ReferenceID       string              `json:"reference_id"`
	ReferenceRevision int64               `json:"reference_revision"`
	AssetID           uint64              `json:"asset_id"`
	Sources           []DramaRemakeSource `json:"sources"`
	Roles             []DramaRemakeRole   `json:"roles"`
	TargetDurationMs  int64               `json:"target_duration_ms"`
	TrimStartMs       int64               `json:"trim_start_ms"`
}

// DramaReferenceBasis 为跟拍或原创改编保留已采用的完整原稿和角色快照。
type DramaReferenceBasis struct {
	AdoptionID        string                 `json:"adoption_id"`
	ReferenceID       string                 `json:"reference_id"`
	ReferenceRevision int64                  `json:"reference_revision"`
	AssetID           uint64                 `json:"asset_id"`
	Mode              string                 `json:"mode"`
	Instruction       string                 `json:"instruction"`
	Document          DramaReferenceDocument `json:"document"`
	Roles             []DramaRemakeRole      `json:"roles"`
}

// AdoptDramaReferenceReq 首次采用可建新集，更新已有集须显式指定镜头与集修订。
type AdoptDramaReferenceReq struct {
	Revision        int64    `json:"revision" binding:"required,min=1"`
	RequestKey      string   `json:"request_key" binding:"required,max=128"`
	Mode            string   `json:"mode" binding:"required,oneof=follow rewrite"`
	Instruction     string   `json:"instruction" binding:"max=4000"`
	TargetEpisodeNo int      `json:"target_episode_no" binding:"min=0"`
	EpisodeRevision int64    `json:"episode_revision" binding:"min=0"`
	ShotIDs         []string `json:"shot_ids" binding:"omitempty,max=2000"`
}

// DramaReferenceAdoptionView 使用当前分集号定位实际片场，已删除关联保留历史记录。
type DramaReferenceAdoptionView struct {
	ID                string   `json:"id"`
	ReferenceID       string   `json:"reference_id"`
	ReferenceRevision int64    `json:"reference_revision"`
	Mode              string   `json:"mode"`
	EpisodeID         string   `json:"episode_id"`
	EpisodeNo         int      `json:"episode_no"`
	EpisodeRevision   int64    `json:"episode_revision"`
	SegmentIDs        []string `json:"segment_ids"`
	Deleted           bool     `json:"deleted"`
}
