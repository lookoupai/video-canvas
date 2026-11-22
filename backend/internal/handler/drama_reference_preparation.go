package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
)

type referencePreparationService interface {
	Create(context.Context, uint64, uint64, uint64, *model.CreateDramaReferencePreparationReq) (*model.DramaReferencePreparationView, error)
	List(context.Context, uint64, uint64, uint64, *model.ListDramaReferenceDraftsReq) (*model.DramaReferencePreparationList, error)
	Get(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferencePreparationView, error)
	Shots(context.Context, uint64, uint64, uint64, uint64, *model.ListDramaReferenceDraftsReq) (*model.DramaReferencePreparedShotList, error)
	Frames(context.Context, uint64, uint64, uint64, uint64, *model.ListDramaReferenceFramesReq) (*model.DramaReferenceFrameList, error)
	Cancel(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferencePreparationView, error)
	Resume(context.Context, uint64, uint64, uint64, uint64, *model.ResumeDramaReferencePreparationReq) (*model.DramaReferencePreparationView, error)
	AddFrames(context.Context, uint64, uint64, uint64, uint64, *model.AddDramaReferenceFramesReq) (*model.DramaReferencePreparationView, error)
}

// DramaReferencePreparationHandler 提供本人参考集持久媒体准备接口。
type DramaReferencePreparationHandler struct{ svc referencePreparationService }

// NewDramaReferencePreparationHandler 创建媒体准备 HTTP 层。
func NewDramaReferencePreparationHandler(svc referencePreparationService) *DramaReferencePreparationHandler {
	return &DramaReferencePreparationHandler{svc: svc}
}

// Create 固定参考修订并提交持久准备任务。
func (h *DramaReferencePreparationHandler) Create(c *gin.Context) {
	p, ref, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var req model.CreateDramaReferencePreparationReq
	if !bindDraftJSON(c, &req) {
		return
	}
	view, err := h.svc.Create(c.Request.Context(), currentUserID(c), p, ref, &req)
	created(c, view, err)
}

// Get 查询准备进度摘要。
func (h *DramaReferencePreparationHandler) Get(c *gin.Context) {
	p, ref, id, ok := preparationPath(c)
	if !ok {
		return
	}
	view, err := h.svc.Get(c.Request.Context(), currentUserID(c), p, ref, id)
	reply(c, view, err)
}

// List 分页查询当前参考集的准备。
func (h *DramaReferencePreparationHandler) List(c *gin.Context) {
	p, ref, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var req model.ListDramaReferenceDraftsReq
	if !BindQuery(c, &req) || !preparationCursor(c, req.Cursor) {
		return
	}
	view, err := h.svc.List(c.Request.Context(), currentUserID(c), p, ref, &req)
	reply(c, view, err)
}

// Shots 分页查询准备中的冻结选镜。
func (h *DramaReferencePreparationHandler) Shots(c *gin.Context) {
	p, ref, id, ok := preparationPath(c)
	if !ok {
		return
	}
	var req model.ListDramaReferenceDraftsReq
	if !BindQuery(c, &req) || !preparationCursor(c, req.Cursor) {
		return
	}
	view, err := h.svc.Shots(c.Request.Context(), currentUserID(c), p, ref, id, &req)
	reply(c, view, err)
}

// Frames 分页读取系统帧图引用。
func (h *DramaReferencePreparationHandler) Frames(c *gin.Context) {
	p, ref, id, ok := preparationPath(c)
	if !ok {
		return
	}
	var req model.ListDramaReferenceFramesReq
	if !BindQuery(c, &req) || !preparationCursor(c, req.Cursor) {
		return
	}
	view, err := h.svc.Frames(c.Request.Context(), currentUserID(c), p, ref, id, &req)
	reply(c, view, err)
}

// Cancel 停止本次准备并保留完成帧。
func (h *DramaReferencePreparationHandler) Cancel(c *gin.Context) {
	p, ref, id, ok := preparationPath(c)
	if !ok {
		return
	}
	view, err := h.svc.Cancel(c.Request.Context(), currentUserID(c), p, ref, id)
	reply(c, view, err)
}

// Resume 用稳定指令键恢复未完成媒体准备。
func (h *DramaReferencePreparationHandler) Resume(c *gin.Context) {
	p, ref, id, ok := preparationPath(c)
	if !ok {
		return
	}
	var req model.ResumeDramaReferencePreparationReq
	if !bindDraftJSON(c, &req) {
		return
	}
	view, err := h.svc.Resume(c.Request.Context(), currentUserID(c), p, ref, id, &req)
	reply(c, view, err)
}

// AddFrames 在冻结镜窗内请求更多系统帧。
func (h *DramaReferencePreparationHandler) AddFrames(c *gin.Context) {
	p, ref, id, ok := preparationPath(c)
	if !ok {
		return
	}
	var req model.AddDramaReferenceFramesReq
	if !bindDraftJSON(c, &req) {
		return
	}
	view, err := h.svc.AddFrames(c.Request.Context(), currentUserID(c), p, ref, id, &req)
	reply(c, view, err)
}

// preparationPath 解析项目、参考集及准备 ID。
func preparationPath(c *gin.Context) (uint64, uint64, uint64, bool) {
	p, ref, ok := referencePath(c, "reference")
	if !ok {
		return 0, 0, 0, false
	}
	id, ok := idcodec.Decode(c.Param("preparation"))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("准备 id 格式错误"))
	}
	return p, ref, id, ok
}

// preparationCursor 在调用服务前拒绝非法分页游标。
func preparationCursor(c *gin.Context, cursor string) bool {
	if _, ok := idcodec.Decode(cursor); cursor != "" && !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("游标格式错误"))
		return false
	}
	return true
}
