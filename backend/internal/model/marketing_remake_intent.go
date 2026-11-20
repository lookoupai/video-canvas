package model

import (
	"time"

	"gorm.io/datatypes"
)

// MarketingRemakeIntent 固定单镜重拍的输入和内部任务键。任务按用户和内部键关联，进程重启可继续提交。
type MarketingRemakeIntent struct {
	ID             uint64         `gorm:"primaryKey"`
	UserID         uint64         `gorm:"not null;uniqueIndex:uk_remake_intent_key,priority:1"`
	RequestKey     string         `gorm:"size:128;not null;uniqueIndex:uk_remake_intent_key,priority:2"`
	TaskKey        string         `gorm:"size:128;not null;uniqueIndex:uk_remake_intent_task"`
	PlanID         uint64         `gorm:"not null;index:idx_remake_intent_plan"`
	ShotID         int            `gorm:"not null"`
	Revision       int64          `gorm:"not null"`
	NodeID         string         `gorm:"size:64;not null"`
	ModelID        string         `gorm:"size:128;not null"`
	Input          datatypes.JSON `gorm:"type:jsonb;not null"`
	ShotSnapshot   datatypes.JSON `gorm:"type:jsonb;not null"`    // 保留当时的动作、来源区间及目标时间窗
	ReferenceAudio bool           `gorm:"not null;default:false"` // 原片只作画面参考
	CreatedAt      time.Time
}

// GenerateRemakeShotReq 只允许提交所见镜头修订，生成输入从服务端快照获取。
type GenerateRemakeShotReq struct {
	Revision int64 `json:"revision" binding:"required,min=1" label:"镜头版本"`
}
