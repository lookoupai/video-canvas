package service_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type preparationAssets struct {
	mu     sync.Mutex
	assets *mktAssets
	keys   map[string]uint64
	next   uint64
}

func (a *preparationAssets) Get(ctx context.Context, u, id uint64) (*model.Asset, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.assets.Get(ctx, u, id)
}
func (a *preparationAssets) View(ctx context.Context, u, id uint64) (*model.AssetView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.assets.View(ctx, u, id)
}
func (a *preparationAssets) Open(ctx context.Context, u, id uint64) (*provider.AssetFile, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	asset, err := a.assets.Get(ctx, u, id)
	if err != nil {
		return nil, err
	}
	return &provider.AssetFile{Asset: asset, Body: io.NopCloser(bytes.NewReader(a.assets.items[id].data))}, nil
}
func (a *preparationAssets) FileURL(key string) string { return "https://frames.test/" + key }
func (a *preparationAssets) SaveReferenceFrame(_ context.Context, u, prep, frame uint64, _ string, data []byte) (*model.AssetView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := fmt.Sprintf("%d/%d/%d", u, prep, frame)
	if id := a.keys[key]; a.assets.items[id] != nil {
		a.assets.items[id].data = append([]byte(nil), data...)
		return a.assets.View(context.Background(), u, id)
	}
	a.next++
	id := a.next
	a.assets.put(u, id, "image", "image/png", 0)
	a.assets.items[id].data = append([]byte(nil), data...)
	a.keys[key] = id
	return a.assets.View(context.Background(), u, id)
}

type preparationQueue struct {
	mu       sync.Mutex
	attempts []int
}

func (q *preparationQueue) EnqueueDramaReferencePreparationTx(_ context.Context, _ *sql.Tx, _, _, _, _ uint64, attempt int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.attempts = append(q.attempts, attempt)
	return nil
}

type preparationRig struct {
	db                 *gorm.DB
	svc                *service.DramaReferencePreparationService
	repo               *repository.DramaReferencePreparationRepository
	assets             *preparationAssets
	frames             *videoframe.Extractor
	queue              *preparationQueue
	project, reference uint64
	revision           int64
}

func newPreparationRig(t *testing.T) *preparationRig {
	t.Helper()
	db := mktDB(t)
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.DramaProject{}, &model.DramaReferenceState{},
		&model.DramaReferenceEpisode{}, &model.DramaReferenceRevision{}, &model.DramaReferenceRole{}, &model.DramaReferenceDraft{},
		&model.DramaReferencePreparation{}, &model.DramaReferencePreparationRequest{}, &model.DramaReferencePreparationFrame{}, &model.DramaReferenceFrameCache{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../marketing/samples/cream_ref.mp4")
	if err != nil {
		t.Fatal(err)
	}
	a := &preparationAssets{assets: &mktAssets{items: map[uint64]*mktAsset{}}, keys: map[string]uint64{}, next: 10}
	a.assets.put(7, 1, "video", "video/mp4", 10000)
	a.assets.items[1].data = data
	reference := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repository.NewDramaReferenceRepository(db), Assets: a})
	project, err := reference.CreateProject(t.Context(), 7, &model.CreateDramaReferenceProjectReq{Title: "媒体证据", RequestKey: "prep-project"})
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := idcodec.Decode(project.ID)
	ref, err := reference.AddEpisode(t.Context(), 7, pid, &model.AddDramaReferenceReq{AssetID: 1, RequestKey: "prep-reference"})
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := idcodec.Decode(ref.ID)
	doc := model.DramaReferenceDocument{Scenes: []model.DramaReferenceScene{{ID: "scene", Title: "商品"}}}
	for i := range 4 {
		doc.Shots = append(doc.Shots, model.DramaReferenceShot{ID: fmt.Sprint(i + 1), SceneID: "scene", StartMs: int64(i) * 2500, EndMs: int64(i+1) * 2500})
	}
	ref, err = reference.UpdateEpisode(t.Context(), 7, pid, rid, &model.UpdateDramaReferenceReq{Revision: ref.Revision, Document: &doc})
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewDramaReferencePreparationRepository(db)
	q := &preparationQueue{}
	repo.SetEnqueuer(q)
	frames := videoframe.New(videoframe.Options{})
	if !frames.Available() {
		t.Skip("需要 ffmpeg 和 ffprobe")
	}
	svc := service.NewDramaReferencePreparationService(service.DramaReferencePreparationDeps{Repo: repo, Assets: a, Frames: frames, Store: a})
	return &preparationRig{db: db, svc: svc, repo: repo, assets: a, frames: frames, queue: q, project: pid, reference: rid, revision: ref.Revision}
}
