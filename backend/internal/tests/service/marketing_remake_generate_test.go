package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
)

// 只重拍第三镜。并发的同一意图共用正式任务和冻结，另一次失败仍保留上次选版。
func TestMarketing_RemakeGenerate_OnlyThirdShot(t *testing.T) {
	env := newMktEnv(t, 300)
	_, round := env.refRound()
	plan, err := env.svc.CreateRemakePlan(t.Context(), env.user, uint64(round.Scripts[0].ID), &model.CreateRemakePlanReq{Revision: round.Scripts[0].Revision})
	if err != nil {
		t.Fatal(err)
	}
	planID := uint64(plan.ID)
	// 此用例聚焦任务提交和积分，缓存中准备好该镜已裁出的测试视频。
	prepRemakeGenerationClip(t, env)
	req := &model.GenerateRemakeShotReq{Revision: 1}
	var wg sync.WaitGroup
	var ids [2]uint64
	for i := range ids {
		wg.Go(func() {
			v, e := env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 3, "shot-three", req)
			if e != nil {
				t.Error(e)
				return
			}
			ids[i] = v.ID
		})
	}
	wg.Wait()
	if ids[0] == 0 || ids[0] != ids[1] {
		t.Fatalf("幂等任务 ID %v", ids)
	}
	var tasks []model.GenerationTask
	if err := env.db.Where("user_id = ?", env.user).Find(&tasks).Error; err != nil || len(tasks) != 1 {
		t.Fatalf("只允许第三镜有一条任务: %v %+v", err, tasks)
	}
	var input map[string]any
	if err := json.Unmarshal(tasks[0].InputJSON, &input); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tasks[0].NodeID, ":3") || input["duration"] != float64(4) || len(input["images"].([]any)) != 2 || input["videos"].([]any)[0] != "5" {
		t.Fatalf("未按单镜输入准备任务: %+v", input)
	}
	var freezes int64
	if err := env.db.Model(&model.CreditLedger{}).Where("task_id = ? AND type = ?", tasks[0].ID, model.LedgerFreeze).Count(&freezes).Error; err != nil || freezes != 1 {
		t.Fatalf("冻结 %d，%v", freezes, err)
	}
	_, err = env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 2, "shot-three", req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	_, err = env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 3, "shot-three", &model.GenerateRemakeShotReq{Revision: 2})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	_, err = env.svc.GenerateRemakeShot(t.Context(), env.user+1, planID, 3, "other-user", req)
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	// 从普通画布入口伪造节点或内部幂等键均不可提交，也不能成为可选版本。
	_, err = env.taskSvc.Create(context.Background(), env.user, "manual", &model.CreateGenerationTaskReq{Kind: model.KindVideo, NodeID: tasks[0].NodeID, ModelID: mktVideoKey, Input: input})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	_, err = env.taskSvc.Create(context.Background(), env.user, tasks[0].IdempotencyKey, &model.CreateGenerationTaskReq{Kind: model.KindVideo, NodeID: "canvas-node", ModelID: mktVideoKey, Input: input})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)

	env.finishTask(tasks[0], model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 5, MediaType: model.KindVideo, URL: "https://test/result.mp4"}})
	selected, err := env.svc.UpdateRemakeShot(t.Context(), env.user, planID, 3, &model.UpdateRemakeShotReq{Revision: plan.Revision, ChosenTaskID: &ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	next, err := env.svc.GenerateRemakeShot(t.Context(), env.user, planID, 3, "retake", &model.GenerateRemakeShotReq{Revision: selected.Shots[2].Revision})
	if err != nil {
		t.Fatal(err)
	}
	var failed model.GenerationTask
	if err := env.db.Take(&failed, next.ID).Error; err != nil {
		t.Fatal(err)
	}
	env.finishTask(failed, model.TaskFailed, "模型超时", nil)
	reloaded, err := env.svc.GetRemakePlan(t.Context(), env.user, planID)
	if err != nil {
		t.Fatal(err)
	}
	third := reloaded.Shots[2]
	if len(third.Takes) != 2 || third.ChosenTaskID == nil || *third.ChosenTaskID != ids[0] || third.Takes[1].Status != model.TaskFailed {
		t.Fatalf("失败版本覆盖了选版: %+v", third)
	}
	for _, i := range []int{0, 1, 3} {
		if len(reloaded.Shots[i].Takes) != 0 {
			t.Fatalf("不应重做第%d镜", i+1)
		}
	}
}

// generationClipRepo 复用已准备的第三镜裁片，其余方法走真实 PostgreSQL 仓储。
type generationClipRepo struct{ service.MarketingRepo }

func (r generationClipRepo) FindRemakeClip(_ context.Context, userID, sourceID uint64, recipe string) (uint64, error) {
	if userID == 0 || sourceID != 4 || recipe == "" {
		return 0, errcode.ErrInvalidParams
	}
	return 5, nil
}

func prepRemakeGenerationClip(t *testing.T, env *mktEnv) {
	t.Helper()
	env.assets.put(env.user, 5, "video", "video/mp4", 2500)
	env.svc = service.NewMarketingService(service.MarketingDeps{Repo: generationClipRepo{env.repo}, Models: env.models, Assets: env.assets, Tasks: env.taskSvc})
}
