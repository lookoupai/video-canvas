package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/repository"
)

const referenceProcessorVersion = "reference-frames-v1"

// DramaReferencePreparationRepo 声明准备任务查询与事务边界。
type DramaReferencePreparationRepo interface {
	repository.DramaReferencePreparationTx
	// WithTx 原子提交准备状态、帧计划和任务队列。
	WithTx(context.Context, func(repository.DramaReferencePreparationTx) error) error
}

// DramaReferenceFrames 提供受并发和超时限制的本地媒体能力。
type DramaReferenceFrames interface {
	// Available 判断运行环境是否具有 FFmpeg 和 ffprobe。
	Available() bool
	// Probe 检查时长、视频轨和音轨存在性。
	Probe(context.Context, string) (videoframe.ProbeInfo, error)
	// Frame 提取指定秒数的单张图片。
	Frame(context.Context, string, float64, videoframe.FrameOpts) ([]byte, error)
}

// DramaReferenceFrameStore 以准备和帧 ID 为稳定业务键保存本人帧图。
type DramaReferenceFrameStore interface {
	// SaveReferenceFrame 重传复用已保存素材，返回可持久引用的素材视图。
	SaveReferenceFrame(context.Context, uint64, uint64, uint64, string, []byte) (*model.AssetView, error)
}

// DramaReferencePreparationDeps 组装准备存储、本人素材、分析和逐帧能力。
type DramaReferencePreparationDeps struct {
	Repo     DramaReferencePreparationRepo
	Assets   MarketingAssets
	Frames   DramaReferenceFrames
	Store    DramaReferenceFrameStore
	Analyzer DramaReferenceAnalyzer
}

// DramaReferencePreparationService 保存可恢复媒体证据，不修改正式参考文档。
type DramaReferencePreparationService struct {
	repo     DramaReferencePreparationRepo
	assets   MarketingAssets
	frames   DramaReferenceFrames
	store    DramaReferenceFrameStore
	analyzer DramaReferenceAnalyzer
}

// NewDramaReferencePreparationService 手动注入持久队列、媒体与素材依赖。
func NewDramaReferencePreparationService(d DramaReferencePreparationDeps) *DramaReferencePreparationService {
	return &DramaReferencePreparationService{repo: d.Repo, assets: d.Assets, frames: d.Frames, store: d.Store, analyzer: d.Analyzer}
}

// Create 冻结指定参考修订，按规范化 recipe 复用准备，并在同一事务提交队列。
func (s *DramaReferencePreparationService) Create(ctx context.Context, u, p, ref uint64, req *model.CreateDramaReferencePreparationReq) (*model.DramaReferencePreparationView, error) {
	// 1. 固定请求指纹并检查归属，已成功重传不依赖当前参考稿是否继续修改。
	input, hash, err := normalizePreparationCreate(req)
	if err != nil {
		return nil, err
	}
	reference, err := s.repo.Reference(ctx, u, p, ref, false)
	if err != nil {
		return nil, dramaError(err)
	}
	prior, err := priorPreparationRequest(ctx, s.repo, u, p, ref, input.RequestKey, "create", hash)
	if err != nil || prior != nil {
		return preparationResult(prior, err)
	}
	if reference.Revision != input.ReferenceRevision {
		return nil, errcode.ErrCanvasConflict
	}
	doc, err := referenceDocumentOf(reference)
	if err != nil {
		return nil, err
	}
	shots, err := preparationSelection(doc, input.ShotIDs, false)
	if err != nil {
		return nil, err
	}
	// 2. 在事务外检查本人视频和处理能力；缺工具明确失败，避免排入必定失败的任务。
	if err := s.validatePreparationRuntime(ctx, u, reference, len(doc.Shots) == 0); err != nil {
		return nil, err
	}
	row, err := newPreparation(reference, doc, shots)
	if err != nil {
		return nil, err
	}
	// 3. 项目锁内重查请求键、参考修订和 recipe；新增请求键即使复用旧准备也要落库。
	err = s.repo.WithTx(ctx, func(tx repository.DramaReferencePreparationTx) error {
		var saveErr error
		row, saveErr = createPreparationTx(ctx, tx, row, input.RequestKey, hash)
		return saveErr
	})
	return preparationResult(row, dramaError(err))
}

