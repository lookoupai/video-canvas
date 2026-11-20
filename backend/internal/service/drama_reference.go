package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/reference"
	"video-canvas/internal/repository"
)

// DramaReferenceRepo 声明参考集查询和短事务边界。
type DramaReferenceRepo interface {
	repository.DramaReferenceTx
	// WithTx 原子提交单集修订、角色或队列。
	WithTx(context.Context, func(repository.DramaReferenceTx) error) error
}

// DramaReferenceAnalyzer 从服务端临时视频分析原片，实际实现可以使用本地 rea-video。
type DramaReferenceAnalyzer interface {
	// Available 返回运行环境是否配置分析器。
	Available() bool
	// Analyze 输出原片时间轴，调用受上游上下文限制。
	Analyze(context.Context, string, int64) (model.DramaReferenceDocument, error)
}

// DramaReferenceDeps 包含用户素材、解析器和参考集持久化。
type DramaReferenceDeps struct {
	Repo     DramaReferenceRepo
	Assets   MarketingAssets
	Analyzer DramaReferenceAnalyzer
	Decode   func([]byte, int64) (model.DramaReferenceDocument, error)
}

// DramaReferenceService 管理参考集拆解和人工校订，不生成目标视频。
type DramaReferenceService struct {
	repo     DramaReferenceRepo
	assets   MarketingAssets
	analyzer DramaReferenceAnalyzer
	decode   func([]byte, int64) (model.DramaReferenceDocument, error)
}

// NewDramaReferenceService 组装参考集服务，默认使用受限拆片包解析器。
func NewDramaReferenceService(d DramaReferenceDeps) *DramaReferenceService {
	decode := d.Decode
	if decode == nil {
		decode = reference.DecodePackage
	}
	return &DramaReferenceService{repo: d.Repo, assets: d.Assets, analyzer: d.Analyzer, decode: decode}
}

// CreateProject 幂等创建真实短剧项目和资产画布，参考片导入不需要文本模型或积分。
func (s *DramaReferenceService) CreateProject(ctx context.Context, u uint64, req *model.CreateDramaReferenceProjectReq) (*model.DramaReferenceProjectView, error) {
	// 1. 归一化名称和画幅，创建键串行化同一用户的网络重传。
	title, key := strings.TrimSpace(req.Title), strings.TrimSpace(req.RequestKey)
	if title == "" || key == "" || len(key) > 128 {
		return nil, errcode.ErrInvalidParams
	}
	ratio := req.Ratio
	if ratio == "" {
		ratio = "9:16"
	}
	if ratio != "9:16" && ratio != "16:9" && ratio != "4:3" {
		return nil, errcode.ErrInvalidParams
	}
	var p *model.DramaProject
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		if err := tx.LockCreateKey(ctx, u, key); err != nil {
			return err
		}
		var err error
		p, err = tx.ProjectByKey(ctx, u, key)
		if err == nil {
			return nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		p = &model.DramaProject{UserID: u, Title: title, Ratio: ratio, StyleKey: "real_city_film", SourceKind: "reference", Status: "ready", Phase: "done", Revision: 1, ImageModels: []byte("{}"), ImageParams: []byte("{}"), PromptSettings: []byte("{}")}
		if err := tx.CreateProject(ctx, p); err != nil {
			return err
		}
		return tx.SaveState(ctx, &model.DramaReferenceState{DramaID: p.ID, UserID: u, RequestKey: key, Revision: 1})
	})
	if err != nil {
		return nil, dramaError(err)
	}
	// 2. 直接返回项目和画布标识供工作台进入。
	return &model.DramaReferenceProjectView{ID: idcodec.Encode(p.ID), CanvasID: idcodec.Encode(p.CanvasID)}, nil
}

// referenceState 懒创建已有短剧的参考集元数据，调用者必须先锁定项目。
func referenceState(ctx context.Context, tx repository.DramaReferenceTx, u, p uint64) (*model.DramaReferenceState, error) {
	row, err := tx.State(ctx, u, p)
	if errors.Is(err, repository.ErrNotFound) {
		row = &model.DramaReferenceState{DramaID: p, UserID: u, RequestKey: uuid.NewString(), Revision: 0}
		err = tx.SaveState(ctx, row)
	}
	return row, err
}

