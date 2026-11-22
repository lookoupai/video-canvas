package service

import (
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
	"video-canvas/internal/repository"
)

type applyActor struct {
	source    string
	sessionID uint64
	runID     uint64
}

// ApplyForAgent 按可信运行来源原子采用草稿并记录审批后的操作回执。
func (s *DramaReferencePlanService) ApplyForAgent(ctx context.Context, u, p, ref, draftID, sessionID, runID uint64, req *model.ApplyDramaReferenceDraftReq) (*model.DramaReferenceApplyResponse, error) {
	if sessionID == 0 || runID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少 Agent 会话或运行标识")
	}
	return s.apply(ctx, u, p, ref, draftID, req, applyActor{source: "agent", sessionID: sessionID, runID: runID})
}

// Apply 将报价中选定的原镜应用到跟拍分集，并在同一事务内写 adoption、segments、草稿状态和回执。
func (s *DramaReferencePlanService) Apply(ctx context.Context, u, p, ref, draftID uint64, req *model.ApplyDramaReferenceDraftReq) (*model.DramaReferenceApplyResponse, error) {
	return s.apply(ctx, u, p, ref, draftID, req, applyActor{source: "manual"})
}

// apply 校验报价摘要后按申请键幂等提交采用事务。
func (s *DramaReferencePlanService) apply(ctx context.Context, u, p, ref, draftID uint64, req *model.ApplyDramaReferenceDraftReq, actor applyActor) (*model.DramaReferenceApplyResponse, error) {
	normalized, err := normalizeReferenceDraftApply(req)
	if err != nil {
		return nil, err
	}
	if s.deps.Adoptions == nil {
		return nil, errcode.ErrInternal.WithMsg("参考采用服务未配置")
	}
	fingerprint, err := applyFingerprint(p, ref, draftID, normalized)
	if err != nil {
		return nil, err
	}
	if prior, err := s.deps.Adoptions.repo.DraftApplicationByKey(ctx, u, normalized.RequestKey); err == nil {
		var replay *model.DramaReferenceApplyResponse
		if err := replayReferenceDraftApplication(prior, fingerprint, &replay); err != nil {
			return nil, err
		}
		return replay, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	var result *model.DramaReferenceApplyResponse
	err = s.deps.Adoptions.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		if err := tx.LockCreateKey(ctx, u, "reference-draft-apply/"+normalized.RequestKey); err != nil {
			return err
		}
		if prior, err := tx.DraftApplicationByKey(ctx, u, normalized.RequestKey); err == nil {
			return replayReferenceDraftApplication(prior, fingerprint, &result)
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		plan, err := s.CheckApproved(ctx, u, p, ref, draftID, &normalized.Quote, normalized.PlanDigest)
		if err != nil {
			return err
		}
		return s.applyReferencePlan(ctx, tx, applyScope{u, p, ref, draftID}, normalized, actor, plan, fingerprint, &result)
	})
	return result, dramaError(err)
}

type applyScope struct{ user, project, reference, draft uint64 }

