package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// HandlePreparation 逐帧保存当前尝试，进程重启沿用已完成帧；用户取消终止在途媒体进程。
func (s *DramaReferencePreparationService) HandlePreparation(ctx context.Context, u, p, ref, id uint64, attempt int) error {
	// 1. 锁内确认归属和尝试，running 允许 River 在进程重启后续跑。
	row, err := s.startPreparation(ctx, u, p, ref, id, attempt)
	if err != nil || row == nil {
		return err
	}
	// 2. 下载、探测与提帧在事务外执行，状态轮询让其他实例提交的取消也能生效。
	work, cancel := context.WithTimeout(ctx, 18*time.Minute)
	done := make(chan struct{})
	identity := *row
	go s.watchPreparation(work, cancel, done, &identity)
	workErr := s.runPreparation(work, row)
	cancel()
	<-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// 3. 已完成帧独立保留；媒体错误只有本次准备失败，不改写正式稿。
	if workErr != nil && !errors.Is(workErr, context.Canceled) {
		logger.Warn("参考媒体准备失败", zap.Uint64("preparation_id", id), zap.Error(workErr))
	}
	return s.finishPreparation(ctx, row, workErr)
}

// startPreparation 在项目锁内允许当前 queued/running 尝试，过时任务直接结束。
func (s *DramaReferencePreparationService) startPreparation(ctx context.Context, u, p, ref, id uint64, attempt int) (*model.DramaReferencePreparation, error) {
	var row *model.DramaReferencePreparation
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		fresh, err := lockedPreparation(ctx, tx, u, p, ref, id)
		if err != nil {
			return err
		}
		if !activePreparation(fresh, attempt) {
			return nil
		}
		fresh.Status = "running"
		fresh.Error = ""
		row = fresh
		return tx.Save(ctx, fresh)
	})
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return row, err
}

// lockedPreparation 先锁存活项目，再读取同一用户项目下的准备。
func lockedPreparation(ctx context.Context, tx repository.DramaReferencePreparationTx, u, p, ref, id uint64) (*model.DramaReferencePreparation, error) {
	if _, err := tx.Reference(ctx, u, p, ref, true); err != nil {
		return nil, err
	}
	return tx.Preparation(ctx, u, p, ref, id)
}

// activePreparation 只允许当前未终止尝试继续提交结果。
func activePreparation(row *model.DramaReferencePreparation, attempt int) bool {
	return row.Attempt == attempt && (row.Status == "queued" || row.Status == "running")
}

// watchPreparation 按持久状态取消工作上下文，数据库故障也停止继续调用媒体工具。
func (s *DramaReferencePreparationService) watchPreparation(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}, row *model.DramaReferencePreparation) {
	defer close(done)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fresh, err := s.ownedPreparation(ctx, row.UserID, row.DramaID, row.ReferenceID, row.ID)
			if err != nil || !activePreparation(fresh, row.Attempt) {
				cancel()
				return
			}
		}
	}
}

// runPreparation 在随机受控目录下载本人视频，所有退出路径清理临时文件。
func (s *DramaReferencePreparationService) runPreparation(ctx context.Context, row *model.DramaReferencePreparation) error {
	dir, err := os.MkdirTemp("", "vc-reference-frames-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "source.mp4")
	source := &model.DramaReferenceEpisode{UserID: row.UserID, AssetID: row.SourceAssetID, DurationMs: row.DurationMs}
	downloader := DramaReferenceService{assets: s.assets}
	if err := downloader.downloadReference(ctx, source, file); err != nil {
		return err
	}
	info, err := s.frames.Probe(ctx, file)
	if err != nil {
		return err
	}
	if info.DurationMs < row.DurationMs-100 || info.DurationMs > row.DurationMs+100 {
		return errors.New("参考视频实际时长与快照不一致")
	}
	if err := s.prepareDocument(ctx, row, file, info.HasAudio); err != nil {
		return err
	}
	for {
		frames, err := s.repo.PendingFrames(ctx, row.UserID, row.ID, 20)
		if err != nil {
			return err
		}
		if len(frames) == 0 {
			return nil
		}
		for i := range frames {
			if err := s.extractPreparedFrame(ctx, row, &frames[i], file); err != nil {
				return err
			}
		}
	}
}

// finishPreparation 用最新计数判断完成，迟到的旧尝试不能复活已取消的任务。
func (s *DramaReferencePreparationService) finishPreparation(ctx context.Context, expected *model.DramaReferencePreparation, workErr error) error {
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		row, err := lockedPreparation(ctx, tx, expected.UserID, expected.DramaID, expected.ReferenceID, expected.ID)
		if err != nil {
			return err
		}
		if !activePreparation(row, expected.Attempt) {
			return nil
		}
		row.Status = "succeeded"
		row.Error = ""
		if workErr != nil {
			row.Status = "failed"
			row.Error = "媒体准备未完成，已保存帧保留，请恢复后继续"
		}
		return savePreparationCounts(ctx, tx, row)
	})
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	return err
}
