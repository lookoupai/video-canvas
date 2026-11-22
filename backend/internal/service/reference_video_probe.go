package service

import (
	"context"
	"errors"
	"io"
	"os"

	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/provider"
)

const dramaReferenceClipMaxBytes int64 = 50 << 20

// ReferenceVideoMetadata holds metadata read from the stored media file.
type ReferenceVideoMetadata struct {
	DurationMs int64
	ByteSize   int64
	FrameRate  float64
}

// ReferenceVideoMetadataProbe inspects an owned video asset before a reference task is charged.
type ReferenceVideoMetadataProbe interface {
	Probe(context.Context, uint64, uint64, int64) (ReferenceVideoMetadata, error)
}

// ReferenceVideoFrames extracts metadata from a local video file.
type ReferenceVideoFrames interface {
	Probe(context.Context, string) (videoframe.ProbeInfo, error)
}

// AssetReferenceVideoProbe downloads a bounded user-owned asset and reads its actual stream metadata.
type AssetReferenceVideoProbe struct {
	assets provider.AssetStore
	frames ReferenceVideoFrames
}

// NewAssetReferenceVideoProbe wires user asset access to the shared ffprobe extractor.
func NewAssetReferenceVideoProbe(assets provider.AssetStore, frames ReferenceVideoFrames) *AssetReferenceVideoProbe {
	return &AssetReferenceVideoProbe{assets: assets, frames: frames}
}

// Probe reads duration and frame rate from the file, using stored size first to bound local copies.
func (p *AssetReferenceVideoProbe) Probe(ctx context.Context, userID, assetID uint64, maxBytes int64) (ReferenceVideoMetadata, error) {
	var result ReferenceVideoMetadata
	if p.assets == nil || p.frames == nil {
		return result, errors.New("reference video probe unavailable")
	}
	file, err := p.assets.Open(ctx, userID, assetID)
	if err != nil {
		return result, err
	}
	if file == nil || file.Body == nil {
		return result, errors.New("reference video body unavailable")
	}
	defer func() { _ = file.Body.Close() }()
	if file.Asset == nil || file.Asset.UserID != userID || file.Asset.Kind != "video" {
		return result, provider.ErrAssetNotFound
	}
	result.ByteSize = file.Asset.ByteSize
	if maxBytes > 0 && result.ByteSize > maxBytes {
		return result, nil
	}
	dir, err := os.MkdirTemp("", "vc-reference-probe-")
	if err != nil {
		return result, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	target, err := os.CreateTemp(dir, "clip-*.mp4")
	if err != nil {
		return result, err
	}
	path := target.Name()
	limit := maxBytes
	if limit <= 0 {
		limit = dramaReferenceClipMaxBytes
	}
	written, copyErr := io.Copy(target, io.LimitReader(&assetSrcReader{ctx: ctx, r: file.Body}, limit+1))
	closeErr := target.Close()
	if copyErr != nil {
		return result, copyErr
	}
	if closeErr != nil {
		return result, closeErr
	}
	result.ByteSize = written
	if written > limit {
		return result, nil
	}
	info, err := p.frames.Probe(ctx, path)
	if err != nil {
		return result, err
	}
	result.DurationMs, result.FrameRate = info.DurationMs, info.FrameRate
	return result, nil
}
