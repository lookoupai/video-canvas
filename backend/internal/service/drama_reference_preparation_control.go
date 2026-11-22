package service

import (
	"context"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// Cancel 保留全部已完成帧，持久取消状态会让在途媒体调用退出。
func (s *DramaReferencePreparationService) Cancel(ctx context.Context, u, p, ref, id uint64) (*model.DramaReferencePreparationView, error) {
	// 1. 锁内核验归属，重复取消和已经完成均返回当前状态。
	var row *model.DramaReferencePreparation
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		var err error
		row, err = lockedPreparation(ctx, tx, u, p, ref, id)
		if err != nil {
			return err
		}
		if row.Status == "succeeded" || row.Status == "canceled" {
			return nil
		}
		row.Status = "canceled"
		row.Error = ""
		return tx.Save(ctx, row)
	})
	// 2. 读取独立进度，不删除已保存证据。
	return preparationResult(row, dramaError(err))
}

// Resume 显式恢复原准备；新的尝试跳过有效帧，同请求键不会重复排队。
func (s *DramaReferencePreparationService) Resume(ctx context.Context, u, p, ref, id uint64, req *model.ResumeDramaReferencePreparationReq) (*model.DramaReferencePreparationView, error) {
	// 1. 请求指纹同时绑定准备 ID，避免同键跨准备误复用。
	if req == nil {
		return nil, errcode.ErrInvalidParams
	}
	key, hash, err := preparationCommand(req.RequestKey, id, nil)
	if err != nil {
		return nil, err
	}
	row, err := s.ownedPreparation(ctx, u, p, ref, id)
	if err != nil {
		return nil, err
	}
	prior, err := priorPreparationRequest(ctx, s.repo, u, p, ref, key, "resume", hash)
	if err != nil || prior != nil {
		return preparationResult(prior, err)
	}
	missing, err := s.missingPreparedFrames(ctx, row)
	if err != nil {
		return nil, err
	}
	// 2. 在锁内重查指令，将失效帧重置与新尝试排队原子提交。
	err = s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		fresh, err := lockedPreparation(ctx, tx, u, p, ref, id)
		if err != nil {
			return err
		}
		prior, err := priorPreparationRequest(ctx, tx, u, p, ref, key, "resume", hash)
		if err != nil {
			return err
		}
		if prior != nil {
			row = prior
			return nil
		}
		if !activePreparation(fresh, fresh.Attempt) {
			if err := resetMissingFrames(ctx, tx, missing); err != nil {
				return err
			}
			if err := queuePreparationAgain(ctx, tx, fresh); err != nil {
				return err
			}
		}
		row = fresh
		return rememberPreparationRequest(ctx, tx, fresh, key, "resume", hash)
	})
	return preparationResult(row, dramaError(err))
}

// preparationCommand 固定用户指令键与准备 ID，并校验键长度。
func preparationCommand(key string, id uint64, body any) (string, string, error) {
	key = strings.TrimSpace(key)
	if key == "" || !validDraftText(key, 128) {
		return "", "", errcode.ErrInvalidParams
	}
	hash, err := preparationHash(struct {
		ID   uint64
		Body any
	}{id, body})
	return key, hash, err
}

// missingPreparedFrames 在事务外按页检查已完成素材；最多每镜 24 帧，避免无界查询。
func (s *DramaReferencePreparationService) missingPreparedFrames(ctx context.Context, row *model.DramaReferencePreparation) ([]model.DramaReferencePreparationFrame, error) {
	missing := []model.DramaReferencePreparationFrame{}
	var cursor uint64
	for {
		frames, err := s.repo.Frames(ctx, row.UserID, row.ID, "", cursor, 100)
		if err != nil {
			return nil, err
		}
		for _, frame := range frames {
			if frame.AssetID == 0 {
				continue
			}
			valid, err := s.validPreparedAsset(ctx, row.UserID, frame.AssetID)
			if err != nil {
				return nil, err
			}
			if !valid {
				frame.AssetID = 0
				missing = append(missing, frame)
			}
		}
		if len(frames) < 100 {
			return missing, nil
		}
		cursor = frames[len(frames)-1].ID
	}
}

// resetMissingFrames 只清除失效素材关联，系统时间证据及完成帧保持不变。
func resetMissingFrames(ctx context.Context, tx repository.DramaReferencePreparationTx, frames []model.DramaReferencePreparationFrame) error {
	for i := range frames {
		if err := tx.SaveFrame(ctx, &frames[i]); err != nil {
			return err
		}
	}
	return nil
}

// queuePreparationAgain 只有存在未完成项或尚无镜头时才增加尝试并排队。
func queuePreparationAgain(ctx context.Context, tx repository.DramaReferencePreparationTx, row *model.DramaReferencePreparation) error {
	total, ready, err := tx.FrameCounts(ctx, row.UserID, row.ID)
	if err != nil {
		return err
	}
	row.TotalFrames, row.CompletedFrames = total, ready
	if total > 0 && ready == total {
		row.Status = "succeeded"
		row.Error = ""
		return tx.Save(ctx, row)
	}
	row.Attempt++
	row.Status = "queued"
	row.Error = ""
	if err := tx.Save(ctx, row); err != nil {
		return err
	}
	return tx.Enqueue(ctx, row.UserID, row.DramaID, row.ReferenceID, row.ID, row.Attempt)
}
