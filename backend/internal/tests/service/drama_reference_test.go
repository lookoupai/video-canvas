package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type referenceQueue struct{ attempts []int }

func (q *referenceQueue) EnqueueDramaReferenceTx(_ context.Context, _ *sql.Tx, _, _, _ uint64, attempt int) error {
	q.attempts = append(q.attempts, attempt)
	return nil
}

func referenceDocument() model.DramaReferenceDocument {
	return model.DramaReferenceDocument{
		Scenes:     []model.DramaReferenceScene{{ID: "room", Title: "客厅"}},
		Characters: []model.DramaReferenceCharacter{{ID: "a", Name: "阿青"}, {ID: "b", Name: "小林"}},
		Shots:      []model.DramaReferenceShot{{ID: "1", SceneID: "room", StartMs: 0, EndMs: 2500, Action: "递出钥匙", Characters: []string{"a", "b"}}, {ID: "2", SceneID: "room", StartMs: 2500, EndMs: 5000, Action: "接过钥匙", Characters: []string{"b"}}},
		Dialogues:  []model.DramaReferenceDialogue{{ID: "d1", StartMs: 1000, EndMs: 3500, Text: "钥匙交给你", Speaker: "a"}, {ID: "d2", StartMs: 3600, EndMs: 4500, Text: "好", Speaker: "b"}},
	}
}

func TestDramaReferenceTwoEpisodesKeepCorrections(t *testing.T) {
	db := mktDB(t)
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.DramaProject{}, &model.DramaReferenceState{}, &model.DramaReferenceEpisode{}, &model.DramaReferenceRevision{}, &model.DramaReferenceRole{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewDramaReferenceRepository(db)
	queue := &referenceQueue{}
	repo.SetEnqueuer(queue)
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	assets.put(7, 2, "video", "video/mp4", 5000)
	parser := func(raw []byte, _ int64) (model.DramaReferenceDocument, error) {
		if string(raw) == "bad" {
			return model.DramaReferenceDocument{}, errors.New("invalid package")
		}
		return referenceDocument(), nil
	}
	svc := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repo, Assets: assets, Decode: parser})
	project, err := svc.CreateProject(t.Context(), 7, &model.CreateDramaReferenceProjectReq{Title: "钥匙", RequestKey: "project"})
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := idcodec.Decode(project.ID)
	canvasID, _ := idcodec.Decode(project.CanvasID)
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", canvasID).Update("payload_json", []byte(`{"nodes":[{"id":"new-qing","type":"card","data":{"kind":"character","label":"新阿青","drama":{"type":"character","name":"新阿青"}}}],"connections":[]}`)).Error; err != nil {
		t.Fatal(err)
	}
	first, err := svc.AddEpisode(t.Context(), 7, pid, &model.AddDramaReferenceReq{AssetID: 1, Title: "第一集", RequestKey: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AddEpisode(t.Context(), 7, pid, &model.AddDramaReferenceReq{AssetID: 2, Title: "第二集", RequestKey: "two"})
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := idcodec.Decode(first.ID)
	secondID, _ := idcodec.Decode(second.ID)
	_, err = svc.Import(t.Context(), 7, pid, firstID, first.Revision, []byte("good"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleDramaReference(t.Context(), 7, pid, firstID, 1); err != nil {
		t.Fatal(err)
	}
	first, err = svc.GetEpisode(t.Context(), 7, pid, firstID, 0)
	if err != nil {
		t.Fatal(err)
	}
	role, err := svc.CreateRole(t.Context(), 7, pid, &model.SaveDramaReferenceRoleReq{Name: "新阿青", TargetNodeID: "new-qing"})
	if err != nil {
		t.Fatal(err)
	}
	doc := *first.Document
	doc.Dialogues[0].Text = "我把钥匙交给你"
	first, err = svc.UpdateEpisode(t.Context(), 7, pid, firstID, &model.UpdateDramaReferenceReq{Revision: first.Revision, Document: &doc, Bindings: map[string]string{"a": role.ID}})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := json.Marshal(first.Document)
	_, err = svc.Import(t.Context(), 7, pid, secondID, second.Revision, []byte("bad"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleDramaReference(t.Context(), 7, pid, secondID, 1); err != nil {
		t.Fatal(err)
	}
	second, err = svc.GetEpisode(t.Context(), 7, pid, secondID, 0)
	if err != nil || second.Status != "failed" {
		t.Fatalf("第二集失败 %+v %v", second, err)
	}
	_, err = svc.Import(t.Context(), 7, pid, secondID, second.Revision, []byte("good"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleDramaReference(t.Context(), 7, pid, secondID, 2); err != nil {
		t.Fatal(err)
	}
	second, err = svc.GetEpisode(t.Context(), 7, pid, secondID, 0)
	if err != nil {
		t.Fatal(err)
	}
	corrected := *second.Document
	corrected.Dialogues[1].Speaker = "a"
	second, err = svc.UpdateEpisode(t.Context(), 7, pid, secondID, &model.UpdateDramaReferenceReq{Revision: second.Revision, Document: &corrected, Bindings: map[string]string{"a": role.ID}})
	if err != nil {
		t.Fatal(err)
	}
	firstAgain, err := svc.GetEpisode(t.Context(), 7, pid, firstID, 0)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(firstAgain.Document)
	if string(saved) != string(actual) || firstAgain.Bindings["a"] != second.Bindings["a"] {
		t.Fatalf("重试破坏其他集 %+v", firstAgain)
	}
	if role.TargetNodeID != "new-qing" {
		t.Fatalf("没有映射真实项目角色 %+v", role)
	}
	_, err = svc.CreateRole(t.Context(), 7, pid, &model.SaveDramaReferenceRoleReq{Name: "外部角色", TargetNodeID: "foreign"})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	history, err := svc.GetEpisode(t.Context(), 7, pid, firstID, 3)
	if err != nil || history.Document.Dialogues[0].Text != "钥匙交给你" {
		t.Fatalf("历史原稿 %+v %v", history, err)
	}
	collection, err := svc.Collection(t.Context(), 7, pid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Import(t.Context(), 7, pid, firstID, firstAgain.Revision, []byte("good"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.OrderEpisodes(t.Context(), 7, pid, &model.OrderDramaReferencesReq{Revision: collection.Revision, IDs: []string{second.ID, first.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleDramaReference(t.Context(), 7, pid, firstID, 2); err != nil {
		t.Fatal(err)
	}
	after, err := svc.GetEpisode(t.Context(), 7, pid, firstID, 0)
	if err != nil || after.Status != "ready" || after.Seq != 2 || after.Bindings["a"] != role.ID {
		t.Fatalf("排序破坏导入 %+v %v", after, err)
	}
	oldEdited, err := svc.GetEpisode(t.Context(), 7, pid, firstID, first.Revision)
	if err != nil || oldEdited.Document.Dialogues[0].Text != "我把钥匙交给你" {
		t.Fatalf("重新分析丢失校订版本 %+v %v", oldEdited, err)
	}
	_, err = svc.GetEpisode(t.Context(), 8, pid, firstID, 0)
	wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
	_, err = svc.UpdateEpisode(t.Context(), 7, pid, firstID, &model.UpdateDramaReferenceReq{Revision: 1, Document: &doc})
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
}
