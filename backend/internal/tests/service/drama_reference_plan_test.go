package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

type referencePlanRegistry struct{ snapshot *provider.Snapshot }

func (r referencePlanRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return nil, nil
}

func (r referencePlanRegistry) Snapshot(_ context.Context, key string) (*provider.Snapshot, error) {
	if key != r.snapshot.Model.Key {
		return nil, provider.ErrModelUnavailable
	}
	return r.snapshot, nil
}

func planFixture(t *testing.T) (*service.DramaReferencePlanService, *draftMemoryRepo, *provider.Snapshot, uint64) {
	t.Helper()
	drafts, repo := newDraftService(t)
	draft, err := drafts.Create(t.Context(), 7, 4, 3, draftRequest())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(draft.ID)
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	assets.put(7, 2, "image", "image/png", 0)
	assets.items[2].asset.Width, assets.items[2].asset.Height = 512, 512
	assets.put(8, 3, "image", "image/png", 0)
	snap := &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "model", Kind: "video", UpstreamModel: "agnes-video-2.5",
			Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerSecond, PerSecond: 3},
			Capabilities: modelcfg.Capabilities{Ops: []string{"t2v", "i2v", "omni"}, Prompt: modelcfg.PromptSpec{MaxLength: 1024},
				Refs: modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 8, MaxMB: 10}, Video: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 50}},
				Params: modelcfg.ParamSet{
					{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Open: true, Default: 5}},
					{Name: "resolution", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Open: true, Default: "720P", Options: []any{"720P", "1080P"}}},
				}}},
		Plugin: provider.PluginSnapshot{Key: "agnes-video", Version: "1.0.0"},
	}
	return service.NewDramaReferencePlanService(service.DramaReferencePlanDeps{Repo: repo, Assets: assets, Registry: referencePlanRegistry{snap}}), repo, snap, id
}

func planRequest(mode string) *model.QuoteDramaReferenceDraftReq {
	return &model.QuoteDramaReferenceDraftReq{Revision: 1, ModelID: "model", Units: []model.DramaReferencePlanUnitReq{{ShotID: "1", Mode: mode, Duration: 4}}}
}

func TestReferencePlanAssetIDsPreserveUint64Precision(t *testing.T) {
	var req model.QuoteDramaReferenceDraftReq
	if err := json.Unmarshal([]byte(`{"revision":1,"model_id":"model","units":[{"shot_id":"1","image_asset_ids":[9007199254740993,"18446744073709551615"]}]}`), &req); err != nil {
		t.Fatal(err)
	}
	got := req.Units[0].ImageAssetIDs
	if len(got) != 2 || got[0] != 9007199254740993 || got[1] != ^uint64(0) {
		t.Fatalf("uint64 素材 ID 被精度转换：%v", got)
	}
	if err := json.Unmarshal([]byte(`{"revision":1,"model_id":"model","units":[{"shot_id":"1","image_asset_ids":[-1]}]}`), &req); err == nil {
		t.Fatal("拒绝非正素材 ID")
	}
}

func TestReferencePlanVideoUsesOriginalWindowAndNormalizedQuote(t *testing.T) {
	svc, _, _, id := planFixture(t)
	req := planRequest("video")
	req.Units[0].TrimStartMs = 200
	got, err := svc.Quote(t.Context(), 7, 4, 3, id, req)
	if err != nil || !got.Executable || got.TotalVideoCredits != 12 || got.ConfigurationDigest == "" || got.PlanDigest == "" {
		t.Fatalf("生成计划错误：%+v %v", got, err)
	}
	unit := got.Units[0]
	if unit.Mode != "video" || unit.SourceStartMs != 0 || unit.SourceEndMs != 2500 || unit.Duration != 4 || unit.TargetDurationMs != 2500 || unit.TrimStartMs != 200 || unit.Input["op"] != "omni" || unit.Prompt != "手自然递出钥匙" {
		t.Fatalf("原镜、申请时长与裁剪区间应独立：%+v", unit)
	}
	if _, present := unit.Input["videos"]; present {
		t.Fatal("报价输入不能把原片源引用伪装成生成裁片")
	}
	auto := planRequest("auto")
	auto.Units[0].Duration = 4
	videoPlan, err := svc.Quote(t.Context(), 7, 4, 3, id, auto)
	if err != nil || !videoPlan.Executable || videoPlan.Units[0].Mode != "video" {
		t.Fatalf("开启视频引用的模型应优先采用原片路线：%+v %v", videoPlan, err)
	}
	job := unit.GenerateRequest(got.ModelID, 9)
	if !job.UseReferenceVideo || job.Revision != 9 || job.Input["prompt"] != unit.Prompt || job.ModelID != "model" {
		t.Fatalf("生成参数未保留报价结果：%+v", job)
	}
	if _, present := job.Input["videos"]; present {
		t.Fatal("原片源素材必须留给片段服务裁切")
	}
}

