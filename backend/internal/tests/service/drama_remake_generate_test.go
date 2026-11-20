package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type dramaClipFake struct{ calls atomic.Int32 }

func (c *dramaClipFake) Clip(_ context.Context, user, source uint64, parts []service.RemakeMediaPart) (uint64, error) {
	c.calls.Add(1)
	if user == 0 || source != 4 || len(parts) != 1 || parts[0].StartMs != 2500 || parts[0].EndMs != 5000 {
		return 0, errors.New("裁片没有使用采用时的原镜区间")
	}
	return 5, nil
}

type dramaGenerateEnv struct {
	*mktEnv
	repo     *repository.DramaRepository
	intents  *repository.DramaSegmentIntentRepository
	service  *service.DramaSegmentService
	clips    *dramaClipFake
	pid, sid uint64
}

func newDramaGenerateEnv(t *testing.T) *dramaGenerateEnv {
	t.Helper()
	env := &dramaGenerateEnv{mktEnv: newMktEnv(t, 300), clips: &dramaClipFake{}}
	if err := env.db.AutoMigrate(&model.CanvasProject{}, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaSegment{}, &model.DramaJob{}, &model.DramaSegmentGenerationIntent{}); err != nil {
		t.Fatal(err)
	}
	env.repo = repository.NewDramaRepository(env.db)
	env.intents = repository.NewDramaSegmentIntentRepository(env.db)
	p := &model.DramaProject{UserID: env.user, Title: "双集仿拍", StyleKey: "realistic", Phase: "done", SourceKind: "reference"}
	err := env.repo.WithTx(t.Context(), func(tx repository.DramaTx) error {
		if err := tx.InsertProject(t.Context(), p); err != nil {
			return err
		}
		now := time.Now()
		ep := &model.DramaEpisode{UserID: env.user, DramaID: p.ID, No: 1, Title: "第一集", LockedAt: &now, Revision: 1}
		if err := tx.SaveEpisode(t.Context(), ep, 0); err != nil {
			return err
		}
		row := &model.DramaSegment{UserID: env.user, EpisodeID: ep.ID, Seconds: 3, Prompt: "新角色递出钥匙", Revision: 1}
		setRemakeWindow(t, row, 0, 2500)
		var remake model.DramaSegmentRemake
		if err := json.Unmarshal(row.Remake, &remake); err != nil {
			return err
		}
		remake.AssetID = 4
		row.Remake, _ = json.Marshal(remake)
		if err := tx.SaveSegment(t.Context(), row, 0); err != nil {
			return err
		}
		env.pid, env.sid = p.ID, row.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	env.assets.put(env.user, 5, "video", "video/mp4", 2500)
	env.service = env.withTasks(env.taskSvc)
	return env
}

func (e *dramaGenerateEnv) withTasks(tasks service.DramaSegmentTaskCreator) *service.DramaSegmentService {
	return service.NewDramaSegmentService(service.DramaSegmentDeps{Repo: e.repo, Tasks: tasks, Assets: e.assets, Intents: e.intents, ReferenceClips: e.clips})
}

func dramaGenerateRequest() *model.GenerateDramaSegmentReq {
	return &model.GenerateDramaSegmentReq{Revision: 1, ModelID: mktVideoKey, UseReferenceVideo: true, Input: map[string]any{
		"op": "omni", "prompt": "新角色递出钥匙", "duration": 4, "aspect_ratio": "9:16", "images": []any{"1"}, "videos": []any{"4"},
	}}
}

func TestDramaRemakeGenerate_ConcurrentReplayAndFailureKeepChoice(t *testing.T) {
	env := newDramaGenerateEnv(t)
	req := dramaGenerateRequest()
	var wg sync.WaitGroup
	var ids [2]uint64
	for i := range ids {
		wg.Go(func() {
			v, err := env.service.Generate(t.Context(), env.user, env.sid, "same-shot", req)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = v.ID
		})
	}
	wg.Wait()
	if ids[0] == 0 || ids[0] != ids[1] {
		t.Fatalf("重传没有复用任务：%v", ids)
	}
	var task model.GenerationTask
	if err := env.db.Take(&task, ids[0]).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(task.InputJSON), `"videos": ["5"]`) {
		var input map[string]any
		_ = json.Unmarshal(task.InputJSON, &input)
		if refs, ok := input["videos"].([]any); !ok || len(refs) != 1 || refs[0] != "5" {
			t.Fatalf("模型没有收到单镜裁片：%s", task.InputJSON)
		}
	}
	var freezes, intents int64
	if err := env.db.Model(&model.CreditLedger{}).Where("task_id = ? AND type = ?", task.ID, model.LedgerFreeze).Count(&freezes).Error; err != nil || freezes != 1 {
		t.Fatalf("冻结次数=%d %v", freezes, err)
	}
	if err := env.db.Model(&model.DramaSegmentGenerationIntent{}).Count(&intents).Error; err != nil || intents != 1 {
		t.Fatalf("意图次数=%d %v", intents, err)
	}
	env.finishTask(task, model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 5, MediaType: model.KindVideo}})
	views, err := env.service.List(t.Context(), env.user, env.pid, 1)
	if err != nil || views[0].ChosenTaskID != idcodec.Encode(task.ID) {
		t.Fatalf("首次成功未采用：%+v %v", views, err)
	}
	retake := dramaGenerateRequest()
	retake.Revision = views[0].Revision
	next, err := env.service.Generate(t.Context(), env.user, env.sid, "retake", retake)
	if err != nil {
		t.Fatal(err)
	}
	var failed model.GenerationTask
	if err := env.db.Take(&failed, next.ID).Error; err != nil {
		t.Fatal(err)
	}
	env.finishTask(failed, model.TaskFailed, "模型失败", nil)
	views, err = env.service.List(t.Context(), env.user, env.pid, 1)
	if err != nil || views[0].ChosenTaskID != idcodec.Encode(task.ID) || views[0].Status != "error" {
		t.Fatalf("重拍失败覆盖了原采用：%+v %v", views, err)
	}
}

