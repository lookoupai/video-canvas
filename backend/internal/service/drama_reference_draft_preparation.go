package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

// DramaReferenceDraftPreparations 校验服务生成的证据，公开 JSON 不能自行声明系统来源。
type DramaReferenceDraftPreparations interface {
	// DraftSnapshot 返回本人准备的冻结来源与完整文档。
	DraftSnapshot(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferenceEpisode, error)
	// ValidateEvidence 核验已完成帧与素材、镜号、时间一一匹配。
	ValidateEvidence(context.Context, uint64, uint64, uint64, uint64, model.DramaReferenceDraftContent) error
}

// draftReference 绑定准备时采用其不可变基准，未绑定时沿用当前参考文档。
func (s *DramaReferenceDraftService) draftReference(ctx context.Context, u, p, ref uint64, current *model.DramaReferenceEpisode, req *model.CreateDramaReferenceDraftReq) (*model.DramaReferenceEpisode, uint64, error) {
	if req.PreparationID == "" {
		return current, 0, nil
	}
	id, ok := idcodec.Decode(req.PreparationID)
	if !ok || s.preparations == nil {
		return nil, 0, errcode.ErrInvalidParams.WithMsg("媒体准备无效")
	}
	snapshot, err := s.preparations.DraftSnapshot(ctx, u, p, ref, id)
	return snapshot, id, err
}

// validatePreparedEvidence 不允许未绑定准备的草稿借用系统帧 ID。
func (s *DramaReferenceDraftService) validatePreparedEvidence(ctx context.Context, u, p, ref, id uint64, content model.DramaReferenceDraftContent) error {
	for _, item := range content.Evidence {
		if item.PreparationFrameID != "" && (id == 0 || s.preparations == nil) {
			return errcode.ErrInvalidParams.WithMsg("系统证据需要绑定媒体准备")
		}
	}
	if id == 0 {
		return nil
	}
	if s.preparations == nil {
		return errcode.ErrInvalidParams.WithMsg("媒体准备不可用")
	}
	return s.preparations.ValidateEvidence(ctx, u, p, ref, id, content)
}

// DraftSnapshot 读取冻结准备文档用于初始化草稿，准备失败但已分析出的镜头仍可保留。
func (s *DramaReferencePreparationService) DraftSnapshot(ctx context.Context, u, p, ref, id uint64) (*model.DramaReferenceEpisode, error) {
	// 1. 核验本人存活项目和准备归属。
	row, err := s.ownedPreparation(ctx, u, p, ref, id)
	if err != nil {
		return nil, err
	}
	// 2. 不依赖当前正式修订，草稿采用时另行进行版本冲突检查。
	return &model.DramaReferenceEpisode{ID: ref, UserID: u, DramaID: p, AssetID: row.SourceAssetID, DurationMs: row.DurationMs, Revision: row.BaseRevision, Document: row.Document}, nil
}

// ValidateEvidence 逐项验证系统帧真实存在且图片仍有效，外部普通图片保持明确的非系统来源。
func (s *DramaReferencePreparationService) ValidateEvidence(ctx context.Context, u, p, ref, id uint64, content model.DramaReferenceDraftContent) error {
	// 1. 即使证据列表为空也必须核验准备归属。
	if _, err := s.ownedPreparation(ctx, u, p, ref, id); err != nil {
		return err
	}
	shots := map[string]model.DramaReferenceShot{}
	for _, shot := range content.Document.Shots {
		shots[shot.ID] = shot
	}
	// 2. 系统来源只能由当前准备中已持久化的帧证明，不能只提交相似时间戳。
	for _, item := range content.Evidence {
		if item.PreparationFrameID == "" {
			continue
		}
		frameID, ok := idcodec.Decode(item.PreparationFrameID)
		if !ok {
			return errcode.ErrInvalidParams
		}
		frame, err := s.repo.Frame(ctx, u, id, frameID)
		if err != nil {
			return draftAssetError(err)
		}
		shot := shots[item.ShotID]
		if shot.StartMs != frame.StartMs || shot.EndMs != frame.EndMs {
			return errcode.ErrInvalidParams.WithMsg("系统帧的原镜时间窗与草稿不匹配")
		}
		if frame.AssetID == 0 || frame.AssetID != item.AssetID || frame.ShotID != item.ShotID || frame.AtMs != item.AtMs {
			return errcode.ErrInvalidParams.WithMsg("系统帧与证据素材、镜号或时间不匹配")
		}
		valid, err := s.validPreparedAsset(ctx, u, frame.AssetID)
		if err != nil {
			return err
		}
		if !valid {
			return errcode.ErrInvalidParams.WithMsg("系统帧已失效，请补帧后重试")
		}
	}
	return nil
}
