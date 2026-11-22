package service_test

import (
	"bytes"
	"image/png"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func TestReferencePreparationRealFramesAndReplay(t *testing.T) {
	r := newPreparationRig(t)
	req := &model.CreateDramaReferencePreparationReq{RequestKey: "first", ReferenceRevision: r.revision}
	created, err := r.svc.Create(t.Context(), 7, r.project, r.reference, req)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(created.ID)
	if created.Status != "queued" || created.HasAudio != nil {
		t.Fatalf("初始状态：%+v", created)
	}
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, created.Attempt); err != nil {
		t.Fatal(err)
	}
	got, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || got.Status != "succeeded" || got.CompletedFrames != 12 || got.HasAudio == nil || *got.HasAudio || got.AudioStatus != "unavailable" {
		t.Fatalf("持久准备结果：%+v %v", got, err)
	}
	seen, cursor := map[string]bool{}, ""
	for {
		page, err := r.svc.Frames(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceFramesReq{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range page.Items {
			if seen[frame.ID] || frame.AtMs < frame.StartMs || frame.AtMs >= frame.EndMs || frame.SourceAssetID != 1 || frame.Asset == nil {
				t.Fatalf("帧证据：%+v", frame)
			}
			seen[frame.ID] = true
			r.assets.mu.Lock()
			data := append([]byte(nil), r.assets.assets.items[frame.Asset.ID].data...)
			r.assets.mu.Unlock()
			if _, err := png.Decode(bytes.NewReader(data)); err != nil {
				t.Fatalf("不是持久帧图：%v", err)
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 12 {
		t.Fatalf("分页丢失帧：%d", len(seen))
	}
	req.RequestKey = "another-client"
	ret, err := r.svc.Create(t.Context(), 7, r.project, r.reference, req)
	if err != nil || ret.ID != created.ID {
		t.Fatalf("同recipe没有复用：%+v %v", ret, err)
	}
	req.ShotIDs = []string{"1"}
	_, err = r.svc.Create(t.Context(), 7, r.project, r.reference, req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	shots, err := r.svc.Shots(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceDraftsReq{Limit: 2})
	if err != nil || len(shots.Items) != 2 || shots.NextCursor == "" {
		t.Fatalf("镜头分页：%+v %v", shots, err)
	}
}

func TestReferencePreparationConcurrentCreateAndTrustedDraft(t *testing.T) {
	r := newPreparationRig(t)
	ids, errs := make(chan string, 8), make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := r.svc.Create(t.Context(), 7, r.project, r.reference, &model.CreateDramaReferencePreparationReq{RequestKey: "concurrent", ReferenceRevision: r.revision, ShotIDs: []string{"1"}})
			if err != nil {
				errs <- err
				return
			}
			ids <- got.ID
		})
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	idText := ""
	for id := range ids {
		if idText != "" && id != idText {
			t.Fatal("创建出现重复准备")
		}
		idText = id
	}
	id, _ := idcodec.Decode(idText)
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	page, err := r.svc.Frames(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceFramesReq{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	frame := page.Items[0]
	drafts := service.NewDramaReferenceDraftService(service.DramaReferenceDraftDeps{Repo: repository.NewDramaReferenceDraftRepository(r.db), Assets: r.assets, Preparations: r.svc})
	req := &model.CreateDramaReferenceDraftReq{RequestKey: "trusted", ReferenceRevision: r.revision, PreparationID: idText,
		Evidence: []model.DramaReferenceEvidence{{ID: "proof", ShotID: "1", AssetID: frame.Asset.ID, AtMs: frame.AtMs, PreparationFrameID: frame.ID}}}
	draft, err := drafts.Create(t.Context(), 7, r.project, r.reference, req)
	if err != nil || draft.PreparationID != idText || len(draft.Document.Shots) != 4 {
		t.Fatalf("关联草稿：%+v %v", draft, err)
	}
	req.RequestKey, req.Evidence[0].AtMs = "forged", 1000
	_, err = drafts.Create(t.Context(), 7, r.project, r.reference, req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	if len(r.queue.attempts) != 1 {
		t.Fatalf("队列重复提交：%v", r.queue.attempts)
	}
}
