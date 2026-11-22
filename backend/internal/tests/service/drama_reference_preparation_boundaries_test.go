package service_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type unavailableFrameAssets struct{ *preparationAssets }

func (a unavailableFrameAssets) Open(ctx context.Context, u, id uint64) (*provider.AssetFile, error) {
	if id != 1 {
		return nil, errors.New("storage temporary unavailable")
	}
	return a.preparationAssets.Open(ctx, u, id)
}

func TestReferencePreparationStorageFailureDoesNotReextract(t *testing.T) {
	r := newPreparationRig(t)
	id := makePreparation(t, r, "store-error")
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	unavailable := unavailableFrameAssets{r.assets}
	svc := service.NewDramaReferencePreparationService(service.DramaReferencePreparationDeps{Repo: r.repo, Assets: unavailable, Store: r.assets, Frames: r.frames})
	_, err := svc.Resume(t.Context(), 7, r.project, r.reference, id, &model.ResumeDramaReferencePreparationReq{RequestKey: "repair-error"})
	if err == nil {
		t.Fatal("存储故障被当成缺失而重新入队")
	}
	got, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || got.Attempt != 1 || got.Status != "succeeded" {
		t.Fatalf("故障改变了准备：%+v %v", got, err)
	}
}

func TestReferencePreparationVeryShortShotIsDeduplicated(t *testing.T) {
	r := newPreparationRig(t)
	svc := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repository.NewDramaReferenceRepository(r.db), Assets: r.assets})
	doc := model.DramaReferenceDocument{Scenes: []model.DramaReferenceScene{{ID: "scene", Title: "短镜"}}, Shots: []model.DramaReferenceShot{{ID: "1", SceneID: "scene", StartMs: 0, EndMs: 100}}}
	updated, err := svc.UpdateEpisode(t.Context(), 7, r.project, r.reference, &model.UpdateDramaReferenceReq{Revision: r.revision, Document: &doc})
	if err != nil {
		t.Fatal(err)
	}
	r.revision = updated.Revision
	id := makePreparation(t, r, "short")
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	page, err := r.svc.Frames(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceFramesReq{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].AtMs != 50 || page.Items[0].Status != "ready" {
		t.Fatalf("短镜采样：%+v %v", page, err)
	}
}

func TestReferencePreparationAudioIsPresentButNotAnalyzed(t *testing.T) {
	r := newPreparationRig(t)
	file := filepath.Join(t.TempDir(), "audio.mp4")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=red:s=32x32:d=10:r=10", "-f", "lavfi", "-i", "sine=frequency=440:duration=10", "-c:v", "libx264", "-c:a", "aac", "-pix_fmt", "yuv420p", "-shortest", file}
	if out, err := exec.CommandContext(t.Context(), "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("有音轨样片：%v %s", err, out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	r.assets.assets.items[1].data = data
	id := makePreparation(t, r, "audio")
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	got, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || got.HasAudio == nil || !*got.HasAudio || got.AudioStatus != "not_analyzed" {
		t.Fatalf("音轨不能当做语义完成：%+v %v", got, err)
	}
}

func TestReferencePreparationRejectsForgedWindowAndForeignAccess(t *testing.T) {
	r := newPreparationRig(t)
	id := makePreparation(t, r, "proof")
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	page, err := r.svc.Frames(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceFramesReq{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	frame := page.Items[0]
	content := model.DramaReferenceDraftContent{Document: model.DramaReferenceDocument{Shots: []model.DramaReferenceShot{{ID: "1", StartMs: 0, EndMs: 2600}}}, Evidence: []model.DramaReferenceEvidence{{ID: "proof", ShotID: "1", AtMs: frame.AtMs, AssetID: frame.Asset.ID, PreparationFrameID: frame.ID}}}
	err = r.svc.ValidateEvidence(t.Context(), 7, r.project, r.reference, id, content)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	for _, who := range []struct{ u, p, ref uint64 }{{8, r.project, r.reference}, {7, r.project + 1, r.reference}, {7, r.project, r.reference + 1}} {
		_, err := r.svc.Get(t.Context(), who.u, who.p, who.ref, id)
		wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
	}
	drafts := service.NewDramaReferenceDraftService(service.DramaReferenceDraftDeps{Repo: repository.NewDramaReferenceDraftRepository(r.db), Assets: r.assets, Preparations: r.svc})
	_, err = drafts.Create(t.Context(), 7, r.project, r.reference, &model.CreateDramaReferenceDraftReq{RequestKey: "no-prep", ReferenceRevision: r.revision, Evidence: content.Evidence})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}