// applyReferencePlan commits all changes after the user key has been locked and the quote recompiled.
func (s *DramaReferencePlanService) applyReferencePlan(ctx context.Context, tx repository.DramaReferenceTx, scope applyScope, req *model.ApplyDramaReferenceDraftReq, actor applyActor, plan *model.DramaReferencePlan, fingerprint string, result **model.DramaReferenceApplyResponse) error {
	u, p, ref, draftID := scope.user, scope.project, scope.reference, scope.draft
	project, err := tx.Project(ctx, u, p, true)
	if err != nil {
		return err
	}
	draft, err := tx.DraftForApply(ctx, u, p, ref, draftID, true)
	if err != nil {
		return err
	}
	if draft.Revision != req.Revision || draft.Status != "editing" && draft.Status != "ready" {
		return errcode.ErrCanvasConflict
	}
	if _, err := tx.DraftApplicationForDraft(ctx, u, draftID, draft.Revision); err == nil {
		return errcode.ErrCanvasConflict.WithMsg("该草稿修订已采用")
	} else if !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	referenceRevision, err := saveApprovedReference(ctx, tx, project, draft, plan)
	if err != nil {
		return err
	}
	adoptionReq, targets := adoptionInputForPlan(referenceRevision, plan, req)
	adoptionReq.RequestKey = adoptionRequestKey(u, req.RequestKey)
	adoptionHash := referenceAdoptionFingerprint(p, ref, adoptionReq)
	adoption, err := priorReferenceAdoption(ctx, tx, project, ref, adoptionReq, adoptionHash)
	if err != nil {
		return err
	}
	if adoption == nil {
		adoption, err = s.deps.Adoptions.createReferenceAdoptionWithPlan(ctx, tx, project, ref, adoptionReq, adoptionHash, targets)
		if err != nil {
			return err
		}
	} else if err := validateReusedReferenceAdoption(ctx, tx, adoption, plan.Units, adoptionReq.Instruction); err != nil {
		return err
	}
	view, err := referenceAdoptionView(ctx, tx.DramaTx(), adoption)
	if err != nil {
		return err
	}
	response, err := appliedSegmentsResponse(ctx, tx.DramaTx(), adoption, view, plan.Units)
	if err != nil {
		return err
	}
	response.DraftID = idcodec.Encode(draftID)
	response.ReferenceRevision = referenceRevision
	response.PlanDigest = req.PlanDigest
	draft.Status, draft.Source, draft.SessionID, draft.RunID = "applied", actor.source, actor.sessionID, actor.runID
	if err := tx.SavePlanDraft(ctx, draft); err != nil {
		return err
	}
	response.DraftRevision = draft.Revision
	if err := saveApprovedReferenceReceipt(ctx, tx, scope, req, fingerprint, adoption, response); err != nil {
		return err
	}
	*result = response
	return nil
}

// saveApprovedReference records the edited document alongside the adoption if the formal reference differs.
func saveApprovedReference(ctx context.Context, tx repository.DramaReferenceTx, project *model.DramaProject, draft *model.DramaReferenceDraft, plan *model.DramaReferencePlan) (int64, error) {
	row, err := lockedReference(ctx, tx, project.UserID, project.ID, draft.ReferenceID, draft.BaseRevision)
	if err != nil {
		return 0, err
	}
	if strconv.FormatUint(row.AssetID, 10) != plan.SourceAssetID {
		return 0, errcode.ErrCanvasConflict
	}
	doc, err := referenceDocumentOf(row)
	if err != nil {
		return 0, err
	}
	content, err := draftContentOf(draft)
	if err != nil {
		return 0, err
	}
	current, _ := json.Marshal(doc)
	approved, _ := json.Marshal(content.Document)
	if string(current) == string(approved) {
		return row.Revision, nil
	}
	var bindings map[string]string
	if err := json.Unmarshal(row.Bindings, &bindings); err != nil {
		return 0, err
	}
	validBindings := make(map[string]string, len(bindings))
	for _, character := range content.Document.Characters {
		if role := bindings[character.ID]; role != "" {
			validBindings[character.ID] = role
		}
	}
	err = applyReferenceEdit(ctx, tx, row, &model.UpdateDramaReferenceReq{Document: &content.Document, Bindings: validBindings})
	return row.Revision, err
}

// saveApprovedReferenceReceipt associates the stable adoption and segment IDs with the original request.
func saveApprovedReferenceReceipt(ctx context.Context, tx repository.DramaReferenceTx, scope applyScope, req *model.ApplyDramaReferenceDraftReq, fingerprint string, adoption *model.DramaReferenceAdoption, response *model.DramaReferenceApplyResponse) error {
	quote, err := json.Marshal(req.Quote)
	if err != nil {
		return err
	}
	receipt := &model.DramaReferenceDraftApplication{UserID: scope.user, DramaID: scope.project, ReferenceID: scope.reference, DraftID: scope.draft,
		DraftRevision: response.DraftRevision, RequestKey: req.RequestKey, Fingerprint: fingerprint,
		PlanDigest: req.PlanDigest, Quote: quote, Response: []byte("{}"), AdoptionID: adoption.ID, EpisodeID: adoption.EpisodeID}
	if err := tx.SaveDraftApplication(ctx, receipt); err != nil {
		return err
	}
	response.ApplicationID = idcodec.Encode(receipt.ID)
	receipt.Response, err = json.Marshal(response)
	if err != nil {
		return err
	}
	return tx.SaveDraftApplication(ctx, receipt)
}

