package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type referenceMemoryRepo struct {
	service.DramaReferenceRepo
	row      model.DramaReferenceEpisode
	versions []model.DramaReferenceRevision
}

func TestDramaReferenceTitleBeforeImportKeepsEmptyDocument(t *testing.T) {
	repo := &referenceMemoryRepo{row: model.DramaReferenceEpisode{ID: 3, UserID: 7, DramaID: 4, AssetID: 1, DurationMs: 5000, Revision: 2, Status: "idle", Bindings: []byte("{}")}}
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	svc := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repo, Assets: assets})
	title := "改名后待拆解"
	got, err := svc.UpdateEpisode(t.Context(), 7, 4, 3, &model.UpdateDramaReferenceReq{Revision: 2, Title: &title})
	if err != nil || got.Document != nil || len(got.Versions) != 0 || got.Title != title {
		t.Fatalf("未导入不应生成拆解修订 %+v %v", got, err)
	}
}

func (r *referenceMemoryRepo) WithTx(_ context.Context, fn func(repository.DramaReferenceTx) error) error {
	return fn(r)
}
func (r *referenceMemoryRepo) Project(_ context.Context, u, p uint64, _ bool) (*model.DramaProject, error) {
	if u != 7 || p != 4 {
		return nil, repository.ErrNotFound
	}
	return &model.DramaProject{BaseModel: model.BaseModel{ID: 4}, UserID: 7}, nil
}
func (r *referenceMemoryRepo) Episode(_ context.Context, u, p, id uint64) (*model.DramaReferenceEpisode, error) {
	if u != 7 || p != 4 || id != 3 {
		return nil, repository.ErrNotFound
	}
	row := r.row
	return &row, nil
}
func (r *referenceMemoryRepo) Roles(_ context.Context, _, _ uint64) ([]model.DramaReferenceRole, error) {
	return []model.DramaReferenceRole{{ID: 8, UserID: 7, DramaID: 4}}, nil
}
func (r *referenceMemoryRepo) SaveEpisode(_ context.Context, row *model.DramaReferenceEpisode) error {
	r.row = *row
	return nil
}
func (r *referenceMemoryRepo) InsertRevision(_ context.Context, row *model.DramaReferenceRevision) error {
	r.versions = append(r.versions, *row)
	return nil
}
func (r *referenceMemoryRepo) Versions(_ context.Context, _, _ uint64) ([]model.DramaReferenceRevision, error) {
	return r.versions, nil
}

func TestDramaReferenceEditRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*model.UpdateDramaReferenceReq, *referenceMemoryRepo)
		code   int
	}{
		{"成功保存跨镜对白", func(req *model.UpdateDramaReferenceReq, _ *referenceMemoryRepo) {
			req.Document.Dialogues[0].Speaker = "b"
		}, 0},
		{"旧修订", func(req *model.UpdateDramaReferenceReq, _ *referenceMemoryRepo) { req.Revision = 1 }, errcode.ErrCanvasConflict.Code},
		{"同集分析中", func(_ *model.UpdateDramaReferenceReq, r *referenceMemoryRepo) { r.row.Status = "queued" }, errcode.ErrDramaJobBusy.Code},
		{"未知角色", func(req *model.UpdateDramaReferenceReq, _ *referenceMemoryRepo) {
			req.Document.Dialogues[0].Speaker = "unknown"
		}, errcode.ErrInvalidParams.Code},
		{"对白时间越界", func(req *model.UpdateDramaReferenceReq, _ *referenceMemoryRepo) {
			req.Document.Dialogues[0].EndMs = 6000
		}, errcode.ErrInvalidParams.Code},
		{"跨项目角色绑定", func(req *model.UpdateDramaReferenceReq, _ *referenceMemoryRepo) {
			req.Bindings = map[string]string{"a": idcodec.Encode(999)}
		}, errcode.ErrInvalidParams.Code},
		{"未知本集角色绑定", func(req *model.UpdateDramaReferenceReq, _ *referenceMemoryRepo) {
			req.Bindings = map[string]string{"unknown": idcodec.Encode(8)}
		}, errcode.ErrInvalidParams.Code},
		{"空更新", func(req *model.UpdateDramaReferenceReq, _ *referenceMemoryRepo) {
			req.Document = nil
			req.Bindings = nil
		}, errcode.ErrInvalidParams.Code},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := referenceDocument()
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			repo := &referenceMemoryRepo{row: model.DramaReferenceEpisode{ID: 3, UserID: 7, DramaID: 4, AssetID: 1, DurationMs: 5000, Revision: 2, Status: "ready", Document: raw, Bindings: []byte("{}")}}
			assets := &mktAssets{items: map[uint64]*mktAsset{}}
			assets.put(7, 1, "video", "video/mp4", 5000)
			svc := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repo, Assets: assets})
			req := &model.UpdateDramaReferenceReq{Revision: 2, Document: &doc, Bindings: map[string]string{"a": idcodec.Encode(8)}}
			tc.mutate(req, repo)
			got, err := svc.UpdateEpisode(t.Context(), 7, 4, 3, req)
			if tc.code != 0 {
				wantMktCode(t, err, tc.code)
				return
			}
			if err != nil || got.Revision != 3 || got.Document.Dialogues[0].Speaker != "b" || len(got.Versions) != 1 {
				t.Fatalf("保存结果 %+v %v", got, err)
			}
		})
	}
}
