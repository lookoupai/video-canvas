package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
)

type referenceDraftService interface {
	Create(context.Context, uint64, uint64, uint64, *model.CreateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error)
	Get(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferenceDraftView, error)
	List(context.Context, uint64, uint64, uint64, *model.ListDramaReferenceDraftsReq) (*model.DramaReferenceDraftList, error)
	Update(context.Context, uint64, uint64, uint64, uint64, *model.UpdateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error)
}

// DramaReferenceDraftHandler 提供本人参考集的独立草稿读写接口。
type DramaReferenceDraftHandler struct{ svc referenceDraftService }

// NewDramaReferenceDraftHandler 创建草稿 HTTP 层。
func NewDramaReferenceDraftHandler(svc referenceDraftService) *DramaReferenceDraftHandler {
	return &DramaReferenceDraftHandler{svc: svc}
}

// Create 从指定参考修订建立可恢复草稿。
func (h *DramaReferenceDraftHandler) Create(c *gin.Context) {
	p, ref, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var req model.CreateDramaReferenceDraftReq
	if !bindDraftJSON(c, &req) {
		return
	}
	view, err := h.svc.Create(c.Request.Context(), currentUserID(c), p, ref, &req)
	created(c, view, err)
}

// Get 读取本人项目中指定草稿的最新修订。
func (h *DramaReferenceDraftHandler) Get(c *gin.Context) {
	p, ref, id, ok := draftPath(c)
	if !ok {
		return
	}
	view, err := h.svc.Get(c.Request.Context(), currentUserID(c), p, ref, id)
	reply(c, view, err)
}

// List 分页读取当前参考集的草稿。
func (h *DramaReferenceDraftHandler) List(c *gin.Context) {
	p, ref, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var req model.ListDramaReferenceDraftsReq
	if !BindQuery(c, &req) {
		return
	}
	if _, ok := idcodec.Decode(req.Cursor); req.Cursor != "" && !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("游标格式错误"))
		return
	}
	view, err := h.svc.List(c.Request.Context(), currentUserID(c), p, ref, &req)
	reply(c, view, err)
}

// Update 使用乐观锁保存用户核对后的草稿字段。
func (h *DramaReferenceDraftHandler) Update(c *gin.Context) {
	p, ref, id, ok := draftPath(c)
	if !ok {
		return
	}
	var req model.UpdateDramaReferenceDraftReq
	if !bindDraftJSON(c, &req) {
		return
	}
	view, err := h.svc.Update(c.Request.Context(), currentUserID(c), p, ref, id, &req)
	reply(c, view, err)
}

// draftPath 解析项目、参考集和草稿的公开编码 ID。
func draftPath(c *gin.Context) (uint64, uint64, uint64, bool) {
	p, ref, ok := referencePath(c, "reference")
	if !ok {
		return 0, 0, 0, false
	}
	id, ok := idcodec.Decode(c.Param("draft"))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("草稿 id 格式错误"))
	}
	return p, ref, id, ok
}

// bindDraftJSON 限制体积、禁止未知字段和尾随 JSON，防止伪造可信来源字段。
func bindDraftJSON(c *gin.Context, req any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if !bindWith(c, decoder.Decode(req)) {
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("请求只能包含一个 JSON 对象"))
		return false
	}
	return bindWith(c, binding.Validator.ValidateStruct(req))
}
