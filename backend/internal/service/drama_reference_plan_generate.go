package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

type referenceGenerationScope struct {
	applyScope
	application uint64
	fingerprint string
}

// Generate validates the applied quote and submits one idempotent task for each approved shot.
func (s *DramaReferencePlanService) Generate(ctx context.Context, user, project, reference, draft uint64, req *model.DramaReferenceGenerateReq) (*model.DramaReferenceGenerateResponse, error) {
	if s.deps.Adoptions == nil || s.deps.Segments == nil {
		return nil, errcode.ErrInternal.WithMsg("参考生成服务未配置")
	}
	prepared, scope, err := prepareReferenceBatch(applyScope{user, project, reference, draft}, req)
	if err != nil {
		return nil, err
	}
	row, state, replayed, err := s.openReferenceBatch(ctx, scope, prepared)
	if err != nil {
		return nil, err
	}
	if state.Status != "submitted" {
		if err := s.submitReferenceBatch(ctx, scope, row, state); err != nil {
			return nil, err
		}
	}
	state.Replayed = replayed
	return state, nil
}

// CheckGenerate validates an existing application before creating an Agent approval.
func (s *DramaReferencePlanService) CheckGenerate(ctx context.Context, user, project, reference, draft uint64, req *model.DramaReferenceGenerateReq) (*model.DramaReferencePlan, error) {
	if s.deps.Adoptions == nil {
		return nil, errcode.ErrInternal.WithMsg("参考生成服务未配置")
	}
	prepared, scope, err := prepareReferenceBatch(applyScope{user, project, reference, draft}, req)
	if err != nil {
		return nil, err
	}
	_, plan, err := s.approvedReferenceBatch(ctx, scope, prepared)
	return plan, err
}

// prepareReferenceBatch binds the application, exact quote, and project to the user-supplied batch key.
func prepareReferenceBatch(scope applyScope, req *model.DramaReferenceGenerateReq) (*model.DramaReferenceGenerateReq, referenceGenerationScope, error) {
	if req == nil {
		return nil, referenceGenerationScope{}, errcode.ErrInvalidParams
	}
	out := *req
	out.RequestKey = strings.TrimSpace(out.RequestKey)
	out.Quote = normalizePlanQuote(out.Quote)
	id, ok := idcodec.Decode(out.ApplicationID)
	if !ok || out.Revision < 1 || out.Revision != out.Quote.Revision || len(out.PlanDigest) != sha256.Size*2 ||
		out.RequestKey == "" || len(out.RequestKey) > 128 {
		return nil, referenceGenerationScope{}, errcode.ErrInvalidParams.WithMsg("生成参数与已采用报价不一致")
	}
	if err := validateQuoteRequest(&out.Quote); err != nil {
		return nil, referenceGenerationScope{}, err
	}
	raw, err := json.Marshal(struct {
		Project, Reference, Draft uint64
		Request                   model.DramaReferenceGenerateReq
	}{scope.project, scope.reference, scope.draft, out})
	if err != nil {
		return nil, referenceGenerationScope{}, errcode.ErrInvalidParams
	}
	sum := sha256.Sum256(raw)
	return &out, referenceGenerationScope{scope, id, hex.EncodeToString(sum[:])}, nil
}

// openReferenceBatch restores the prior batch or freezes all item intents before the first paid call.
func (s *DramaReferencePlanService) openReferenceBatch(ctx context.Context, scope referenceGenerationScope, req *model.DramaReferenceGenerateReq) (*model.DramaReferenceDraftGeneration, *model.DramaReferenceGenerateResponse, bool, error) {
	repo := s.deps.Adoptions.repo
	existing, err := repo.DraftGenerationByKey(ctx, scope.user, req.RequestKey)
	if err == nil {
		state, err := readReferenceBatch(existing, scope)
		return existing, state, true, err
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, nil, false, err
	}
	adopted, plan, err := s.approvedReferenceBatch(ctx, scope, req)
	if err != nil {
		// A simultaneous first submission may have materialized a valid receipt during preflight.
		if existing, lookupErr := repo.DraftGenerationByKey(ctx, scope.user, req.RequestKey); lookupErr == nil {
			state, readErr := readReferenceBatch(existing, scope)
			return existing, state, true, readErr
		}
		return nil, nil, false, err
	}
	state := &model.DramaReferenceGenerateResponse{ApplicationID: req.ApplicationID, PlanDigest: req.PlanDigest, Status: "pending", Items: []model.DramaReferenceGenerateItem{}}
	row := &model.DramaReferenceDraftGeneration{UserID: scope.user, DramaID: scope.project, ReferenceID: scope.reference, DraftID: scope.draft,
		ApplicationID: scope.application, RequestKey: req.RequestKey, Fingerprint: scope.fingerprint, PlanDigest: req.PlanDigest, Quote: adopted.Quote, Response: []byte("{}"), Status: state.Status}
	replayed := false
	err = repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		if err := tx.LockCreateKey(ctx, scope.user, "reference-draft-generate/"+req.RequestKey); err != nil {
			return err
		}
		if existing, err := tx.DraftGenerationByKey(ctx, scope.user, req.RequestKey); err == nil {
			row, replayed = existing, true
			state, err = readReferenceBatch(row, scope)
			return err
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		return saveNewReferenceBatch(ctx, tx, row, state, adopted, plan)
	})
	return row, state, replayed, dramaError(err)
}

