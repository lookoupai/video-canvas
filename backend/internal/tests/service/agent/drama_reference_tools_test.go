package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service/agent"
)

type referencePreparationFake struct {
	userID, projectID, referenceID uint64
	request                        *model.CreateDramaReferencePreparationReq
	keys                           []string
	calls                          int
}

func (f *referencePreparationFake) Create(_ context.Context, userID, projectID, referenceID uint64, req *model.CreateDramaReferencePreparationReq) (*model.DramaReferencePreparationView, error) {
	f.userID, f.projectID, f.referenceID, f.request = userID, projectID, referenceID, req
	f.keys = append(f.keys, req.RequestKey)
	f.calls++
	return &model.DramaReferencePreparationView{ID: idcodec.Encode(70), Status: "queued"}, nil
}
func (*referencePreparationFake) Get(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferencePreparationView, error) {
	return nil, nil
}
func (*referencePreparationFake) Frames(context.Context, uint64, uint64, uint64, uint64, *model.ListDramaReferenceFramesReq) (*model.DramaReferenceFrameList, error) {
	return nil, nil
}

type referenceDraftFake struct {
	userID, projectID, referenceID uint64
	sessionID, runID               uint64
	request                        *model.CreateDramaReferenceDraftReq
	view                           *model.DramaReferenceDraftView
}

type referenceReadFake struct {
	view model.DramaReferenceView
}

func (*referenceReadFake) Collection(context.Context, uint64, uint64) (*model.DramaReferenceCollection, error) {
	return &model.DramaReferenceCollection{}, nil
}
func (f *referenceReadFake) GetEpisode(context.Context, uint64, uint64, uint64, int64) (*model.DramaReferenceView, error) {
	view := f.view
	return &view, nil
}

func (f *referenceDraftFake) CreateForAgent(_ context.Context, userID, projectID, referenceID, sessionID, runID uint64, req *model.CreateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	f.userID, f.projectID, f.referenceID = userID, projectID, referenceID
	f.sessionID, f.runID, f.request = sessionID, runID, req
	return &model.DramaReferenceDraftView{ID: idcodec.Encode(71), Source: "agent", Revision: 1}, nil
}
func (f *referenceDraftFake) Get(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferenceDraftView, error) {
	if f.view != nil {
		return f.view, nil
	}
	return &model.DramaReferenceDraftView{ID: idcodec.Encode(47), Revision: 1, Status: "editing", DramaReferenceDraftContent: model.DramaReferenceDraftContent{Instruction: "替换商品"}}, nil
}
func (*referenceDraftFake) List(context.Context, uint64, uint64, uint64, *model.ListDramaReferenceDraftsReq) (*model.DramaReferenceDraftList, error) {
	return nil, nil
}
func (*referenceDraftFake) UpdateForAgent(context.Context, uint64, uint64, uint64, uint64, uint64, uint64, *model.UpdateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	return nil, nil
}

type referencePlanFake struct {
	digest   string
	apply    *model.ApplyDramaReferenceDraftReq
	generate *model.DramaReferenceGenerateReq
	checks   int
}

func (f *referencePlanFake) Quote(_ context.Context, _, _, _, _ uint64, req *model.QuoteDramaReferenceDraftReq) (*model.DramaReferencePlan, error) {
	f.checks++
	if req.Revision != 1 {
		return nil, errcode.ErrCanvasConflict
	}
	return &model.DramaReferencePlan{Executable: true, PlanDigest: f.digest, Units: []model.DramaReferencePlanUnit{{ShotID: "shot-1"}}}, nil
}

func (f *referencePlanFake) ApplyForAgent(_ context.Context, _, _, _, _, _, _ uint64, req *model.ApplyDramaReferenceDraftReq) (*model.DramaReferenceApplyResponse, error) {
	f.apply = req
	return &model.DramaReferenceApplyResponse{ApplicationID: idcodec.Encode(51), DraftRevision: req.Revision}, nil
}

