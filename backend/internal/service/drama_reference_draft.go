package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// DramaReferenceDraftRepo 声明草稿持久化与短事务边界。
type DramaReferenceDraftRepo interface {
	repository.DramaReferenceDraftTx
	// WithTx 将同一项目内的修订检查和草稿写入原子提交。
	WithTx(context.Context, func(repository.DramaReferenceDraftTx) error) error
}

// DramaReferenceDraftDeps 提供用户图片核验和草稿存储。
type DramaReferenceDraftDeps struct {
	Repo         DramaReferenceDraftRepo
	Assets       MarketingAssets
	Preparations DramaReferenceDraftPreparations
}

// DramaReferenceDraftService 保存待核对的分析结果，不改写正式参考稿。
type DramaReferenceDraftService struct {
	repo         DramaReferenceDraftRepo
	assets       MarketingAssets
	preparations DramaReferenceDraftPreparations
}

// NewDramaReferenceDraftService 组装草稿服务，可信来源不从公开请求体读取。
func NewDramaReferenceDraftService(d DramaReferenceDraftDeps) *DramaReferenceDraftService {
	return &DramaReferenceDraftService{repo: d.Repo, assets: d.Assets, preparations: d.Preparations}
}

// Create 从指定参考修订建立独立草稿，同键同输入重传返回最初创建的草稿。
func (s *DramaReferenceDraftService) Create(ctx context.Context, u, p, ref uint64, req *model.CreateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	return s.create(ctx, u, p, ref, req, draftActor{source: "external"})
}

// CreateForAgent 从可信运行上下文创建草稿，并记录 Agent 会话和运行来源。
func (s *DramaReferenceDraftService) CreateForAgent(ctx context.Context, u, p, ref, sessionID, runID uint64, req *model.CreateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	if sessionID == 0 || runID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少 Agent 会话或运行标识")
	}
	return s.create(ctx, u, p, ref, req, draftActor{source: "agent", sessionID: sessionID, runID: runID})
}

// create 按调用入口提供的可信来源记录新草稿。
func (s *DramaReferenceDraftService) create(ctx context.Context, u, p, ref uint64, req *model.CreateDramaReferenceDraftReq, actor draftActor) (*model.DramaReferenceDraftView, error) {
	// 1. 先固定规范化输入指纹；已提交重传不依赖之后的参考修订和素材状态。
	input, hash, err := normalizeDraftRequest(req)
	if err != nil {
		return nil, err
	}
	reference, err := s.repo.Reference(ctx, u, p, ref, false)
	if err != nil {
		return nil, dramaError(err)
	}
	prior, err := findDraftByKey(ctx, s.repo, u, p, ref, input.RequestKey, hash)
	if err != nil || prior != nil {
		return draftResult(prior, err)
	}
	reference, preparationID, err := s.draftReference(ctx, u, p, ref, reference, input)
	if err != nil {
		return nil, err
	}
	if reference.Revision != input.ReferenceRevision {
		return nil, errcode.ErrCanvasConflict
	}
	// 2. 在事务外检查完整文档、时间证据和用户素材，避免锁内发生对象存储访问。
	content, err := initialDraftContent(reference, input)
	if err != nil {
		return nil, err
	}
	if err := s.validateDraftContent(ctx, u, reference.DurationMs, content); err != nil {
		return nil, err
	}
	if err := s.validatePreparedEvidence(ctx, u, p, ref, preparationID, content); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	row := &model.DramaReferenceDraft{UserID: u, DramaID: p, ReferenceID: ref, RequestKey: input.RequestKey,
		PreparationID: preparationID, Fingerprint: hash, BaseRevision: input.ReferenceRevision, Revision: 1, Status: "editing",
		Source: actor.source, SessionID: actor.sessionID, RunID: actor.runID, Content: raw}
	// 3. 项目锁内再次检查创建键和基准，保证并发重传只有一份持久化草稿。
	err = s.repo.WithTx(ctx, func(tx repository.DramaReferenceDraftTx) error {
		var saveErr error
		row, saveErr = insertDraft(ctx, tx, row)
		return saveErr
	})
	return draftResult(row, dramaError(err))
}

