package service_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/service"
)

type remakeRecordingProcessor struct {
	windows [][]videoframe.Window
	mute    []bool
}

func (p *remakeRecordingProcessor) ComposeRanges(_ context.Context, w []videoframe.Window, dst string, mute bool) error {
	p.windows = append(p.windows, slices.Clone(w))
	p.mute = append(p.mute, mute)
	return os.WriteFile(dst, []byte("prepared-video"), 0600)
}

type remakeAssetFiles struct {
	*mktAssets
	prepared int
}

func (a *remakeAssetFiles) PrepareDramaVideo(_ context.Context, u uint64, path string) (*model.Asset, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	a.prepared++
	id := uint64(1000 + a.prepared)
	a.put(u, id, model.KindVideo, "video/mp4", 5000)
	return a.items[id].asset, nil
}
func (a *remakeAssetFiles) DiscardCopies(_ context.Context, rows []*model.Asset) {
	for _, row := range rows {
		delete(a.items, row.ID)
	}
}
func (a *remakeAssetFiles) ViewOf(ctx context.Context, row *model.Asset) (*model.AssetView, error) {
	return a.View(ctx, row.UserID, row.ID)
}

func TestMarketing_RemakeMediaPreparation(t *testing.T) {
	env := newMktEnv(t, 300)
	_, round := env.refRound()
	plan, err := env.svc.CreateRemakePlan(t.Context(), env.user, uint64(round.Scripts[0].ID), &model.CreateRemakePlanReq{Revision: round.Scripts[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	proc := &remakeRecordingProcessor{}
	files := &remakeAssetFiles{mktAssets: env.assets}
	env.svc = service.NewMarketingService(service.MarketingDeps{Repo: env.repo, Models: env.models, Assets: files, Tasks: env.taskSvc, RemakeAssets: files, RemakeComposer: proc})
	planID := uint64(plan.ID)
	sources := []model.MarketingRemakeSource{plan.SourceCatalog[2], plan.SourceCatalog[0]}
	plan, err = env.svc.UpdateRemakeShot(t.Context(), env.user, planID, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, Sources: &sources})
	if err != nil {
		t.Fatal(err)
	}
	req := &model.GenerateRemakeShotReq{Revision: plan.Shots[2].Revision}
	var task *model.GenerationTaskView
	for i := 1; i <= 2; i++ {
		task, err = env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 3, fmt.Sprintf("intent-%d", i), req)
		if err != nil {
			t.Fatal(err)
		}
		env.assets.put(env.user, 6000, "video", "video/mp4", 5000)
		var row model.GenerationTask
		if err := env.db.Take(&row, task.ID).Error; err != nil {
			t.Fatal(err)
		}
		env.finishTask(row, model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 6000, MediaType: model.KindVideo, URL: "https://files.test/k6000"}})
	}
	if len(proc.windows) != 1 || len(proc.windows[0]) != 2 || !proc.mute[0] || proc.windows[0][0].StartMs != 5000 || proc.windows[0][0].EndMs != 7500 || proc.windows[0][1].StartMs != 0 || proc.windows[0][1].EndMs != 2500 || proc.windows[0][0].Path != proc.windows[0][1].Path {
		t.Fatalf("原镜独立有序静音区间应只生成一次: %+v %v", proc.windows, proc.mute)
	}
	start := 400
	plan, err = env.svc.UpdateRemakeShot(t.Context(), env.user, planID, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, ChosenTaskID: &task.ID, TrimStartMs: &start})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := env.svc.ComposeRemakePlan(t.Context(), env.user, planID, "compose", &model.ComposeRemakePlanReq{Revision: plan.Revision, ShotIDs: []int{3}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(rec.ID)
	// 成片执行时会再次确认采用素材的所有者；即使 ID 已快照也不能跨用户读取。
	env.assets.items[6000].asset.UserID++
	if err := env.svc.HandleRemakeComposition(t.Context(), env.user, planID, id, 1); err != nil {
		t.Fatal(err)
	}
	jobs, err := env.svc.RemakeCompositions(t.Context(), env.user, planID)
	if err != nil || len(jobs) != 1 || jobs[0].Status != "failed" || len(proc.windows) != 1 {
		t.Fatalf("越权素材应失败: %+v %v", jobs, err)
	}
	env.assets.items[6000].asset.UserID = env.user
	_, err = env.svc.ComposeRemakePlan(t.Context(), env.user, planID, "compose", &model.ComposeRemakePlanReq{Revision: rec.Revision, ShotIDs: rec.ShotIDs})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.svc.HandleRemakeComposition(t.Context(), env.user, planID, id, 2); err != nil {
		t.Fatal(err)
	}
	jobs, err = env.svc.RemakeCompositions(t.Context(), env.user, planID)
	if err != nil || len(jobs) != 1 || jobs[0].Status != "succeeded" || jobs[0].Asset == nil || len(proc.windows) != 2 || proc.mute[1] || proc.windows[1][0].StartMs != 400 || proc.windows[1][0].EndMs != 2900 {
		t.Fatalf("采用素材带声裁切: %+v, %+v, %v", jobs, proc.windows, err)
	}
}
