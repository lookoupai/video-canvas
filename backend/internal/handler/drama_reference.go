package handler

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/reference"
)

type referenceService interface {
	Adopt(context.Context, uint64, uint64, uint64, *model.AdoptDramaReferenceReq) (*model.DramaReferenceAdoptionView, error)
	Adoptions(context.Context, uint64, uint64) ([]model.DramaReferenceAdoptionView, error)
	CreateProject(context.Context, uint64, *model.CreateDramaReferenceProjectReq) (*model.DramaReferenceProjectView, error)
	Collection(context.Context, uint64, uint64) (*model.DramaReferenceCollection, error)
	AddEpisode(context.Context, uint64, uint64, *model.AddDramaReferenceReq) (*model.DramaReferenceView, error)
	GetEpisode(context.Context, uint64, uint64, uint64, int64) (*model.DramaReferenceView, error)
	UpdateEpisode(context.Context, uint64, uint64, uint64, *model.UpdateDramaReferenceReq) (*model.DramaReferenceView, error)
	OrderEpisodes(context.Context, uint64, uint64, *model.OrderDramaReferencesReq) (*model.DramaReferenceCollection, error)
	CreateRole(context.Context, uint64, uint64, *model.SaveDramaReferenceRoleReq) (*model.DramaReferenceRoleView, error)
	UpdateRole(context.Context, uint64, uint64, uint64, *model.SaveDramaReferenceRoleReq) (*model.DramaReferenceRoleView, error)
	Import(context.Context, uint64, uint64, uint64, int64, []byte) (*model.DramaReferenceView, error)
	Analyze(context.Context, uint64, uint64, uint64, int64) (*model.DramaReferenceView, error)
}

// DramaReferenceHandler 提供参考集导入、分析及校订接口。
type DramaReferenceHandler struct{ svc referenceService }

// NewDramaReferenceHandler 创建参考集 HTTP 层。
func NewDramaReferenceHandler(svc referenceService) *DramaReferenceHandler {
	return &DramaReferenceHandler{svc: svc}
}

// Adopt 把指定参考修订采用为真实分集，重传返回原采用关联。
func (h *DramaReferenceHandler) Adopt(c *gin.Context) {
	p, id, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var req model.AdoptDramaReferenceReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Adopt(c.Request.Context(), currentUserID(c), p, id, &req)
	reply(c, v, err)
}

// Adoptions 返回本人项目采用记录及其当前分集位置。
func (h *DramaReferenceHandler) Adoptions(c *gin.Context) {
	p, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Adoptions(c.Request.Context(), currentUserID(c), p)
	reply(c, v, err)
}

// CreateProject 创建包含专属资产画布的参考短剧项目。
func (h *DramaReferenceHandler) CreateProject(c *gin.Context) {
	var req model.CreateDramaReferenceProjectReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateProject(c.Request.Context(), currentUserID(c), &req)
	created(c, v, err)
}

// List 返回项目各集与跨集角色映射。
func (h *DramaReferenceHandler) List(c *gin.Context) {
	p, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Collection(c.Request.Context(), currentUserID(c), p)
	reply(c, v, err)
}

// Add 追加本人视频为参考集。
func (h *DramaReferenceHandler) Add(c *gin.Context) {
	p, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.AddDramaReferenceReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.AddEpisode(c.Request.Context(), currentUserID(c), p, &req)
	created(c, v, err)
}

// Get 返回当前集或指定历史修订。
func (h *DramaReferenceHandler) Get(c *gin.Context) {
	p, id, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var query struct {
		Revision int64 `form:"revision" binding:"min=0"`
	}
	if !BindQuery(c, &query) {
		return
	}
	v, err := h.svc.GetEpisode(c.Request.Context(), currentUserID(c), p, id, query.Revision)
	reply(c, v, err)
}

// Update 带修订保存本集对白、场次、镜头和人物对应。
func (h *DramaReferenceHandler) Update(c *gin.Context) {
	p, id, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var req model.UpdateDramaReferenceReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateEpisode(c.Request.Context(), currentUserID(c), p, id, &req)
	reply(c, v, err)
}

// Order 按集合修订保存完整参考集顺序。
func (h *DramaReferenceHandler) Order(c *gin.Context) {
	p, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.OrderDramaReferencesReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.OrderEpisodes(c.Request.Context(), currentUserID(c), p, &req)
	reply(c, v, err)
}

// CreateRole 新建跨集角色身份。
func (h *DramaReferenceHandler) CreateRole(c *gin.Context) {
	p, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.SaveDramaReferenceRoleReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateRole(c.Request.Context(), currentUserID(c), p, &req)
	created(c, v, err)
}

// UpdateRole 带修订更新用户确认的新角色资产。
func (h *DramaReferenceHandler) UpdateRole(c *gin.Context) {
	p, id, ok := referencePath(c, "role")
	if !ok {
		return
	}
	var req model.SaveDramaReferenceRoleReq
	if !BindJSON(c, &req) {
		return
	}
	if req.Revision < 1 {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	v, err := h.svc.UpdateRole(c.Request.Context(), currentUserID(c), p, id, &req)
	reply(c, v, err)
}

// Import 接收限额内的拆片包，后台解析结束后返回本集结果。
func (h *DramaReferenceHandler) Import(c *gin.Context) {
	p, id, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, reference.MaxPackageBytes+(1<<20))
	revision, err := strconv.ParseInt(c.PostForm("revision"), 10, 64)
	if err != nil || revision < 1 {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	file, err := c.FormFile("package")
	if err != nil || file.Size > reference.MaxPackageBytes {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("请选择不超过 8 MB 的拆片包"))
		return
	}
	src, err := file.Open()
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	defer func() { _ = src.Close() }()
	raw, err := io.ReadAll(io.LimitReader(src, reference.MaxPackageBytes+1))
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	v, err := h.svc.Import(c.Request.Context(), currentUserID(c), p, id, revision, raw)
	referenceAccepted(c, v, err)
}

// Analyze 为当前参考集发起本地分析。
func (h *DramaReferenceHandler) Analyze(c *gin.Context) {
	p, id, ok := referencePath(c, "reference")
	if !ok {
		return
	}
	var req model.AnalyzeDramaReferenceReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Analyze(c.Request.Context(), currentUserID(c), p, id, req.Revision)
	referenceAccepted(c, v, err)
}

// referenceAccepted 返回可供页面轮询的后台处理状态。
func referenceAccepted(c *gin.Context, v any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, v)
}

// referencePath 同时校验项目及参考集或角色的十六进制 ID。
func referencePath(c *gin.Context, key string) (uint64, uint64, bool) {
	p, ok := CanvasPathID(c)
	if !ok {
		return 0, 0, false
	}
	id, ok := idcodec.Decode(c.Param(key))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("id 格式错误"))
		return 0, 0, false
	}
	return p, id, true
}
