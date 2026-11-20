package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// failedRemakeSubmission 模拟意图已落库、任务服务临时不可用的提交间隙。
type failedRemakeSubmission struct{ service.MarketingTaskCreator }

func (failedRemakeSubmission) Create(context.Context, uint64, string, *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error) {
	return nil, errors.New("任务提交暂时不可用")
}

// staleRemakeIntentRead 模拟另一个请求提交任务前已完成的意图任务查询。
type staleRemakeIntentRead struct{ service.MarketingRepo }

func (staleRemakeIntentRead) RemakeIntentTask(context.Context, *model.MarketingRemakeIntent) (*model.GenerationTask, error) {
	return nil, repository.ErrNotFound
}

// staleRemakeTaskRead 模拟事务外幂等查询落在另一个请求提交之前，事务内仍读取真实状态。
type staleRemakeTaskRead struct{ service.GenerationTaskRepo }

func (staleRemakeTaskRead) FindByIdempotencyKey(context.Context, uint64, string) (*model.GenerationTask, error) {
	return nil, repository.ErrNotFound
}

func TestMarketing_RemakeGenerate_ConcurrentReplayReturnsCompletedTake(t *testing.T) {
	env := newMktEnv(t, 300)
	_, round := env.refRound()
	plan, err := env.svc.CreateRemakePlan(t.Context(), env.user, uint64(round.Scripts[0].ID), &model.CreateRemakePlanReq{Revision: round.Scripts[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	prepRemakeGenerationClip(t, env)
	req := &model.GenerateRemakeShotReq{Revision: 1}
	first, err := env.svc.GenerateRemakeShot(t.Context(), env.user, uint64(plan.ID), 3, "old-intent", req)
	if err != nil {
		t.Fatal(err)
	}
	var task model.GenerationTask
	if err := env.db.Take(&task, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	env.finishTask(task, model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 5, MediaType: model.KindVideo}})
	if _, err := env.svc.GenerateRemakeShot(t.Context(), env.user, uint64(plan.ID), 3, "new-intent", req); err != nil {
		t.Fatal(err)
	}
	staleTasks := service.NewGenerationTaskService(service.GenerationTaskDeps{
		Repo: staleRemakeTaskRead{env.taskRepo}, Registry: &mktRegistry{snap: mktVideoSnapshot()}, Assets: env.assets,
		Executor: &fakeTaskExecutor{}, Limits: &fakeLimits{initial: 300, defMax: 20},
	})
	replaying := service.NewMarketingService(service.MarketingDeps{Repo: staleRemakeIntentRead{env.repo}, Models: env.models, Assets: env.assets, Tasks: staleTasks})
	replayed, err := replaying.GenerateRemakeShot(t.Context(), env.user, uint64(plan.ID), 3, "old-intent", req)
	if err != nil || replayed.ID != first.ID {
		t.Fatalf("已提交意图的重传应返回原任务，实际 %+v %v", replayed, err)
	}
}

func TestMarketing_RemakeGenerate_RetryKeepsFrozenInput(t *testing.T) {
	env := newMktEnv(t, 300)
	_, round := env.refRound()
	plan, err := env.svc.CreateRemakePlan(t.Context(), env.user, uint64(round.Scripts[0].ID), &model.CreateRemakePlanReq{Revision: round.Scripts[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	planID := uint64(plan.ID)
	env.svc = service.NewMarketingService(service.MarketingDeps{Repo: generationClipRepo{env.repo}, Models: env.models, Assets: env.assets, Tasks: failedRemakeSubmission{env.taskSvc}})
	request := &model.GenerateRemakeShotReq{Revision: 1}
	if _, err := env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 3, "retry-frozen", request); err == nil {
		t.Fatal("应在意图落库后模拟提交失败")
	}
	var before model.MarketingRemakeIntent
	if err := env.db.Where("user_id = ? AND request_key = ?", env.user, "retry-frozen").Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	scene := "新动作只用于下一次重拍"
	if _, err := env.svc.UpdateRemakeShot(t.Context(), env.user, planID, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, Scene: &scene}); err != nil {
		t.Fatal(err)
	}
	// 重建服务模拟进程重启。旧键继续完成原输入，当前编辑稿不会被重新裁切或替换。
	prepRemakeGenerationClip(t, env)
	got, err := env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 3, "retry-frozen", request)
	if err != nil {
		t.Fatal(err)
	}
	var task model.GenerationTask
	if err := env.db.Take(&task, got.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.IdempotencyKey != before.TaskKey || strings.Contains(string(task.InputJSON), scene) {
		t.Fatalf("重试没有使用第一次的输入 %s", task.InputJSON)
	}
	// 已创建任务的重传不需要模型仍然可用。
	delete(env.models.Snapshots, mktVideoKey)
	retry, err := env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 3, "retry-frozen", request)
	if err != nil || retry.ID != got.ID {
		t.Fatalf("模型下线后重传 %+v %v", retry, err)
	}
}

func TestMarketing_RemakeGenerate_DistinctIntentsCannotOverlap(t *testing.T) {
	env := newMktEnv(t, 300)
	project, round := env.refRound()
	plan, err := env.svc.CreateRemakePlan(t.Context(), env.user, uint64(round.Scripts[0].ID), &model.CreateRemakePlanReq{Revision: round.Scripts[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	prepRemakeGenerationClip(t, env)
	var wg sync.WaitGroup
	var tasks [2]*model.GenerationTaskView
	var failures [2]error
	for i, key := range []string{"retake-one", "retake-two"} {
		wg.Go(func() {
			tasks[i], failures[i] = env.svc.GenerateRemakeShot(t.Context(), env.user, uint64(plan.ID), 3, key, &model.GenerateRemakeShotReq{Revision: 1})
		})
	}
	wg.Wait()
	if (tasks[0] == nil) == (tasks[1] == nil) {
		t.Fatalf("同一镜只能有一个在途任务 %+v %v", tasks, failures)
	}
	for i, task := range tasks {
		if task == nil {
			wantMktCode(t, failures[i], errcode.ErrMarketingScriptBusy.Code)
		}
	}
	wantMktCode(t, env.svc.DeleteProject(t.Context(), env.user, uint64(project.ID)), errcode.ErrMarketingBusyDelete.Code)
	var freezes int64
	if err := env.db.Model(&model.CreditLedger{}).Where("user_id = ? AND type = ? AND task_id IS NOT NULL", env.user, model.LedgerFreeze).Count(&freezes).Error; err != nil || freezes != 1 {
		t.Fatalf("只能冻结一笔任务，实际 %d %v", freezes, err)
	}
}