// normalizeReferenceDraftApply fixes the full request before fingerprinting or opening a transaction.
func normalizeReferenceDraftApply(req *model.ApplyDramaReferenceDraftReq) (*model.ApplyDramaReferenceDraftReq, error) {
	if req == nil {
		return nil, errcode.ErrInvalidParams
	}
	out := *req
	out.Quote = normalizePlanQuote(out.Quote)
	out.RequestKey = strings.TrimSpace(out.RequestKey)
	out.Instruction = strings.TrimSpace(out.Instruction)
	if out.Revision < 1 || out.RequestKey == "" || len(out.RequestKey) > 128 || len(out.PlanDigest) != 64 || out.Mode != "follow" || len([]rune(out.Instruction)) > 4000 ||
		out.TargetEpisodeNo < 0 || out.EpisodeRevision < 0 || out.TargetEpisodeNo == 0 && out.EpisodeRevision != 0 || out.TargetEpisodeNo > 0 && out.EpisodeRevision < 1 {
		return nil, errcode.ErrInvalidParams.WithMsg("草稿采用参数无效")
	}
	if out.Quote.Revision != out.Revision || len(out.Quote.Units) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("报价修订或选镜范围与草稿不一致")
	}
	if out.Quote.MaxCredits != nil && *out.Quote.MaxCredits < 0 {
		return nil, errcode.ErrInvalidParams
	}
	if out.Quote.ModelID == "" || len(out.PlanDigest) != 64 {
		return nil, errcode.ErrInvalidParams.WithMsg("报价模型或计划摘要无效")
	}
	return &out, nil
}

