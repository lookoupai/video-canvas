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

type draftMemoryRepo struct {
	service.DramaReferenceDraftRepo
	ref  model.DramaReferenceEpisode
	rows map[uint64]model.DramaReferenceDraft
}

func newDraftService(t *testing.T) (*service.DramaReferenceDraftService, *draftMemoryRepo) {
	t.Helper()
	doc := referenceDocument()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	repo := &draftMemoryRepo{ref: model.DramaReferenceEpisode{ID: 3, UserID: 7, DramaID: 4,
		AssetID: 1, DurationMs: 5000, Revision: 2, Status: "ready", Document: raw}, rows: map[uint64]model.DramaReferenceDraft{}}
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	assets.put(7, 2, "image", "image/png", 0)
	assets.put(8, 3, "image", "image/png", 0)
	return service.NewDramaReferenceDraftService(service.DramaReferenceDraftDeps{Repo: repo, Assets: assets}), repo
}

func draftRequest() *model.CreateDramaReferenceDraftReq {
	return &model.CreateDramaReferenceDraftReq{RequestKey: "first", ReferenceRevision: 2,
		Instruction: "换成自己的商品", Prompts: []model.DramaReferencePrompt{{ShotID: "1", Prompt: "手自然递出钥匙"}},
		Evidence: []model.DramaReferenceEvidence{{ID: "f1", ShotID: "1", AssetID: 2, AtMs: 500}},
		Observations: []model.DramaReferenceObservation{{ShotID: "1", Field: "action", Source: "observation",
			ReviewState: "observed", EvidenceIDs: []string{"f1"}}}}
}

func TestReferenceDraftCreateKeepsSourceAndReplays(t *testing.T) {
	svc, repo := newDraftService(t)
	before := string(repo.ref.Document)
	req := draftRequest()
	got, err := svc.Create(t.Context(), 7, 4, 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "external" || got.Status != "editing" || got.Revision != 1 || got.BaseRevision != 2 {
		t.Fatalf("初始草稿状态错误：%+v", got)
	}
	if got.Coverage.Shots != "complete" || got.Coverage.Visual != "not_analyzed" || got.Coverage.Dialogue != "not_analyzed" {
		t.Fatalf("离线 ready 不代表语义完成：%+v", got.Coverage)
	}
	if string(repo.ref.Document) != before || got.Prompts[0].Prompt != req.Prompts[0].Prompt {
		t.Fatal("草稿未保存或原参考稿被覆盖")
	}
	repo.ref.Revision++
	ret, err := svc.Create(t.Context(), 7, 4, 3, req)
	if err != nil || ret.ID != got.ID || len(repo.rows) != 1 {
		t.Fatalf("已成功创建应可跨后续参考修订恢复：%+v %v", ret, err)
	}
	req.Instruction = "另一种意图"
	_, err = svc.Create(t.Context(), 7, 4, 3, req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}

func TestReferenceDraftAgentSourceRecordsSessionAndRun(t *testing.T) {
	svc, repo := newDraftService(t)
	req := draftRequest()
	req.RequestKey = "agent-source"
	got, err := svc.CreateForAgent(t.Context(), 7, 4, 3, 55, 66, req)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(got.ID)
	row := repo.rows[id]
	if got.Source != "agent" || row.SessionID != 55 || row.RunID != 66 {
		t.Fatalf("Agent 来源缺失可信会话信息：view=%+v row=%+v", got, row)
	}

	updated := "Agent 续跑后的修订"
	got, err = svc.UpdateForAgent(t.Context(), 7, 4, 3, id, 77, 88, &model.UpdateDramaReferenceDraftReq{Revision: 1, Instruction: &updated})
	if err != nil || got.Source != "agent" || repo.rows[id].SessionID != 77 || repo.rows[id].RunID != 88 {
		t.Fatalf("Agent 修改来源未更新：view=%+v row=%+v err=%v", got, repo.rows[id], err)
	}
}

func TestReferenceDraftUpdateChecksRevisionAndMergedEvidence(t *testing.T) {
	svc, _ := newDraftService(t)
	created, err := svc.Create(t.Context(), 7, 4, 3, draftRequest())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(created.ID)
	text := "用户亲自改好的要求"
	got, err := svc.Update(t.Context(), 7, 4, 3, id, &model.UpdateDramaReferenceDraftReq{Revision: 1, Instruction: &text})
	if err != nil || got.Revision != 2 || got.Source != "manual" || got.Instruction != text {
		t.Fatalf("保存用户修改失败：%+v %v", got, err)
	}
	old := "迟到的 Agent 修改"
	_, err = svc.Update(t.Context(), 7, 4, 3, id, &model.UpdateDramaReferenceDraftReq{Revision: 1, Instruction: &old})
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
	empty := []model.DramaReferenceEvidence{}
	_, err = svc.Update(t.Context(), 7, 4, 3, id, &model.UpdateDramaReferenceDraftReq{Revision: 2, Evidence: &empty})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	fresh, err := svc.Get(t.Context(), 7, 4, 3, id)
	if err != nil || fresh.Instruction != text || len(fresh.Evidence) != 1 || fresh.Revision != 2 {
		t.Fatalf("失败更新破坏草稿：%+v %v", fresh, err)
	}
}

func TestReferenceDraftEmptySourceStaysUnanalyzed(t *testing.T) {
	svc, repo := newDraftService(t)
	repo.ref.Document = nil
	got, err := svc.Create(t.Context(), 7, 4, 3, &model.CreateDramaReferenceDraftReq{RequestKey: "empty", ReferenceRevision: 2})
	if err != nil || len(got.Document.Shots) != 0 || got.Coverage.Shots != "not_analyzed" {
		t.Fatalf("空参考集草稿：%+v %v", got, err)
	}
	id, _ := idcodec.Decode(got.ID)
	doc := referenceDocument()
	got, err = svc.Update(t.Context(), 7, 4, 3, id, &model.UpdateDramaReferenceDraftReq{Revision: 1, Document: &doc})
	if err != nil || len(got.Document.Shots) != 2 {
		t.Fatalf("补充分析文档失败：%+v %v", got, err)
	}
}

func (r *draftMemoryRepo) WithTx(_ context.Context, fn func(repository.DramaReferenceDraftTx) error) error {
	return fn(r)
}
func (r *draftMemoryRepo) Reference(_ context.Context, u, p, ref uint64, _ bool) (*model.DramaReferenceEpisode, error) {
	if u != 7 || p != 4 || ref != 3 {
		return nil, repository.ErrNotFound
	}
	out := r.ref
	return &out, nil
}
func (r *draftMemoryRepo) ByKey(_ context.Context, u, p, ref uint64, key string) (*model.DramaReferenceDraft, error) {
	for _, row := range r.rows {
		if row.UserID == u && row.DramaID == p && row.ReferenceID == ref && row.RequestKey == key {
			return &row, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *draftMemoryRepo) Draft(_ context.Context, u, p, ref, id uint64, _ bool) (*model.DramaReferenceDraft, error) {
	row, ok := r.rows[id]
	if !ok || row.UserID != u || row.DramaID != p || row.ReferenceID != ref {
		return nil, repository.ErrNotFound
	}
	return &row, nil
}
func (r *draftMemoryRepo) Save(_ context.Context, row *model.DramaReferenceDraft) error {
	if row.ID == 0 {
		row.ID = uint64(len(r.rows) + 1)
	}
	r.rows[row.ID] = *row
	return nil
}
