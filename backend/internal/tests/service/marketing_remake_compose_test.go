package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 与轮次测试共用隔离数据库，本队列替身不做转码。
func (q *mktQueue) EnqueueRemakeCompositionTx(_ context.Context, _ *sql.Tx, _, _, _ uint64, _ int) error {
	return nil
}

type composePlanRepo struct {
	service.MarketingRepo
	plan  model.MarketingRemakePlan
	takes []model.GenerationTask
	row   *model.MarketingRemakeComposition
	calls int
}

func (r *composePlanRepo) WithTx(_ context.Context, fn func(repository.MarketingTx) error) error {
	return fn(r)
}
func (r *composePlanRepo) RemakePlan(_ context.Context, u, p uint64, _ bool) (*model.MarketingRemakePlan, error) {
	if u != 7 || p != 10 {
		return nil, repository.ErrNotFound
	}
	return &r.plan, nil
}
func (r *composePlanRepo) RemakeTakeTasks(_ context.Context, _, _ uint64) ([]model.GenerationTask, error) {
	return r.takes, nil
}
func (r *composePlanRepo) RemakeCompositionByKey(_ context.Context, _, _ uint64, key string) (*model.MarketingRemakeComposition, error) {
	if r.row == nil || r.row.IdempotencyKey != key {
		return nil, repository.ErrNotFound
	}
	v := *r.row
	return &v, nil
}
func (r *composePlanRepo) InsertRemakeComposition(_ context.Context, row *model.MarketingRemakeComposition) error {
	row.ID = 42
	v := *row
	r.row = &v
	return nil
}
func (r *composePlanRepo) EnqueueRemakeComposition(_ context.Context, _, _, _ uint64, _ int) error {
	r.calls++
	return nil
}
func (r *composePlanRepo) RetryRemakeComposition(_ context.Context, row *model.MarketingRemakeComposition) error {
	v := *row
	r.row = &v
	return nil
}

func (r *composePlanRepo) ListRemakeCompositions(_ context.Context, _, _ uint64) ([]model.MarketingRemakeComposition, error) {
	if r.row == nil {
		return nil, nil
	}
	return []model.MarketingRemakeComposition{*r.row}, nil
}

func TestMarketing_RemakeCompositionList(t *testing.T) {
	repo := &composePlanRepo{}
	svc := service.NewMarketingService(service.MarketingDeps{Repo: repo})
	items, err := svc.RemakeCompositions(t.Context(), 7, 10)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("首次查看清单: %v %v", items, err)
	}
	_, err = svc.RemakeCompositions(t.Context(), 8, 10)
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	repo.row = &model.MarketingRemakeComposition{ID: 42, Revision: 4, Status: "queued", Parts: []byte(`[{"shot_id":3},{"shot_id":2}]`)}
	items, err = svc.RemakeCompositions(t.Context(), 7, 10)
	if err != nil || len(items) != 1 || items[0].ID != idcodec.Encode(42) || !slices.Equal(items[0].ShotIDs, []int{3, 2}) {
		t.Fatalf("按快照读取: %v %v", items, err)
	}
}

func TestMarketing_RemakeCompositionBlocksDeleteWhileQueued(t *testing.T) {
	env := newMktEnv(t, 300)
	project, round := env.refRound()
	plan, err := env.svc.CreateRemakePlan(t.Context(), env.user, uint64(round.Scripts[0].ID), &model.CreateRemakePlanReq{Revision: round.Scripts[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	row := &model.MarketingRemakeComposition{UserID: env.user, PlanID: uint64(plan.ID), IdempotencyKey: "queued", Revision: plan.Revision, Status: "queued", Attempt: 1, Parts: []byte("[]")}
	if err := env.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	err = env.svc.DeleteProject(t.Context(), env.user, uint64(project.ID))
	wantMktCode(t, err, errcode.ErrMarketingBusyDelete.Code)
	if err := env.db.Model(row).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := env.svc.DeleteProject(t.Context(), env.user, uint64(project.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestMarketing_RemakeCompositionSnapshot(t *testing.T) {
	repo := &composePlanRepo{}
	first, third := uint64(1), uint64(3)
	data := model.RemakePlanData{Shots: []model.RemakePlanShot{
		{ID: 3, Revision: 2, DurationMs: 2500, TrimStartMs: 500, ChosenTaskID: &third},
		{ID: 2, Revision: 1, DurationMs: 1500},
		{ID: 1, Revision: 1, DurationMs: 1000, ChosenTaskID: &first},
	}}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	repo.plan.Data = raw
	repo.plan.Revision = 4
	repo.plan.ID = 10
	repo.plan.UserID = 7
	for _, id := range []uint64{1, 3} {
		tsk := model.GenerationTask{ID: id, UserID: 7, NodeID: fmt.Sprintf("mkt-shot:%s:%d", idcodec.Encode(10), id), Kind: model.KindVideo, Status: model.TaskSucceeded}
		tsk.OutputJSON = []byte(fmt.Sprintf(`[{"asset_id":%d,"media_type":"video"}]`, id+100))
		repo.takes = append(repo.takes, tsk)
	}
	svc := service.NewMarketingService(service.MarketingDeps{Repo: repo})
	req := &model.ComposeRemakePlanReq{Revision: 4}
	for _, ids := range [][]int{nil, {}, {1, 1}, {99}} {
		req.ShotIDs = ids
		_, e := svc.ComposeRemakePlan(t.Context(), 7, 10, "test", req)
		wantMktCode(t, e, errcode.ErrInvalidParams.Code)
	}
	req.ShotIDs = []int{1, 3}
	_, err = svc.ComposeRemakePlan(t.Context(), 8, 10, "test", req)
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	req.Revision = 3
	_, err = svc.ComposeRemakePlan(t.Context(), 7, 10, "test", req)
	wantMktCode(t, err, errcode.ErrMarketingConflict.Code)
	req.Revision = 4
	v, err := svc.ComposeRemakePlan(t.Context(), 7, 10, "test", req)
	if err != nil || !slices.Equal(v.ShotIDs, []int{3, 1}) || repo.calls != 1 {
		t.Fatalf("合成快照: %+v, %v", v, err)
	}
	var snap []model.RemakeCompositionPart
	if err := json.Unmarshal(repo.row.Parts, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap) != 2 || snap[0].AssetID != 103 || snap[0].StartMs != 500 || snap[0].EndMs != 3000 || snap[0].TaskID != 3 {
		t.Fatalf("选版和裁片: %+v", snap)
	}
	repo.plan.Revision++
	repo.plan.Data = []byte("{}")
	_, err = svc.ComposeRemakePlan(t.Context(), 7, 10, "test", req)
	if err != nil || repo.calls != 1 {
		t.Fatalf("重复提交: %v", err)
	}
	repo.row.Status = "failed"
	_, err = svc.ComposeRemakePlan(t.Context(), 7, 10, "test", req)
	if err != nil || repo.calls != 2 || repo.row.Attempt != 2 {
		t.Fatalf("重试原清单: %v", err)
	}
	req.ShotIDs = []int{1}
	_, err = svc.ComposeRemakePlan(t.Context(), 7, 10, "test", req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}
