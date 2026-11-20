package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// VirtualHumanHandler 真人库接口：真人和图片的 id 都是十六进制串，素材 id 沿用数字。
type VirtualHumanHandler struct {
	svc *service.VirtualHumanService
}

// NewVirtualHumanHandler 创建真人库接口。
func NewVirtualHumanHandler(svc *service.VirtualHumanService) *VirtualHumanHandler {
	return &VirtualHumanHandler{svc: svc}
}

// listVirtualHumansReq 列表参数：refresh=1 时先向方舟刷新审核中的图片（页面轮询用）。
type listVirtualHumansReq struct {
	Refresh bool `form:"refresh"`
}

// List 当前用户的全部真人：?refresh=1
func (h *VirtualHumanHandler) List(c *gin.Context) {
	var req listVirtualHumansReq
	if !BindQuery(c, &req) {
		return
	}
	v, err := h.svc.List(c.Request.Context(), currentUserID(c), req.Refresh)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Create 用一张图片新建真人并提交备案，201
func (h *VirtualHumanHandler) Create(c *gin.Context) {
	var req model.CreateVirtualHumanReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), currentUserID(c), req.Name, req.AssetID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// Rename 修改真人名称
func (h *VirtualHumanHandler) Rename(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.RenameVirtualHumanReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Rename(c.Request.Context(), currentUserID(c), id, req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Remove 移除真人（连同方舟侧素材），图片被角色卡使用时 409
func (h *VirtualHumanHandler) Remove(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.Remove(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// References 真人的图片被哪些角色卡使用
func (h *VirtualHumanHandler) References(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.References(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// AddImage 给真人加一张图片并提交备案，201
func (h *VirtualHumanHandler) AddImage(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.AddVirtualHumanImageReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.AddImage(c.Request.Context(), currentUserID(c), id, req.AssetID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// SetCover 把一张图片设为封面
func (h *VirtualHumanHandler) SetCover(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.SetVirtualHumanCoverReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SetCover(c.Request.Context(), currentUserID(c), id, uint64(req.ImageID))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// RetryImage 重新提交一张备案失败的图片
func (h *VirtualHumanHandler) RetryImage(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	v, err := h.svc.RetryImage(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// RemoveImage 从真人里移除一张图片（连同方舟侧素材），被角色卡使用时 409
func (h *VirtualHumanHandler) RemoveImage(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.RemoveImage(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
