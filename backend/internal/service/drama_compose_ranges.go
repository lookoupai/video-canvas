package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/videoframe"
)

// dramaComposeRange 与采用素材一一对应，零终点表示普通片段保持整条视频。
type dramaComposeRange struct {
	StartMs int64 `json:"start_ms"`
	EndMs   int64 `json:"end_ms"`
}

// segmentComposeRange 校验并冻结成片窗口，源片时间只供参考，不参与目标素材裁剪。
func segmentComposeRange(raw []byte) (dramaComposeRange, error) {
	var remake model.DramaSegmentRemake
	if len(raw) == 0 {
		return dramaComposeRange{}, nil
	}
	if err := json.Unmarshal(raw, &remake); err != nil {
		return dramaComposeRange{}, err
	}
	if remake.ReferenceID == "" {
		return dramaComposeRange{}, nil
	}
	if err := validateRemakeWindow(remake.TrimStartMs, remake.TargetDurationMs); err != nil {
		return dramaComposeRange{}, err
	}
	return dramaComposeRange{StartMs: remake.TrimStartMs, EndMs: remake.TrimStartMs + remake.TargetDurationMs}, nil
}

// validateRemakeWindow 拒绝空窗口、少于一帧及过大的整数，避免截断或相加溢出。
func validateRemakeWindow(start, duration int64) error {
	const maxWindowMs = 24 * 60 * 60 * 1000
	if start < 0 || duration < 33 || start > maxWindowMs || duration > maxWindowMs-start {
		return errcode.ErrInvalidParams.WithMsg("成片裁剪区间须至少 33 毫秒且位于 24 小时以内")
	}
	return nil
}

// hasDramaComposeRanges 保持旧项目和未带仿拍片段的合成行为不变。
func hasDramaComposeRanges(ranges []dramaComposeRange) bool {
	for _, window := range ranges {
		if window.EndMs > 0 {
			return true
		}
	}
	return false
}

// composeDramaPaths 对采用时固定的窗口转码，混合的普通片段仍取本素材全长。
func (s *DramaService) composeDramaPaths(ctx context.Context, userID uint64, input *dramaComposeInput, paths []string, dst string) error {
	if len(input.Ranges) == 0 {
		return s.compose.Compose(ctx, paths, dst)
	}
	composer, ok := s.compose.(MarketingRangeComposer)
	if !ok || len(input.Ranges) != len(paths) || len(paths) != len(input.AssetIDs) {
		return errors.New("合成窗口或裁剪服务不可用")
	}
	windows := make([]videoframe.Window, 0, len(paths))
	for i, path := range paths {
		window := input.Ranges[i]
		if window.EndMs == 0 {
			asset, err := s.assets.View(ctx, userID, input.AssetIDs[i])
			if err != nil {
				return err
			}
			if asset == nil || asset.Kind != model.KindVideo || asset.DurationMs <= 0 {
				return errors.New("普通片段的视频时长不可用")
			}
			window.EndMs = asset.DurationMs
		}
		if err := validateRemakeWindow(window.StartMs, window.EndMs-window.StartMs); err != nil {
			return err
		}
		windows = append(windows, videoframe.Window{Path: path, StartMs: int(window.StartMs), EndMs: int(window.EndMs)})
	}
	return composer.ComposeRanges(ctx, windows, dst, false)
}
