package model

import (
	"time"

	"gorm.io/datatypes"
)

// DramaReferenceDraft 保存独立分析草稿，正式参考稿和采用结果不随编辑改变。
type DramaReferenceDraft struct {
	ID            uint64         `gorm:"primaryKey"`
	UserID        uint64         `gorm:"not null"`
	DramaID       uint64         `gorm:"not null"`
	ReferenceID   uint64         `gorm:"not null;index:idx_reference_draft_page,priority:1;uniqueIndex:uk_reference_draft_key,priority:1"`
	RequestKey    string         `gorm:"size:128;not null;uniqueIndex:uk_reference_draft_key,priority:2"`
	Fingerprint   string         `gorm:"size:64;not null"`
	PreparationID uint64         `gorm:"not null;default:0"`
	BaseRevision  int64          `gorm:"not null"`
	Revision      int64          `gorm:"not null;default:1"`
	Status        string         `gorm:"size:16;not null;default:editing"`
	Source        string         `gorm:"size:16;not null"`
	SessionID     uint64         `gorm:"not null;default:0" json:"-"`
	RunID         uint64         `gorm:"not null;default:0" json:"-"`
	Content       datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DramaReferenceCoverage 分别标记镜头边界、视觉、对白与说话人的已分析范围。
type DramaReferenceCoverage struct {
	Shots    string `json:"shots"`
	Visual   string `json:"visual"`
	Dialogue string `json:"dialogue"`
	Speakers string `json:"speakers"`
}

// DramaReferencePrompt 是用户或 Agent 为单镜编写、等待采用的生成描述。
type DramaReferencePrompt struct {
	ShotID string `json:"shot_id"`
	Prompt string `json:"prompt"`
}

// DramaReferenceEvidence 关联本人帧图与原镜内时间；外部声明不代表系统提帧证明。
type DramaReferenceEvidence struct {
	PreparationFrameID string `json:"preparation_frame_id,omitempty"`
	ID                 string `json:"id"`
	ShotID             string `json:"shot_id"`
	AssetID            uint64 `json:"asset_id"`
	AtMs               int64  `json:"at_ms"`
}

// DramaReferenceObservation 记录字段依据与待核对状态，不能用来声明可信 Agent 身份。
type DramaReferenceObservation struct {
	ShotID      string   `json:"shot_id"`
	Field       string   `json:"field"`
	Source      string   `json:"source"`
	ReviewState string   `json:"review_state"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// DramaReferenceDraftContent 是整体校验、整体持久化的编辑内容。
type DramaReferenceDraftContent struct {
	Instruction  string                      `json:"instruction"`
	Document     DramaReferenceDocument      `json:"document"`
	Prompts      []DramaReferencePrompt      `json:"prompts"`
	Evidence     []DramaReferenceEvidence    `json:"evidence"`
	Observations []DramaReferenceObservation `json:"observations"`
	Coverage     DramaReferenceCoverage      `json:"coverage"`
}

// CreateDramaReferenceDraftReq 固定参考基准与创建键，来源由服务入口设置。
type CreateDramaReferenceDraftReq struct {
	PreparationID     string                      `json:"preparation_id,omitempty"`
	RequestKey        string                      `json:"request_key" binding:"required,max=128"`
	ReferenceRevision int64                       `json:"reference_revision" binding:"required,min=1"`
	Instruction       string                      `json:"instruction" binding:"max=4000"`
	Document          *DramaReferenceDocument     `json:"document"`
	Prompts           []DramaReferencePrompt      `json:"prompts"`
	Evidence          []DramaReferenceEvidence    `json:"evidence"`
	Observations      []DramaReferenceObservation `json:"observations"`
	Coverage          *DramaReferenceCoverage     `json:"coverage"`
}

// UpdateDramaReferenceDraftReq 按修订修改指定字段；数组整体替换，省略保留。
type UpdateDramaReferenceDraftReq struct {
	Revision     int64                        `json:"revision" binding:"required,min=1"`
	Instruction  *string                      `json:"instruction" binding:"omitempty,max=4000"`
	Document     *DramaReferenceDocument      `json:"document"`
	Prompts      *[]DramaReferencePrompt      `json:"prompts"`
	Evidence     *[]DramaReferenceEvidence    `json:"evidence"`
	Observations *[]DramaReferenceObservation `json:"observations"`
	Coverage     *DramaReferenceCoverage      `json:"coverage"`
}

// ListDramaReferenceDraftsReq 以草稿 ID 游标倒序分页。
type ListDramaReferenceDraftsReq struct {
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit,default=20" binding:"min=1,max=100"`
}

// DramaReferenceDraftView 返回可恢复的草稿内容和并发修改修订。
type DramaReferenceDraftView struct {
	DramaReferenceDraftContent
	PreparationID string    `json:"preparation_id,omitempty"`
	ID            string    `json:"id"`
	ReferenceID   string    `json:"reference_id"`
	BaseRevision  int64     `json:"base_revision"`
	Revision      int64     `json:"revision"`
	Status        string    `json:"status"`
	Source        string    `json:"source"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DramaReferenceDraftList 明确给出下一页游标；空游标表示遍历结束。
type DramaReferenceDraftList struct {
	Items      []DramaReferenceDraftView `json:"items"`
	NextCursor string                    `json:"next_cursor"`
}
