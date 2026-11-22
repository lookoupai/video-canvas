package model

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"
)

// DramaReferenceAssetIDs 接受 API 素材接口的 uint64 JSON ID 或无精度损失的十进制字符串。
type DramaReferenceAssetIDs []uint64

// UnmarshalJSON 按原始十进制词法解析素材 ID，避免浏览器或 Go float64 丢失 uint64 精度。
func (ids *DramaReferenceAssetIDs) UnmarshalJSON(raw []byte) error {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	parsed := make(DramaReferenceAssetIDs, 0, len(values))
	for _, value := range values {
		var decimal string
		if len(value) > 0 && value[0] == '"' {
			if err := json.Unmarshal(value, &decimal); err != nil {
				return err
			}
		} else {
			decimal = string(value)
		}
		id, err := strconv.ParseUint(strings.TrimSpace(decimal), 10, 64)
		if err != nil || id == 0 {
			return errors.New("素材 ID 必须为正整数")
		}
		parsed = append(parsed, id)
	}
	*ids = parsed
	return nil
}

// QuoteDramaReferenceDraftReq 固定草稿修订、选镜、目标规格和可选的站内积分上限。
type QuoteDramaReferenceDraftReq struct {
	Revision   int64                       `json:"revision" binding:"required,min=1"`
	ModelID    string                      `json:"model_id" binding:"required,max=128"`
	Units      []DramaReferencePlanUnitReq `json:"units" binding:"required,min=1,max=200,dive"`
	MaxCredits *int                        `json:"max_credits" binding:"omitempty,min=0"`
}

// ApplyDramaReferenceDraftReq 把当前草稿修订与报价摘要一次性采用为跟拍分集。
type ApplyDramaReferenceDraftReq struct {
	Revision        int64                       `json:"revision" binding:"required,min=1"`
	RequestKey      string                      `json:"request_key" binding:"required,max=128"`
	PlanDigest      string                      `json:"plan_digest" binding:"required,len=64"`
	Mode            string                      `json:"mode" binding:"required,oneof=follow"`
	Instruction     string                      `json:"instruction" binding:"max=4000"`
	TargetEpisodeNo int                         `json:"target_episode_no" binding:"min=0"`
	EpisodeRevision int64                       `json:"episode_revision" binding:"min=0"`
	Quote           QuoteDramaReferenceDraftReq `json:"quote" binding:"required"`
}

// DramaReferenceDraftApplication 记录唯一的草稿采用回执和批准计划，供请求重放及任务恢复使用。
type DramaReferenceDraftApplication struct {
	ID            uint64         `gorm:"primaryKey"`
	UserID        uint64         `gorm:"not null;uniqueIndex:uk_drama_reference_apply_key,priority:1;uniqueIndex:uk_drama_reference_apply_draft,priority:1"`
	DramaID       uint64         `gorm:"not null;index"`
	ReferenceID   uint64         `gorm:"not null;index"`
	DraftID       uint64         `gorm:"not null;uniqueIndex:uk_drama_reference_apply_draft,priority:2"`
	DraftRevision int64          `gorm:"not null;uniqueIndex:uk_drama_reference_apply_draft,priority:3"`
	RequestKey    string         `gorm:"size:128;not null;uniqueIndex:uk_drama_reference_apply_key,priority:2"`
	Fingerprint   string         `gorm:"size:64;not null"`
	PlanDigest    string         `gorm:"size:64;not null"`
	Quote         datatypes.JSON `gorm:"type:jsonb;not null"`
	Response      datatypes.JSON `gorm:"type:jsonb;not null"`
	AdoptionID    uint64         `gorm:"not null;index"`
	EpisodeID     uint64         `gorm:"not null;index"`
	CreatedAt     time.Time
}

// TableName 返回草稿采用回执的表名。
func (DramaReferenceDraftApplication) TableName() string { return "drama_reference_draft_applications" }

