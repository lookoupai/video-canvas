package service_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// planEditRepo 将选版校验与测试数据库解耦，所有查询都固定到一个用户和一份计划。
type planEditRepo struct {
	service.MarketingRepo
	row   model.MarketingRemakePlan
	takes []model.GenerationTask
}

func (r *planEditRepo) WithTx(_ context.Context, fn func(repository.MarketingTx) error) error {
	return fn(r)
}
func (r *planEditRepo) RemakePlan(_ context.Context, u, p uint64, _ bool) (*model.MarketingRemakePlan, error) {
	if u != r.row.UserID || p != r.row.ID {
		return nil, repository.ErrNotFound
	}
	row := r.row
	return &row, nil
}
func (r *planEditRepo) RemakeTakeTasks(_ context.Context, _, _ uint64) ([]model.GenerationTask, error) {
	return r.takes, nil
}

func TestMarketing_RemakeShotRejectsWrongVersion(t *testing.T) {
	raw, err := json.Marshal(model.RemakePlanData{Shots: []model.RemakePlanShot{{ID: 3, DurationMs: 2000, Revision: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	repo := &planEditRepo{row: model.MarketingRemakePlan{UserID: 4, ID: 5, Revision: 2, Data: raw}}
	svc := service.NewMarketingService(service.MarketingDeps{Repo: repo})
	node := "mkt-shot:" + idcodec.Encode(5) + ":"
	for _, tc := range []struct {
		name, shot, status, kind, outputs string
		userID                            uint64
		isTest                            bool
	}{
		{"其他镜头", "2", model.TaskSucceeded, "video", `[{"media_type":"video","asset_id":5}]`, 4, false},
		{"仍在生成", "3", model.TaskRunning, "video", "[]", 4, false},
		{"失败版本", "3", model.TaskFailed, "video", "[]", 4, false},
		{"缺少持久化产物", "3", model.TaskSucceeded, "video", `[{"media_type":"video"}]`, 4, false},
		{"图片产物", "3", model.TaskSucceeded, "video", `[{"media_type":"image","asset_id":5}]`, 4, false},
		{"他人任务", "3", model.TaskSucceeded, "video", `[{"media_type":"video","asset_id":5}]`, 9, false},
		{"后台试跑", "3", model.TaskSucceeded, "video", `[{"media_type":"video","asset_id":5}]`, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID := uint64(10)
			repo.takes = []model.GenerationTask{{ID: taskID, UserID: tc.userID, IsTest: tc.isTest, NodeID: node + tc.shot, Kind: tc.kind, Status: tc.status, OutputJSON: []byte(tc.outputs)}}
			_, err := svc.UpdateRemakeShot(t.Context(), 4, 5, 3, &model.UpdateRemakeShotReq{Revision: 2, ChosenTaskID: &taskID})
			wantMktCode(t, err, errcode.ErrInvalidParams.Code)
		})
	}
}

func TestMarketing_RemakePlanKeepsRevisionAndStableShotIDs(t *testing.T) {
	e := newMktEnv(t, 300)
	p, r := e.refRound()
	script := r.Scripts[0]
	ctx := t.Context()
	req := &model.CreateRemakePlanReq{Revision: script.Revision}
	plan, err := e.svc.CreateRemakePlan(ctx, e.user, uint64(script.ID), req)
	if err != nil {
		t.Fatal(err)
	}
	id := uint64(plan.ID)
	if len(plan.Shots) != 4 || plan.Shots[2].ID != 3 || plan.Shots[2].DurationMs != 2500 || plan.Shots[2].Sources[0].StartMs != 5000 || plan.Shots[2].Narration == "" {
		t.Fatalf("分镜快照 %+v", plan)
	}
	old := plan.Shots[0]
	scene := "用自己的护手霜演示涂抹"
	draft, err := e.svc.UpdateRemakeShot(ctx, e.user, id, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, Scene: &scene})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(draft.Shots[0], old) || draft.Shots[2].Scene != scene || draft.Shots[2].Revision != 2 {
		t.Fatal(draft.Shots)
	}
	_, err = e.svc.UpdateRemakeShot(ctx, e.user, id, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, Scene: &scene})
	wantMktCode(t, err, errcode.ErrMarketingConflict.Code)
	for _, ids := range [][]int{{1, 2}, {1, 1, 3, 4}, {1, 2, 3, 9}} {
		_, err = e.svc.OrderRemakeShots(ctx, e.user, id, &model.OrderRemakeShotsReq{Revision: draft.Revision, ShotIDs: ids})
		wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	}
	ordered, err := e.svc.OrderRemakeShots(ctx, e.user, id, &model.OrderRemakeShotsReq{Revision: draft.Revision, ShotIDs: []int{3, 1, 2, 4}})
	if err != nil || ordered.Shots[0].ID != 3 || ordered.Shots[0].Scene != scene {
		t.Fatalf("重排 %+v %v", ordered, err)
	}
	same, err := e.svc.CreateRemakePlan(ctx, e.user, uint64(script.ID), req)
	if err != nil || same.ID != plan.ID || same.Shots[0].ID != 3 {
		t.Fatalf("重复建计划: %+v %v", same, err)
	}
	if err = e.repo.UpdateScript(ctx, uint64(script.ID), map[string]any{"revision": script.Revision + 1}); err != nil {
		t.Fatal(err)
	}
	next, err := e.svc.CreateRemakePlan(ctx, e.user, uint64(script.ID), &model.CreateRemakePlanReq{Revision: script.Revision + 1})
	if err != nil || next.ID == plan.ID || next.Shots[0].ID != 1 || next.Shots[2].Scene == scene {
		t.Fatalf("新稿 %+v, %v", next, err)
	}
	list, err := e.svc.ListRemakePlans(ctx, e.user, uint64(script.ID))
	if err != nil || len(list) != 2 || list[1].ID != plan.ID {
		t.Fatalf("历史计划 %+v, %v", list, err)
	}
	_, err = e.svc.GetRemakePlan(ctx, e.user+1, id)
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	if err = e.svc.DeleteProject(ctx, e.user, uint64(p.ID)); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.GetRemakePlan(ctx, e.user, id)
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
}

func TestMarketing_RemakePlanConcurrentOpenAndSourceEdits(t *testing.T) {
	e := newMktEnv(t, 300)
	_, round := e.refRound()
	script := round.Scripts[0]
	req := &model.CreateRemakePlanReq{Revision: script.Revision}
	ctx := t.Context()
	var wg sync.WaitGroup
	var plans [2]*model.MarketingRemakePlanView
	var errs [2]error
	for i := range plans {
		wg.Go(func() { plans[i], errs[i] = e.svc.CreateRemakePlan(ctx, e.user, uint64(script.ID), req) })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || plans[0].ID != plans[1].ID {
		t.Fatalf("并发建计划 %+v %+v", plans, errs)
	}
	view := plans[0]
	id := uint64(view.ID)
	_, err := e.svc.CreateRemakePlan(ctx, e.user, uint64(round.Scripts[1].ID), req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	_, err = e.svc.CreateRemakePlan(ctx, e.user, uint64(script.ID), &model.CreateRemakePlanReq{Revision: 99})
	wantMktCode(t, err, errcode.ErrMarketingConflict.Code)
	for _, ref := range []model.MarketingRemakeSource{
		{RefShot: 1, StartMs: 2000, EndMs: 3000}, {RefShot: 1, StartMs: 1500, EndMs: 1000}, {RefShot: 8, StartMs: 0, EndMs: 250},
	} {
		sources := []model.MarketingRemakeSource{ref}
		_, err = e.svc.UpdateRemakeShot(ctx, e.user, id, 3, &model.UpdateRemakeShotReq{Revision: view.Revision, Sources: &sources})
		wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	}
	foreign := uint64(1234)
	_, err = e.svc.UpdateRemakeShot(ctx, e.user, id, 3, &model.UpdateRemakeShotReq{Revision: view.Revision, ChosenTaskID: &foreign})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	sources := []model.MarketingRemakeSource{{RefShot: 3, StartMs: 5100, EndMs: 7100}, {RefShot: 1, StartMs: 0, EndMs: 500}}
	got, err := e.svc.UpdateRemakeShot(ctx, e.user, id, 3, &model.UpdateRemakeShotReq{Revision: view.Revision, Sources: &sources})
	if err != nil || !reflect.DeepEqual(got.Shots[2].Sources, sources) {
		t.Fatalf("独立裁片范围 %+v, %v", got, err)
	}
}