// approvedReferenceBatch confirms the original approval, current quote, and current formal reference.
func (s *DramaReferencePlanService) approvedReferenceBatch(ctx context.Context, scope referenceGenerationScope, req *model.DramaReferenceGenerateReq) (*model.DramaReferenceDraftApplication, *model.DramaReferencePlan, error) {
	adopted, err := s.deps.Adoptions.repo.DraftApplicationByID(ctx, scope.user, scope.project, scope.application)
	if err != nil {
		return nil, nil, dramaError(err)
	}
	if adopted.ReferenceID != scope.reference || adopted.DraftID != scope.draft {
		return nil, nil, errcode.ErrDramaNotFound
	}
	if adopted.DraftRevision != req.Revision || adopted.PlanDigest != req.PlanDigest {
		return nil, nil, errcode.ErrCanvasConflict.WithMsg("生成请求与已采用的报价不一致")
	}
	var approved model.QuoteDramaReferenceDraftReq
	decoder := json.NewDecoder(bytes.NewReader(adopted.Quote))
	decoder.UseNumber()
	if err := decoder.Decode(&approved); err != nil {
		return nil, nil, err
	}
	want, err := json.Marshal(req.Quote)
	if err != nil {
		return nil, nil, errcode.ErrInvalidParams
	}
	got, err := json.Marshal(approved)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(want, got) {
		return nil, nil, errcode.ErrCanvasConflict.WithMsg("生成输入与已采用的报价不一致")
	}
	plan, err := s.CheckApproved(ctx, scope.user, scope.project, scope.reference, scope.draft, &req.Quote, req.PlanDigest)
	return adopted, plan, err
}

// readReferenceBatch decodes the persisted item inputs while preserving numeric identifiers.
func readReferenceBatch(row *model.DramaReferenceDraftGeneration, scope referenceGenerationScope) (*model.DramaReferenceGenerateResponse, error) {
	if row.Fingerprint != scope.fingerprint {
		return nil, errcode.ErrInvalidParams.WithMsg("request_key 已用于不同的生成内容")
	}
	var state model.DramaReferenceGenerateResponse
	decoder := json.NewDecoder(bytes.NewReader(row.Response))
	decoder.UseNumber()
	return &state, decoder.Decode(&state)
}

// saveNewReferenceBatch checks every segment before persisting any task requests.
func saveNewReferenceBatch(ctx context.Context, tx repository.DramaReferenceTx, row *model.DramaReferenceDraftGeneration, state *model.DramaReferenceGenerateResponse, adopted *model.DramaReferenceDraftApplication, plan *model.DramaReferencePlan) error {
	var receipt model.DramaReferenceApplyResponse
	if err := json.Unmarshal(adopted.Response, &receipt); err != nil {
		return err
	}
	ref, err := lockedReference(ctx, tx, row.UserID, row.DramaID, row.ReferenceID, receipt.ReferenceRevision)
	if err != nil {
		return err
	}
	if len(receipt.Segments) != len(plan.Units) {
		return errcode.ErrCanvasConflict
	}
	for i, unit := range plan.Units {
		if !referencePlanMediaMatches(unit, plan.SourceAssetID) {
			return errcode.ErrCanvasConflict.WithMsg("报价中的参考素材与计划不一致，请重新报价")
		}
		item, err := referenceBatchItem(ctx, tx.DramaTx(), row, adopted, receipt.Segments[i], unit, plan, ref.Revision)
		if err != nil {
			return err
		}
		state.Items = append(state.Items, item)
	}
	state.ConfigurationDigest = plan.ConfigurationDigest
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	row.Response = raw
	return tx.SaveDraftGeneration(ctx, row)
}

