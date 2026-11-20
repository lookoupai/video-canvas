package model

import (
	"time"

	"gorm.io/datatypes"
)

// DramaReferenceState 串行化同一项目的参考集顺序，创建键支持网络重传。
type DramaReferenceState struct {
	DramaID    uint64 `gorm:"primaryKey;autoIncrement:false"`
	UserID     uint64 `gorm:"not null;uniqueIndex:uk_drama_reference_create,priority:1"`
	RequestKey string `gorm:"size:128;not null;uniqueIndex:uk_drama_reference_create,priority:2"`
	Revision   int64  `gorm:"not null;default:1"`
}

// DramaReferenceEpisode 每个参考集独立导入和校订，Document 在分析失败时保留。
type DramaReferenceEpisode struct {
	ID         uint64         `gorm:"primaryKey"`
	UserID     uint64         `gorm:"not null"`
	DramaID    uint64         `gorm:"not null;index:idx_drama_reference_order,priority:1;uniqueIndex:uk_drama_reference_episode_key,priority:1"`
	RequestKey string         `gorm:"size:128;not null;uniqueIndex:uk_drama_reference_episode_key,priority:2"`
	Seq        int            `gorm:"not null;index:idx_drama_reference_order,priority:2"`
	Title      string         `gorm:"size:255;not null"`
	AssetID    uint64         `gorm:"not null"`
	DurationMs int64          `gorm:"not null"`
	Revision   int64          `gorm:"not null;default:1"`
	Attempt    int            `gorm:"not null;default:0"`
	Status     string         `gorm:"size:16;not null;default:idle"`
	Error      string         `gorm:"type:text;not null;default:''"`
	Method     string         `gorm:"size:32;not null;default:''"`
	Input      []byte         `gorm:"type:bytea"`
	Document   datatypes.JSON `gorm:"type:jsonb"`
	Bindings   datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	UpdatedAt  time.Time
}

// DramaReferenceRevision 保留导入与人工校订的不可变原片依据。
type DramaReferenceRevision struct {
	ID          uint64         `gorm:"primaryKey"`
	UserID      uint64         `gorm:"not null"`
	ReferenceID uint64         `gorm:"not null;uniqueIndex:uk_drama_reference_revision,priority:1"`
	Revision    int64          `gorm:"not null;uniqueIndex:uk_drama_reference_revision,priority:2"`
	Method      string         `gorm:"size:32;not null"`
	Document    datatypes.JSON `gorm:"type:jsonb;not null"`
	Bindings    datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time
}

// DramaReferenceRole 是用户确认的跨集身份，目标节点引用当前项目角色画布。
type DramaReferenceRole struct {
	ID           uint64 `gorm:"primaryKey"`
	UserID       uint64 `gorm:"not null"`
	DramaID      uint64 `gorm:"not null;index"`
	Name         string `gorm:"size:128;not null"`
	TargetNodeID string `gorm:"size:128;not null;default:''"`
	Revision     int64  `gorm:"not null;default:1"`
}

// CreateDramaReferenceProjectReq 建立等待导入参考片的短剧项目。
type CreateDramaReferenceProjectReq struct {
	Title      string `json:"title" binding:"required,max=255"`
	Ratio      string `json:"ratio" binding:"omitempty,oneof=9:16 16:9 4:3"`
	RequestKey string `json:"request_key" binding:"required,max=128"`
}

// DramaReferenceProjectView 返回新项目及其专属资产画布。
type DramaReferenceProjectView struct {
	ID       string `json:"id"`
	CanvasID string `json:"canvas_id"`
}

// AddDramaReferenceReq 把本人视频素材关联为新参考集。
type AddDramaReferenceReq struct {
	AssetID    uint64 `json:"asset_id" binding:"required"`
	Title      string `json:"title" binding:"max=255"`
	RequestKey string `json:"request_key" binding:"required,max=128"`
}

// UpdateDramaReferenceReq 使用本集修订保存描述与说话人校订，Bindings 关联本集人物与全局角色。
type UpdateDramaReferenceReq struct {
	Revision int64                   `json:"revision" binding:"required,min=1"`
	Title    *string                 `json:"title" binding:"omitempty,max=255"`
	Document *DramaReferenceDocument `json:"document"`
	Bindings map[string]string       `json:"bindings"`
}

// OrderDramaReferencesReq 以完整 ID 排列更新参考集顺序。
type OrderDramaReferencesReq struct {
	Revision int64    `json:"revision" binding:"required,min=1"`
	IDs      []string `json:"ids" binding:"required,min=1,max=200"`
}

// AnalyzeDramaReferenceReq 指定本次开始分析时所见的参考集修订。
type AnalyzeDramaReferenceReq struct {
	Revision int64 `json:"revision" binding:"required,min=1"`
}

// SaveDramaReferenceRoleReq 新建或带修订编辑跨集身份。
type SaveDramaReferenceRoleReq struct {
	Revision     int64  `json:"revision"`
	Name         string `json:"name" binding:"required,max=128"`
	TargetNodeID string `json:"target_node_id" binding:"max=128"`
}

// DramaReferenceRoleView 是可跨多个参考集复用的同一角色身份。
type DramaReferenceRoleView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TargetNodeID string `json:"target_node_id"`
	Revision     int64  `json:"revision"`
}

// DramaReferenceVersionView 指向一份不可变的原片拆解修订。
type DramaReferenceVersionView struct {
	Revision  int64     `json:"revision"`
	Method    string    `json:"method"`
	CreatedAt time.Time `json:"created_at"`
}

// DramaReferenceView 同时给出处理状态和最新可用校订稿，失败时仍可回看原稿。
type DramaReferenceView struct {
	ID         string                      `json:"id"`
	Seq        int                         `json:"seq"`
	Title      string                      `json:"title"`
	Asset      *AssetView                  `json:"asset"`
	DurationMs int64                       `json:"duration_ms"`
	Revision   int64                       `json:"revision"`
	Status     string                      `json:"status"`
	Error      string                      `json:"error"`
	Method     string                      `json:"method"`
	Document   *DramaReferenceDocument     `json:"document"`
	Bindings   map[string]string           `json:"bindings"`
	Versions   []DramaReferenceVersionView `json:"versions"`
}

// DramaReferenceTarget 是本项目中可作为新角色的画布节点。
type DramaReferenceTarget struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

// DramaReferenceCollection 提供参考集顺序、跨集身份及可绑定角色。
type DramaReferenceCollection struct {
	Title             string                   `json:"title"`
	CanvasID          string                   `json:"canvas_id"`
	Revision          int64                    `json:"revision"`
	AnalyzerAvailable bool                     `json:"analyzer_available"`
	Episodes          []DramaReferenceView     `json:"episodes"`
	Roles             []DramaReferenceRoleView `json:"roles"`
	Targets           []DramaReferenceTarget   `json:"targets"`
}
