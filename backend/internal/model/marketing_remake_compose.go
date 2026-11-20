package model

import (
	"time"

	"gorm.io/datatypes"
)

// MarketingRemakeComposition 是制作计划某次合成的固定选版清单与输出素材。
type MarketingRemakeComposition struct {
	ID             uint64         `gorm:"primaryKey"`
	UserID         uint64         `gorm:"not null"`
	PlanID         uint64         `gorm:"not null;uniqueIndex:uk_remake_composition_key,priority:1"`
	IdempotencyKey string         `gorm:"size:128;not null;uniqueIndex:uk_remake_composition_key,priority:2"`
	Revision       int64          `gorm:"not null"`
	AllShots       bool           `gorm:"not null"`
	Parts          datatypes.JSON `gorm:"type:jsonb;not null"`
	Status         string         `gorm:"size:16;not null"`
	Attempt        int            `gorm:"not null"`
	AssetID        *uint64
	Error          string `gorm:"type:text;not null;default:''"`
	CreatedAt      time.Time
}

// TableName 返回营销逐镜合成记录的表名。
func (MarketingRemakeComposition) TableName() string { return "marketing_remake_compositions" }

// RemakeCompositionPart 固定一镜采用的任务、素材及该素材自己的时间窗。
type RemakeCompositionPart struct {
	ShotID   int    `json:"shot_id"`
	Revision int64  `json:"revision"`
	TaskID   uint64 `json:"task_id"`
	AssetID  uint64 `json:"asset_id"`
	StartMs  int    `json:"start_ms"`
	EndMs    int    `json:"end_ms"`
}

// ComposeRemakePlanReq 省略镜头集合即要求整片出齐，显式选择时仍按计划顺序拼接。
type ComposeRemakePlanReq struct {
	Revision int64 `json:"revision" binding:"required,min=1"`
	ShotIDs  []int `json:"shot_ids" binding:"omitempty,min=1,dive,min=1"`
}

// RemakeCompositionView 公开合成任务与成功成片，失败的记录可以原键重试。
type RemakeCompositionView struct {
	ID             string     `json:"id"`
	Revision       int64      `json:"revision"`
	IdempotencyKey string     `json:"idempotency_key"`
	ShotIDs        []int      `json:"shot_ids"`
	Status         string     `json:"status"`
	Asset          *AssetView `json:"asset"`
	Error          string     `json:"error"`
	CreatedAt      time.Time  `json:"created_at"`
}