// referencePlanMediaMatches confirms every image and source video listed in the quote.
func referencePlanMediaMatches(unit model.DramaReferencePlanUnit, sourceAssetID string) bool {
	if unit.Input == nil {
		return false
	}
	images := make([]string, 0, len(unit.ImageAssetIDs))
	videoCount := 0
	for _, material := range unit.Materials {
		switch material.Kind {
		case "image":
			images = append(images, material.ID)
		case "video":
			if material.ID != sourceAssetID {
				return false
			}
			videoCount++
		}
	}
	actualImages := make([]string, 0, len(unit.ImageAssetIDs))
	for _, ref := range modelcfg.MediaRefs(unit.Input) {
		if ref.Key == "images" {
			actualImages = append(actualImages, strconv.FormatUint(ref.ID, 10))
		}
	}
	return slices.Equal(images, unit.ImageAssetIDs) && slices.Equal(actualImages, unit.ImageAssetIDs) &&
		(videoCount == 1) == (unit.Mode == "video")
}

// submitReferenceBatch retries failed items using the exact task keys and inputs frozen at first submission.
func (s *DramaReferencePlanService) submitReferenceBatch(ctx context.Context, scope referenceGenerationScope, row *model.DramaReferenceDraftGeneration, state *model.DramaReferenceGenerateResponse) error {
	for index := range state.Items {
		if state.Items[index].TaskID != "" {
			continue
		}
		item := state.Items[index]
		segmentID, ok := idcodec.Decode(item.SegmentID)
		if !ok {
			return errcode.ErrCanvasConflict
		}
		approved := referenceTaskQuote{configuration: state.ConfigurationDigest, credits: item.MaxCredits, validateAgnesVideo: item.ValidateAgnesVideo}
		task, err := s.deps.Segments.Generate(context.WithValue(ctx, referenceTaskQuoteContext{}, approved), scope.user, segmentID, item.TaskKey, &item.Request)
		if err != nil {
			item.Status = "failed"
			item.Error = referenceGenerationError(err)
		} else if task == nil {
			item.Status, item.Error = "failed", "生成服务未返回任务"
		} else {
			item.TaskID, item.Status, item.Error = task.TaskRef, task.Status, ""
		}
		if err := s.persistReferenceBatchItem(ctx, row, index, item); err != nil {
			return err
		}
		state.Items[index] = item
	}
	state.Status = referenceBatchStatus(state.Items)
	return nil
}

// persistReferenceBatchItem stores each task result before moving to the next paid submission.
func (s *DramaReferencePlanService) persistReferenceBatchItem(ctx context.Context, row *model.DramaReferenceDraftGeneration, index int, item model.DramaReferenceGenerateItem) error {
	return s.deps.Adoptions.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		current, err := tx.DraftGeneration(ctx, row.UserID, row.DramaID, row.ID, true)
		if err != nil {
			return err
		}
		state, err := readReferenceBatch(current, referenceGenerationScope{applyScope: applyScope{user: row.UserID, project: row.DramaID, reference: row.ReferenceID, draft: row.DraftID}, application: row.ApplicationID, fingerprint: row.Fingerprint})
		if err != nil {
			return err
		}
		if index >= len(state.Items) || state.Items[index].TaskKey != item.TaskKey {
			return errcode.ErrCanvasConflict
		}
		if state.Items[index].TaskID == "" {
			state.Items[index] = item
		}
		state.Status = referenceBatchStatus(state.Items)
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		current.Response, current.Status = raw, state.Status
		return tx.SaveDraftGeneration(ctx, current)
	})
}

// referenceBatchStatus distinguishes completed task submissions from per-shot failures.
func referenceBatchStatus(items []model.DramaReferenceGenerateItem) string {
	for _, item := range items {
		if item.TaskID == "" {
			return "partial"
		}
	}
	return "submitted"
}