func TestReferencePlanFlashRejectsVideoUsesImages(t *testing.T) {
	svc, _, snap, id := planFixture(t)
	snap.Model.UpstreamModel = "agnes-video-2.5-flash"
	got, err := svc.Quote(t.Context(), 7, 4, 3, id, planRequest("video"))
	if err != nil || got.Executable || got.Units[0].Issues[0].Code != "protocol_video_unsupported" {
		t.Fatalf("Flash 视频参考必须被明确阻止：%+v %v", got, err)
	}
	req := planRequest("auto")
	req.Units[0].ImageAssetIDs = model.DramaReferenceAssetIDs{2}
	got, err = svc.Quote(t.Context(), 7, 4, 3, id, req)
	if err != nil || !got.Executable || got.Units[0].Mode != "keyframe" || got.Units[0].Input["op"] != "i2v" {
		t.Fatalf("Flash 图片参考没有进入首帧路线：%+v %v", got, err)
	}
	if refs := modelcfg.MediaRefs(got.Units[0].Input); len(refs) != 1 || refs[0].ID != 2 {
		t.Fatalf("实际输入丢失图片引用：%v", refs)
	}
	textFallback := planRequest("auto")
	textFallback.Units[0].Duration = 4
	got, err = svc.Quote(t.Context(), 7, 4, 3, id, textFallback)
	if err != nil || !got.Executable || got.Units[0].Mode != "text" {
		t.Fatalf("关闭视频引用的模型应回退到文本路线：%+v %v", got, err)
	}
	if !hasPlanIssue(got.Issues, "reference_consistency_risk") {
		t.Fatalf("文本回退必须提示失去原片动作参考：%+v", got.Issues)
	}
	req.Units[0].Params = map[string]any{"resolution": "1080P"}
	got, err = svc.Quote(t.Context(), 7, 4, 3, id, req)
	if err != nil || got.Executable || got.Units[0].Issues[0].Code != "protocol_resolution_unsupported" {
		t.Fatalf("Flash 的实际 1080P 配置必须被阻止：%+v %v", got, err)
	}
}

