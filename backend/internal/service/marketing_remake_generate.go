package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

const remakeTaskKeyPrefix = "mkt-remake:"
const remakeNodePrefix = "mkt-shot:"

// GenerateRemakeShot 固定本次所见的单镜修订、参考裁片和完整模型输入。相同意图只会产生一次任务冻结。
func (s *MarketingService) GenerateRemakeShot(ctx context.Context, userID, planID uint64, shotID int, key string, req *model.GenerateRemakeShotReq) (*model.GenerationTaskView, error) {
	// 1. 每个意图用用户级唯一键。校验计划归属后优先复用已提交的快照，允许脚本与模型之后变化。
	key = strings.TrimSpace(key)
	if req == nil || req.Revision < 1 || shotID < 1 || key == "" || len(key) > 128 {
		return nil, errcode.ErrInvalidParams
	}
	_, data, err := s.loadRemakePlan(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	in, err := s.repo.RemakeIntent(ctx, userID, key)
	if mktNotFound(err) {
		// 2. 准备独立参考视频并持久化输入。事务内复查镜头版本，避免裁切途中用户修改镜头。
		in, err = s.prepareRemakeIntent(ctx, userID, planID, shotID, key, req.Revision, data)
	}
	if err != nil {
		return nil, err
	}
	if in.PlanID != planID || in.ShotID != shotID || in.Revision != req.Revision {
		return nil, errcode.ErrInvalidParams.WithMsg("幂等键已用于其他镜头或修订")
	}
	// 3. 通过不可由公有入口提交的内部键关联正式任务。重试沿用同一任务与冻结。
	return s.submitRemakeIntent(ctx, in)
}

// prepareRemakeIntent 用未锁快照做裁切及报价，再在短事务里验版本并插入唯一意图。
func (s *MarketingService) prepareRemakeIntent(ctx context.Context, userID, planID uint64, shotID int, key string, revision int64, data *model.RemakePlanData) (*model.MarketingRemakeIntent, error) {
	shot, err := findRemakeShot(data, shotID)
	if err != nil {
		return nil, err
	}
	if shot.Revision != revision {
		return nil, errcode.ErrMarketingConflict
	}
	input, _, _, err := s.remakeShotInput(ctx, data, shot)
	if err != nil {
		return nil, err
	}
	if len(shot.Sources) > 0 {
		clip, err := s.remakeReferenceClip(ctx, userID, *data.RefVideoAssetID, shot.Sources)
		if err != nil {
			return nil, err
		}
		input["videos"] = []string{assetIDString(clip)}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	frozenShot, err := json.Marshal(shot)
	if err != nil {
		return nil, err
	}
	in := &model.MarketingRemakeIntent{
		UserID: userID, RequestKey: key, TaskKey: remakeTaskKeyPrefix + uuid.NewString(), PlanID: planID, ShotID: shotID, Revision: revision,
		NodeID: remakeShotNode(planID, shotID), ModelID: data.Settings.VideoModel, Input: raw, ShotSnapshot: frozenShot,
	}
	err = s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		row, err := tx.RemakePlan(ctx, userID, planID, true)
		if err != nil {
			return remakeReadError(err)
		}
		current, err := decodeRemakePlan(row)
		if err != nil {
			return err
		}
		updated, err := findRemakeShot(current, shotID)
		if err != nil {
			return err
		}
		if updated.Revision != revision {
			return errcode.ErrMarketingConflict
		}
		in, err = tx.InsertRemakeIntent(ctx, in)
		return err
	})
	return in, err
}

