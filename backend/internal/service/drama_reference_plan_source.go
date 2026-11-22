package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
)

// loadReferencePlanSource retrieves the immutable shot document and owned source video.
func (s *DramaReferencePlanService) loadReferencePlanSource(ctx context.Context, u, p, ref, draftID uint64, req *model.QuoteDramaReferenceDraftReq) (*referencePlanSource, error) {
	if err := validateQuoteRequest(req); err != nil {
		return nil, err
	}
	reference, err := s.deps.Repo.Reference(ctx, u, p, ref, false)
	if err != nil {
		return nil, errcode.ErrDramaNotFound
	}
	draft, err := s.deps.Repo.Draft(ctx, u, p, ref, draftID, false)
	if err != nil {
		return nil, errcode.ErrDramaNotFound
	}
	content, err := validateQuoteDraft(draft, reference, req.Revision)
	if err != nil {
		return nil, err
	}
	document, asset, err := s.loadQuoteSource(ctx, u, p, ref, reference, draft, content)
	if err != nil {
		return nil, err
	}
	if asset == nil || asset.Kind != "video" {
		return nil, errcode.ErrInvalidParams.WithMsg("原片素材不可用")
	}
	if err := verifyReferencePlanAsset(ctx, s.deps.Assets, u, asset.ID); err != nil {
		if errors.Is(err, provider.ErrAssetNotFound) {
			return nil, errcode.ErrInvalidParams.WithMsg("原片文件不可读取")
		}
		return nil, err
	}
	return &referencePlanSource{draft: draft, asset: asset, document: document, shots: referenceShots(document)}, nil
}

// validateQuoteRequest rejects incomplete plans before reading project or draft records.
func validateQuoteRequest(req *model.QuoteDramaReferenceDraftReq) error {
	if req == nil || req.Revision < 1 || req.ModelID == "" || len(req.Units) == 0 || len(req.Units) > 200 {
		return errcode.ErrInvalidParams.WithMsg("报价计划参数无效")
	}
	return nil
}

// validateQuoteDraft checks revisions and decodes the latest user draft content.
func validateQuoteDraft(draft *model.DramaReferenceDraft, reference *model.DramaReferenceEpisode, revision int64) (model.DramaReferenceDraftContent, error) {
	if draft.Revision != revision || draft.Status == "discarded" ||
		draft.BaseRevision != reference.Revision && draft.PreparationID == 0 && draft.Status != "applied" {
		return model.DramaReferenceDraftContent{}, errcode.ErrCanvasConflict
	}
	var content model.DramaReferenceDraftContent
	if err := json.Unmarshal(draft.Content, &content); err != nil {
		return model.DramaReferenceDraftContent{}, errcode.ErrInvalidParams.WithMsg("草稿内容不可读取")
	}
	return content, nil
}

// loadQuoteSource returns the source document and asset attached to the draft.
func (s *DramaReferencePlanService) loadQuoteSource(ctx context.Context, u, p, ref uint64, reference *model.DramaReferenceEpisode, draft *model.DramaReferenceDraft, content model.DramaReferenceDraftContent) (model.DramaReferenceDocument, *model.Asset, error) {
	if draft.PreparationID != 0 {
		return s.loadPreparedQuoteSource(ctx, u, p, ref, draft, content)
	}
	asset, err := s.deps.Assets.Get(ctx, u, reference.AssetID)
	if errors.Is(err, provider.ErrAssetNotFound) {
		return model.DramaReferenceDocument{}, nil, errcode.ErrInvalidParams.WithMsg("原片素材不可用")
	}
	if err != nil {
		return model.DramaReferenceDocument{}, nil, err
	}
	var source model.DramaReferenceDocument
	if err := json.Unmarshal(reference.Document, &source); err != nil {
		return model.DramaReferenceDocument{}, nil, errcode.ErrInvalidParams.WithMsg("原片参考修订不可读取")
	}
	if err := validateDraftShotWindows(content.Document, source); err != nil {
		return model.DramaReferenceDocument{}, nil, err
	}
	return content.Document, asset, nil
}

// loadPreparedQuoteSource validates the preparation snapshot and fixed source asset.
func (s *DramaReferencePlanService) loadPreparedQuoteSource(ctx context.Context, u, p, ref uint64, draft *model.DramaReferenceDraft, content model.DramaReferenceDraftContent) (model.DramaReferenceDocument, *model.Asset, error) {
	if s.deps.Preparations == nil {
		return model.DramaReferenceDocument{}, nil, errcode.ErrInvalidParams.WithMsg("草稿的媒体准备已不可用")
	}
	frozen, err := s.deps.Preparations.DraftSnapshot(ctx, u, p, ref, draft.PreparationID)
	if err != nil {
		return model.DramaReferenceDocument{}, nil, err
	}
	source, asset := decodePreparationDocument(model.DramaReferenceDocument{}, frozen)
	if asset == nil {
		return model.DramaReferenceDocument{}, nil, errcode.ErrInvalidParams.WithMsg("草稿的原片快照不可用")
	}
	if err := validateDraftShotWindows(content.Document, source); err != nil {
		return model.DramaReferenceDocument{}, nil, err
	}
	return content.Document, asset, nil
}

// referenceShots indexes shot windows used by route compilation.
func referenceShots(document model.DramaReferenceDocument) map[string]model.DramaReferenceShot {
	shots := make(map[string]model.DramaReferenceShot, len(document.Shots))
	for _, shot := range document.Shots {
		shots[shot.ID] = shot
	}
	return shots
}

// decodePreparationDocument loads the frozen document and source metadata saved by preparation.
func decodePreparationDocument(_ model.DramaReferenceDocument, frozen *model.DramaReferenceEpisode) (model.DramaReferenceDocument, *model.Asset) {
	var document model.DramaReferenceDocument
	if frozen != nil {
		_ = json.Unmarshal(frozen.Document, &document)
	}
	if frozen == nil {
		return document, nil
	}
	return document, &model.Asset{ID: frozen.AssetID, UserID: frozen.UserID, Kind: "video", DurationMs: frozen.DurationMs}
}

// validateDraftShotWindows prevents edits from changing their original source crop.
func validateDraftShotWindows(draft, source model.DramaReferenceDocument) error {
	sourceShots := make(map[string]model.DramaReferenceShot, len(source.Shots))
	for _, shot := range source.Shots {
		sourceShots[shot.ID] = shot
	}
	for _, shot := range draft.Shots {
		original, exists := sourceShots[shot.ID]
		if !exists || shot.StartMs != original.StartMs || shot.EndMs != original.EndMs {
			return errcode.ErrInvalidParams.WithMsg("草稿镜头区间必须与原片引用证据一致")
		}
	}
	return nil
}