func hasPlanIssue(issues []model.DramaReferencePlanIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestReferencePlanApprovedDigestDetectsCurrentChanges(t *testing.T) {
	svc, repo, snap, id := planFixture(t)
	req := planRequest("text")
	got, err := svc.Quote(t.Context(), 7, 4, 3, id, req)
	if err != nil || !got.Executable {
		t.Fatalf("初次报价失败：%+v %v", got, err)
	}
	if _, err := svc.CheckApproved(t.Context(), 7, 4, 3, id, req, got.PlanDigest); err != nil {
		t.Fatalf("等价的生成计划应该能重新验证：%v", err)
	}
	otherDraft, err := repoFixtureDraft(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := idcodec.Decode(otherDraft.ID)
	otherPlan, err := svc.Quote(t.Context(), 7, 4, 3, otherID, req)
	if err != nil || otherPlan.PlanDigest == got.PlanDigest {
		t.Fatalf("相同内容的其他草稿不能复用批准摘要：%+v %v", otherPlan, err)
	}
	snap.Model.Pricing.PerSecond++
	_, err = svc.CheckApproved(t.Context(), 7, 4, 3, id, req, got.PlanDigest)
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
	repo.ref.Revision++
	_, err = svc.Quote(t.Context(), 7, 4, 3, id, req)
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
	_, err = svc.Quote(t.Context(), 8, 4, 3, id, req)
	wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
}

func repoFixtureDraft(t *testing.T, repo *draftMemoryRepo) (*model.DramaReferenceDraftView, error) {
	t.Helper()
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 2, "image", "image/png", 0)
	draftService := service.NewDramaReferenceDraftService(service.DramaReferenceDraftDeps{Repo: repo, Assets: assets})
	req := draftRequest()
	req.RequestKey = "second-plan-draft"
	return draftService.Create(t.Context(), 7, 4, 3, req)
}

func TestReferencePlanBlocksLostReferencesInvalidAssetsAndBudget(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		setup      func(*model.QuoteDramaReferenceDraftReq, *provider.Snapshot)
	}{
		{"图片开关关闭", "model_route_unsupported", func(r *model.QuoteDramaReferenceDraftReq, s *provider.Snapshot) {
			r.Units[0].Mode, r.Units[0].ImageAssetIDs, s.Model.Capabilities.Refs.Image.On = "image", model.DramaReferenceAssetIDs{2}, false
		}},
		{"文本模式不能吞掉请求的图片", "route_media_mismatch", func(r *model.QuoteDramaReferenceDraftReq, _ *provider.Snapshot) {
			r.Units[0].ImageAssetIDs = model.DramaReferenceAssetIDs{2}
		}},
		{"其它用户的图片", "asset_unavailable", func(r *model.QuoteDramaReferenceDraftReq, _ *provider.Snapshot) {
			r.Units[0].Mode, r.Units[0].ImageAssetIDs = "keyframe", model.DramaReferenceAssetIDs{3}
		}},
		{"固定 mode 会让插件忽略参考素材", "protocol_mode_conflict", func(r *model.QuoteDramaReferenceDraftReq, s *provider.Snapshot) {
			r.Units[0].Mode = "video"
			s.Model.Capabilities.Params = append(s.Model.Capabilities.Params, modelcfg.ParamEntry{Name: "mode", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Default: "text", Options: []any{"text"}}})
		}},
		{"生成时长太短不能裁剪", "duration_insufficient", func(r *model.QuoteDramaReferenceDraftReq, _ *provider.Snapshot) {
			r.Units[0].TrimStartMs = 2500
		}},
		{"超预算", "budget_exceeded", func(r *model.QuoteDramaReferenceDraftReq, _ *provider.Snapshot) {
			r.MaxCredits = new(0)
		}},
		{"重复选镜", "shot_duplicate", func(r *model.QuoteDramaReferenceDraftReq, _ *provider.Snapshot) {
			r.Units = append(r.Units, r.Units[0])
		}},
		{"参数不能覆盖提示词", "model_input_unavailable", func(r *model.QuoteDramaReferenceDraftReq, _ *provider.Snapshot) {
			r.Units[0].Params = map[string]any{"prompt": "另一个请求"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, snap, id := planFixture(t)
			req := planRequest("text")
			tc.setup(req, snap)
			got, err := svc.Quote(t.Context(), 7, 4, 3, id, req)
			if err != nil || got.Executable {
				t.Fatalf("错误计划未阻止：%+v %v", got, err)
			}
			issues := append(got.Issues, got.Units[0].Issues...)
			for _, issue := range issues {
				if issue.Code == tc.code {
					return
				}
			}
			t.Fatalf("期望阻止原因 %s，实际 %+v", tc.code, issues)
		})
	}
}

