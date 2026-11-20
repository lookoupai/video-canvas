package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// VirtualHumanRepo 是真人库的数据访问接口（真实实现是 repository.VirtualHumanRepository）。
type VirtualHumanRepo interface {
	// ListHumans 返回用户全部真人，新建的在前。
	ListHumans(ctx context.Context, userID uint64) ([]model.VirtualHuman, error)
	// CountHumans 统计用户的真人数。
	CountHumans(ctx context.Context, userID uint64) (int64, error)
	// GetHuman 按 id + user_id 查询真人，不存在返回 repository.ErrNotFound。
	GetHuman(ctx context.Context, userID, id uint64) (*model.VirtualHuman, error)
	// RenameHuman 修改真人名称，不存在返回 repository.ErrNotFound。
	RenameHuman(ctx context.Context, userID, id uint64, name string) error
	// CreateHumanWithImage 在一个事务里新建真人和第一张图片；素材已在真人库里返回 repository.ErrDuplicate。
	CreateHumanWithImage(ctx context.Context, h *model.VirtualHuman, img *model.VirtualHumanImage) error
	// DeleteHuman 在一个事务里删除真人和它的全部图片行，不存在返回 repository.ErrNotFound。
	DeleteHuman(ctx context.Context, userID, id uint64) error
	// ListImages 返回用户全部真人图片，按真人、sort、id 升序。
	ListImages(ctx context.Context, userID uint64) ([]model.VirtualHumanImage, error)
	// ListImagesByHuman 返回一个真人的图片，按 sort、id 升序。
	ListImagesByHuman(ctx context.Context, userID, humanID uint64) ([]model.VirtualHumanImage, error)
	// GetImage 按 id + user_id 查询图片，不存在返回 repository.ErrNotFound。
	GetImage(ctx context.Context, userID, id uint64) (*model.VirtualHumanImage, error)
	// GetImageByAsset 按 user_id + asset_id 查询图片，不在真人库里返回 repository.ErrNotFound。
	GetImageByAsset(ctx context.Context, userID, assetID uint64) (*model.VirtualHumanImage, error)
	// CreateImageInHuman 确认真人仍存在后新建图片：真人不存在返回 repository.ErrNotFound，素材重复返回 repository.ErrDuplicate。
	CreateImageInHuman(ctx context.Context, img *model.VirtualHumanImage) error
	// UpdateImageFiling 写回图片的备案结果（渠道、方舟素材 ID、状态、失败原因），不存在返回 repository.ErrNotFound。
	UpdateImageFiling(ctx context.Context, img *model.VirtualHumanImage) error
	// SetImageSort 修改图片顺序，不存在返回 repository.ErrNotFound。
	SetImageSort(ctx context.Context, userID, id uint64, sort int) error
	// DeleteImage 删除一张图片行，不存在返回 repository.ErrNotFound。
	DeleteImage(ctx context.Context, userID, id uint64) error
}

// VirtualHumanCanvases 查哪些画布的角色卡用到了某些素材（真实实现是 repository.CanvasProjectRepository）。
type VirtualHumanCanvases interface {
	// CanvasesUsingAssets 返回该用户未删除的画布里，角色卡主图或补充图用到这些素材（十进制字符串）的画布内容。
	CanvasesUsingAssets(ctx context.Context, userID uint64, assetIDs []string) ([]model.CanvasPayloadRow, error)
}

// VirtualHumanAssetRows 按 ID 批量查素材（真实实现是 repository.AssetRepository）。
type VirtualHumanAssetRows interface {
	// ListByIDs 按 id 批量查询，不存在的缺席；不按用户过滤，调用方自己校验归属。
	ListByIDs(ctx context.Context, ids []uint64) ([]model.Asset, error)
}

// FilingTargetSource 给出当前启用了虚拟人像库的渠道和凭证（真实实现是 AIChannelService）。
type FilingTargetSource interface {
	// FilingTarget 返回备案目标；没有渠道启用、或启用了但素材组 / AK / SK 没配齐时返回 nil, nil。
	FilingTarget(ctx context.Context) (*provider.FilingTarget, error)
}

// FilingClient 调方舟虚拟人像库（真实实现是 provider/arkasset.Client）。
type FilingClient interface {
	// CreateAsset 把一张图片上传到素材组（异步审核），返回方舟素材 ID。
	CreateAsset(ctx context.Context, t provider.FilingTarget, imageURL, name string) (string, error)
	// GetAsset 查询素材的审核状态，返回 provider.FilingAsset*。
	GetAsset(ctx context.Context, t provider.FilingTarget, assetID string) (string, error)
	// DeleteAsset 删除素材，不可恢复。
	DeleteAsset(ctx context.Context, t provider.FilingTarget, assetID string) error
}

