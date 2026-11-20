package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// MarketingRemakePlan 固定某版跟拍脚本的生产资料；Original 永远保留创建时的镜头，Data 是当前编辑稿。
type MarketingRemakePlan struct {
	ID             uint64         `gorm:"primaryKey"`
	UserID         uint64         `gorm:"not null"`
	ProjectID      uint64         `gorm:"not null"`
	ScriptID       uint64         `gorm:"not null;uniqueIndex:uk_mkt_plan_script_rev,priority:1"`
	ScriptRevision int64          `gorm:"not null;uniqueIndex:uk_mkt_plan_script_rev,priority:2"`
	Revision       int64          `gorm:"not null;default:1"`
	Original       datatypes.JSON `gorm:"type:jsonb;not null"`
	Data           datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TableName 返回营销逐镜制作计划表名。
func (MarketingRemakePlan) TableName() string { return "marketing_remake_plans" }

// RemakePlanData 保存来源素材、商品、轮次设置以及有序的成片镜。
type RemakePlanData struct {
	Product         json.RawMessage         `json:"product"`
	Brands          []string                `json:"brands"`
	Claims          []string                `json:"claims"`
	ProductAssets   []uint64                `json:"product_assets"`
	ModelAssetID    *uint64                 `json:"model_asset_id"`
	RefVideoAssetID *uint64                 `json:"ref_video_asset_id"`
	Settings        MarketingSettings       `json:"settings"`
	SourceCatalog   []MarketingRemakeSource `json:"source_catalog"`
	Shots           []RemakePlanShot        `json:"shots"`
}

// RemakePlanShot 的 ID 固定为初始镜号；数组位置决定展示和合成顺序。
type RemakePlanShot struct {
	ID           int                     `json:"id"`
	Revision     int64                   `json:"revision"`
	Scene        string                  `json:"scene"`
	Narration    string                  `json:"narration"`
	DurationMs   int                     `json:"duration_ms"`
	TrimStartMs  int                     `json:"trim_start_ms"`
	Sources      []MarketingRemakeSource `json:"sources"`
	Unresolved   bool                    `json:"unresolved"`
	ChosenTaskID *uint64                 `json:"chosen_task_id"`
}

// MarketingRemakePlanView 包含当前编辑稿及向用户展示的分镜任务记录和报价。
type MarketingRemakePlanView struct {
	MarketingRemakePlanSummary
	Revision        int64                         `json:"revision"`
	VideoModel      string                        `json:"video_model"`
	RefVideoAssetID *uint64                       `json:"ref_video_asset_id"`
	SourceCatalog   []MarketingRemakeSource       `json:"source_catalog"`
	Shots           []MarketingRemakePlanShotView `json:"shots"`
}

// MarketingRemakePlanShotView 返回画面、来源、当前可用的生成选项和完整版本列表。
type MarketingRemakePlanShotView struct {
	RemakePlanShot
	Takes           []MarketingTaskLite `json:"takes"`
	GenerateSeconds int                 `json:"generate_seconds"`
	RenderCredits   int                 `json:"render_credits"`
	GenerateError   string              `json:"generate_error"`
}

// CreateRemakePlanReq 使用创建者预览时所见的脚本修订。
type CreateRemakePlanReq struct {
	Revision int64 `json:"revision" binding:"required,min=1"`
}

// UpdateRemakeShotReq 只修改传入字段。采用任务 ID 为 0 表示取消采用。
type UpdateRemakeShotReq struct {
	Revision     int64                    `json:"revision" binding:"required,min=1"`
	Scene        *string                  `json:"scene" binding:"omitempty,min=1,max=4000"`
	Narration    *string                  `json:"narration" binding:"omitempty,max=4000"`
	DurationMs   *int                     `json:"duration_ms" binding:"omitempty,min=1,max=600000"`
	TrimStartMs  *int                     `json:"trim_start_ms" binding:"omitempty,min=0,max=600000"`
	Sources      *[]MarketingRemakeSource `json:"sources" binding:"omitempty,max=128"`
	ChosenTaskID *uint64                  `json:"chosen_task_id"`
}

// OrderRemakeShotsReq 提交所有稳定镜号的完整排列。
type OrderRemakeShotsReq struct {
	Revision int64 `json:"revision" binding:"required,min=1"`
	ShotIDs  []int `json:"shot_ids" binding:"required,min=1"`
}