// DramaReferenceDraftGeneration 保存单个付费批次的计划、逐镜任务键及可重放结果。
type DramaReferenceDraftGeneration struct {
	ID            uint64         `gorm:"primaryKey"`
	UserID        uint64         `gorm:"not null;uniqueIndex:uk_drama_reference_generation_key,priority:1"`
	DramaID       uint64         `gorm:"not null;index"`
	ReferenceID   uint64         `gorm:"not null;index"`
	DraftID       uint64         `gorm:"not null;index"`
	ApplicationID uint64         `gorm:"not null;index"`
	RequestKey    string         `gorm:"size:128;not null;uniqueIndex:uk_drama_reference_generation_key,priority:2"`
	Fingerprint   string         `gorm:"size:64;not null"`
	PlanDigest    string         `gorm:"size:64;not null"`
	Quote         datatypes.JSON `gorm:"type:jsonb;not null"`
	Response      datatypes.JSON `gorm:"type:jsonb;not null"`
	Status        string         `gorm:"size:16;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TableName 返回生成批次回执的表名。
func (DramaReferenceDraftGeneration) TableName() string { return "drama_reference_draft_generations" }

// DramaReferenceApplyUnit 固定采用时写入单镜片段的生成提示词和输出裁剪窗口。
type DramaReferenceApplyUnit struct {
	ShotID           string `json:"shot_id"`
	Prompt           string `json:"prompt"`
	Duration         int    `json:"duration"`
	TrimStartMs      int64  `json:"trim_start_ms"`
	TargetDurationMs int64  `json:"target_duration_ms"`
}

// DramaReferenceApplyResponse 是 apply API 和重复请求共同返回的持久化回执。
type DramaReferenceApplyResponse struct {
	ApplicationID     string                         `json:"application_id"`
	DraftID           string                         `json:"draft_id"`
	DraftRevision     int64                          `json:"draft_revision"`
	ReferenceRevision int64                          `json:"reference_revision"`
	PlanDigest        string                         `json:"plan_digest"`
	AdoptionID        string                         `json:"adoption_id"`
	EpisodeID         string                         `json:"episode_id"`
	EpisodeNo         int                            `json:"episode_no"`
	EpisodeRevision   int64                          `json:"episode_revision"`
	Segments          []DramaReferenceAppliedSegment `json:"segments"`
	Replayed          bool                           `json:"replayed"`
}

// DramaReferenceGenerateReq 只生成已采用回执中的选镜，幂等键作用于整个批次。
type DramaReferenceGenerateReq struct {
	Revision      int64                       `json:"revision" binding:"required,min=1"`
	RequestKey    string                      `json:"request_key" binding:"required,max=128"`
	ApplicationID string                      `json:"application_id" binding:"required"`
	PlanDigest    string                      `json:"plan_digest" binding:"required,len=64"`
	Quote         QuoteDramaReferenceDraftReq `json:"quote" binding:"required"`
}

// DramaReferenceGenerateItem 保存单镜生成重放所需的原请求和分配的幂等任务键。
type DramaReferenceGenerateItem struct {
	ShotID             string                  `json:"shot_id"`
	SegmentID          string                  `json:"segment_id"`
	Revision           int64                   `json:"revision"`
	TaskID             string                  `json:"task_id,omitempty"`
	Status             string                  `json:"status"`
	Error              string                  `json:"error,omitempty"`
	TaskKey            string                  `json:"task_key"`
	MaxCredits         int                     `json:"max_credits"`
	ValidateAgnesVideo bool                    `json:"validate_agnes_video"`
	Request            GenerateDramaSegmentReq `json:"request"`
}

// DramaReferenceGenerateResponse 汇总批次状态和逐镜任务 ID。
type DramaReferenceGenerateResponse struct {
	ApplicationID       string                       `json:"application_id"`
	PlanDigest          string                       `json:"plan_digest"`
	ConfigurationDigest string                       `json:"configuration_digest"`
	Status              string                       `json:"status"`
	Items               []DramaReferenceGenerateItem `json:"items"`
	Replayed            bool                         `json:"replayed"`
}

// DramaReferenceAppliedSegment 返回采用创建或更新的原镜片段和裁剪边界。
type DramaReferenceAppliedSegment struct {
	ShotID           string `json:"shot_id"`
	SegmentID        string `json:"segment_id"`
	Revision         int64  `json:"revision"`
	TrimStartMs      int64  `json:"trim_start_ms"`
	TargetDurationMs int64  `json:"target_duration_ms"`
}

// DramaReferencePlanUnitReq 选择一镜的生成路线、参考图和目标裁剪。
type DramaReferencePlanUnitReq struct {
	ShotID           string                 `json:"shot_id" binding:"required,max=128"`
	Mode             string                 `json:"mode" binding:"omitempty,oneof=auto text keyframe image video"`
	ImageAssetIDs    DramaReferenceAssetIDs `json:"image_asset_ids" binding:"omitempty,max=12"`
	Duration         int                    `json:"duration" binding:"min=0,max=86400"`
	TrimStartMs      int64                  `json:"trim_start_ms" binding:"min=0"`
	TargetDurationMs int64                  `json:"target_duration_ms" binding:"min=0"`
	Params           map[string]any         `json:"params,omitempty" binding:"omitempty,max=32"`
}

// DramaReferencePlanIssue 描述需要修改的设置或需要用户核对的路线风险。
type DramaReferencePlanIssue struct {
	Code     string `json:"code"`
	Field    string `json:"field"`
	Message  string `json:"message"`
	Blocking bool   `json:"blocking"`
}

// DramaReferencePlanUnit 给出一次单镜生成请求的有效输入、原镜及输出裁剪。
type DramaReferencePlanUnit struct {
	ShotID           string                    `json:"shot_id"`
	Mode             string                    `json:"mode"`
	SourceStartMs    int64                     `json:"source_start_ms"`
	SourceEndMs      int64                     `json:"source_end_ms"`
	Duration         int                       `json:"duration"`
	TrimStartMs      int64                     `json:"trim_start_ms"`
	TargetDurationMs int64                     `json:"target_duration_ms"`
	Prompt           string                    `json:"prompt"`
	ImageAssetIDs    []string                  `json:"image_asset_ids"`
	Materials        []DramaReferencePlanAsset `json:"materials"`
	Input            map[string]any            `json:"input,omitempty"`
	Credits          *int                      `json:"credits"`
	Issues           []DramaReferencePlanIssue `json:"issues"`
}

// DramaReferencePlanAsset 固定报价所用素材身份与大小，避免同 ID 内容变化沿用旧批准。
type DramaReferencePlanAsset struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	MimeType   string `json:"mime_type"`
	Sha256     string `json:"sha256"`
	ByteSize   int64  `json:"byte_size"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMs int64  `json:"duration_ms"`
}