// AddEpisode 把本人的视频素材追加为独立参考集；同一创建键不会追加两次。
func (s *DramaReferenceService) AddEpisode(ctx context.Context, u, p uint64, req *model.AddDramaReferenceReq) (*model.DramaReferenceView, error) {
	// 1. 网络查询在事务外，时长取已探测的素材元数据。
	asset, err := s.assets.View(ctx, u, req.AssetID)
	if err != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("请选择自己的参考视频")
	}
	if asset.Kind != "video" || asset.DurationMs <= 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("参考视频必须有已测量的时长")
	}
	var row *model.DramaReferenceEpisode
	err = s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		row, err = addReferenceEpisode(ctx, tx, u, p, req, asset)
		return err
	})
	if err != nil {
		return nil, dramaError(err)
	}
	// 2. 返回初始状态，用户可选择导入拆片包或本地分析。
	return s.referenceView(ctx, row, 0)
}

// addReferenceEpisode 在项目锁内复用提交键或追加新的参考集。
func addReferenceEpisode(ctx context.Context, tx repository.DramaReferenceTx, u, p uint64, req *model.AddDramaReferenceReq, asset *model.AssetView) (*model.DramaReferenceEpisode, error) {
	if _, err := tx.Project(ctx, u, p, true); err != nil {
		return nil, err
	}
	state, err := referenceState(ctx, tx, u, p)
	if err != nil {
		return nil, err
	}
	row, err := tx.EpisodeByKey(ctx, u, p, req.RequestKey)
	if err == nil {
		if row.AssetID != req.AssetID {
			return nil, errcode.ErrInvalidParams.WithMsg("请求编号已用于其他参考视频")
		}
		return row, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	episodes, err := tx.Episodes(ctx, u, p)
	if err != nil {
		return nil, err
	}
	if len(episodes) >= 200 {
		return nil, errcode.ErrInvalidParams.WithMsg("参考集不能超过 200 集")
	}
	row = &model.DramaReferenceEpisode{UserID: u, DramaID: p, RequestKey: req.RequestKey, Seq: len(episodes) + 1, Title: strings.TrimSpace(req.Title), AssetID: req.AssetID, DurationMs: asset.DurationMs, Revision: 1, Status: "idle", Bindings: []byte("{}")}
	if err := tx.SaveEpisode(ctx, row); err != nil {
		return nil, err
	}
	state.Revision++
	return row, tx.SaveState(ctx, state)
}

// GetEpisode 按项目归属读取本集当前稿或指定的历史修订。
func (s *DramaReferenceService) GetEpisode(ctx context.Context, u, p, id uint64, revision int64) (*model.DramaReferenceView, error) {
	// 1. 已删除与他人的项目均返回不存在。
	if _, err := s.repo.Project(ctx, u, p, false); err != nil {
		return nil, dramaError(err)
	}
	row, err := s.repo.Episode(ctx, u, p, id)
	if err != nil {
		return nil, dramaError(err)
	}
	// 2. 历史修订不会覆盖当前处理状态或校订稿。
	return s.referenceView(ctx, row, revision)
}

// referenceView 将当前原稿和历史修订转为工作台视图，素材删除仍保留校订记录。
func (s *DramaReferenceService) referenceView(ctx context.Context, row *model.DramaReferenceEpisode, revision int64) (*model.DramaReferenceView, error) {
	versions, err := s.repo.Versions(ctx, row.UserID, row.ID)
	if err != nil {
		return nil, err
	}
	view := &model.DramaReferenceView{ID: idcodec.Encode(row.ID), Seq: row.Seq, Title: row.Title, DurationMs: row.DurationMs, Revision: row.Revision, Status: row.Status, Error: row.Error, Method: row.Method, Bindings: map[string]string{}, Versions: []model.DramaReferenceVersionView{}}
	raw, bindings := row.Document, row.Bindings
	found := revision == 0 || revision == row.Revision
	for _, v := range versions {
		view.Versions = append(view.Versions, model.DramaReferenceVersionView{Revision: v.Revision, Method: v.Method, CreatedAt: v.CreatedAt})
		if revision == v.Revision {
			raw, bindings = v.Document, v.Bindings
			view.Revision, view.Method = v.Revision, v.Method
			found = true
		}
	}
	if !found {
		return nil, errcode.ErrDramaNotFound
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &view.Document); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(bindings, &view.Bindings); err != nil {
		return nil, err
	}
	asset, err := s.assets.View(ctx, row.UserID, row.AssetID)
	if err != nil {
		var business *errcode.Error
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, provider.ErrAssetNotFound) || (errors.As(err, &business) && business.Code == errcode.ErrAssetNotFound.Code) {
			return view, nil
		}
		return nil, err
	}
	view.Asset = asset
	return view, nil
}