// validatePreparationRuntime 只接收本人有效视频，原稿无镜头时还需要本地切镜分析器。
func (s *DramaReferencePreparationService) validatePreparationRuntime(ctx context.Context, u uint64, ref *model.DramaReferenceEpisode, analyze bool) error {
	if s.assets == nil {
		return errcode.ErrInvalidParams.WithMsg("素材服务不可用")
	}
	asset, err := s.assets.View(ctx, u, ref.AssetID)
	if err != nil {
		return draftAssetError(err)
	}
	if asset == nil || asset.Kind != "video" || asset.DurationMs != ref.DurationMs || asset.DurationMs <= 0 {
		return errcode.ErrInvalidParams.WithMsg("参考视频已失效或时长改变")
	}
	if s.frames == nil || !s.frames.Available() || s.store == nil {
		return errcode.ErrInvalidParams.WithMsg("当前环境未配置视频提帧能力")
	}
	if analyze && (s.analyzer == nil || !s.analyzer.Available()) {
		return errcode.ErrInvalidParams.WithMsg("请先导入镜头文档或配置本地分析器")
	}
	return nil
}

// newPreparation 把参考快照和用户选镜固定，recipe 不包含可变请求键。
func newPreparation(ref *model.DramaReferenceEpisode, doc model.DramaReferenceDocument, selected []string) (*model.DramaReferencePreparation, error) {
	normalizeDraftDocument(&doc)
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	ids, err := json.Marshal(draftSlice(selected))
	if err != nil {
		return nil, err
	}
	recipe, err := preparationRecipe(ref, doc, selected)
	if err != nil {
		return nil, err
	}
	return &model.DramaReferencePreparation{UserID: ref.UserID, DramaID: ref.DramaID, ReferenceID: ref.ID,
		BaseRevision: ref.Revision, SourceAssetID: ref.AssetID, DurationMs: ref.DurationMs,
		RecipeHash: recipe, ProcessorVersion: referenceProcessorVersion, Document: raw, ShotIDs: ids,
		Status: "queued", Attempt: 1}, nil
}

// createPreparationTx 将指令去重、准备保存、帧计划和 River 入队原子提交。
func createPreparationTx(ctx context.Context, tx repository.DramaReferencePreparationTx, next *model.DramaReferencePreparation, key, hash string) (*model.DramaReferencePreparation, error) {
	ref, err := tx.Reference(ctx, next.UserID, next.DramaID, next.ReferenceID, true)
	if err != nil {
		return nil, err
	}
	prior, err := priorPreparationRequest(ctx, tx, next.UserID, next.DramaID, next.ReferenceID, key, "create", hash)
	if err != nil || prior != nil {
		return prior, err
	}
	if ref.Revision != next.BaseRevision || ref.AssetID != next.SourceAssetID {
		return nil, errcode.ErrCanvasConflict
	}
	row, err := tx.ByRecipe(ctx, next.UserID, next.DramaID, next.ReferenceID, next.RecipeHash)
	if errors.Is(err, repository.ErrNotFound) {
		row = next
		if err := insertPreparation(ctx, tx, row); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return row, rememberPreparationRequest(ctx, tx, row, key, "create", hash)
}

// insertPreparation 创建准备后按已有冻结镜头插入默认采样点，最后入队。
func insertPreparation(ctx context.Context, tx repository.DramaReferencePreparationTx, row *model.DramaReferencePreparation) error {
	if err := tx.Save(ctx, row); err != nil {
		return err
	}
	if err := planPreparationFrames(ctx, tx, row); err != nil {
		return err
	}
	return tx.Enqueue(ctx, row.UserID, row.DramaID, row.ReferenceID, row.ID, row.Attempt)
}

// priorPreparationRequest 检查同键是否仍代表相同操作，恢复绑定到原准备的结果。
func priorPreparationRequest(ctx context.Context, tx repository.DramaReferencePreparationTx, u, p, ref uint64, key, kind, hash string) (*model.DramaReferencePreparation, error) {
	request, err := tx.Request(ctx, u, p, ref, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if request.Kind != kind || request.Fingerprint != hash {
		return nil, errcode.ErrInvalidParams.WithMsg("请求键已用于其他媒体准备指令")
	}
	return tx.Preparation(ctx, u, p, ref, request.PreparationID)
}

// rememberPreparationRequest 保存每个成功指令键，跨客户端复用 recipe 也不能丢失键约束。
func rememberPreparationRequest(ctx context.Context, tx repository.DramaReferencePreparationTx, row *model.DramaReferencePreparation, key, kind, hash string) error {
	return tx.SaveRequest(ctx, &model.DramaReferencePreparationRequest{UserID: row.UserID, DramaID: row.DramaID,
		ReferenceID: row.ReferenceID, PreparationID: row.ID, RequestKey: key, Kind: kind, Fingerprint: hash})
}