// submitRemakeIntent 根据冻结输入创建任务，或返回首次已入队的结果（模型下线也可重查）。
func (s *MarketingService) submitRemakeIntent(ctx context.Context, in *model.MarketingRemakeIntent) (*model.GenerationTaskView, error) {
	if t, err := s.repo.RemakeIntentTask(ctx, in); err == nil {
		return taskView(t), nil
	} else if !mktNotFound(err) {
		return nil, err
	}
	var input map[string]any
	dec := json.NewDecoder(bytes.NewReader(in.Input))
	dec.UseNumber() // 素材主键可能大于可由 float64 精确表示的范围。
	if err := dec.Decode(&input); err != nil {
		return nil, err
	}
	req := &model.CreateGenerationTaskReq{Kind: model.KindVideo, NodeID: in.NodeID, ModelID: in.ModelID, Input: input}
	ctx = context.WithValue(ctx, remakeIntentContext{}, in)
	result, err := s.tasks.Create(ctx, in.UserID, in.TaskKey, req)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Items) != 1 {
		return nil, errors.New("单镜任务创建未返回唯一结果")
	}
	item := result.Items[0]
	if item.Error != nil {
		return nil, errcode.New(item.Error.Code, item.Error.Message, item.Error.Status)
	}
	if item.Task == nil || item.Task.NodeID != in.NodeID || item.Task.Kind != model.KindVideo {
		return nil, errors.New("单镜任务未绑定到当前镜头")
	}
	return item.Task, nil
}

// remakeIntentContext 仅由服务内提交入口设置，不允许公开生成 API 冒用镜头节点或内部任务键。
type remakeIntentContext struct{}

// checkRemakeSubmission 同时检查请求的所有节点，避免重复幂等键的快速路径绕过来源校验。
func checkRemakeSubmission(ctx context.Context, userID uint64, key string, nodes []string) error {
	if err := checkDramaRemakeSubmission(ctx, userID, key, nodes); err != nil {
		return err
	}
	in, _ := ctx.Value(remakeIntentContext{}).(*model.MarketingRemakeIntent)
	if in != nil && in.UserID == userID && in.TaskKey == key && len(nodes) == 1 && nodes[0] == in.NodeID {
		return nil
	}
	if strings.HasPrefix(key, remakeTaskKeyPrefix) {
		return errcode.ErrInvalidParams.WithMsg("该幂等键由逐镜仿拍使用")
	}
	for _, node := range nodes {
		if strings.HasPrefix(node, remakeNodePrefix) {
			return errcode.ErrInvalidParams.WithMsg("请通过逐镜仿拍提交此镜头")
		}
	}
	return nil
}

// guardRemakeTask 在任务冻结事务中锁定存活项目后复查幂等，阻止删除后入队和同一镜头重复占用任务。
func guardRemakeTask(ctx context.Context, tx repository.GenerationTaskTx, task *model.GenerationTask) (*model.GenerationTask, error) {
	in, _ := ctx.Value(remakeIntentContext{}).(*model.MarketingRemakeIntent)
	if in == nil {
		return nil, errcode.ErrInvalidParams
	}
	adapter, ok := tx.(interface{ MarketingTx() repository.MarketingTx })
	if !ok {
		return nil, errcode.ErrInternal
	}
	mtx := adapter.MarketingTx()
	if _, err := mtx.RemakePlan(ctx, in.UserID, in.PlanID, true); err != nil {
		return nil, remakeReadError(err)
	}
	// 锁等待期间同键请求可能已经提交。先返回它，不能被后来开始的另一轮重拍阻挡。
	existing, err := tx.FindByIdempotencyKey(ctx, task.UserID, in.TaskKey)
	if err == nil {
		return existing, nil
	}
	if !mktNotFound(err) {
		return nil, err
	}
	tasks, err := mtx.ScriptTasks(ctx, task.UserID, []string{in.NodeID})
	if err != nil {
		return nil, err
	}
	for _, other := range tasks {
		if other.IdempotencyKey != in.TaskKey && slices.Contains(model.ActiveTaskStatuses, other.Status) {
			return nil, errcode.ErrMarketingScriptBusy.WithMsg("该镜头已有进行中的重拍任务")
		}
	}
	return nil, nil
}
