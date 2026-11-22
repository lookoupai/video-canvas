package service_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func TestReferenceDraftApplyUsesAdoptionTransaction(t *testing.T) {
	rig := referenceApplyRig(t)
	plan, err := rig.plans.Quote(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, rig.quote)
	if err != nil || !plan.Executable {
		t.Fatalf("先获取可执行报价：%+v %v", plan, err)
	}
	request := &model.ApplyDramaReferenceDraftReq{Revision: rig.quote.Revision, RequestKey: "apply-transaction", Mode: "follow", PlanDigest: plan.PlanDigest, Quote: *rig.quote}
	response, err := rig.plans.Apply(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, request)
	if err != nil || response.ApplicationID == "" || len(response.Segments) != 1 || response.DraftRevision != rig.quote.Revision {
		t.Fatalf("原子采用没有返回完整 receipt：%+v %v", response, err)
	}
	draft, err := rig.drafts.Get(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID)
	if err != nil || draft.Status != "applied" {
		t.Fatalf("草稿状态没有与采用一起提交：%+v %v", draft, err)
	}
	segments, err := rig.referenceRepo.DramaTx().Segments(t.Context(), 7, decodeTestID(t, response.EpisodeID))
	if err != nil || len(segments) != 1 || segments[0].Prompt != plan.Units[0].Prompt {
		t.Fatalf("采用片段缺失或未使用固定提示词：%+v %v", segments, err)
	}
	remake := mustSegmentRemake(t, segments[0])
	if remake.TrimStartMs != plan.Units[0].TrimStartMs || remake.TargetDurationMs != plan.Units[0].TargetDurationMs {
		t.Fatalf("采用片段没有保存报价裁剪窗口：%+v", remake)
	}
	replayed, err := rig.plans.Apply(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, request)
	if err != nil || !replayed.Replayed || replayed.ApplicationID != response.ApplicationID {
		t.Fatalf("重复采用应重放原回执：%+v %v", replayed, err)
	}
	request.Quote.Units[0].TrimStartMs++
	_, err = rig.plans.Apply(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, request)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	if len(mustLoadSegments(t, rig.referenceRepo, 7, decodeTestID(t, response.EpisodeID))) != 1 {
		t.Fatal("键输入不一致不应写入第二份采用")
	}
}

func TestReferenceDraftApplyRechecksPlanBeforeAdoption(t *testing.T) {
	rig := referenceApplyRig(t)
	plan, err := rig.plans.Quote(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, rig.quote)
	if err != nil || !plan.Executable {
		t.Fatalf("先获取可执行报价：%+v %v", plan, err)
	}
	request := &model.ApplyDramaReferenceDraftReq{Revision: rig.quote.Revision, RequestKey: "apply-stale", Mode: "follow", PlanDigest: plan.PlanDigest, Quote: *rig.quote}
	rig.snapshot.Model.Pricing.Unit++
	_, err = rig.plans.Apply(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, request)
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
	draft, getErr := rig.drafts.Get(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID)
	if getErr != nil || draft.Status != "editing" {
		t.Fatalf("过期报价摘要不应改变草稿状态：%+v %v", draft, getErr)
	}
	if _, err := rig.drafts.Get(t.Context(), 8, rig.projectID, rig.referenceID, rig.draftID); err == nil {
		t.Fatal("其他用户不能查看 apply 草稿")
	}
}

func TestReferenceApplyDTORejectsInvalidDigestAndInvalidAssetIDs(t *testing.T) {
	var req model.ApplyDramaReferenceDraftReq
	if err := json.Unmarshal([]byte(`{"revision":1,"request_key":"k","plan_digest":"short","mode":"follow","quote":{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"image","image_asset_ids":["18446744073709551615"]}]}}`), &req); err != nil {
		t.Fatal(err)
	}
	ids := req.Quote.Units[0].ImageAssetIDs
	if len(ids) != 1 || ids[0] != ^uint64(0) {
		t.Fatalf("apply quote did not preserve full asset ID: %v", ids)
	}
}

type referenceApplyRigType struct {
	plans         *service.DramaReferencePlanService
	drafts        *service.DramaReferenceDraftService
	referenceRepo *repository.DramaReferenceRepository
	snapshot      *provider.Snapshot
	projectID     uint64
	referenceID   uint64
	draftID       uint64
	quote         *model.QuoteDramaReferenceDraftReq
}