// applyFingerprint binds the complete approved quote and target episode while excluding the retry key itself.
func applyFingerprint(projectID, referenceID, draftID uint64, req *model.ApplyDramaReferenceDraftReq) (string, error) {
	normalized := *req
	normalized.RequestKey = ""
	raw, err := json.Marshal(struct {
		Project, Reference, Draft uint64
		Request                   model.ApplyDramaReferenceDraftReq
	}{projectID, referenceID, draftID, normalized})
	if err != nil {
		return "", errcode.ErrInvalidParams
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// adoptionRequestKey bounds the internal adoption key while deterministically deriving it from the user's operation key.
func adoptionRequestKey(userID uint64, requestKey string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", userID, requestKey)))
	return "draft-apply-" + hex.EncodeToString(sum[:])[:48]
}

// adoptionInputForPlan maps the frozen draft and exact quote selection to existing follow adoption semantics.
func adoptionInputForPlan(referenceRevision int64, plan *model.DramaReferencePlan, req *model.ApplyDramaReferenceDraftReq) (*model.AdoptDramaReferenceReq, map[string]model.DramaReferenceApplyUnit) {
	selected := make([]string, 0, len(plan.Units))
	targets := make(map[string]model.DramaReferenceApplyUnit, len(plan.Units))
	for _, unit := range plan.Units {
		selected = append(selected, unit.ShotID)
		targets[unit.ShotID] = model.DramaReferenceApplyUnit{ShotID: unit.ShotID, Prompt: unit.Prompt, Duration: unit.Duration,
			TrimStartMs: unit.TrimStartMs, TargetDurationMs: unit.TargetDurationMs}
	}
	slices.Sort(selected)
	return &model.AdoptDramaReferenceReq{Revision: referenceRevision, Mode: "follow", Instruction: req.Instruction,
		TargetEpisodeNo: req.TargetEpisodeNo, EpisodeRevision: req.EpisodeRevision, ShotIDs: selected}, targets
}

// replayReferenceDraftApplication returns the original receipt only when a retry preserves the full input.
func replayReferenceDraftApplication(prior *model.DramaReferenceDraftApplication, fingerprint string, result **model.DramaReferenceApplyResponse) error {
	if prior.Fingerprint != fingerprint {
		return errcode.ErrInvalidParams.WithMsg("request_key 已用于不同的草稿采用内容")
	}
	var response model.DramaReferenceApplyResponse
	if err := json.Unmarshal(prior.Response, &response); err != nil {
		return err
	}
	response.Replayed = true
	*result = &response
	return nil
}

// validateReusedReferenceAdoption ensures an existing adoption matches the exact quoted shot set and crop.
func validateReusedReferenceAdoption(ctx context.Context, tx repository.DramaReferenceTx, adoption *model.DramaReferenceAdoption, units []model.DramaReferencePlanUnit, instruction string) error {
	var basis model.DramaReferenceBasis
	if err := json.Unmarshal(adoption.Snapshot, &basis); err != nil {
		return err
	}
	if basis.Instruction != instruction {
		return errcode.ErrCanvasConflict.WithMsg("本参考修订已采用其他制作要求")
	}
	segments, err := tx.DramaTx().Segments(ctx, adoption.UserID, adoption.EpisodeID)
	if err != nil {
		return err
	}
	for _, unit := range units {
		if !matchingAppliedSegment(segments, basis.ReferenceID, unit) {
			return errcode.ErrCanvasConflict.WithMsg("原采用记录没有相同的分镜生成计划")
		}
	}
	return nil
}

// matchingAppliedSegment finds the adopted segment whose source and output window match a quoted shot.
func matchingAppliedSegment(rows []model.DramaSegment, referenceID string, unit model.DramaReferencePlanUnit) bool {
	for _, row := range rows {
		var remake model.DramaSegmentRemake
		if json.Unmarshal(row.Remake, &remake) == nil && remake.ReferenceID == referenceID && len(remake.Sources) == 1 &&
			remake.Sources[0].ShotID == unit.ShotID && remake.Sources[0].StartMs == unit.SourceStartMs && remake.Sources[0].EndMs == unit.SourceEndMs &&
			remake.TrimStartMs == unit.TrimStartMs && remake.TargetDurationMs == unit.TargetDurationMs && row.Prompt == unit.Prompt && row.Seconds == unit.Duration {
			return true
		}
	}
	return false
}

// appliedSegmentsResponse converts committed adoption rows into stable IDs and revisions.
func appliedSegmentsResponse(ctx context.Context, tx repository.DramaTx, adoption *model.DramaReferenceAdoption, view *model.DramaReferenceAdoptionView, units []model.DramaReferencePlanUnit) (*model.DramaReferenceApplyResponse, error) {
	rows, err := tx.Segments(ctx, adoption.UserID, adoption.EpisodeID)
	if err != nil {
		return nil, err
	}
	var applied model.DramaReferenceBasis
	if err := json.Unmarshal(adoption.Snapshot, &applied); err != nil {
		return nil, err
	}
	result := &model.DramaReferenceApplyResponse{DraftID: "", AdoptionID: idcodec.Encode(adoption.ID), EpisodeID: idcodec.Encode(adoption.EpisodeID),
		EpisodeNo: view.EpisodeNo, EpisodeRevision: view.EpisodeRevision, Segments: []model.DramaReferenceAppliedSegment{}}
	for _, unit := range units {
		for _, row := range rows {
			var remake model.DramaSegmentRemake
			if json.Unmarshal(row.Remake, &remake) != nil || remake.ReferenceID != applied.ReferenceID || len(remake.Sources) != 1 || remake.Sources[0].ShotID != unit.ShotID {
				continue
			}
			result.Segments = append(result.Segments, model.DramaReferenceAppliedSegment{ShotID: unit.ShotID, SegmentID: idcodec.Encode(row.ID),
				Revision: row.Revision, TrimStartMs: remake.TrimStartMs, TargetDurationMs: remake.TargetDurationMs})
			break
		}
	}
	if len(result.Segments) != len(units) {
		return nil, errcode.ErrCanvasConflict.WithMsg("采用后没有返回完整的选中片段")
	}
	return result, nil
}