func TestReferencePlanQuoteReadsPersistentDraftAndRejectsStaleRevision(t *testing.T) {
	db, drafts, projectID, referenceID, referenceRevision := draftDatabase(t)
	req := draftRequest()
	req.ReferenceRevision = referenceRevision
	draft, err := drafts.Create(t.Context(), 7, projectID, referenceID, req)
	if err != nil {
		t.Fatal(err)
	}
	draftID, _ := idcodec.Decode(draft.ID)
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	snap := &provider.Snapshot{Model: provider.ModelSnapshot{Key: "model", Kind: "video",
		Capabilities: modelcfg.Capabilities{Ops: []string{"t2v"}, Prompt: modelcfg.PromptSpec{MaxLength: 2000},
			Refs: modelcfg.Refs{}, Params: modelcfg.ParamSet{{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Open: true, Default: 5}}}},
		Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 7}}, Plugin: provider.PluginSnapshot{Key: "simple-video"}}
	service := service.NewDramaReferencePlanService(service.DramaReferencePlanDeps{Repo: repository.NewDramaReferenceDraftRepository(db), Assets: assets, Registry: referencePlanRegistry{snapshot: snap}})
	quote := &model.QuoteDramaReferenceDraftReq{Revision: draft.Revision, ModelID: "model", Units: []model.DramaReferencePlanUnitReq{{ShotID: "1", Mode: "text"}}}
	first, err := service.Quote(t.Context(), 7, projectID, referenceID, draftID, quote)
	if err != nil || !first.Executable || first.TotalVideoCredits != 7 {
		t.Fatalf("持久草稿报价失败：%+v %v", first, err)
	}
	if _, err := service.Quote(t.Context(), 8, projectID, referenceID, draftID, quote); err == nil || !errors.Is(err, errcode.ErrDramaNotFound) {
		t.Fatalf("其他用户应无法报价草稿：%v", err)
	}
	quote.Revision++
	if _, err := service.Quote(t.Context(), 7, projectID, referenceID, draftID, quote); err == nil || !errors.Is(err, errcode.ErrCanvasConflict) {
		t.Fatalf("旧草稿修订应冲突：%v", err)
	}
}

func TestReferencePlanQuoteRealLoginRoute(t *testing.T) {
	db, draftService, projectID, referenceID, referenceRevision := draftDatabase(t)
	req := draftRequest()
	req.ReferenceRevision = referenceRevision
	draft, err := draftService.Create(t.Context(), 7, projectID, referenceID, req)
	if err != nil {
		t.Fatal(err)
	}
	draftID, _ := idcodec.Decode(draft.ID)
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	snap := &provider.Snapshot{Model: provider.ModelSnapshot{Key: "model", Kind: "video", Capabilities: modelcfg.Capabilities{
		Ops: []string{"t2v"}, Prompt: modelcfg.PromptSpec{MaxLength: 2000},
		Params: modelcfg.ParamSet{{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Open: true, Default: 5}}},
	}, Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 7}}, Plugin: provider.PluginSnapshot{Key: "simple-video"}}
	quoteService := service.NewDramaReferencePlanService(service.DramaReferencePlanDeps{Repo: repository.NewDramaReferenceDraftRepository(db), Assets: assets, Registry: referencePlanRegistry{snapshot: snap}})
	const secret = "reference-plan-http-test-secret"
	token, _, err := utils.GenerateToken(7, "plan-owner", 1, secret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	routes := router.New("test", secret, router.Handlers{Health: &handler.HealthHandler{},
		DramaReferencePlan: handler.NewDramaReferencePlanHandler(quoteService),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Role: "user", Status: "active", TokenVersion: 1}, nil
		}})
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	path := fmt.Sprintf("%s/api/v1/dramas/%s/references/%s/drafts/%s/quote", server.URL,
		idcodec.Encode(projectID), idcodec.Encode(referenceID), idcodec.Encode(draftID))
	body := `{"revision":1,"model_id":"model","units":[{"shot_id":"1","mode":"text"}]}`
	status, _ := postReferencePlan(t, server.Client(), path, "", body)
	if status != http.StatusUnauthorized {
		t.Fatalf("报价需要登录：%d", status)
	}
	status, result := postReferencePlan(t, server.Client(), path, token, body)
	if status != http.StatusOK || !result.Data.Executable || result.Data.TotalVideoCredits != 7 || len(result.Data.Units) != 1 {
		t.Fatalf("登录后的报价响应错误：status=%d response=%+v", status, result)
	}
}

// postReferencePlan performs one authenticated or anonymous quote request through the registered router.
func postReferencePlan(t *testing.T, client *http.Client, path, token, body string) (int, struct {
	Data model.DramaReferencePlan `json:"data"`
}) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	result := struct {
		Data model.DramaReferencePlan `json:"data"`
	}{}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, result
}
