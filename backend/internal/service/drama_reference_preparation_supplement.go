package service

import (
	"context"
	"sort"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// AddFrames 在冻结镜窗内加入有限采样点，保留已有结果并提交新的可恢复尝试。
func (s *DramaReferencePreparationService) AddFrames(ctx context.Context, u, p, ref, id uint64, req *model.AddDramaReferenceFramesReq) (*model.DramaReferencePreparationView, error) {
	// 1. 按镜号和时间排序固定指纹；合法重传优先返回已提交结果。
	points, err := normalizeFramePoints(req)
	if err != nil {
		return nil, err
	}
	key, hash, err := preparationCommand(req.RequestKey, id, points)
	if err != nil {
		return nil, err
	}
	row, err := s.ownedPreparation(ctx, u, p, ref, id)
	if err != nil {
		return nil, err
	}
	prior, err := priorPreparationRequest(ctx, s.repo, u, p, ref, key, "frames", hash)
	if err != nil || prior != nil {
		return preparationResult(prior, err)
	}
	if activePreparation(row, row.Attempt) {
		return nil, errcode.ErrCanvasConflict.WithMsg("请等待本次准备完成或取消后再补帧")
	}
	missing, err := s.missingPreparedFrames(ctx, row)
	if err != nil {
		return nil, err
	}
	// 2. 项目锁内校验单镜预算，采样计划与新尝试一并提交。
	err = s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		fresh, err := lockedPreparation(ctx, tx, u, p, ref, id)
		if err != nil {
			return err
		}
		prior, err := priorPreparationRequest(ctx, tx, u, p, ref, key, "frames", hash)
		if err != nil {
			return err
		}
		if prior != nil {
			row = prior
			return nil
		}
		if activePreparation(fresh, fresh.Attempt) {
			return errcode.ErrCanvasConflict
		}
		if err := supplementPreparation(ctx, tx, fresh, points, missing); err != nil {
			return err
		}
		row = fresh
		return rememberPreparationRequest(ctx, tx, fresh, key, "frames", hash)
	})
	return preparationResult(row, dramaError(err))
}

// normalizeFramePoints 明确拒绝重复点，保证不同顺序表达同一补帧请求。
func normalizeFramePoints(req *model.AddDramaReferenceFramesReq) ([]model.DramaReferenceFramePoint, error) {
	if req == nil || len(req.Frames) == 0 || len(req.Frames) > 24 {
		return nil, errcode.ErrInvalidParams
	}
	points := append([]model.DramaReferenceFramePoint{}, req.Frames...)
	sort.Slice(points, func(i, j int) bool {
		if points[i].ShotID != points[j].ShotID {
			return points[i].ShotID < points[j].ShotID
		}
		return points[i].AtMs < points[j].AtMs
	})
	for i, point := range points {
		if point.ShotID == "" || point.AtMs < 0 || i > 0 && point == points[i-1] {
			return nil, errcode.ErrInvalidParams
		}
	}
	return points, nil
}

// additionalFramePlan 检查原镜范围与每镜 24 帧上限，已有采样点不消耗额外预算。
func additionalFramePlan(ctx context.Context, tx repository.DramaReferencePreparationTx, row *model.DramaReferencePreparation, points []model.DramaReferenceFramePoint) ([]model.DramaReferencePreparationFrame, error) {
	doc, ids, err := preparationDocument(row)
	if err != nil {
		return nil, err
	}
	shots := map[string]model.DramaReferenceShot{}
	for _, shot := range selectedPreparationShots(doc, ids) {
		shots[shot.ID] = shot
	}
	existing := map[string]map[int64]bool{}
	frames := []model.DramaReferencePreparationFrame{}
	for _, point := range points {
		shot, ok := shots[point.ShotID]
		if !ok || point.AtMs < shot.StartMs || point.AtMs >= shot.EndMs {
			return nil, errcode.ErrInvalidParams.WithMsg("补帧时间必须位于已选择的原镜内")
		}
		if existing[shot.ID] == nil {
			prior, err := tx.Frames(ctx, row.UserID, row.ID, shot.ID, 0, 25)
			if err != nil {
				return nil, err
			}
			existing[shot.ID] = map[int64]bool{}
			for _, frame := range prior {
				existing[shot.ID][frame.AtMs] = true
			}
		}
		if existing[shot.ID][point.AtMs] {
			continue
		}
		if len(existing[shot.ID]) >= 24 {
			return nil, errcode.ErrInvalidParams.WithMsg("每镜最多保留 24 个采样点")
		}
		existing[shot.ID][point.AtMs] = true
		frames = append(frames, model.DramaReferencePreparationFrame{UserID: row.UserID, PreparationID: row.ID, ShotID: shot.ID, StartMs: shot.StartMs, EndMs: shot.EndMs, AtMs: point.AtMs})
	}
	return frames, nil
}

// supplementPreparation 原子合并新采样点、失效帧重置和下一次入队。
func supplementPreparation(ctx context.Context, tx repository.DramaReferencePreparationTx, row *model.DramaReferencePreparation, points []model.DramaReferenceFramePoint, missing []model.DramaReferencePreparationFrame) error {
	frames, err := additionalFramePlan(ctx, tx, row, points)
	if err != nil {
		return err
	}
	if err := resetMissingFrames(ctx, tx, missing); err != nil {
		return err
	}
	if err := insertPreparedFrames(ctx, tx, row, frames); err != nil {
		return err
	}
	return queuePreparationAgain(ctx, tx, row)
}
