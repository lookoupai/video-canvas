package service

import (
	"context"
	"encoding/json"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"

	"go.uber.org/zap"
)

// HandleRemakeComposition 处理队列中的一次合成尝试。业务失败写回合成记录，基础设施失败由队列重试。
func (s *MarketingService) HandleRemakeComposition(ctx context.Context, userID, planID, id uint64, attempt int) error {
	// 1. 用计划归属和尝试号检查这次投递仍需处理，项目已删除时跳过。
	if _, err := s.repo.RemakePlan(ctx, userID, planID, false); err != nil {
		if mktNotFound(err) {
			return nil
		}
		return err
	}
	row, err := s.repo.RemakeComposition(ctx, userID, planID, id)
	if mktNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status != "queued" || row.Attempt != attempt {
		return nil
	}
	// 2. 下载、裁切、上传均在事务外。worker 中断时留下未完成记录由 River 恢复。
	asset, renderErr := s.renderRemakeComposition(ctx, row)
	if ctx.Err() != nil {
		if asset != nil {
			s.remakeAssets.DiscardCopies(context.WithoutCancel(ctx), []*model.Asset{asset})
		}
		return ctx.Err()
	}
	failure := ""
	if renderErr != nil {
		logger.Warn("营销逐镜合成失败", zap.Uint64("composition_id", id), zap.Error(renderErr))
		failure = "合成失败，请检查采用视频和裁剪时间后重试"
	}
	// 3. 原子登记成片与成功状态。若已由其他投递完成、父项目已删除或提交失败，回收临时对象。
	adopted, err := s.finishRemakeComposition(ctx, row, asset, failure)
	if !adopted && asset != nil {
		s.remakeAssets.DiscardCopies(context.WithoutCancel(ctx), []*model.Asset{asset})
	}
	return err
}

// renderRemakeComposition 固定使用提交时的素材及裁切时间窗，重试期间计划可能已改动。
func (s *MarketingService) renderRemakeComposition(ctx context.Context, row *model.MarketingRemakeComposition) (*model.Asset, error) {
	var input []model.RemakeCompositionPart
	if err := json.Unmarshal(row.Parts, &input); err != nil {
		return nil, fmt.Errorf("解析营销合成清单：%w", err)
	}
	parts := make([]RemakeMediaPart, 0, len(input))
	for _, part := range input {
		parts = append(parts, RemakeMediaPart{AssetID: part.AssetID, StartMs: part.StartMs, EndMs: part.EndMs})
	}
	return s.remakeComposeMedia(ctx, row.UserID, parts)
}

// finishRemakeComposition 在计划锁内检查尝试号，防止迟到的回调覆盖之后的重试或重复插入素材。
func (s *MarketingService) finishRemakeComposition(ctx context.Context, row *model.MarketingRemakeComposition, asset *model.Asset, failure string) (bool, error) {
	adopted := false
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		if _, err := tx.RemakePlan(ctx, row.UserID, row.PlanID, true); err != nil {
			return err
		}
		current, err := tx.RemakeComposition(ctx, row.UserID, row.PlanID, row.ID)
		if err != nil {
			return err
		}
		if current.Status != "queued" || current.Attempt != row.Attempt {
			return nil
		}
		if asset != nil && (asset.UserID != row.UserID || asset.Kind != model.KindVideo) {
			return fmt.Errorf("成片素材与提交方不匹配")
		}
		if err := tx.FinishRemakeComposition(ctx, current, asset, failure); err != nil {
			return err
		}
		adopted = asset != nil
		return nil
	})
	if mktNotFound(err) {
		return false, nil
	}
	return adopted && err == nil, err
}
