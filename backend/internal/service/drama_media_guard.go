package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// checkDramaMediaTasks 分镜、合成提交前检查所选集里有没有进行中的片段视频，不影响其他集与文本任务。
func checkDramaMediaTasks(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, eps []model.DramaEpisode, key string) error {
	for _, ep := range eps {
		if ep.LockedAt == nil {
			return errcode.ErrDramaStepNotReady.WithMsg("媒体任务只允许已锁定的集")
		}
		rows, err := tx.Segments(ctx, p.UserID, ep.ID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			tasks, err := tx.SegmentTasks(ctx, p.UserID, p.CanvasID, segmentNode(row.ID))
			if err != nil {
				return err
			}
			for _, task := range tasks {
				if slices.Contains(model.ActiveTaskStatuses, task.Status) && (key == "" || task.IdempotencyKey != key) {
					return errcode.ErrDramaJobBusy
				}
			}
		}
	}
	return nil
}

// guardDramaGeneration 在生成任务自身事务里锁短剧项目，防止同一片段的两次提交、或片段与同集分镜、合成同时穿透预检查。
// 同一集的不同片段可以同时生成，总并发交给任务队列统一限制。
func guardDramaGeneration(ctx context.Context, tx repository.GenerationTaskTx, task *model.GenerationTask) error {
	if task.IsTest || !strings.HasPrefix(task.NodeID, "seg:") {
		return nil
	}
	adapter, ok := tx.(interface{ DramaTx() repository.DramaTx })
	if !ok {
		return errcode.ErrInternal
	}
	dtx := adapter.DramaTx()
	sid, ok := idcodec.Decode(strings.TrimPrefix(task.NodeID, "seg:"))
	if !ok {
		return errcode.ErrInvalidParams
	}
	p, ep, row, err := lockSegment(ctx, dtx, task.UserID, sid)
	if err != nil {
		return segmentError(err)
	}
	// 项目锁等待期间原请求可能已经完成并开始下一轮重拍；旧键仍恢复自己的任务。
	if task.IdempotencyKey != "" {
		if existing, lookupErr := tx.FindByIdempotencyKey(ctx, task.UserID, task.IdempotencyKey); lookupErr == nil && existing != nil {
			return nil
		} else if lookupErr != nil && !errors.Is(lookupErr, repository.ErrNotFound) {
			return lookupErr
		}
	}
	if task.CanvasProjectID == nil || *task.CanvasProjectID != p.CanvasID || task.Kind != model.KindVideo {
		return errcode.ErrInvalidParams
	}
	if ep.LockedAt == nil {
		return errcode.ErrDramaStepNotReady
	}
	if err := guardSegmentIntent(ctx, task, row); err != nil {
		return err
	}
	jobs, err := dtx.Jobs(ctx, p.UserID, p.ID, true)
	if err != nil {
		return err
	}
	if dramaJobsConflict(jobs, &model.DramaJobReq{Step: "segment", Episodes: []int{ep.No}}) {
		return errcode.ErrDramaJobBusy
	}
	tasks, err := dtx.SegmentTasks(ctx, p.UserID, p.CanvasID, task.NodeID)
	if err != nil {
		return err
	}
	for _, other := range tasks {
		if slices.Contains(model.ActiveTaskStatuses, other.Status) && other.IdempotencyKey != task.IdempotencyKey {
			return errcode.ErrDramaJobBusy
		}
	}
	return nil
}