func referenceApplyRig(t *testing.T) referenceApplyRigType {
	t.Helper()
	return referenceApplyRigWithGenerator(t, nil)
}

func referenceApplyRigWithGenerator(t *testing.T, generator service.DramaReferencePlanSegments) referenceApplyRigType {
	t.Helper()
	db := mktDB(t)
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaSegment{}, &model.DramaJob{},
		&model.DramaReferenceState{}, &model.DramaReferenceEpisode{}, &model.DramaReferenceRevision{}, &model.DramaReferenceRole{},
		&model.DramaReferenceAdoption{}, &model.DramaReferenceDraft{}, &model.DramaReferenceDraftApplication{}, &model.DramaReferenceDraftGeneration{}); err != nil {
		t.Fatal(err)
	}
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 2500)
	referenceRepo := repository.NewDramaReferenceRepository(db)
	adoptions := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: referenceRepo, Assets: assets})
	created, err := adoptions.CreateProject(t.Context(), 7, &model.CreateDramaReferenceProjectReq{Title: "逐镜采用", RequestKey: "apply-project"})
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := idcodec.Decode(created.ID)
	reference, err := adoptions.AddEpisode(t.Context(), 7, projectID, &model.AddDramaReferenceReq{AssetID: 1, Title: "原片", RequestKey: "apply-reference"})
	if err != nil {
		t.Fatal(err)
	}
	referenceID, _ := idcodec.Decode(reference.ID)
	document := model.DramaReferenceDocument{Scenes: []model.DramaReferenceScene{{ID: "scene", Title: "桌面"}}, Shots: []model.DramaReferenceShot{{ID: "1", SceneID: "scene", StartMs: 0, EndMs: 2500, Action: "递出钥匙"}}}
	reference, err = adoptions.UpdateEpisode(t.Context(), 7, projectID, referenceID, &model.UpdateDramaReferenceReq{Revision: reference.Revision, Document: &document})
	if err != nil {
		t.Fatal(err)
	}
	draftService := service.NewDramaReferenceDraftService(service.DramaReferenceDraftDeps{Repo: repository.NewDramaReferenceDraftRepository(db), Assets: assets})
	draft, err := draftService.Create(t.Context(), 7, projectID, referenceID, &model.CreateDramaReferenceDraftReq{RequestKey: "apply-draft", ReferenceRevision: reference.Revision,
		Instruction: "换成自己的商品", Prompts: []model.DramaReferencePrompt{{ShotID: "1", Prompt: "镜头里自然递出自己的商品"}}})
	if err != nil {
		t.Fatal(err)
	}
	draftID, _ := idcodec.Decode(draft.ID)
	snapshot := &provider.Snapshot{Model: provider.ModelSnapshot{Key: "model", Kind: "video", Capabilities: modelcfg.Capabilities{
		Ops: []string{"t2v"}, Prompt: modelcfg.PromptSpec{MaxLength: 2000}, Params: modelcfg.ParamSet{{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Open: true, Default: 5}}},
	}, Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 4}}, Plugin: provider.PluginSnapshot{Key: "test"}}
	planService := service.NewDramaReferencePlanService(service.DramaReferencePlanDeps{Repo: repository.NewDramaReferenceDraftRepository(db), Assets: assets,
		Registry: referencePlanRegistry{snapshot: snapshot}, Adoptions: adoptions, Segments: generator})
	return referenceApplyRigType{plans: planService, drafts: draftService, referenceRepo: referenceRepo, snapshot: snapshot, projectID: projectID,
		referenceID: referenceID, draftID: draftID, quote: planRequest("text")}
}

func decodeTestID(t *testing.T, value string) uint64 {
	t.Helper()
	id, ok := idcodec.Decode(value)
	if !ok {
		t.Fatalf("invalid encoded ID %q", value)
	}
	return id
}

func mustLoadSegments(t *testing.T, repo *repository.DramaReferenceRepository, userID, episodeID uint64) []model.DramaSegment {
	t.Helper()
	segments, err := repo.DramaTx().Segments(t.Context(), userID, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	return segments
}

func mustSegmentRemake(t *testing.T, segment model.DramaSegment) model.DramaSegmentRemake {
	t.Helper()
	var remake model.DramaSegmentRemake
	if err := json.Unmarshal(segment.Remake, &remake); err != nil {
		t.Fatal(err)
	}
	return remake
}
