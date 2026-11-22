package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/service"
)

func TestReferenceDraftGenerateResumesFromStableSegmentIntent(t *testing.T) {
	generator := &referenceDraftGenerator{fail: true}
	rig := referenceApplyRigWithGenerator(t, generator)
	ctx := t.Context()
	plan, err := rig.plans.Quote(ctx, 7, rig.projectID, rig.referenceID, rig.draftID, rig.quote)
	if err != nil || !plan.Executable {
		t.Fatalf("报价失败：%+v %v", plan, err)
	}
	adopted, err := rig.plans.Apply(ctx, 7, rig.projectID, rig.referenceID, rig.draftID, &model.ApplyDramaReferenceDraftReq{
		Revision: rig.quote.Revision, RequestKey: "generate-apply", Mode: "follow", PlanDigest: plan.PlanDigest, Quote: *rig.quote,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := &model.DramaReferenceGenerateReq{Revision: adopted.DraftRevision, RequestKey: "generate-batch",
		ApplicationID: adopted.ApplicationID, PlanDigest: plan.PlanDigest, Quote: *rig.quote}

	first, err := rig.plans.Generate(ctx, 7, rig.projectID, rig.referenceID, rig.draftID, req)
	if err != nil || first.Status != "partial" || first.Items[0].TaskID != "" || first.Items[0].Error == "" {
		t.Fatalf("批次应保留单镜失败：%+v %v", first, err)
	}
	generator.fail = false
	second, err := rig.plans.Generate(ctx, 7, rig.projectID, rig.referenceID, rig.draftID, req)
	if err != nil || !second.Replayed || second.Status != "submitted" || second.Items[0].TaskID != "task-42" {
		t.Fatalf("重传应恢复原批次：%+v %v", second, err)
	}
	if len(generator.keys) != 2 || generator.keys[0] == "" || generator.keys[0] != generator.keys[1] {
		t.Fatalf("重试生成改变了单镜幂等键：%v", generator.keys)
	}
	rig.snapshot.Model.Pricing.Unit++
	third, err := rig.plans.Generate(ctx, 7, rig.projectID, rig.referenceID, rig.draftID, req)
	if err != nil || !third.Replayed || third.Items[0].TaskID != second.Items[0].TaskID || len(generator.keys) != 2 {
		t.Fatalf("已提交的批次应返回原任务：%+v %v", third, err)
	}
	req.RequestKey = "fresh-charge"
	_, err = rig.plans.Generate(ctx, 7, rig.projectID, rig.referenceID, rig.draftID, req)
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
}

func TestReferenceAgnesClipIsValidatedBeforeCreditsFreeze(t *testing.T) {
	tests := []struct {
		name        string
		clip        service.ReferenceVideoMetadata
		includeClip bool
		wantFrozen  int
	}{
		{name: "valid clip", clip: service.ReferenceVideoMetadata{DurationMs: 2500, ByteSize: 1 << 20, FrameRate: 30}, includeClip: true, wantFrozen: 4},
		{name: "too short", clip: service.ReferenceVideoMetadata{DurationMs: 1999, ByteSize: 1 << 20, FrameRate: 30}, includeClip: true},
		{name: "too large", clip: service.ReferenceVideoMetadata{DurationMs: 2500, ByteSize: (50 << 20) + 1, FrameRate: 30}, includeClip: true},
		{name: "low frame rate", clip: service.ReferenceVideoMetadata{DurationMs: 2500, ByteSize: 1 << 20, FrameRate: 23.9}, includeClip: true},
		{name: "high frame rate", clip: service.ReferenceVideoMetadata{DurationMs: 2500, ByteSize: 1 << 20, FrameRate: 60.1}, includeClip: true},
		{name: "clip missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { testAgnesReferenceClip(t, test) })
	}
}

func testAgnesReferenceClip(t *testing.T, test struct {
	name        string
	clip        service.ReferenceVideoMetadata
	includeClip bool
	wantFrozen  int
}) {
	t.Helper()
	env, rig, probe := prepareAgnesReferenceTaskRig(t, test)
	plan, err := rig.plans.Quote(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, rig.quote)
	if err != nil || !plan.Executable {
		t.Fatalf("视频报价失败：%+v %v", plan, err)
	}
	applied := applyReferencePlanForGeneration(t, rig, plan.PlanDigest)
	result, err := rig.plans.Generate(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, &model.DramaReferenceGenerateReq{
		Revision: applied.DraftRevision, RequestKey: "clip-" + test.name, ApplicationID: applied.ApplicationID, PlanDigest: plan.PlanDigest, Quote: *rig.quote,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantProbeCalls := 0
	if test.includeClip {
		wantProbeCalls = 1
	}
	if len(probe.assetIDs) != wantProbeCalls || test.includeClip && probe.assetIDs[0] != 2 {
		t.Fatalf("应检查实际生成的裁片素材：%+v", probe.assetIDs)
	}
	if test.wantFrozen > 0 {
		if result.Status != "submitted" || env.repo.credits[7].Frozen != test.wantFrozen {
			t.Fatalf("有效裁片应提交且冻结报价积分：result=%+v credit=%+v probe=%+v", result, env.repo.credits[7], probe)
		}
		return
	}
	if result.Status != "partial" || result.Items[0].TaskID != "" || env.repo.credits[7].Frozen != 0 {
		t.Fatalf("不合规裁片不能提交或冻结积分：result=%+v credit=%+v", result, env.repo.credits[7])
	}
}

func prepareAgnesReferenceTaskRig(t *testing.T, test struct {
	name        string
	clip        service.ReferenceVideoMetadata
	includeClip bool
	wantFrozen  int
}) (*taskSvcEnv, referenceApplyRigType, *referenceVideoClipProbe) {
	t.Helper()
	env := newTaskSvcEnv(defaultTaskLimits())
	env.repo.addCredit(7, 50, 0)
	probe := &referenceVideoClipProbe{metadata: test.clip}
	generator := &referenceTaskGenerator{env: env, includeClip: test.includeClip}
	rig := referenceApplyRigWithGenerator(t, generator)
	rig.quote = planRequest("video")
	rig.snapshot.Plugin.Key = "agnes-video"
	rig.snapshot.Model.UpstreamModel = "agnes-video-2.5"
	rig.snapshot.Model.Capabilities.Ops = []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni}
	rig.snapshot.Model.Capabilities.Refs.Video = modelcfg.RefSpec{On: true, Max: 1, MaxMB: 50}
	env.registry.snap = rig.snapshot
	env.assets.items[2] = &model.Asset{ID: 2, UserID: 7, Kind: "video", ByteSize: 1 << 20}
	env.svc = newTaskSvcWithVideoProbe(env, probe)
	return env, rig, probe
}

func newTaskSvcWithVideoProbe(env *taskSvcEnv, probe service.ReferenceVideoMetadataProbe) *service.GenerationTaskService {
	return service.NewGenerationTaskService(service.GenerationTaskDeps{
		Repo: env.repo, Registry: env.registry, Executor: env.exec, Assets: env.assets,
		VideoProbe: probe, Broadcaster: env.bc, Limits: defaultTaskLimits(),
	}, service.WithTaskClock(func() time.Time { return env.now }))
}

func applyReferencePlanForGeneration(t *testing.T, rig referenceApplyRigType, digest string) *model.DramaReferenceApplyResponse {
	t.Helper()
	applied, err := rig.plans.Apply(t.Context(), 7, rig.projectID, rig.referenceID, rig.draftID, &model.ApplyDramaReferenceDraftReq{
		Revision: rig.quote.Revision, RequestKey: "clip-apply", Mode: "follow", PlanDigest: digest, Quote: *rig.quote,
	})
	if err != nil {
		t.Fatal(err)
	}
	return applied
}

type referenceVideoClipProbe struct {
	metadata service.ReferenceVideoMetadata
	assetIDs []uint64
}

func (p *referenceVideoClipProbe) Probe(_ context.Context, userID, assetID uint64, _ int64) (service.ReferenceVideoMetadata, error) {
	if userID != 7 {
		return service.ReferenceVideoMetadata{}, errors.New("unexpected probe user")
	}
	p.assetIDs = append(p.assetIDs, assetID)
	return p.metadata, nil
}

type referenceTaskGenerator struct {
	env         *taskSvcEnv
	includeClip bool
}

func (g *referenceTaskGenerator) Generate(ctx context.Context, userID, _ uint64, key string, req *model.GenerateDramaSegmentReq) (*model.GenerationTaskView, error) {
	input := make(map[string]any, len(req.Input)+1)
	for name, value := range req.Input {
		input[name] = value
	}
	if g.includeClip {
		input["videos"] = []any{"2"}
	}
	created, err := g.env.svc.Create(ctx, userID, key, &model.CreateGenerationTaskReq{
		Kind: model.KindVideo, ModelID: req.ModelID, CanvasID: idcodec.ID(9), NodeID: "reference-segment", Input: input,
	})
	if err != nil {
		return nil, err
	}
	if len(created.Items) != 1 || created.Items[0].Task == nil {
		return nil, errors.New("reference segment task was not created")
	}
	return created.Items[0].Task, nil
}

type referenceDraftGenerator struct {
	fail bool
	keys []string
}

func (g *referenceDraftGenerator) Generate(_ context.Context, _, _ uint64, key string, _ *model.GenerateDramaSegmentReq) (*model.GenerationTaskView, error) {
	g.keys = append(g.keys, key)
	if g.fail {
		return nil, errors.New("simulated interruption")
	}
	return &model.GenerationTaskView{ID: 42, TaskRef: "task-42", Status: "queued"}, nil
}