// referenceBatchItem links a quoted original shot to one current remake segment.
func referenceBatchItem(ctx context.Context, tx repository.DramaTx, row *model.DramaReferenceDraftGeneration, adopted *model.DramaReferenceDraftApplication, segment model.DramaReferenceAppliedSegment, unit model.DramaReferencePlanUnit, plan *model.DramaReferencePlan, referenceRevision int64) (model.DramaReferenceGenerateItem, error) {
	id, ok := idcodec.Decode(segment.SegmentID)
	if !ok || segment.ShotID != unit.ShotID || unit.Credits == nil {
		return model.DramaReferenceGenerateItem{}, errcode.ErrCanvasConflict
	}
	current, err := tx.Segment(ctx, row.UserID, id, false)
	if err != nil {
		return model.DramaReferenceGenerateItem{}, err
	}
	remake, err := segmentRemakeOf(current)
	if err != nil {
		return model.DramaReferenceGenerateItem{}, err
	}
	if current.EpisodeID != adopted.EpisodeID || current.Revision != segment.Revision || remake == nil ||
		remake.AdoptionID != idcodec.Encode(adopted.AdoptionID) || remake.ReferenceID != idcodec.Encode(row.ReferenceID) ||
		remake.ReferenceRevision != referenceRevision || fmt.Sprint(remake.AssetID) != plan.SourceAssetID ||
		len(remake.Sources) != 1 || remake.Sources[0].ShotID != unit.ShotID ||
		remake.Sources[0].StartMs != unit.SourceStartMs || remake.Sources[0].EndMs != unit.SourceEndMs ||
		remake.TrimStartMs != unit.TrimStartMs || remake.TargetDurationMs != unit.TargetDurationMs ||
		current.Prompt != unit.Prompt || current.Seconds != unit.Duration {
		return model.DramaReferenceGenerateItem{}, errcode.ErrCanvasConflict.WithMsg("片场已变更，请重新采用当前镜头计划")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%d/%s", row.UserID, row.ApplicationID, id, row.RequestKey)))
	return model.DramaReferenceGenerateItem{ShotID: unit.ShotID, SegmentID: segment.SegmentID, Revision: segment.Revision,
		Status: "pending", TaskKey: "draft-generate-" + hex.EncodeToString(sum[:]), MaxCredits: *unit.Credits,
		ValidateAgnesVideo: unit.Mode == "video" && hasReferencePlanIssue(unit.Issues, "protocol_video_window_review"),
		Request:            *unit.GenerateRequest(plan.ModelID, segment.Revision)}, nil
}

// hasReferencePlanIssue identifies a quote-time protocol check that requires generated media inspection.
func hasReferencePlanIssue(issues []model.DramaReferencePlanIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

// referenceGenerationError hides infrastructure details while retaining actionable business errors.
func referenceGenerationError(err error) string {
	var business *errcode.Error
	if errors.As(err, &business) {
		return business.Msg
	}
	return "生成提交失败，请稍后重试"
}

type referenceTaskQuoteContext struct{}

type referenceTaskQuote struct {
	configuration      string
	credits            int
	validateAgnesVideo bool
}

// validateReferenceTaskQuote checks the exact snapshot that will be frozen before any credit reservation.
func (s *GenerationTaskService) validateReferenceTaskQuote(ctx context.Context, userID uint64, snap *provider.Snapshot, credits int, input map[string]any) error {
	approved, ok := ctx.Value(referenceTaskQuoteContext{}).(referenceTaskQuote)
	if !ok {
		return nil
	}
	digest, err := referencePlanConfigurationDigest(snap)
	if err != nil {
		return err
	}
	if digest != approved.configuration || credits > approved.credits {
		return errcode.ErrCanvasConflict.WithMsg("模型配置或积分报价已变化，请重新确认")
	}
	if approved.validateAgnesVideo {
		return s.validateAgnesReferenceClip(ctx, userID, input)
	}
	return nil
}

// validateAgnesReferenceClip checks the actual silent clip before task creation freezes credits.
func (s *GenerationTaskService) validateAgnesReferenceClip(ctx context.Context, userID uint64, input map[string]any) error {
	if s.videoProbe == nil {
		return errcode.ErrInternal.WithMsg("参考视频协议检查未配置")
	}
	var videoID uint64
	for _, ref := range modelcfg.MediaRefs(input) {
		if ref.Key == "videos" {
			if videoID != 0 {
				return errcode.ErrCanvasConflict
			}
			videoID = ref.ID
		}
	}
	if videoID == 0 {
		return errcode.ErrCanvasConflict.WithMsg("原片裁片未进入任务参考素材")
	}
	info, err := s.videoProbe.Probe(ctx, userID, videoID, dramaReferenceClipMaxBytes)
	if err != nil {
		return err
	}
	if info.ByteSize > dramaReferenceClipMaxBytes || info.DurationMs < 2000 || info.DurationMs > 12000 || info.FrameRate < 24 || info.FrameRate > 60 {
		return errcode.ErrCanvasConflict.WithMsg("原片裁片不符合 Agnes 视频参考限制，请选择其他路线并重新报价")
	}
	return nil
}
