package service_test

import (
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

func TestReferencePreparationCancelResumeAndSupplement(t *testing.T) {
	r := newPreparationRig(t)
	made, err := r.svc.Create(t.Context(), 7, r.project, r.reference, &model.CreateDramaReferencePreparationReq{RequestKey: "cancel", ReferenceRevision: r.revision, ShotIDs: []string{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(made.ID)
	canceled, err := r.svc.Cancel(t.Context(), 7, r.project, r.reference, id)
	if err != nil || canceled.Status != "canceled" {
		t.Fatalf("取消：%+v %v", canceled, err)
	}
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	resumed, err := r.svc.Resume(t.Context(), 7, r.project, r.reference, id, &model.ResumeDramaReferencePreparationReq{RequestKey: "resume"})
	if err != nil || resumed.Attempt != 2 {
		t.Fatalf("恢复：%+v %v", resumed, err)
	}
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 2); err != nil {
		t.Fatal(err)
	}
	req := &model.AddDramaReferenceFramesReq{RequestKey: "more", Frames: []model.DramaReferenceFramePoint{{ShotID: "1", AtMs: 1000}}}
	more, err := r.svc.AddFrames(t.Context(), 7, r.project, r.reference, id, req)
	if err != nil || more.TotalFrames != 4 || more.Attempt != 3 {
		t.Fatalf("补帧：%+v %v", more, err)
	}
	replay, err := r.svc.AddFrames(t.Context(), 7, r.project, r.reference, id, req)
	if err != nil || replay.Attempt != 3 {
		t.Fatalf("补帧重传：%+v %v", replay, err)
	}
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 3); err != nil {
		t.Fatal(err)
	}
	ready, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || ready.CompletedFrames != 4 {
		t.Fatalf("补帧完成：%+v %v", ready, err)
	}
	req.RequestKey = "outside"
	req.Frames[0].AtMs = 2500
	_, err = r.svc.AddFrames(t.Context(), 7, r.project, r.reference, id, req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	if len(r.queue.attempts) != 3 {
		t.Fatalf("重复入队：%v", r.queue.attempts)
	}
}
