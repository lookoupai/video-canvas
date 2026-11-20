package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/service"
)

type remakeFailingProcessor struct {
	remakeRecordingProcessor
	fail bool
}

func (p *remakeFailingProcessor) ComposeRanges(ctx context.Context, windows []videoframe.Window, path string, mute bool) error {
	if p.fail {
		return errors.New("转码暂时失败")
	}
	return p.remakeRecordingProcessor.ComposeRanges(ctx, windows, path, mute)
}

// successfulRemakeTake 保留真实意图与成功任务的关联，模拟供应商已返回当前用户的视频。
func successfulRemakeTake(t *testing.T, env *mktEnv, planID uint64, shotID int, assetID uint64) uint64 {
	t.Helper()
	node := fmt.Sprintf("mkt-shot:%s:%d", idcodec.Encode(planID), shotID)
	key := fmt.Sprintf("fixture-%d-%d", planID, shotID)
	outputs, err := json.Marshal([]model.TaskOutput{{AssetID: assetID, MediaType: model.KindVideo}})
	if err != nil {
		t.Fatal(err)
	}
	task := &model.GenerationTask{UserID: env.user, NodeID: node, Kind: model.KindVideo, Status: model.TaskSucceeded, IdempotencyKey: key, InputJSON: []byte("{}"), ConfigSnapshot: []byte("{}"), OutputJSON: outputs}
	if err := env.db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	intent := &model.MarketingRemakeIntent{UserID: env.user, PlanID: planID, ShotID: shotID, RequestKey: key, TaskKey: key, NodeID: node, Revision: 1, Input: []byte("{}"), ShotSnapshot: []byte("{}")}
	if err := env.db.Create(intent).Error; err != nil {
		t.Fatal(err)
	}
	env.assets.put(env.user, assetID, model.KindVideo, "video/mp4", 5000)
	return task.ID
}

func TestMarketing_RemakeCompositionWorkerUsesSnapshotAndRetries(t *testing.T) {
	env := newMktEnv(t, 300)
	_, round := env.refRound()
	plan, err := env.svc.CreateRemakePlan(t.Context(), env.user, uint64(round.Scripts[0].ID), &model.CreateRemakePlanReq{Revision: round.Scripts[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	planID := uint64(plan.ID)
	taskID := successfulRemakeTake(t, env, planID, 3, 6000)
	proc := &remakeFailingProcessor{}
	files := &remakeAssetFiles{mktAssets: env.assets}
	svc := service.NewMarketingService(service.MarketingDeps{Repo: env.repo, Models: env.models, Assets: files, RemakeAssets: files, RemakeComposer: proc})
	start := 400
	plan, err = svc.UpdateRemakeShot(t.Context(), env.user, planID, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, ChosenTaskID: &taskID, TrimStartMs: &start})
	if err != nil {
		t.Fatal(err)
	}
	req := &model.ComposeRemakePlanReq{Revision: plan.Revision, ShotIDs: []int{3}}
	submitted, err := svc.ComposeRemakePlan(t.Context(), env.user, planID, "first", req)
	if err != nil {
		t.Fatal(err)
	}
	// 提交后修改裁剪窗口，排队中的合成仍采用最初的 0.4–2.9 秒。
	start, duration := 1000, 1500
	_, err = svc.UpdateRemakeShot(t.Context(), env.user, planID, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, TrimStartMs: &start, DurationMs: &duration})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(submitted.ID)
	proc.fail = true
	if err := svc.HandleRemakeComposition(t.Context(), env.user, planID, id, 1); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.RemakeCompositions(t.Context(), env.user, planID)
	if err != nil || len(rows) != 1 || rows[0].Status != "failed" || rows[0].Asset != nil {
		t.Fatalf("失败记录: %+v %v", rows, err)
	}
	retry, err := svc.ComposeRemakePlan(t.Context(), env.user, planID, "first", req)
	if err != nil || retry.ID != submitted.ID || retry.Status != "queued" {
		t.Fatalf("原快照重试: %+v %v", retry, err)
	}
	proc.fail = false
	if err := svc.HandleRemakeComposition(t.Context(), env.user, planID, id, 1); err != nil || files.prepared != 0 {
		t.Fatalf("旧尝试号应忽略: prepared=%d %v", files.prepared, err)
	}
	for range 2 {
		if err := svc.HandleRemakeComposition(t.Context(), env.user, planID, id, 2); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = svc.RemakeCompositions(t.Context(), env.user, planID)
	if err != nil || len(rows) != 1 || rows[0].Status != "succeeded" || rows[0].Asset == nil || files.prepared != 1 || len(proc.windows) != 1 {
		t.Fatalf("成功结果重复登记: %+v prepared=%d windows=%+v %v", rows, files.prepared, proc.windows, err)
	}
	if got := proc.windows[0][0]; got.StartMs != 400 || got.EndMs != 2900 || proc.mute[0] {
		t.Fatalf("应保留提交时的带声裁剪窗口: %+v", got)
	}
}
