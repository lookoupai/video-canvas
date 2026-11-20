package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type adoptionMemory struct {
	*referenceMemoryRepo
	drama     *adoptionDramaMemory
	adoptions []model.DramaReferenceAdoption
	roles     []model.DramaReferenceRole
}
type adoptionDramaMemory struct{ *dramaMemory }

func (r *adoptionMemory) LockCreateKey(context.Context, uint64, string) error { return nil }

func (r *adoptionMemory) WithTx(_ context.Context, fn func(repository.DramaReferenceTx) error) error {
	return fn(r)
}
func (r *adoptionMemory) DramaTx() repository.DramaTx { return r.drama }
func (r *adoptionMemory) Project(ctx context.Context, u, p uint64, lock bool) (*model.DramaProject, error) {
	return r.drama.Project(ctx, u, p, lock)
}
func (r *adoptionMemory) Roles(context.Context, uint64, uint64) ([]model.DramaReferenceRole, error) {
	return r.roles, nil
}
func (r *adoptionMemory) Canvas(context.Context, uint64, uint64) (*model.CanvasProject, error) {
	return &model.CanvasProject{PayloadJSON: []byte(`{"nodes":[{"id":"target-a","type":"card","data":{"drama":{"type":"character","name":"新阿青"}}},{"id":"target-b","type":"card","data":{"drama":{"type":"character","name":"新小林"}}}]}`)}, nil
}
func (r *adoptionMemory) Adoptions(context.Context, uint64, uint64) ([]model.DramaReferenceAdoption, error) {
	return r.adoptions, nil
}
func (r *adoptionMemory) AdoptionByKey(_ context.Context, u uint64, key string) (*model.DramaReferenceAdoption, error) {
	for _, row := range r.adoptions {
		var keys map[string]string
		if len(row.RequestKeys) != 0 {
			if err := json.Unmarshal(row.RequestKeys, &keys); err != nil {
				return nil, err
			}
		}
		if row.UserID == u && (row.RequestKey == key || keys[key] != "") {
			return &row, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *adoptionMemory) InsertAdoption(_ context.Context, row *model.DramaReferenceAdoption) error {
	row.ID = uint64(len(r.adoptions) + 20)
	r.adoptions = append(r.adoptions, *row)
	return nil
}
func (r *adoptionMemory) FinishAdoption(_ context.Context, row *model.DramaReferenceAdoption) error {
	for i := range r.adoptions {
		if r.adoptions[i].ID == row.ID {
			r.adoptions[i] = *row
		}
	}
	return nil
}

func (r *adoptionMemory) RememberAdoptionKey(ctx context.Context, row *model.DramaReferenceAdoption) error {
	return r.FinishAdoption(ctx, row)
}
func (r *adoptionDramaMemory) SaveSegment(_ context.Context, row *model.DramaSegment, revision int64) error {
	for i := range r.segments {
		if r.segments[i].ID == row.ID && row.ID != 0 {
			if r.segments[i].Revision != revision {
				return repository.ErrStateConflict
			}
			row.Revision++
			r.segments[i] = *row
			return nil
		}
	}
	r.nextID++
	row.ID = r.nextID
	r.segments = append(r.segments, *row)
	return nil
}

func newAdoptionMemory(t *testing.T) (*service.DramaReferenceService, *adoptionMemory) {
	t.Helper()
	raw, err := json.Marshal(referenceDocument())
	if err != nil {
		t.Fatal(err)
	}
	r := &adoptionMemory{referenceMemoryRepo: &referenceMemoryRepo{row: model.DramaReferenceEpisode{ID: 3, UserID: 7, DramaID: 4, AssetID: 1, DurationMs: 5000, Revision: 2, Status: "ready", Title: "钥匙", Document: raw, Bindings: []byte(`{"a":"` + idcodec.Encode(8) + `","b":"` + idcodec.Encode(9) + `"}`)}}, drama: &adoptionDramaMemory{newDramaMemory()}, roles: []model.DramaReferenceRole{{ID: 8, TargetNodeID: "target-a"}, {ID: 9, TargetNodeID: "target-b"}}}
	r.drama.project.ID = 4
	assets := &mktAssets{items: map[uint64]*mktAsset{}}
	assets.put(7, 1, "video", "video/mp4", 5000)
	return service.NewDramaReferenceService(service.DramaReferenceDeps{Repo: r, Assets: assets}), r
}

func TestDramaReferenceAdoptionFollowAndIdempotency(t *testing.T) {
	svc, r := newAdoptionMemory(t)
	req := &model.AdoptDramaReferenceReq{Revision: 2, RequestKey: "adopt", Mode: "follow"}
	got, err := svc.Adopt(t.Context(), 7, 4, 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.EpisodeNo != 1 || len(got.SegmentIDs) != 2 || r.drama.episodes[0].LockedAt == nil {
		t.Fatalf("采用没有建立片场 %+v", got)
	}
	if r.drama.segments[0].Seq != 0 || r.drama.segments[1].Seq != 1 {
		t.Fatalf("采用片段序号应从0开始：%+v", r.drama.segments)
	}
	var remake model.DramaSegmentRemake
	if err := json.Unmarshal(r.drama.segments[0].Remake, &remake); err != nil {
		t.Fatal(err)
	}
	if remake.TargetDurationMs != 2500 || remake.Sources[0].EndMs != 2500 || r.drama.segments[0].Seconds != 3 || !strings.Contains(r.drama.segments[0].Prompt, "@[新阿青](target-a)") {
		t.Fatalf("原镜与目标角色不符 %+v %+v", remake, r.drama.segments[0])
	}
	// 默认全量采用时，未传和空镜头数组表达同一输入，重传应复用。
	req.ShotIDs = []string{}
	for _, key := range []string{"adopt", "repeated-click"} {
		req.RequestKey = key
		duplicate, err := svc.Adopt(t.Context(), 7, 4, 3, req)
		if err != nil || duplicate.EpisodeID != got.EpisodeID || len(r.drama.episodes) != 1 {
			t.Fatalf("重复建集 %+v %v", duplicate, err)
		}
	}
	req.RequestKey = "adopt"
	req.Mode = "rewrite"
	_, err = svc.Adopt(t.Context(), 7, 4, 3, req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	req.RequestKey = "repeated-click"
	_, err = svc.Adopt(t.Context(), 7, 4, 3, req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}

func TestDramaReferenceAdoptionRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*model.AdoptDramaReferenceReq, *adoptionMemory)
		code   int
	}{
		{"旧修订", func(req *model.AdoptDramaReferenceReq, _ *adoptionMemory) { req.Revision = 1 }, errcode.ErrCanvasConflict.Code},
		{"角色未对应", func(_ *model.AdoptDramaReferenceReq, r *adoptionMemory) { r.row.Bindings = []byte(`{}`) }, errcode.ErrInvalidParams.Code},
		{"角色跨项目", func(_ *model.AdoptDramaReferenceReq, r *adoptionMemory) { r.roles[0].TargetNodeID = "foreign" }, errcode.ErrInvalidParams.Code},
		{"仍在分析", func(_ *model.AdoptDramaReferenceReq, r *adoptionMemory) { r.row.Status = "queued" }, errcode.ErrDramaJobBusy.Code},
		{"原镜不足一帧", func(_ *model.AdoptDramaReferenceReq, r *adoptionMemory) {
			doc := referenceDocument()
			doc.Shots[0].EndMs = 1
			r.row.Document = []byte(mustJSONText(doc))
		}, errcode.ErrInvalidParams.Code},
		{"缺更新镜头", func(req *model.AdoptDramaReferenceReq, _ *adoptionMemory) {
			req.TargetEpisodeNo = 1
			req.EpisodeRevision = 1
		}, errcode.ErrInvalidParams.Code},
		{"未知原镜", func(req *model.AdoptDramaReferenceReq, _ *adoptionMemory) { req.ShotIDs = []string{"missing"} }, errcode.ErrInvalidParams.Code},
		{"空创建键", func(req *model.AdoptDramaReferenceReq, _ *adoptionMemory) { req.RequestKey = " " }, errcode.ErrInvalidParams.Code},
		{"冲突任务", func(_ *model.AdoptDramaReferenceReq, r *adoptionMemory) {
			r.drama.jobs = []model.DramaJob{{UserID: 7, DramaID: 4, Status: "running", Step: "scripts"}}
		}, errcode.ErrDramaJobBusy.Code},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, r := newAdoptionMemory(t)
			req := &model.AdoptDramaReferenceReq{Revision: 2, Mode: "follow", RequestKey: "adopt"}
			tc.mutate(req, r)
			_, err := svc.Adopt(t.Context(), 7, 4, 3, req)
			wantMktCode(t, err, tc.code)
			if len(r.drama.episodes) != 0 {
				t.Fatal("拒绝的采用创建了分集")
			}
		})
	}
	svc, _ := newAdoptionMemory(t)
	_, err := svc.Adopt(t.Context(), 8, 4, 3, &model.AdoptDramaReferenceReq{Revision: 2, Mode: "follow", RequestKey: "adopt"})
	wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
}

