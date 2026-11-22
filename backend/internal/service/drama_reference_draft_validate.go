package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/reference"
	"video-canvas/internal/repository"
)

// validateDraftContent 验证合并后的完整内容，未知内容可以保留但不能伪称已观察。
func (s *DramaReferenceDraftService) validateDraftContent(ctx context.Context, u uint64, duration int64, content model.DramaReferenceDraftContent) error {
	raw, err := json.Marshal(content)
	if err != nil || len(raw) > maxReferenceDraftBytes || !validDraftText(content.Instruction, 4000) {
		return errcode.ErrInvalidParams.WithMsg("草稿内容超过允许范围")
	}
	if err := validateDraftDocument(content.Document, duration); err != nil {
		return err
	}
	if err := validateDraftCoverage(content.Coverage, len(content.Document.Shots)); err != nil {
		return err
	}
	shots := map[string]model.DramaReferenceShot{}
	for _, shot := range content.Document.Shots {
		shots[shot.ID] = shot
	}
	if err := validateDraftPrompts(content.Prompts, shots); err != nil {
		return err
	}
	if err := validateDraftEvidence(content.Evidence, shots); err != nil {
		return err
	}
	if err := validateDraftObservations(content.Observations, content.Evidence, shots); err != nil {
		return err
	}
	return s.validateDraftAssets(ctx, u, content.Evidence)
}

// validateDraftDocument 允许尚未分析的完全空稿，有镜头后复用正式参考稿的时间与引用校验。
func validateDraftDocument(doc model.DramaReferenceDocument, duration int64) error {
	if len(doc.Shots) == 0 {
		if len(doc.Scenes)+len(doc.Characters)+len(doc.Dialogues) != 0 {
			return errcode.ErrInvalidParams.WithMsg("空草稿不能包含孤立场次、人物或对白")
		}
		return nil
	}
	if err := reference.ValidateDocument(doc, duration); err != nil {
		return errcode.ErrInvalidParams.WithMsg("镜头、对白时间或人物引用无效")
	}
	return nil
}

// validateDraftCoverage 未知状态必须明确保留，空稿不能声称任一维度已完成。
func validateDraftCoverage(coverage model.DramaReferenceCoverage, shots int) error {
	for _, state := range []string{coverage.Shots, coverage.Visual, coverage.Dialogue, coverage.Speakers} {
		switch state {
		case "not_analyzed", "partial", "unavailable":
		case "complete":
			if shots == 0 {
				return errcode.ErrInvalidParams.WithMsg("空草稿不能标记为分析完成")
			}
		default:
			return errcode.ErrInvalidParams.WithMsg("分析覆盖状态无效")
		}
	}
	return nil
}

// validateDraftPrompts 每镜只允许一条有限长度描述，并要求镜号属于当前草稿。
func validateDraftPrompts(prompts []model.DramaReferencePrompt, shots map[string]model.DramaReferenceShot) error {
	seen := map[string]bool{}
	for _, prompt := range prompts {
		if _, ok := shots[prompt.ShotID]; !ok || seen[prompt.ShotID] || !validDraftText(prompt.Prompt, 40000) {
			return errcode.ErrInvalidParams.WithMsg("镜头提示词重复、过长或引用未知镜头")
		}
		seen[prompt.ShotID] = true
	}
	return nil
}

// validateDraftEvidence 固定每张图片所在镜头和左闭右开的原片时间窗。
func validateDraftEvidence(evidence []model.DramaReferenceEvidence, shots map[string]model.DramaReferenceShot) error {
	seen := map[string]bool{}
	for _, frame := range evidence {
		shot, ok := shots[frame.ShotID]
		if !ok || strings.TrimSpace(frame.ID) == "" || len(frame.ID) > 128 || seen[frame.ID] || frame.AssetID == 0 || frame.AtMs < shot.StartMs || frame.AtMs >= shot.EndMs {
			return errcode.ErrInvalidParams.WithMsg("帧图证据重复、镜号无效或时间不在当前镜头内")
		}
		seen[frame.ID] = true
	}
	return nil
}

// validateDraftObservations 字段来源只是用户声明；所有证据仍须引用当前草稿中的同镜图片。
func validateDraftObservations(observations []model.DramaReferenceObservation, evidence []model.DramaReferenceEvidence, shots map[string]model.DramaReferenceShot) error {
	frames := map[string]string{}
	for _, frame := range evidence {
		frames[frame.ID] = frame.ShotID
	}
	seen := map[[2]string]bool{}
	for _, item := range observations {
		key := [2]string{item.ShotID, item.Field}
		if _, ok := shots[item.ShotID]; !ok || seen[key] {
			return errcode.ErrInvalidParams.WithMsg("字段观察重复或引用未知镜头")
		}
		if err := validateDraftObservation(item, frames); err != nil {
			return err
		}
		seen[key] = true
	}
	return nil
}

// validateDraftObservation 约束观察枚举和证据，明确 observed 至少需要一张同镜图片。
func validateDraftObservation(item model.DramaReferenceObservation, frames map[string]string) error {
	if !draftEnum(item.Field, "action", "camera", "characters", "prompt") ||
		!draftEnum(item.Source, "observation", "imported", "manual") ||
		!draftEnum(item.ReviewState, "observed", "inferred", "unknown", "confirmed") {
		return errcode.ErrInvalidParams.WithMsg("字段观察分类无效")
	}
	if item.ReviewState == "observed" && len(item.EvidenceIDs) == 0 {
		return errcode.ErrInvalidParams.WithMsg("已观察字段必须提供帧图证据")
	}
	seen := map[string]bool{}
	for _, id := range item.EvidenceIDs {
		if frames[id] != item.ShotID || seen[id] {
			return errcode.ErrInvalidParams.WithMsg("字段证据不属于当前镜头或存在重复")
		}
		seen[id] = true
	}
	return nil
}

// validateDraftAssets 在事务外检查本人图片，基础设施错误保留为内部失败而不伪装输入错误。
func (s *DramaReferenceDraftService) validateDraftAssets(ctx context.Context, u uint64, evidence []model.DramaReferenceEvidence) error {
	seen := map[uint64]bool{}
	for _, frame := range evidence {
		if seen[frame.AssetID] {
			continue
		}
		if s.assets == nil {
			return errcode.ErrInvalidParams.WithMsg("帧图素材服务不可用")
		}
		asset, err := s.assets.View(ctx, u, frame.AssetID)
		if err != nil {
			return draftAssetError(err)
		}
		if asset == nil || asset.Kind != "image" {
			return errcode.ErrInvalidParams.WithMsg("帧图证据必须是本人的图片素材")
		}
		seen[frame.AssetID] = true
	}
	return nil
}

// draftAssetError 将不存在与越权统一转换为参数错误，避免泄露素材所有者。
func draftAssetError(err error) error {
	var business *errcode.Error
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, provider.ErrAssetNotFound) ||
		(errors.As(err, &business) && business.Code == errcode.ErrAssetNotFound.Code) {
		return errcode.ErrInvalidParams.WithMsg("请选择自己的帧图素材")
	}
	return err
}

// draftEnum 用有限枚举限制客户端可声明的分类。
func draftEnum(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
