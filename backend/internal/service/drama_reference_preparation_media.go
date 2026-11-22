package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"io"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/provider"
	"video-canvas/internal/reference"
	"video-canvas/internal/repository"
)

// prepareDocument 仅在缺少镜头时运行本地分析，结果固定在准备中，不覆盖正式文档。
func (s *DramaReferencePreparationService) prepareDocument(ctx context.Context, row *model.DramaReferencePreparation, file string, audio bool) error {
	doc, ids, err := preparationDocument(row)
	if err != nil {
		return err
	}
	if len(doc.Shots) == 0 {
		if s.analyzer == nil || !s.analyzer.Available() {
			return errors.New("本地分析器不可用")
		}
		doc, err = s.analyzer.Analyze(ctx, file, row.DurationMs)
		if err != nil {
			return err
		}
		if err := reference.ValidateDocument(doc, row.DurationMs); err != nil {
			return err
		}
		ids, err = preparationSelection(doc, ids, true)
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	selected, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		fresh, err := lockedPreparation(ctx, tx, row.UserID, row.DramaID, row.ReferenceID, row.ID)
		if err != nil {
			return err
		}
		if !activePreparation(fresh, row.Attempt) {
			return context.Canceled
		}
		fresh.Document, fresh.ShotIDs, fresh.HasAudio = raw, selected, &audio
		if err := planPreparationFrames(ctx, tx, fresh); err != nil {
			return err
		}
		*row = *fresh
		return nil
	})
}

// extractPreparedFrame 先验证缓存图片，失效时重新提取；只有素材落盘后才提交完成进度。
func (s *DramaReferencePreparationService) extractPreparedFrame(ctx context.Context, row *model.DramaReferencePreparation, frame *model.DramaReferencePreparationFrame, file string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cache, err := s.repo.FrameCache(ctx, row.UserID, frame.CacheID)
	if err != nil {
		return err
	}
	ready, err := s.validPreparedAsset(ctx, row.UserID, cache.AssetID)
	if err != nil {
		return err
	}
	assetID := cache.AssetID
	if !ready {
		data, err := s.frames.Frame(ctx, file, float64(frame.AtMs)/1000, videoframe.FrameOpts{Format: videoframe.PNG, MaxEdge: 1280})
		if err != nil {
			return err
		}
		asset, err := s.store.SaveReferenceFrame(ctx, row.UserID, row.SourceAssetID, cache.ID, cache.StorageToken, data)
		if err != nil {
			return err
		}
		if asset == nil || asset.Kind != "image" {
			return errors.New("提帧存储没有返回图片")
		}
		assetID = asset.ID
	}
	return s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		fresh, err := lockedPreparation(ctx, tx, row.UserID, row.DramaID, row.ReferenceID, row.ID)
		if err != nil {
			return err
		}
		if !activePreparation(fresh, row.Attempt) {
			return context.Canceled
		}
		cache.AssetID, frame.AssetID = assetID, assetID
		if err := tx.SaveFrameCache(ctx, cache); err != nil {
			return err
		}
		if err := tx.SaveFrame(ctx, frame); err != nil {
			return err
		}
		return savePreparationCounts(ctx, tx, fresh)
	})
}

// validPreparedAsset 检查真实文件仍可读且能解码，素材行存在不代表缓存可复用。
func (s *DramaReferencePreparationService) validPreparedAsset(ctx context.Context, u, id uint64) (bool, error) {
	if id == 0 {
		return false, nil
	}
	file, err := s.assets.Open(ctx, u, id)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, provider.ErrAssetNotFound) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = file.Body.Close() }()
	if file.Asset == nil || file.Asset.UserID != u || file.Asset.Kind != "image" {
		return false, nil
	}
	data, err := io.ReadAll(io.LimitReader(&assetSrcReader{ctx: ctx, r: file.Body}, (16<<20)+1))
	if err != nil {
		return false, err
	}
	if len(data) > 16<<20 {
		return false, nil
	}
	_, err = png.Decode(bytes.NewReader(data))
	return err == nil, nil
}
