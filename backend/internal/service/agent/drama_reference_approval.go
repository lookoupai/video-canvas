package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

type dramaReferenceApplyArgs struct {
	ReferenceID     idcodec.ID                        `json:"reference_id"`
	DraftID         idcodec.ID                        `json:"draft_id"`
	Revision        int64                             `json:"revision"`
	PlanDigest      string                            `json:"plan_digest"`
	Quote           model.QuoteDramaReferenceDraftReq `json:"quote"`
	TargetEpisodeNo int                               `json:"target_episode_no"`
	EpisodeRevision int64                             `json:"episode_revision"`
}

type dramaReferenceGenerateArgs struct {
	ReferenceID   idcodec.ID                        `json:"reference_id"`
	DraftID       idcodec.ID                        `json:"draft_id"`
	ApplicationID idcodec.ID                        `json:"application_id"`
	Revision      int64                             `json:"revision"`
	PlanDigest    string                            `json:"plan_digest"`
	Quote         model.QuoteDramaReferenceDraftReq `json:"quote"`
}

type dramaReferenceApplyApproval struct {
	ReferenceID idcodec.ID                        `json:"reference_id"`
	DraftID     idcodec.ID                        `json:"draft_id"`
	Request     model.ApplyDramaReferenceDraftReq `json:"request"`
}

type dramaReferenceGenerateApproval struct {
	ReferenceID idcodec.ID                      `json:"reference_id"`
	DraftID     idcodec.ID                      `json:"draft_id"`
	Request     model.DramaReferenceGenerateReq `json:"request"`
}

// toolDramaReferenceApply validates the quote before requesting approval for adoption.
func (b *AgentBridge) toolDramaReferenceApply(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var args dramaReferenceApplyArgs
	if err := parseDramaArgs(tc.args, &args); err != nil {
		return nil, err
	}
	if args.ReferenceID == 0 || args.DraftID == 0 || args.Revision < 1 || args.Revision != args.Quote.Revision ||
		b.d.DramaDrafts == nil || b.d.DramaPlans == nil {
		return nil, fail("采用计划需要有效的参考集、草稿、修订和报价服务")
	}
	project, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	draft, err := b.d.DramaDrafts.Get(ctx, tc.run.UserID, uint64(project.ID), uint64(args.ReferenceID), uint64(args.DraftID))
	if err != nil {
		return nil, err
	}
	if draft.Revision != args.Revision || draft.Status != "editing" && draft.Status != "ready" {
		return nil, fail("草稿已变化或已经采用，请读取最新草稿")
	}
	plan, err := b.d.DramaPlans.Quote(ctx, tc.run.UserID, uint64(project.ID), uint64(args.ReferenceID), uint64(args.DraftID), &args.Quote)
	if err != nil {
		return nil, err
	}
	if !plan.Executable || plan.PlanDigest != args.PlanDigest {
		return nil, fail("当前草稿报价不可执行或已变化，请重新报价")
	}
	req := model.ApplyDramaReferenceDraftReq{Revision: args.Revision, RequestKey: dramaReferenceApprovalKey("apply", uint64(project.ID), uint64(args.ReferenceID), uint64(args.DraftID), 0, args.PlanDigest),
		PlanDigest: args.PlanDigest, Mode: "follow", Instruction: draft.Instruction, TargetEpisodeNo: args.TargetEpisodeNo, EpisodeRevision: args.EpisodeRevision, Quote: args.Quote}
	raw, err := json.Marshal(dramaReferenceApplyApproval{ReferenceID: args.ReferenceID, DraftID: args.DraftID, Request: req})
	if err != nil {
		return nil, err
	}
	after := fmt.Sprintf("采用 %d 个原镜并保存当前草稿", len(plan.Units))
	return b.dramaProposal(ctx, tc, "drama_reference_apply", DramaApprovalPayload{DramaID: project.ID, Before: fmt.Sprintf("草稿修订 %d", args.Revision), After: after, Args: raw})
}

// toolDramaReferenceGenerate checks the approved application and requests approval for its frozen quote.
func (b *AgentBridge) toolDramaReferenceGenerate(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var args dramaReferenceGenerateArgs
	if err := parseDramaArgs(tc.args, &args); err != nil {
		return nil, err
	}
	if args.ReferenceID == 0 || args.DraftID == 0 || args.ApplicationID == 0 || b.d.DramaPlans == nil {
		return nil, fail("逐镜生成需要参考集、草稿、采用回执和计划服务")
	}
	project, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	req := model.DramaReferenceGenerateReq{Revision: args.Revision,
		RequestKey:    dramaReferenceApprovalKey("generate", uint64(project.ID), uint64(args.ReferenceID), uint64(args.DraftID), uint64(args.ApplicationID), args.PlanDigest),
		ApplicationID: idcodec.Encode(uint64(args.ApplicationID)), PlanDigest: args.PlanDigest, Quote: args.Quote}
	plan, err := b.d.DramaPlans.CheckGenerate(ctx, tc.run.UserID, uint64(project.ID), uint64(args.ReferenceID), uint64(args.DraftID), &req)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(dramaReferenceGenerateApproval{ReferenceID: args.ReferenceID, DraftID: args.DraftID, Request: req})
	if err != nil {
		return nil, err
	}
	after := fmt.Sprintf("提交 %d 个逐镜视频任务", len(plan.Units))
	return b.dramaProposal(ctx, tc, "drama_generate_segments", DramaApprovalPayload{DramaID: project.ID, After: after, Args: raw, Estimate: plan.TotalVideoCredits})
}

// dramaReferenceApprovalKey remains stable across model tool-call IDs and Agent runs.
func dramaReferenceApprovalKey(kind string, project, reference, draft, application uint64, digest string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%d:%d:%s", kind, project, reference, draft, application, digest)))
	return kind + "-" + hex.EncodeToString(sum[:24])
}