// GenerateRequest 提供经过报价的有效输入。视频由片段服务按已采用的原镜静音裁出。
func (u DramaReferencePlanUnit) GenerateRequest(modelID string, segmentRevision int64) *GenerateDramaSegmentReq {
	input := maps.Clone(u.Input)
	if ids, ok := input["images"].([]uint64); ok {
		input["images"] = slices.Clone(ids)
	}
	if u.Mode == "video" {
		delete(input, "videos")
	}
	return &GenerateDramaSegmentReq{ModelID: modelID, Input: input, Revision: segmentRevision, UseReferenceVideo: u.Mode == "video"}
}

// DramaReferencePlan 是报价预览及随后审批校验所需的不可歧义摘要。
type DramaReferencePlan struct {
	DraftID             string                    `json:"draft_id"`
	ProjectID           string                    `json:"project_id"`
	ReferenceID         string                    `json:"reference_id"`
	DraftRevision       int64                     `json:"draft_revision"`
	ReferenceRevision   int64                     `json:"reference_revision"`
	SourceAssetID       string                    `json:"source_asset_id"`
	ModelID             string                    `json:"model_id"`
	PromptFormat        string                    `json:"prompt_format"`
	ConfigurationDigest string                    `json:"configuration_digest"`
	PlanDigest          string                    `json:"plan_digest"`
	Executable          bool                      `json:"executable"`
	TotalVideoCredits   int                       `json:"total_video_credits"`
	MaxCredits          *int                      `json:"max_credits,omitempty"`
	Units               []DramaReferencePlanUnit  `json:"units"`
	Issues              []DramaReferencePlanIssue `json:"issues"`
}