// VirtualHumanDeps 是真人库服务的依赖。
type VirtualHumanDeps struct {
	Repo      VirtualHumanRepo
	Canvases  VirtualHumanCanvases
	Assets    provider.AssetStore // 校验素材归属并拿到方舟能访问的地址
	AssetRows VirtualHumanAssetRows
	Views     AssetLibraryViewer
	Filing    FilingTargetSource
	Client    FilingClient
	Now       func() time.Time // 刷新节流用，测试里可替换；nil 用 time.Now
}

// VirtualHumanService 是真人库的业务：只在个人资产库里加入 / 移除真人，每张图片单独在方舟虚拟人像库备案；
// 角色卡等其他地方只是引用这些图片（素材 ID），生成时由 FiledAssetResolver 把可用的图片换成 asset://。
type VirtualHumanService struct {
	repo      VirtualHumanRepo
	canvases  VirtualHumanCanvases
	assets    provider.AssetStore
	assetRows VirtualHumanAssetRows
	views     AssetLibraryViewer
	filing    FilingTargetSource
	client    FilingClient
	now       func() time.Time

	mu          sync.Mutex
	lastRefresh map[uint64]time.Time // 每个用户上次向方舟刷新审核状态的时间
}

// NewVirtualHumanService 创建真人库服务。
func NewVirtualHumanService(d VirtualHumanDeps) *VirtualHumanService {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &VirtualHumanService{
		repo: d.Repo, canvases: d.Canvases, assets: d.Assets, assetRows: d.AssetRows, views: d.Views,
		filing: d.Filing, client: d.Client, now: now, lastRefresh: map[uint64]time.Time{},
	}
}

// refreshInterval 是同一用户两次向方舟刷新审核状态的最小间隔：页面每 3 秒轮询一次，
// 多个标签页同时开着也不会把 GetAsset（账号级 100 QPS）打满。
const refreshInterval = 3 * time.Second

// 给用户看的备案失败原因：上游原文可能带内部信息，只写进日志，不透传。
const (
	filingSubmitFailedMsg = "提交方舟失败，请稍后重新提交"
	filingRejectedMsg     = "方舟审核未通过，请换一张清晰的正面照"
)

