package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func TestDramaReferenceAdoptionPersistsTwoEpisodes(t *testing.T) {
	db := mktDB(t)
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaSegment{}, &model.DramaJob{}, &model.DramaReferenceState{}, &model.DramaReferenceEpisode{}, &model.DramaReferenceRole{}, &model.DramaReferenceAdoption{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewDramaReferenceRepository(db)
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	svc := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repo, Assets: assets})
	created, err := svc.CreateProject(t.Context(), 7, &model.CreateDramaReferenceProjectReq{Title: "两集参考", RequestKey: "project"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := idcodec.Decode(created.ID)
	canvas, _ := idcodec.Decode(created.CanvasID)
	payload := []byte(`{"nodes":[{"id":"new-role","type":"card","data":{"drama":{"type":"character","name":"新人"}}}],"connections":[]}`)
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", canvas).Update("payload_json", payload).Error; err != nil {
		t.Fatal(err)
	}
	role := model.DramaReferenceRole{UserID: 7, DramaID: p, Name: "全剧身份", TargetNodeID: "new-role", Revision: 1}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	doc := referenceDocument()
	document := []byte(mustJSONText(doc))
	bindings := []byte(mustJSONText(map[string]string{"a": idcodec.Encode(role.ID), "b": idcodec.Encode(role.ID)}))
	refs := []model.DramaReferenceEpisode{{UserID: 7, DramaID: p, AssetID: 1, Seq: 1, Title: "第一集", RequestKey: "ref1", Revision: 2, Status: "ready", DurationMs: 5000, Document: document, Bindings: bindings}, {UserID: 7, DramaID: p, AssetID: 1, Seq: 2, Title: "第二集", RequestKey: "ref2", Revision: 2, Status: "ready", DurationMs: 5000, Document: document, Bindings: bindings}}
	if err := db.Create(&refs).Error; err != nil {
		t.Fatal(err)
	}
	first, err := svc.Adopt(t.Context(), 7, p, refs[0].ID, &model.AdoptDramaReferenceReq{Revision: 2, RequestKey: "first", Mode: "follow"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Adopt(t.Context(), 7, p, refs[1].ID, &model.AdoptDramaReferenceReq{Revision: 2, RequestKey: "second", Mode: "follow"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		duplicate, duplicateErr := svc.Adopt(t.Context(), 7, p, refs[0].ID, &model.AdoptDramaReferenceReq{Revision: 2, RequestKey: "another-click", Mode: "follow"})
		if duplicateErr != nil || duplicate.ID != first.ID {
			t.Fatalf("持久提交键别名 %+v %v", duplicate, duplicateErr)
		}
	}
	_, err = svc.Adopt(t.Context(), 7, p, refs[0].ID, &model.AdoptDramaReferenceReq{Revision: 2, RequestKey: "another-click", Mode: "rewrite"})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	firstID, _ := idcodec.Decode(first.EpisodeID)
	otherID, _ := idcodec.Decode(second.EpisodeID)
	dtx := repo.DramaTx()
	before, err := dtx.Segments(t.Context(), 7, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 || before[0].Seq != 0 || before[1].Seq != 1 {
		t.Fatalf("真实片场镜序应从0开始：%+v", before)
	}
	other, err := dtx.Segments(t.Context(), 7, otherID)
	if err != nil {
		t.Fatal(err)
	}
	chosen := uint64(911)
	if err := db.Model(&model.DramaSegment{}).Where("id = ?", before[1].ID).Update("chosen_task_id", chosen).Error; err != nil {
		t.Fatal(err)
	}
	doc.Shots[1].Action = "关门后离开"
	refs[0].Document = []byte(mustJSONText(doc))
	refs[0].Revision = 3
	if err := repo.SaveEpisode(t.Context(), &refs[0]); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Adopt(t.Context(), 7, p, refs[0].ID, &model.AdoptDramaReferenceReq{Revision: 3, RequestKey: "update", Mode: "follow", TargetEpisodeNo: first.EpisodeNo, EpisodeRevision: first.EpisodeRevision, ShotIDs: []string{"2"}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := dtx.Segments(t.Context(), 7, firstID)
	if err != nil {
		t.Fatal(err)
	}
	otherAfter, err := dtx.Segments(t.Context(), 7, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Revision != before[0].Revision || after[0].Prompt != before[0].Prompt || after[1].ChosenTaskID == nil || *after[1].ChosenTaskID != chosen || !strings.Contains(after[1].Prompt, "关门") || otherAfter[0].Revision != other[0].Revision {
		t.Fatalf("更新污染其他片段：%+v %+v", after, otherAfter)
	}
	var remake model.DramaSegmentRemake
	if err := json.Unmarshal(after[1].Remake, &remake); err != nil {
		t.Fatal(err)
	}
	if remake.ReferenceRevision != 3 || remake.TargetDurationMs != 2500 || updated.EpisodeRevision != first.EpisodeRevision+1 {
		t.Fatalf("持久快照不符 %+v %+v", remake, updated)
	}
	repeated, err := svc.Adopt(t.Context(), 7, p, refs[0].ID, &model.AdoptDramaReferenceReq{Revision: 3, RequestKey: "update", Mode: "follow", TargetEpisodeNo: first.EpisodeNo, EpisodeRevision: first.EpisodeRevision, ShotIDs: []string{"2"}})
	if err != nil || repeated.ID != updated.ID {
		t.Fatalf("过时修订的网络重传未复用 %+v %v", repeated, err)
	}
	task := model.GenerationTask{UserID: 7, CanvasProjectID: &canvas, NodeID: "seg:" + idcodec.Encode(after[1].ID), Kind: model.KindVideo, Status: model.TaskRunning, IdempotencyKey: "running", InputJSON: []byte("{}"), ConfigSnapshot: []byte("{}")}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.Adopt(t.Context(), 7, p, refs[0].ID, &model.AdoptDramaReferenceReq{Revision: 3, RequestKey: "busy", Mode: "follow", TargetEpisodeNo: first.EpisodeNo, EpisodeRevision: updated.EpisodeRevision, ShotIDs: []string{"2"}})
	wantMktCode(t, err, errcode.ErrDramaJobBusy.Code)
	if err := db.Delete(&model.DramaEpisode{}, firstID).Error; err != nil {
		t.Fatal(err)
	}
	adoptions, err := svc.Adoptions(t.Context(), 7, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, adoption := range adoptions {
		if adoption.EpisodeID == first.EpisodeID && !adoption.Deleted {
			t.Fatalf("删除分集后丢失历史状态 %+v", adoption)
		}
	}
	_, err = svc.Adopt(t.Context(), 8, p, refs[0].ID, &model.AdoptDramaReferenceReq{Revision: 3, RequestKey: "foreign", Mode: "follow"})
	wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
}

func TestDramaReferenceAdoptionRewriteKeepsUnchangedBasis(t *testing.T) {
	svc, r := newAdoptionMemory(t)
	got, err := svc.Adopt(t.Context(), 7, 4, 3, &model.AdoptDramaReferenceReq{Revision: 2, Mode: "rewrite", RequestKey: "rewrite", Instruction: "改写为职场悬疑"})
	if err != nil {
		t.Fatal(err)
	}
	if got.EpisodeNo != 1 || len(got.SegmentIDs) != 0 || r.drama.episodes[0].LockedAt != nil || r.drama.episodes[0].ScriptStatus != "empty" {
		t.Fatalf("原创改编没有进入审核流程 %+v", got)
	}
	r.roles[0].TargetNodeID = "changed"
	r.row.Document = []byte(`{}`)
	var basis model.DramaReferenceBasis
	if err := json.Unmarshal(r.drama.episodes[0].ReferenceBasis, &basis); err != nil {
		t.Fatal(err)
	}
	if basis.Instruction != "改写为职场悬疑" || basis.Roles[0].TargetNodeID != "target-a" || len(basis.Document.Shots) != 2 {
		t.Fatalf("原稿修改改变已采用依据 %+v", basis)
	}
}