func (f *referencePlanFake) CheckGenerate(_ context.Context, _, _, _, _ uint64, req *model.DramaReferenceGenerateReq) (*model.DramaReferencePlan, error) {
	f.checks++
	f.generate = req
	return &model.DramaReferencePlan{Executable: true, PlanDigest: f.digest, TotalVideoCredits: 9, Units: []model.DramaReferencePlanUnit{{ShotID: "shot-1"}}}, nil
}

func (f *referencePlanFake) Generate(_ context.Context, _, _, _, _ uint64, req *model.DramaReferenceGenerateReq) (*model.DramaReferenceGenerateResponse, error) {
	f.generate = req
	return &model.DramaReferenceGenerateResponse{ApplicationID: req.ApplicationID, Status: "submitted"}, nil
}

func newReferenceToolBridge(t *testing.T, references AgentDramaReferences, prep AgentDramaPreparations, drafts AgentDramaDrafts, plans AgentDramaPlans) (*AgentBridge, *bridgeEnv) {
	t.Helper()
	_, drama, env := newDramaBridge(t)
	bridge := NewAgentBridge(BridgeDeps{
		Repo: env.repo, Canvas: NewAgentCanvasService(env.repo, env.bc), Agent: env.svc,
		Registry: env.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: env.str,
		RunUsage: &fakeRunUsage{bill: env.bill}, Drama: drama, DramaReferences: references,
		DramaPreparations: prep, DramaDrafts: drafts, DramaPlans: plans, DramaApprovalKey: []byte("test-drama-signing-secret"),
	})
	return bridge, env
}

func TestDramaReferencePrepareUsesProjectAndDurableRequestKey(t *testing.T) {
	prep := &referencePreparationFake{}
	bridge, _ := newReferenceToolBridge(t, nil, prep, nil, nil)
	ctx := WithIdempotencyKey(context.Background(), "durable-task-42")
	raw, _ := json.Marshal(map[string]any{"reference_id": idcodec.Encode(23), "revision": 4})
	for range 2 {
		result, err := bridge.ExecutePlatformTool(ctx, platformCtx(991, "drama"), "model-call", "drama_reference_prepare", raw)
		if err != nil || result.IsError {
			t.Fatalf("媒体准备工具失败：%+v %v", result, err)
		}
	}
	if prep.userID != 1 || prep.projectID != 10 || prep.referenceID != 23 || prep.calls != 2 {
		t.Fatalf("工具没有从运行和短剧画布绑定范围：%+v", prep)
	}
	if prep.request.ReferenceRevision != 4 || !strings.HasPrefix(prep.request.RequestKey, "prepare-") {
		t.Fatalf("准备请求键或参考修订错误：%+v", prep.request)
	}
	firstKey := prep.request.RequestKey
	if len(firstKey) != len("prepare-")+48 || prep.keys[0] != prep.keys[1] {
		t.Fatalf("准备请求键格式不稳定：%q", firstKey)
	}
}