type draftActor struct {
	source    string
	sessionID uint64
	runID     uint64
}

// Get 读取本人存活项目下的草稿，越权与不存在统一返回不存在。
func (s *DramaReferenceDraftService) Get(ctx context.Context, u, p, ref, id uint64) (*model.DramaReferenceDraftView, error) {
	// 1. 项目归属单独核验，以免已删除项目的草稿成为可访问的孤立资源。
	if _, err := s.repo.Reference(ctx, u, p, ref, false); err != nil {
		return nil, dramaError(err)
	}
	// 2. 草稿读取继续带上用户、项目和参考集条件。
	row, err := s.repo.Draft(ctx, u, p, ref, id, false)
	return draftResult(row, dramaError(err))
}

// List 返回本人参考集的草稿页，使用排他游标避免翻页重复。
func (s *DramaReferenceDraftService) List(ctx context.Context, u, p, ref uint64, req *model.ListDramaReferenceDraftsReq) (*model.DramaReferenceDraftList, error) {
	// 1. 服务层也校验分页参数，可信工具调用不能绕过数量上限。
	cursor, limit, err := draftPageArgs(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.Reference(ctx, u, p, ref, false); err != nil {
		return nil, dramaError(err)
	}
	// 2. 多读取一条判断是否还有下一页，最后一页明确返回空游标。
	rows, err := s.repo.Drafts(ctx, u, p, ref, cursor, limit+1)
	if err != nil {
		return nil, dramaError(err)
	}
	page := &model.DramaReferenceDraftList{Items: []model.DramaReferenceDraftView{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = idcodec.Encode(rows[len(rows)-1].ID)
	}
	for i := range rows {
		view, err := draftResult(&rows[i], nil)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, *view)
	}
	return page, nil
}

// insertDraft 在项目锁内检查重传与基准修订，成功后只写草稿表。
func insertDraft(ctx context.Context, tx repository.DramaReferenceDraftTx, row *model.DramaReferenceDraft) (*model.DramaReferenceDraft, error) {
	ref, err := tx.Reference(ctx, row.UserID, row.DramaID, row.ReferenceID, true)
	if err != nil {
		return nil, err
	}
	prior, err := findDraftByKey(ctx, tx, row.UserID, row.DramaID, row.ReferenceID, row.RequestKey, row.Fingerprint)
	if err != nil || prior != nil {
		return prior, err
	}
	if row.PreparationID == 0 && ref.Revision != row.BaseRevision {
		return nil, errcode.ErrCanvasConflict
	}
	return row, tx.Save(ctx, row)
}

// findDraftByKey 将同键不同意图视为参数错误，防止重试键静默指向别的内容。
func findDraftByKey(ctx context.Context, repo repository.DramaReferenceDraftTx, u, p, ref uint64, key, hash string) (*model.DramaReferenceDraft, error) {
	row, err := repo.ByKey(ctx, u, p, ref, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.Fingerprint != hash {
		return nil, errcode.ErrInvalidParams.WithMsg("创建键已用于另一份草稿，请更换创建键")
	}
	return row, nil
}

// draftPageArgs 解析公开 ID 游标并限制单页大小。
func draftPageArgs(req *model.ListDramaReferenceDraftsReq) (uint64, int, error) {
	if req == nil || req.Limit < 0 || req.Limit > 100 {
		return 0, 0, errcode.ErrInvalidParams
	}
	limit := req.Limit
	if limit == 0 {
		limit = 20
	}
	if req.Cursor == "" {
		return 0, limit, nil
	}
	cursor, ok := idcodec.Decode(req.Cursor)
	if !ok {
		return 0, 0, errcode.ErrInvalidParams
	}
	return cursor, limit, nil
}