func TestDramaRemakeGenerate_RetryUsesFrozenInput(t *testing.T) {
	env := newDramaGenerateEnv(t)
	req := dramaGenerateRequest()
	broken := env.withTasks(failedRemakeSubmission{})
	if _, err := broken.Generate(t.Context(), env.user, env.sid, "recovery", req); err == nil {
		t.Fatal("应模拟任务提交失败")
	}
	var before model.DramaSegmentGenerationIntent
	if err := env.db.Where("request_key = ?", "recovery").Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	changed := "后来的动作"
	if _, err := env.service.Update(t.Context(), env.user, env.sid, &model.UpdateDramaSegmentReq{Revision: 1, Prompt: &changed}); err != nil {
		t.Fatal(err)
	}
	got, err := env.service.Generate(t.Context(), env.user, env.sid, "recovery", req)
	if err != nil {
		t.Fatal(err)
	}
	var task model.GenerationTask
	if err := env.db.Take(&task, got.ID).Error; err != nil {
		t.Fatal(err)
	}
	if env.clips.calls.Load() != 1 || task.IdempotencyKey != before.TaskKey || strings.Contains(string(task.InputJSON), changed) || len(before.RemakeSnapshot) == 0 {
		t.Fatalf("恢复没有固定原输入：calls=%d task=%+v intent=%+v", env.clips.calls.Load(), task, before)
	}
	changedReq := dramaGenerateRequest()
	changedReq.Input["prompt"] = changed
	_, err = env.service.Generate(t.Context(), env.user, env.sid, "recovery", changedReq)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}