func TestDramaReferenceApplyAndGenerateRequireSignedApproval(t *testing.T) {
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	drafts, plans := &referenceDraftFake{}, &referencePlanFake{digest: digest}
	bridge, _ := newReferenceToolBridge(t, nil, nil, drafts, plans)
	args := map[string]any{
		"reference_id": idcodec.Encode(23), "draft_id": idcodec.Encode(47), "revision": 1, "plan_digest": digest,
		"quote": map[string]any{"revision": 1, "model_id": "model", "units": []any{map[string]any{"shot_id": "shot-1", "mode": "text"}}},
	}
	raw, _ := json.Marshal(args)
	rc := platformCtx(995, "drama")
	proposal, err := bridge.ProposeApproval(context.Background(), rc, "drama_reference_apply", raw)
	if err != nil || proposal.IsError || proposal.QuoteCredits != 0 {
		t.Fatalf("采用应产生无费用审批：%+v %v", proposal, err)
	}
	outcome, err := bridge.ResolveApproval(context.Background(), rc, "drama_reference_apply", proposal.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	tampered := *outcome
	changed := *tampered.Drama
	changed.Args = json.RawMessage(`{"changed":true}`)
	tampered.Drama = &changed
	if _, err := bridge.ExecuteApproval(context.Background(), rc, "drama_reference_apply", 71, "apply-call", tampered); err == nil || plans.apply != nil {
		t.Fatalf("改动签名内容后不能执行采用：%v %+v", err, plans.apply)
	}
	result, err := bridge.ExecuteApproval(context.Background(), rc, "drama_reference_apply", 71, "apply-call", *outcome)
	if err != nil || result.Status != ExecExecuted || plans.apply == nil || plans.apply.Instruction != "替换商品" {
		t.Fatalf("批准后应使用已签名计划：%+v %v %+v", result, err, plans.apply)
	}
	applyKey := plans.apply.RequestKey

	args["application_id"] = idcodec.Encode(51)
	raw, _ = json.Marshal(args)
	generated, err := bridge.ProposeApproval(context.Background(), rc, "drama_generate_segments", raw)
	if err != nil || generated.IsError || generated.QuoteCredits != 9 {
		t.Fatalf("逐镜生成应返回报价审批：%+v %v", generated, err)
	}
	generateOutcome, err := bridge.ResolveApproval(context.Background(), rc, "drama_generate_segments", generated.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	result, err = bridge.ExecuteApproval(context.Background(), rc, "drama_generate_segments", 72, "generate-call", *generateOutcome)
	if err != nil || result.Status != ExecExecuted || plans.generate == nil || plans.generate.RequestKey == "" {
		t.Fatalf("批准后应生成已报价批次：%+v %v %+v", result, err, plans.generate)
	}
	generateKey := plans.generate.RequestKey
	rc.RunID++
	replay, err := bridge.ProposeApproval(context.Background(), rc, "drama_generate_segments", raw)
	if err != nil || replay.IsError {
		t.Fatalf("重启后同一计划仍可报价：%+v %v", replay, err)
	}
	var approval DramaApprovalPayload
	if err := json.Unmarshal(replay.Payload, &approval); err != nil {
		t.Fatal(err)
	}
	var saved struct {
		ReferenceID idcodec.ID                      `json:"reference_id"`
		DraftID     idcodec.ID                      `json:"draft_id"`
		Request     model.DramaReferenceGenerateReq `json:"request"`
	}
	if err := json.Unmarshal(approval.Args, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Request.RequestKey != generateKey || saved.ReferenceID != idcodec.ID(23) || saved.DraftID != idcodec.ID(47) {
		t.Fatalf("批准重放必须固定业务键和资源范围：%+v", saved)
	}
	if applyKey == "" || plans.checks != 3 {
		t.Fatalf("申请审批须重验报价：apply=%q checks=%d", applyKey, plans.checks)
	}
}

func TestDramaReferenceDraftRecordsTrustedRunAndPreparation(t *testing.T) {
	drafts := &referenceDraftFake{}
	bridge, _ := newReferenceToolBridge(t, nil, nil, drafts, nil)
	raw, _ := json.Marshal(map[string]any{
		"action": "create", "reference_id": idcodec.Encode(23), "preparation_id": idcodec.Encode(70),
		"reference_revision": 4, "instruction": "保留动作并替换商品", "coverage": map[string]string{"shots": "complete", "visual": "partial", "dialogue": "not_analyzed", "speakers": "not_analyzed"},
	})
	result, err := bridge.ExecutePlatformTool(context.Background(), platformCtx(992, "drama"), "model-call", "drama_reference_draft", raw)
	if err != nil || result.IsError {
		t.Fatalf("分析草稿工具失败：%+v %v", result, err)
	}
	if drafts.userID != 1 || drafts.projectID != 10 || drafts.referenceID != 23 || drafts.sessionID != 5 || drafts.runID != 992 {
		t.Fatalf("草稿来源没有取自可信运行上下文：%+v", drafts)
	}
	if drafts.request.PreparationID != idcodec.Encode(70) || drafts.request.ReferenceRevision != 4 || drafts.request.Instruction != "保留动作并替换商品" {
		t.Fatalf("草稿未绑定输入基准：%+v", drafts.request)
	}
}

func TestDramaReferencePrepareRejectsModelSuppliedRequestKey(t *testing.T) {
	prep := &referencePreparationFake{}
	bridge, _ := newReferenceToolBridge(t, nil, prep, nil, nil)
	raw, _ := json.Marshal(map[string]any{"reference_id": idcodec.Encode(23), "revision": 4, "request_key": "forged"})
	result, err := bridge.ExecutePlatformTool(context.Background(), platformCtx(993, "drama"), "model-call", "drama_reference_prepare", raw)
	if err != nil || !result.IsError || prep.calls != 0 {
		t.Fatalf("模型不得伪造请求键或启动准备：%+v %v calls=%d", result, err, prep.calls)
	}
}

func TestDramaReferenceReadPaginatesShotsAndRelatedData(t *testing.T) {
	refs := &referenceReadFake{view: model.DramaReferenceView{ID: idcodec.Encode(23), Revision: 4, Document: &model.DramaReferenceDocument{
		Scenes:     []model.DramaReferenceScene{{ID: "scene-1", Title: "厨房"}, {ID: "scene-2", Title: "客厅"}},
		Characters: []model.DramaReferenceCharacter{{ID: "char-1", Name: "甲"}, {ID: "char-2", Name: "乙"}},
		Shots: []model.DramaReferenceShot{
			{ID: "shot-1", SceneID: "scene-1", StartMs: 0, EndMs: 1000, Characters: []string{"char-1"}},
			{ID: "shot-2", SceneID: "scene-1", StartMs: 1000, EndMs: 2000, Characters: []string{"char-1"}},
			{ID: "shot-3", SceneID: "scene-2", StartMs: 2000, EndMs: 3000, Characters: []string{"char-2"}},
		},
		Dialogues: []model.DramaReferenceDialogue{{ID: "line-1", StartMs: 500, EndMs: 600, Text: "开始"}, {ID: "line-2", StartMs: 2500, EndMs: 2600, Text: "结束"}},
	}}}
	bridge, _ := newReferenceToolBridge(t, refs, nil, nil, nil)
	first := execReferenceRead(t, bridge, map[string]any{"scope": "reference", "reference_id": idcodec.Encode(23), "shot_limit": 2})
	if len(first.Reference.Document.Shots) != 2 || first.NextShotCursor != "2" || len(first.Reference.Document.Dialogues) != 1 || len(first.Reference.Document.Characters) != 1 {
		t.Fatalf("第一页应只含对应镜头、对白和人物：%+v", first)
	}
	second := execReferenceRead(t, bridge, map[string]any{"scope": "reference", "reference_id": idcodec.Encode(23), "shot_cursor": first.NextShotCursor, "shot_limit": 2})
	if len(second.Reference.Document.Shots) != 1 || second.Reference.Document.Shots[0].ID != "shot-3" || second.NextShotCursor != "" {
		t.Fatalf("第二页镜头游标错误：%+v", second)
	}
}

func execReferenceRead(t *testing.T, bridge *AgentBridge, args map[string]any) struct {
	Reference      model.DramaReferenceView `json:"reference"`
	NextShotCursor string                   `json:"next_shot_cursor"`
} {
	t.Helper()
	raw, _ := json.Marshal(args)
	result, err := bridge.ExecutePlatformTool(context.Background(), platformCtx(994, "drama"), "read-reference", "drama_reference_read", raw)
	if err != nil || result.IsError {
		t.Fatalf("读取参考剧集失败：%+v %v", result, err)
	}
	var page struct {
		Reference      model.DramaReferenceView `json:"reference"`
		NextShotCursor string                   `json:"next_shot_cursor"`
	}
	if err := json.Unmarshal([]byte(result.Content), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
