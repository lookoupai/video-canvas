package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type controlledFrames struct {
	service.DramaReferenceFrames
	calls   int
	failAt  int
	entered chan struct{}
}

func (f *controlledFrames) Frame(ctx context.Context, path string, at float64, opts videoframe.FrameOpts) ([]byte, error) {
	f.calls++
	if f.entered != nil {
		close(f.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.failAt == f.calls {
		return nil, errors.New("test interrupted media")
	}
	return f.DramaReferenceFrames.Frame(ctx, path, at, opts)
}
func preparationWithFrames(r *preparationRig, frames service.DramaReferenceFrames) *service.DramaReferencePreparationService {
	return service.NewDramaReferencePreparationService(service.DramaReferencePreparationDeps{Repo: r.repo, Assets: r.assets, Store: r.assets, Frames: frames})
}
func makePreparation(t *testing.T, r *preparationRig, key string) uint64 {
	t.Helper()
	view, err := r.svc.Create(t.Context(), 7, r.project, r.reference, &model.CreateDramaReferencePreparationReq{RequestKey: key, ReferenceRevision: r.revision, ShotIDs: []string{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(view.ID)
	return id
}

func TestReferencePreparationKeepsProgressAfterFailure(t *testing.T) {
	r := newPreparationRig(t)
	frames := &controlledFrames{DramaReferenceFrames: r.frames, failAt: 2}
	r.svc = preparationWithFrames(r, frames)
	id := makePreparation(t, r, "failure")
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	failed, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || failed.Status != "failed" || failed.CompletedFrames != 1 {
		t.Fatalf("保留进度：%+v %v", failed, err)
	}
	resumed, err := r.svc.Resume(t.Context(), 7, r.project, r.reference, id, &model.ResumeDramaReferencePreparationReq{RequestKey: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	frames.failAt = 0
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, resumed.Attempt); err != nil {
		t.Fatal(err)
	}
	got, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || got.Status != "succeeded" || got.CompletedFrames != 3 || frames.calls != 4 {
		t.Fatalf("恢复重复提帧：%+v calls=%d %v", got, frames.calls, err)
	}
}

func TestReferencePreparationCancelStopsInFlightFrame(t *testing.T) {
	r := newPreparationRig(t)
	frames := &controlledFrames{DramaReferenceFrames: r.frames, entered: make(chan struct{})}
	r.svc = preparationWithFrames(r, frames)
	id := makePreparation(t, r, "cancel-running")
	done := make(chan error, 1)
	go func() { done <- r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1) }()
	select {
	case <-frames.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker未开始提帧")
	}
	if _, err := r.svc.Cancel(t.Context(), 7, r.project, r.reference, id); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消未中止在途调用")
	}
	got, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || got.Status != "canceled" || got.CompletedFrames != 0 {
		t.Fatalf("取消结果：%+v %v", got, err)
	}
}

func TestReferencePreparationReusesFramesAcrossRevision(t *testing.T) {
	r := newPreparationRig(t)
	frames := &controlledFrames{DramaReferenceFrames: r.frames}
	r.svc = preparationWithFrames(r, frames)
	first := makePreparation(t, r, "original")
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, first, 1); err != nil {
		t.Fatal(err)
	}
	referenceSvc := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repository.NewDramaReferenceRepository(r.db), Assets: r.assets})
	title := "改名不改变镜窗"
	changed, err := referenceSvc.UpdateEpisode(t.Context(), 7, r.project, r.reference, &model.UpdateDramaReferenceReq{Revision: r.revision, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	r.revision = changed.Revision
	second := makePreparation(t, r, "new-revision")
	if first == second {
		t.Fatal("基准修订被混用")
	}
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, second, 1); err != nil {
		t.Fatal(err)
	}
	if frames.calls != 3 {
		t.Fatalf("跨修订有效帧未复用：%d", frames.calls)
	}
	page, err := r.svc.Frames(t.Context(), 7, r.project, r.reference, second, &model.ListDramaReferenceFramesReq{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	r.assets.mu.Lock()
	r.assets.assets.items[page.Items[0].Asset.ID].data = []byte("corrupt")
	r.assets.mu.Unlock()
	page, err = r.svc.Frames(t.Context(), 7, r.project, r.reference, second, &model.ListDramaReferenceFramesReq{Limit: 20})
	if err != nil || page.Items[0].Status != "missing" {
		t.Fatalf("缺文件显示：%+v %v", page, err)
	}
	resumed, err := r.svc.Resume(t.Context(), 7, r.project, r.reference, second, &model.ResumeDramaReferencePreparationReq{RequestKey: "repair"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, second, resumed.Attempt); err != nil {
		t.Fatal(err)
	}
	if frames.calls != 4 {
		t.Fatalf("补失效帧处理了有效帧：%d", frames.calls)
	}
}