// List 返回当前用户的全部真人（新建的在前）和虚拟人像库是否已配置。
// refresh 为真时先向方舟刷新审核中的图片（同一用户 3 秒内只刷新一次）；方舟没有回调，页面靠轮询它拿到结果。
func (s *VirtualHumanService) List(ctx context.Context, userID uint64, refresh bool) (*model.VirtualHumanListView, error) {
	// 1. 备案目标：决定能不能刷新、页面上能不能加入新图片
	target, err := s.filing.FilingTarget(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 刷新审核中的图片；单张查询失败只记日志、保持审核中，不影响列表
	if refresh && target != nil && s.takeRefreshSlot(userID) {
		if err := s.refreshPending(ctx, userID, target); err != nil {
			return nil, err
		}
	}
	// 3. 组装视图
	humans, err := s.repo.ListHumans(ctx, userID)
	if err != nil {
		return nil, err
	}
	images, err := s.repo.ListImages(ctx, userID)
	if err != nil {
		return nil, err
	}
	items, err := s.buildViews(ctx, userID, humans, images)
	if err != nil {
		return nil, err
	}
	return &model.VirtualHumanListView{Items: items, FilingReady: target != nil}, nil
}

// takeRefreshSlot 判断这个用户现在能不能刷新：距上次超过 refreshInterval 才放行，并记下这次的时间。
func (s *VirtualHumanService) takeRefreshSlot(userID uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if last, ok := s.lastRefresh[userID]; ok && now.Sub(last) < refreshInterval {
		return false
	}
	s.lastRefresh[userID] = now
	return true
}

// refreshPending 逐张查询审核中的图片并写回结果。备案渠道不是当前渠道的图片拿不到凭证，跳过。
func (s *VirtualHumanService) refreshPending(ctx context.Context, userID uint64, target *provider.FilingTarget) error {
	images, err := s.repo.ListImages(ctx, userID)
	if err != nil {
		return err
	}
	for i := range images {
		img := &images[i]
		if img.Status != model.FilingPending || img.ArkAssetID == "" || img.ChannelKey != target.ChannelKey {
			continue
		}
		status, err := s.client.GetAsset(ctx, *target, img.ArkAssetID)
		if err != nil {
			logger.Warn("查询方舟素材状态失败", zap.Uint64("image_id", img.ID), zap.Error(err))
			continue
		}
		switch status {
		case provider.FilingAssetActive:
			img.Status, img.Error = model.FilingActive, ""
		case provider.FilingAssetFailed:
			img.Status, img.Error = model.FilingFailed, filingRejectedMsg
		default:
			continue
		}
		if err := s.repo.UpdateImageFiling(ctx, img); err != nil && !errors.Is(err, repository.ErrNotFound) {
			return err
		}
	}
	return nil
}

// Create 用一张图片新建真人并立即提交备案，返回新真人。名称去首尾空白后 1–30 字，每用户最多 200 个真人。
// 提交方舟失败不算创建失败：图片标记为失败并写明原因，用户可以重新提交。
func (s *VirtualHumanService) Create(ctx context.Context, userID uint64, name string, assetID uint64) (*model.VirtualHumanView, error) {
	// 1. 校验名称与数量上限；上限先查再建，并发下可能略微超出，可接受（防滥用，不是计费约束）
	name, err := normalizeHumanName(name)
	if err != nil {
		return nil, err
	}
	n, err := s.repo.CountHumans(ctx, userID)
	if err != nil {
		return nil, err
	}
	if n >= model.MaxVirtualHumans {
		return nil, errcode.ErrVirtualHumanLimit
	}
	// 2. 虚拟人像库要已配置，素材要是自己的、在对象存储里的图片
	target, file, err := s.prepareFiling(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	// 3. 建真人和图片（审核中）；素材已在真人库里由唯一索引兜底
	h := &model.VirtualHuman{UserID: userID, Name: name}
	img := &model.VirtualHumanImage{UserID: userID, AssetID: assetID, Status: model.FilingPending, ChannelKey: target.ChannelKey}
	if err := s.repo.CreateHumanWithImage(ctx, h, img); errors.Is(err, repository.ErrDuplicate) {
		return nil, errcode.ErrVirtualHumanImageDup
	} else if err != nil {
		return nil, err
	}
	// 4. 事务外提交方舟（网络调用不进事务）
	if err := s.submit(ctx, img, target, file.URL, name); err != nil {
		return nil, err
	}
	return s.humanView(ctx, userID, h.ID)
}

// AddImage 给真人加一张图片并立即提交备案，返回更新后的真人。每个真人最多 5 张图。
func (s *VirtualHumanService) AddImage(ctx context.Context, userID, humanID, assetID uint64) (*model.VirtualHumanView, error) {
	// 1. 真人要存在，图片没满；新图片排在最后
	h, err := s.loadHuman(ctx, userID, humanID)
	if err != nil {
		return nil, err
	}
	images, err := s.repo.ListImagesByHuman(ctx, userID, humanID)
	if err != nil {
		return nil, err
	}
	if len(images) >= model.MaxVirtualHumanImages {
		return nil, errcode.ErrVirtualHumanImageLimit
	}
	sortNo := 0
	for _, img := range images {
		sortNo = max(sortNo, img.Sort+1)
	}
	// 2. 校验备案配置与素材
	target, file, err := s.prepareFiling(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	// 3. 建图片：真人刚被删掉返回不存在，素材已在真人库里返回重复
	img := &model.VirtualHumanImage{
		UserID: userID, HumanID: humanID, AssetID: assetID, Sort: sortNo, Status: model.FilingPending, ChannelKey: target.ChannelKey,
	}
	switch err := s.repo.CreateImageInHuman(ctx, img); {
	case errors.Is(err, repository.ErrNotFound):
		return nil, errcode.ErrVirtualHumanNotFound
	case errors.Is(err, repository.ErrDuplicate):
		return nil, errcode.ErrVirtualHumanImageDup
	case err != nil:
		return nil, err
	}
	// 4. 提交方舟
	if err := s.submit(ctx, img, target, file.URL, h.Name); err != nil {
		return nil, err
	}
	return s.humanView(ctx, userID, humanID)
}

// RetryImage 重新提交一张备案失败的图片，返回更新后的真人。只有失败的图片可以重新提交（审核中、已可用的不行）。
func (s *VirtualHumanService) RetryImage(ctx context.Context, userID, imageID uint64) (*model.VirtualHumanView, error) {
	// 1. 图片要存在且是失败状态
	img, err := s.loadImage(ctx, userID, imageID)
	if err != nil {
		return nil, err
	}
	if img.Status != model.FilingFailed {
		return nil, errcode.ErrVirtualHumanRetryInvalid
	}
	h, err := s.loadHuman(ctx, userID, img.HumanID)
	if err != nil {
		return nil, err
	}
	// 2. 重新校验备案配置与素材（素材可能已被删除、渠道可能已换），按当前渠道提交
	target, file, err := s.prepareFiling(ctx, userID, img.AssetID)
	if err != nil {
		return nil, err
	}
	img.ChannelKey = target.ChannelKey
	if err := s.submit(ctx, img, target, file.URL, h.Name); err != nil {
		return nil, err
	}
	return s.humanView(ctx, userID, img.HumanID)
}

// Rename 修改真人名称并返回更新后的真人。只改本地名称，不影响方舟侧已备案的素材。
func (s *VirtualHumanService) Rename(ctx context.Context, userID, humanID uint64, name string) (*model.VirtualHumanView, error) {
	name, err := normalizeHumanName(name)
	if err != nil {
		return nil, err
	}
	if err := s.repo.RenameHuman(ctx, userID, humanID, name); errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrVirtualHumanNotFound
	} else if err != nil {
		return nil, err
	}
	return s.humanView(ctx, userID, humanID)
}

// SetCover 把一张图片设为封面（排到第一张），返回更新后的真人。
func (s *VirtualHumanService) SetCover(ctx context.Context, userID, humanID, imageID uint64) (*model.VirtualHumanView, error) {
	// 1. 真人要存在，图片要属于这个真人
	if _, err := s.loadHuman(ctx, userID, humanID); err != nil {
		return nil, err
	}
	images, err := s.repo.ListImagesByHuman(ctx, userID, humanID)
	if err != nil {
		return nil, err
	}
	found, minSort := false, 0
	for i, img := range images {
		found = found || img.ID == imageID
		if i == 0 || img.Sort < minSort {
			minSort = img.Sort
		}
	}
	if !found {
		return nil, errcode.ErrVirtualHumanImageNotFound
	}
	// 2. 排到最前：sort 取当前最小值减一，不用重排其余图片
	if err := s.repo.SetImageSort(ctx, userID, imageID, minSort-1); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	return s.humanView(ctx, userID, humanID)
}

// prepareFiling 校验能否提交备案：虚拟人像库已配置，素材是当前用户的图片且地址方舟能从公网拉到。
// 返回备案目标和打开的素材（只用它的地址，内容已关闭）。
func (s *VirtualHumanService) prepareFiling(ctx context.Context, userID, assetID uint64) (*provider.FilingTarget, *provider.AssetFile, error) {
	target, err := s.filing.FilingTarget(ctx)
	if err != nil {
		return nil, nil, err
	}
	if target == nil {
		return nil, nil, errcode.ErrFilingNotConfigured
	}
	file, err := s.assets.Open(ctx, userID, assetID)
	if errors.Is(err, provider.ErrAssetNotFound) {
		return nil, nil, errcode.ErrVirtualHumanInvalid.WithMsg("素材不存在")
	} else if err != nil {
		return nil, nil, err
	}
	_ = file.Body.Close() // 只要地址，不读内容
	if file.Asset.Kind != "image" {
		return nil, nil, errcode.ErrVirtualHumanInvalid.WithMsg("只能把图片加入真人库")
	}
	// 方舟要从公网拉图：对象存储的地址可以；本地存储配了公网 base_url 时也是绝对地址，同样可以。
	// 相对地址（base_url 没配）指向本服务自己，方舟拉不到，提前拦下，免得提交后才失败
	if !file.Remote && !isAbsoluteHTTPURL(file.URL) {
		return nil, nil, errcode.ErrVirtualHumanImageNotPublic
	}
	return target, file, nil
}

// isAbsoluteHTTPURL 判断地址是不是带主机名的 http(s) 绝对地址。
func isAbsoluteHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// submit 把图片提交到方舟并写回结果：成功记下方舟素材 ID、进入审核中；失败标记失败并写用户看得懂的原因，
// 上游原文只进日志。只有写库失败才返回错误。
func (s *VirtualHumanService) submit(ctx context.Context, img *model.VirtualHumanImage, target *provider.FilingTarget, imageURL, name string) error {
	arkID, err := s.client.CreateAsset(ctx, *target, imageURL, name)
	if err != nil {
		logger.Warn("提交方舟虚拟人像库失败", zap.Uint64("image_id", img.ID), zap.Error(err))
		img.ArkAssetID, img.Status, img.Error = "", model.FilingFailed, filingSubmitFailedMsg
	} else {
		img.ArkAssetID, img.Status, img.Error = arkID, model.FilingPending, ""
	}
	if err := s.repo.UpdateImageFiling(ctx, img); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	return nil
}

// loadHuman 取真人，不存在翻译成 ErrVirtualHumanNotFound。
func (s *VirtualHumanService) loadHuman(ctx context.Context, userID, id uint64) (*model.VirtualHuman, error) {
	h, err := s.repo.GetHuman(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrVirtualHumanNotFound
	}
	return h, err
}

// loadImage 取图片，不存在翻译成 ErrVirtualHumanImageNotFound。
func (s *VirtualHumanService) loadImage(ctx context.Context, userID, id uint64) (*model.VirtualHumanImage, error) {
	img, err := s.repo.GetImage(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrVirtualHumanImageNotFound
	}
	return img, err
}

// humanView 重新读出一个真人并组装视图。
func (s *VirtualHumanService) humanView(ctx context.Context, userID, humanID uint64) (*model.VirtualHumanView, error) {
	h, err := s.loadHuman(ctx, userID, humanID)
	if err != nil {
		return nil, err
	}
	images, err := s.repo.ListImagesByHuman(ctx, userID, humanID)
	if err != nil {
		return nil, err
	}
	items, err := s.buildViews(ctx, userID, []model.VirtualHuman{*h}, images)
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}

// buildViews 把真人和图片组装成视图：图片按传入顺序归到各自的真人下，素材一次批量查出（不属于该用户的按已删除处理）。
func (s *VirtualHumanService) buildViews(ctx context.Context, userID uint64, humans []model.VirtualHuman, images []model.VirtualHumanImage) ([]model.VirtualHumanView, error) {
	assets, err := s.assetViews(ctx, userID, images)
	if err != nil {
		return nil, err
	}
	byHuman := map[uint64][]model.VirtualHumanImageView{}
	for _, img := range images {
		byHuman[img.HumanID] = append(byHuman[img.HumanID], model.VirtualHumanImageView{
			ID: idcodec.ID(img.ID), Asset: assets[img.AssetID], Status: img.Status, Error: img.Error,
		})
	}
	out := make([]model.VirtualHumanView, 0, len(humans))
	for _, h := range humans {
		imgs := byHuman[h.ID]
		if imgs == nil {
			imgs = []model.VirtualHumanImageView{}
		}
		out = append(out, model.VirtualHumanView{
			ID: idcodec.ID(h.ID), Name: h.Name, Status: humanStatus(imgs), Images: imgs, CreatedAt: h.CreatedAt,
		})
	}
	return out, nil
}

// assetViews 批量查出图片对应的素材视图，键是素材 ID。
func (s *VirtualHumanService) assetViews(ctx context.Context, userID uint64, images []model.VirtualHumanImage) (map[uint64]*model.AssetView, error) {
	ids := make([]uint64, 0, len(images))
	for _, img := range images {
		ids = append(ids, img.AssetID)
	}
	out := map[uint64]*model.AssetView{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.assetRows.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].UserID != userID {
			continue
		}
		v, err := s.views.ViewOf(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		out[rows[i].ID] = v
	}
	return out, nil
}

// humanStatus 由图片状态得出真人的整体状态：有一张可用就算可用（引用时只带可用的图），否则有审核中的算审核中，否则失败。
func humanStatus(images []model.VirtualHumanImageView) string {
	pending := false
	for _, img := range images {
		if img.Status == model.FilingActive {
			return model.FilingActive
		}
		pending = pending || img.Status == model.FilingPending
	}
	if pending {
		return model.FilingPending
	}
	return model.FilingFailed
}

// normalizeHumanName 去首尾空白后校验名称长度（1–30 字）。
func normalizeHumanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errcode.ErrVirtualHumanInvalid.WithMsg("名称不能为空")
	}
	if utf8.RuneCountInString(name) > model.MaxVirtualHumanNameLen {
		return "", errcode.ErrVirtualHumanInvalid.WithMsg("名称不能超过 30 个字")
	}
	return name, nil
}
