package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/videoframe"
)

// MarketingClipRepo 保存可复用的参考裁片。
type MarketingClipRepo interface {
	// FindRemakeClip 按原素材、用户和有序裁切配方查找仍可用的素材。
	FindRemakeClip(ctx context.Context, userID, sourceID uint64, recipe string) (uint64, error)
	// KeepRemakeClip 登记派生素材与配方，返回并发登记后被采用的那一个 ID。
	KeepRemakeClip(ctx context.Context, userID, sourceID uint64, recipe string, asset *model.Asset) (uint64, error)
}

// MarketingRangeComposer 生成独立原片裁片和按采用窗口合成的成片。
type MarketingRangeComposer interface {
	// ComposeRanges 按顺序转码本地时间窗；mute 为真时去除声音。
	ComposeRanges(context.Context, []videoframe.Window, string, bool) error
}

// RemakeMediaPart 是某个已校验归属的视频需要保留的毫秒窗口。
type RemakeMediaPart struct {
	AssetID        uint64
	StartMs, EndMs int
}

// RemakeMediaService 为营销与短剧共用原镜裁片和成片裁剪，缓存继续按用户与源素材隔离。
type RemakeMediaService struct {
	repo     MarketingClipRepo
	assets   DramaComposeAssets
	composer MarketingRangeComposer
}

// NewRemakeMediaService 组装可复用的媒体处理能力，不启动后台任务。
func NewRemakeMediaService(repo MarketingClipRepo, assets DramaComposeAssets, composer MarketingRangeComposer) *RemakeMediaService {
	return &RemakeMediaService{repo: repo, assets: assets, composer: composer}
}

// remakeReferenceClip 逐个保留非连续区间并去掉原片音轨，经所有者隔离的配方缓存复用。
func (s *MarketingService) remakeReferenceClip(ctx context.Context, userID, assetID uint64, sources []model.MarketingRemakeSource) (uint64, error) {
	if len(sources) == 0 || assetID == 0 {
		return 0, errcode.ErrMarketingAssets
	}
	parts := make([]RemakeMediaPart, 0, len(sources))
	for _, source := range sources {
		parts = append(parts, RemakeMediaPart{AssetID: assetID, StartMs: source.StartMs, EndMs: source.EndMs})
	}
	return NewRemakeMediaService(s.repo, s.remakeAssets, s.remakeComposer).Clip(ctx, userID, assetID, parts)
}

// Clip 按固定顺序合并原镜区间并去除声音；缓存失效时重新确认素材归属后裁片。
func (s *RemakeMediaService) Clip(ctx context.Context, userID, assetID uint64, parts []RemakeMediaPart) (uint64, error) {
	// 1. 只接受同一源片的有效区间，配方包含顺序，不能把不连续片段扩成一个大区间。
	if len(parts) == 0 || assetID == 0 || s.repo == nil {
		return 0, errcode.ErrInvalidParams.WithMsg("原片参考区间不可用")
	}
	hash := sha256.New()
	_, _ = fmt.Fprint(hash, "v1/h264/30fps/mute")
	for _, part := range parts {
		if part.AssetID != assetID || part.StartMs < 0 || part.EndMs <= part.StartMs {
			return 0, errcode.ErrInvalidParams.WithMsg("原片参考区间不可用")
		}
		_, _ = fmt.Fprintf(hash, "/%d-%d", part.StartMs, part.EndMs)
	}
	recipe := fmt.Sprintf("%x", hash.Sum(nil))
	cached, err := s.repo.FindRemakeClip(ctx, userID, assetID, recipe)
	if err == nil {
		return cached, nil
	}
	if !mktNotFound(err) {
		return 0, err
	}
	// 2. 转码在事务外，登记时再次确认源素材归属；并发失败方回收未引用的文件。
	asset, err := s.rangeMedia(ctx, userID, parts, true)
	if err != nil {
		return 0, err
	}
	asset.FileName = "跟拍镜头参考.mp4"
	winner, err := s.repo.KeepRemakeClip(ctx, userID, assetID, recipe, asset)
	if err != nil || winner != asset.ID {
		s.assets.DiscardCopies(context.WithoutCancel(ctx), []*model.Asset{asset})
	}
	return winner, err
}

// remakeComposeMedia 按提交时固定的素材、顺序和区间转码，保留每个采用版本的声音。
func (s *MarketingService) remakeComposeMedia(ctx context.Context, userID uint64, parts []RemakeMediaPart) (*model.Asset, error) {
	return NewRemakeMediaService(s.repo, s.remakeAssets, s.remakeComposer).rangeMedia(ctx, userID, parts, false)
}

// rangeMedia 仅读取本人的素材，用受控本地文件交给 FFmpeg；上传后由调用者在短事务中登记。
func (s *RemakeMediaService) rangeMedia(ctx context.Context, userID uint64, parts []RemakeMediaPart, mute bool) (*model.Asset, error) {
	if s.assets == nil || s.composer == nil {
		return nil, errcode.ErrInternal.WithMsg("未配置视频裁切服务")
	}
	ctx, cancel := context.WithTimeout(ctx, dramaComposeTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "vc-remake-")
	if err != nil {
		return nil, fmt.Errorf("创建仿拍临时目录：%w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	windows := make([]videoframe.Window, 0, len(parts))
	paths := map[uint64]string{}
	for _, part := range parts {
		path, ok := paths[part.AssetID]
		if !ok {
			path = filepath.Join(dir, strconv.Itoa(len(paths))+".mp4")
			if err := downloadComposeVideo(ctx, s.assets, userID, part.AssetID, path); err != nil {
				return nil, err
			}
			paths[part.AssetID] = path
		}
		windows = append(windows, videoframe.Window{Path: path, StartMs: part.StartMs, EndMs: part.EndMs})
	}
	dest := filepath.Join(dir, "final.mp4")
	if err := s.composer.ComposeRanges(ctx, windows, dest, mute); err != nil {
		if errors.Is(err, videoframe.ErrWindow) {
			return nil, errcode.ErrInvalidParams.WithMsg("所选视频不包含完整的裁切区间")
		}
		return nil, fmt.Errorf("仿拍镜头裁切转码：%w", err)
	}
	return s.assets.PrepareDramaVideo(ctx, userID, dest)
}
