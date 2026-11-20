package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// Generate 固定视频种类并在短剧画布的seg节点创建任务，幂等键沿用HTTP请求头。
// 归属检查事务先结束，再提交任务，避免同步观察者再次锁项目造成死锁。
func (s *DramaSegmentService) Generate(ctx context.Context, userID, sid uint64, key string, req *model.GenerateDramaSegmentReq) (*model.GenerationTaskView, error) {
	// 1. 生成任务统一由既有服务校验模型、输入、积分和并发。
	if req == nil || req.ModelID == "" || req.Input == nil {
		return nil, errcode.ErrInvalidParams
	}
	row, err := s.repo.Segment(ctx, userID, sid, false)
	if err != nil {
		return nil, segmentError(err)
	}
	remake, err := segmentRemakeOf(row)
	if err != nil {
		return nil, err
	}
	if remake != nil {
		return s.generateRemake(ctx, row, key, req)
	}
	var project *model.DramaProject
	err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, ep, _, err := lockSegment(ctx, tx, userID, sid)
		if err != nil {
			return err
		}
		if ep.LockedAt == nil {
			return errcode.ErrDramaStepNotReady.WithMsg("媒体任务只允许已锁定的集")
		}
		jobs, err := tx.Jobs(ctx, p.UserID, p.ID, true)
		if err != nil {
			return err
		}
		if dramaJobsConflict(jobs, &model.DramaJobReq{Step: "segment", Episodes: []int{ep.No}}) {
			return errcode.ErrDramaJobBusy
		}
		project = p
		return nil
	})
	if err != nil {
		return nil, segmentError(err)
	}
	result, err := s.tasks.Create(ctx, userID, key, &model.CreateGenerationTaskReq{Kind: model.KindVideo, ModelID: req.ModelID, CanvasID: idcodec.ID(project.CanvasID), NodeID: segmentNode(sid), Input: req.Input})
	if err != nil {
		return nil, err
	}
	return segmentCreatedTask(result, project.CanvasID, sid)
}