func TestDramaReferenceAdoptionSelectiveUpdate(t *testing.T) {
	svc, r := newAdoptionMemory(t)
	first, err := svc.Adopt(t.Context(), 7, 4, 3, &model.AdoptDramaReferenceReq{Revision: 2, Mode: "follow", RequestKey: "first"})
	if err != nil {
		t.Fatal(err)
	}
	chosen := uint64(90)
	r.drama.segments[1].ChosenTaskID = &chosen
	before := r.drama.segments[0]
	doc := referenceDocument()
	doc.Shots[1].Action = "新小林锁上门"
	raw, _ := json.Marshal(doc)
	r.row.Document = raw
	r.row.Revision++
	updated, err := svc.Adopt(t.Context(), 7, 4, 3, &model.AdoptDramaReferenceReq{Revision: 3, Mode: "follow", RequestKey: "update", TargetEpisodeNo: 1, EpisodeRevision: first.EpisodeRevision, ShotIDs: []string{"2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.drama.segments) != 2 || r.drama.segments[0].Prompt != before.Prompt || r.drama.segments[0].Revision != before.Revision || *r.drama.segments[1].ChosenTaskID != 90 || !strings.Contains(r.drama.segments[1].Prompt, "锁上门") || len(updated.SegmentIDs) != 1 {
		t.Fatalf("选镜更新影响其他内容 %+v", r.drama.segments)
	}
	for range 2 {
		defaultReuse, reuseErr := svc.Adopt(t.Context(), 7, 4, 3, &model.AdoptDramaReferenceReq{Revision: 3, Mode: "follow", RequestKey: "default-after-update"})
		if reuseErr != nil || defaultReuse.ID != updated.ID || len(r.drama.episodes) != 1 {
			t.Fatalf("局部采用后默认采用应复用 %+v %v", defaultReuse, reuseErr)
		}
	}
	rows, err := svc.Adoptions(t.Context(), 7, 4)
	if err != nil || len(rows) != 2 {
		t.Fatalf("采用记录 %+v %v", rows, err)
	}
	r.drama.episodes = nil
	rows, err = svc.Adoptions(t.Context(), 7, 4)
	if err != nil || !rows[0].Deleted || rows[0].EpisodeID != idcodec.Encode(11) {
		t.Fatalf("已删集记录 %+v %v", rows, err)
	}
}
