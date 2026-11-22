package service_test

import (
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/reference"
	"video-canvas/internal/service"
)

func TestReferencePreparationAnalyzesWithoutChangingReference(t *testing.T) {
	analyzer := reference.NewAnalyzer()
	if !analyzer.Available() {
		t.Skip("需要 rea-video")
	}
	r := newPreparationRig(t)
	if err := r.db.Model(&model.DramaReferenceEpisode{}).Where("id = ?", r.reference).Update("document", []byte(`{"scenes":[],"characters":[],"shots":[],"dialogues":[]}`)).Error; err != nil {
		t.Fatal(err)
	}
	r.svc = service.NewDramaReferencePreparationService(service.DramaReferencePreparationDeps{Repo: r.repo, Assets: r.assets, Store: r.assets, Frames: r.frames, Analyzer: analyzer})
	id := makePreparation(t, r, "analyze")
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	ready, err := r.svc.Get(t.Context(), 7, r.project, r.reference, id)
	if err != nil || ready.Status != "succeeded" || ready.CompletedFrames != 3 || ready.ShotCount != 1 {
		t.Fatalf("真实分析：%+v %v", ready, err)
	}
	snapshot, err := r.svc.DraftSnapshot(t.Context(), 7, r.project, r.reference, id)
	if err != nil {
		t.Fatal(err)
	}
	var current model.DramaReferenceEpisode
	if err := r.db.First(&current, r.reference).Error; err != nil {
		t.Fatal(err)
	}
	if current.Revision != r.revision || string(current.Document) == string(snapshot.Document) {
		t.Fatal("准备覆盖了正式文档")
	}
}
