package model

import (
	"time"

	"gorm.io/datatypes"
)

// DramaSegmentGenerationIntent 固定单镜重拍输入；请求重传与服务重启后沿同一个内部键恢复任务。
type DramaSegmentGenerationIntent struct {
	ID              uint64         `gorm:"primaryKey"`
	UserID          uint64         `gorm:"not null;uniqueIndex:uk_drama_segment_intent_request,priority:1"`
	RequestKey      string         `gorm:"size:128;not null;uniqueIndex:uk_drama_segment_intent_request,priority:2"`
	TaskKey         string         `gorm:"size:128;not null;uniqueIndex"`
	DramaID         uint64         `gorm:"not null;index"`
	CanvasID        uint64         `gorm:"not null"`
	EpisodeID       uint64         `gorm:"not null"`
	SegmentID       uint64         `gorm:"not null;index"`
	SegmentRevision int64          `gorm:"not null"`
	Fingerprint     string         `gorm:"size:64;not null"`
	ModelID         string         `gorm:"size:128;not null"`
	Input           datatypes.JSON `gorm:"type:jsonb;not null"`
	RemakeSnapshot  datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt       time.Time
}

// TableName 返回独立生成意图表，正式任务和积分仍由生成任务服务管理。
func (DramaSegmentGenerationIntent) TableName() string { return "drama_segment_generation_intents" }
