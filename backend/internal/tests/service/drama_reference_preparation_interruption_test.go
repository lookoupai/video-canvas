package service_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/service"
)

type interruptibleFrames struct {
	service.DramaReferenceFrames
	calls   int
	blocked chan struct{}
}

func (f *interruptibleFrames) Frame(ctx context.Context, path string, at float64, opts videoframe.FrameOpts) ([]byte, error) {
	f.calls++
	if f.calls == 2 {
		close(f.blocked)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.DramaReferenceFrames.Frame(ctx, path, at, opts)
}

func TestReferencePreparationRestartContinuesSameAttempt(t *testing.T) {
	r := newPreparationRig(t)
	frames := &interruptibleFrames{DramaReferenceFrames: r.frames, blocked: make(chan struct{})}
	r.svc = preparationWithFrames(r, frames)
	id := makePreparation(t, r, "restart")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.svc.HandlePreparation(ctx, 7, r.project, r.reference, id, 1) }()
	select {
	case <-frames.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("第二帧未启动")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("未将中断交给队列重试")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("中断未退出")
	}
	interrupted, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || interrupted.Status != "running" || interrupted.CompletedFrames != 1 {
		t.Fatalf("重启前状态：%+v %v", interrupted, err)
	}
	freshFrames := &controlledFrames{DramaReferenceFrames: r.frames}
	fresh := preparationWithFrames(r, freshFrames)
	if err := fresh.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	got, err := fresh.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || got.Status != "succeeded" || got.Attempt != 1 || freshFrames.calls != 2 {
		t.Fatalf("重启重复处理：%+v calls=%d %v", got, freshFrames.calls, err)
	}
}

func TestReferencePreparationInvalidSupplementAndPagination(t *testing.T) {
	r := newPreparationRig(t)
	id := makePreparation(t, r, "limits")
	if _, err := r.svc.Cancel(t.Context(), 7, r.project, r.reference, id); err != nil {
		t.Fatal(err)
	}
	for _, req := range []*model.AddDramaReferenceFramesReq{
		nil, {RequestKey: "empty", Frames: []model.DramaReferenceFramePoint{}},
		{RequestKey: "duplicate", Frames: []model.DramaReferenceFramePoint{{ShotID: "1", AtMs: 10}, {ShotID: "1", AtMs: 10}}},
		{RequestKey: "outside", Frames: []model.DramaReferenceFramePoint{{ShotID: "1", AtMs: 2500}}},
		{RequestKey: "unselected", Frames: []model.DramaReferenceFramePoint{{ShotID: "2", AtMs: 3000}}},
	} {
		if _, err := r.svc.AddFrames(t.Context(), 7, r.project, r.reference, id, req); err == nil {
			t.Fatalf("错误补帧被接受：%+v", req)
		}
	}
	if _, err := r.svc.Shots(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceDraftsReq{Limit: 101}); err == nil {
		t.Fatal("允许超限分页")
	}
	if _, err := r.svc.Frames(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceFramesReq{Limit: 20, ShotID: "2"}); err == nil {
		t.Fatal("允许未选镜头筛选")
	}
}
