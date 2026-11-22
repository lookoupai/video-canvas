package service_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func draftDatabase(t *testing.T) (*gorm.DB, *service.DramaReferenceDraftService, uint64, uint64, int64) {
	t.Helper()
	db := mktDB(t)
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.DramaProject{}, &model.DramaReferenceState{},
		&model.DramaReferenceEpisode{}, &model.DramaReferenceRevision{}, &model.DramaReferenceRole{}, &model.DramaReferenceDraft{}); err != nil {
		t.Fatal(err)
	}
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	assets.put(7, 2, "image", "image/png", 0)
	referenceSvc := service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: repository.NewDramaReferenceRepository(db), Assets: assets})
	project, err := referenceSvc.CreateProject(t.Context(), 7, &model.CreateDramaReferenceProjectReq{Title: "草稿集成", RequestKey: "project"})
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := idcodec.Decode(project.ID)
	ref, err := referenceSvc.AddEpisode(t.Context(), 7, pid, &model.AddDramaReferenceReq{AssetID: 1, RequestKey: "reference"})
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := idcodec.Decode(ref.ID)
	doc := referenceDocument()
	ref, err = referenceSvc.UpdateEpisode(t.Context(), 7, pid, rid, &model.UpdateDramaReferenceReq{Revision: ref.Revision, Document: &doc})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewDramaReferenceDraftService(service.DramaReferenceDraftDeps{Repo: repository.NewDramaReferenceDraftRepository(db), Assets: assets})
	return db, svc, pid, rid, ref.Revision
}

func TestReferenceDraftConcurrentCreateIsPersistent(t *testing.T) {
	db, svc, pid, rid, revision := draftDatabase(t)
	const clients = 8
	results := make(chan string, clients)
	errs := make(chan error, clients)
	var wg sync.WaitGroup
	for range clients {
		wg.Go(func() {
			req := draftRequest()
			req.ReferenceRevision = revision
			got, err := svc.Create(t.Context(), 7, pid, rid, req)
			if err != nil {
				errs <- err
				return
			}
			results <- got.ID
		})
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatal(err)
	}
	var first string
	for id := range results {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("重复创建产生多个草稿：%s %s", first, id)
		}
	}
	var count int64
	if err := db.Model(&model.DramaReferenceDraft{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("数据库草稿数 %d %v", count, err)
	}
	id, _ := idcodec.Decode(first)
	fresh := service.NewDramaReferenceDraftService(service.DramaReferenceDraftDeps{Repo: repository.NewDramaReferenceDraftRepository(db)})
	got, err := fresh.Get(t.Context(), 7, pid, rid, id)
	if err != nil || got.Prompts[0].Prompt != "手自然递出钥匙" {
		t.Fatalf("新服务实例无法恢复草稿：%+v %v", got, err)
	}
}

func TestReferenceDraftPagination(t *testing.T) {
	_, svc, pid, rid, revision := draftDatabase(t)
	for i := range 3 {
		req := &model.CreateDramaReferenceDraftReq{RequestKey: fmt.Sprintf("draft-%d", i), ReferenceRevision: revision}
		if _, err := svc.Create(t.Context(), 7, pid, rid, req); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := svc.List(t.Context(), 7, pid, rid, &model.ListDramaReferenceDraftsReq{Limit: 1, Cursor: cursor})
		if err != nil || len(page.Items) != 1 || seen[page.Items[0].ID] {
			t.Fatalf("分页错误：%+v %v", page, err)
		}
		seen[page.Items[0].ID] = true
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
		if len(seen) > 3 {
			t.Fatal("分页没有终点")
		}
	}
	if len(seen) != 3 {
		t.Fatalf("漏了草稿：%v", seen)
	}
}

func TestReferenceDraftDatabaseConcurrentEdit(t *testing.T) {
	db, svc, pid, rid, revision := draftDatabase(t)
	created, err := svc.Create(t.Context(), 7, pid, rid, &model.CreateDramaReferenceDraftReq{RequestKey: "edit", ReferenceRevision: revision})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(created.ID)
	var before model.DramaReferenceEpisode
	if err := db.First(&before, rid).Error; err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, instruction := range []string{"人工先保存", "Agent 同时保存"} {
		wg.Go(func() {
			_, err := svc.Update(t.Context(), 7, pid, rid, id, &model.UpdateDramaReferenceDraftReq{Revision: 1, Instruction: &instruction})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for err := range errs {
		var business *errcode.Error
		switch {
		case err == nil:
			success++
		case errors.As(err, &business) && business.Code == errcode.ErrCanvasConflict.Code:
			conflicts++
		default:
			t.Fatalf("并发修改返回意外错误：%v", err)
		}
	}
	fresh, err := svc.Get(t.Context(), 7, pid, rid, id)
	if success != 1 || conflicts != 1 || err != nil || fresh.Revision != 2 {
		t.Fatalf("乐观锁结果：成功=%d 冲突=%d 草稿=%+v 错误=%v", success, conflicts, fresh, err)
	}
	var after model.DramaReferenceEpisode
	if err := db.First(&after, rid).Error; err != nil || string(before.Document) != string(after.Document) || before.Revision != after.Revision {
		t.Fatalf("草稿修改影响了正式参考稿：%v", err)
	}
}

func TestReferenceDraftDeletedProjectIsNotAccessible(t *testing.T) {
	db, svc, pid, rid, revision := draftDatabase(t)
	created, err := svc.Create(t.Context(), 7, pid, rid, &model.CreateDramaReferenceDraftReq{RequestKey: "deleted", ReferenceRevision: revision})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(created.ID)
	if err := db.Delete(&model.DramaProject{}, pid).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.Get(t.Context(), 7, pid, rid, id)
	wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
	_, err = svc.List(t.Context(), 7, pid, rid, &model.ListDramaReferenceDraftsReq{Limit: 20})
	wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
	text := "应拒绝修改"
	_, err = svc.Update(t.Context(), 7, pid, rid, id, &model.UpdateDramaReferenceDraftReq{Revision: 1, Instruction: &text})
	wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
}
