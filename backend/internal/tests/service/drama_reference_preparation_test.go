package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type preparationMemoryRepo struct {
	service.DramaReferencePreparationRepo
	ref model.DramaReferenceEpisode
}

func (r *preparationMemoryRepo) Reference(_ context.Context, u, p, ref uint64, _ bool) (*model.DramaReferenceEpisode, error) {
	if u != 7 || p != 4 || ref != 3 {
		return nil, repository.ErrNotFound
	}
	row := r.ref
	return &row, nil
}

func (r *preparationMemoryRepo) Request(_ context.Context, _, _, _ uint64, _ string) (*model.DramaReferencePreparationRequest, error) {
	return nil, repository.ErrNotFound
}

func TestReferencePreparationRejectsInvalidCreate(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		user, project, reference uint64
		req                      *model.CreateDramaReferencePreparationReq
		code                     int
	}{
		{"缺请求", 7, 4, 3, nil, 10001},
		{"缺键", 7, 4, 3, &model.CreateDramaReferencePreparationReq{ReferenceRevision: 2}, 10001},
		{"缺修订", 7, 4, 3, &model.CreateDramaReferencePreparationReq{RequestKey: "key"}, 10001},
		{"重复镜", 7, 4, 3, &model.CreateDramaReferencePreparationReq{RequestKey: "key", ReferenceRevision: 2, ShotIDs: []string{"1", "1"}}, 10001},
		{"未知镜", 7, 4, 3, &model.CreateDramaReferencePreparationReq{RequestKey: "key", ReferenceRevision: 2, ShotIDs: []string{"missing"}}, 10001},
		{"旧修订", 7, 4, 3, &model.CreateDramaReferencePreparationReq{RequestKey: "key", ReferenceRevision: 1}, 30002},
		{"越权", 8, 4, 3, &model.CreateDramaReferencePreparationReq{RequestKey: "key", ReferenceRevision: 2}, 67001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := json.Marshal(referenceDocument())
			if err != nil {
				t.Fatal(err)
			}
			repo := &preparationMemoryRepo{ref: model.DramaReferenceEpisode{ID: 3, UserID: 7, DramaID: 4,
				AssetID: 1, DurationMs: 5000, Revision: 2, Document: doc}}
			assets := &mktAssets{items: map[uint64]*mktAsset{}}
			assets.put(7, 1, "video", "video/mp4", 5000)
			svc := service.NewDramaReferencePreparationService(service.DramaReferencePreparationDeps{Repo: repo, Assets: assets})
			_, err = svc.Create(t.Context(), tc.user, tc.project, tc.reference, tc.req)
			wantMktCode(t, err, tc.code)
		})
	}
}

func TestReferencePreparationMissingRuntimeIsBusinessError(t *testing.T) {
	doc, err := json.Marshal(referenceDocument())
	if err != nil {
		t.Fatal(err)
	}
	repo := &preparationMemoryRepo{ref: model.DramaReferenceEpisode{ID: 3, UserID: 7, DramaID: 4,
		AssetID: 1, DurationMs: 5000, Revision: 2, Document: doc}}
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	svc := service.NewDramaReferencePreparationService(service.DramaReferencePreparationDeps{Repo: repo, Assets: assets})
	_, err = svc.Create(t.Context(), 7, 4, 3, &model.CreateDramaReferencePreparationReq{RequestKey: "key", ReferenceRevision: 2})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}
