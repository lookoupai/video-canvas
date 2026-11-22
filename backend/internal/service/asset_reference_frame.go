package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// SaveReferenceFrame 按本人源视频与随机持久缓存令牌保存，重传复用素材行并修复丢失文件。
func (s *AssetService) SaveReferenceFrame(ctx context.Context, u, source, frame uint64, token string, data []byte) (*model.AssetView, error) {
	// 1. 只接收有界且真实可解码的 PNG，系统媒体输入也不能制造无效素材行。
	if u == 0 || source == 0 || frame == 0 || len(data) == 0 || len(data) > 16<<20 || len(token) != 48 {
		return nil, errcode.ErrAssetInvalid
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 {
		return nil, errcode.ErrAssetInvalid
	}
	digest := sha256.Sum256(data)
	sum := hex.EncodeToString(digest[:])
	if _, err := hex.DecodeString(token); err != nil {
		return nil, errcode.ErrAssetInvalid
	}
	key := fmt.Sprintf("reference/%d/%s.png", u, token)
	// 2. 稳定存储键保证崩溃后再提交不会多建一份；已有记录使用原存储而非当前默认存储。
	existing, err := s.repo.GetByStorageKey(ctx, key)
	if err == nil {
		if existing.UserID != u || existing.Kind != "image" {
			return nil, errcode.ErrAssetInvalid
		}
		return s.restoreReferenceFrame(ctx, existing, data)
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	handle, err := s.stores.Default(ctx)
	if err != nil {
		return nil, err
	}
	if err := handle.Storage.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "image/png"); err != nil {
		return nil, err
	}
	row := &model.Asset{UserID: u, Kind: "image", StorageID: handle.ID, StorageKey: key, MimeType: "image/png", ByteSize: int64(len(data)),
		Width: config.Width, Height: config.Height, Source: model.AssetSourceUpload, FileName: fmt.Sprintf("reference-frame-%d.png", frame), Sha256: sum}
	// 3. 唯一键竞争时读取胜出的行；不删除共享对象，避免失败请求破坏成功请求的文件。
	if err := s.repo.Create(ctx, row); err != nil {
		prior, readErr := s.repo.GetByStorageKey(ctx, key)
		if readErr != nil {
			return nil, fmt.Errorf("保存参考帧素材失败: %w", err)
		}
		if prior.UserID != u || prior.Kind != "image" {
			return nil, errcode.ErrAssetInvalid
		}
		return s.restoreReferenceFrame(ctx, prior, data)
	}
	return assetView(row, s.fileURL(key)), nil
}

// restoreReferenceFrame 原键覆盖相同摘要的图片可修复对象缺失，不创建重复素材记录。
func (s *AssetService) restoreReferenceFrame(ctx context.Context, row *model.Asset, data []byte) (*model.AssetView, error) {
	handle, err := s.stores.Get(ctx, row.StorageID)
	if err != nil {
		return nil, err
	}
	if err := handle.Storage.Put(ctx, row.StorageKey, bytes.NewReader(data), int64(len(data)), "image/png"); err != nil {
		return nil, err
	}
	return assetView(row, s.fileURL(row.StorageKey)), nil
}