func TestDramaRemakeGenerate_RejectsStaleAndBypass(t *testing.T) {
	env := newDramaGenerateEnv(t)
	req := dramaGenerateRequest()
	req.Revision = 2
	_, err := env.service.Generate(t.Context(), env.user, env.sid, "stale", req)
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
	_, err = env.service.Generate(t.Context(), env.user+1, env.sid, "foreign", req)
	wantMktCode(t, err, errcode.ErrDramaSegmentNotFound.Code)
	p, err := env.repo.Project(t.Context(), env.user, env.pid, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := env.taskSvc.Create(t.Context(), env.user, "bypass", &model.CreateGenerationTaskReq{Kind: model.KindVideo, CanvasID: idcodec.ID(p.CanvasID), NodeID: "seg:" + idcodec.Encode(env.sid), ModelID: mktVideoKey, Input: req.Input})
	if err != nil || result == nil || len(result.Items) != 1 || result.Items[0].Task != nil || result.Items[0].Error == nil || result.Items[0].Error.Code != errcode.ErrInvalidParams.Code {
		t.Fatalf("公有入口绕过仿拍依据校验：%+v %v", result, err)
	}
	_, err = env.taskSvc.Create(t.Context(), env.user, "drama-remake:forged", &model.CreateGenerationTaskReq{Kind: model.KindVideo, ModelID: mktVideoKey, Input: req.Input})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}

func TestDramaRemakeGenerate_RejectsModelDurationShorterThanWindow(t *testing.T) {
	env := newDramaGenerateEnv(t)
	start, duration := int64(2500), int64(2500)
	updated, err := env.service.Update(t.Context(), env.user, env.sid, &model.UpdateDramaSegmentReq{Revision: 1, TrimStartMs: &start, TargetDurationMs: &duration})
	if err != nil {
		t.Fatal(err)
	}
	req := dramaGenerateRequest()
	req.Revision = updated.Revision
	_, err = env.service.Generate(t.Context(), env.user, env.sid, "too-short", req)
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	var tasks, freezes int64
	_ = env.db.Model(&model.GenerationTask{}).Count(&tasks).Error
	_ = env.db.Model(&model.CreditLedger{}).Where("type = ?", model.LedgerFreeze).Count(&freezes).Error
	if tasks != 0 || freezes != 0 {
		t.Fatalf("不够裁剪的模型视频不能入队或冻结积分：%d/%d", tasks, freezes)
	}
}

type staleDramaIntentTask struct{ service.DramaSegmentIntentRepo }

func (s staleDramaIntentTask) IntentTask(context.Context, *model.DramaSegmentGenerationIntent) (*model.GenerationTask, error) {
	return nil, repository.ErrNotFound
}

type delayedDramaTaskRepo struct{ service.GenerationTaskRepo }

func (r delayedDramaTaskRepo) FindByIdempotencyKey(context.Context, uint64, string) (*model.GenerationTask, error) {
	return nil, repository.ErrNotFound
}

func (r delayedDramaTaskRepo) WithTx(ctx context.Context, fn func(repository.GenerationTaskTx) error) error {
	return r.GenerationTaskRepo.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		return fn(&delayedDramaTaskTx{GenerationTaskTx: tx})
	})
}

type delayedDramaTaskTx struct {
	repository.GenerationTaskTx
	read bool
}

func (tx *delayedDramaTaskTx) FindByIdempotencyKey(ctx context.Context, userID uint64, key string) (*model.GenerationTask, error) {
	if !tx.read {
		tx.read = true
		return nil, repository.ErrNotFound
	}
	return tx.GenerationTaskTx.FindByIdempotencyKey(ctx, userID, key)
}

func (tx *delayedDramaTaskTx) DramaTx() repository.DramaTx {
	return tx.GenerationTaskTx.(interface{ DramaTx() repository.DramaTx }).DramaTx()
}

func TestDramaRemakeGenerate_DelayedReplaySurvivesNewerRunningTask(t *testing.T) {
	env := newDramaGenerateEnv(t)
	req := dramaGenerateRequest()
	first, err := env.service.Generate(t.Context(), env.user, env.sid, "completed", req)
	if err != nil {
		t.Fatal(err)
	}
	var task model.GenerationTask
	if err := env.db.Take(&task, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	env.finishTask(task, model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 5, MediaType: model.KindVideo}})
	if _, err := env.service.Generate(t.Context(), env.user, env.sid, "newer", req); err != nil {
		t.Fatal(err)
	}
	tasks := service.NewGenerationTaskService(service.GenerationTaskDeps{Repo: delayedDramaTaskRepo{env.taskRepo}, Registry: &mktRegistry{snap: mktVideoSnapshot()}, Assets: env.assets, Executor: &fakeTaskExecutor{}, Limits: &fakeLimits{initial: 300, defMax: 20}})
	replaying := service.NewDramaSegmentService(service.DramaSegmentDeps{Repo: env.repo, Tasks: tasks, Assets: env.assets, Intents: staleDramaIntentTask{env.intents}, ReferenceClips: env.clips})
	replayed, err := replaying.Generate(t.Context(), env.user, env.sid, "completed", req)
	if err != nil || replayed.ID != first.ID {
		t.Fatalf("迟到的已完成意图不应被之后的新任务阻挡：%+v %v", replayed, err)
	}
}
