package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

type referencePlanService interface {
	Quote(context.Context, uint64, uint64, uint64, uint64, *model.QuoteDramaReferenceDraftReq) (*model.DramaReferencePlan, error)
	Apply(context.Context, uint64, uint64, uint64, uint64, *model.ApplyDramaReferenceDraftReq) (*model.DramaReferenceApplyResponse, error)
	Generate(context.Context, uint64, uint64, uint64, uint64, *model.DramaReferenceGenerateReq) (*model.DramaReferenceGenerateResponse, error)
}

// Generate submits a batch from an existing application receipt and quote.
func (h *DramaReferencePlanHandler) Generate(c *gin.Context) {
	projectID, referenceID, draftID, ok := draftPath(c)
	if !ok {
		return
	}
	var req model.DramaReferenceGenerateReq
	if !bindDraftJSON(c, &req) {
		return
	}
	receipt, err := h.svc.Generate(c.Request.Context(), currentUserID(c), projectID, referenceID, draftID, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, receipt)
}

// DramaReferencePlanHandler 暴露参考草稿的报价与采用。
type DramaReferencePlanHandler struct{ svc referencePlanService }

// NewDramaReferencePlanHandler 组装参考计划 HTTP 层。
func NewDramaReferencePlanHandler(svc referencePlanService) *DramaReferencePlanHandler {
	return &DramaReferencePlanHandler{svc: svc}
}

// Apply 使用批准的报价把所选镜头采用到跟拍分集。
func (h *DramaReferencePlanHandler) Apply(c *gin.Context) {
	projectID, referenceID, draftID, ok := draftPath(c)
	if !ok {
		return
	}
	var req model.ApplyDramaReferenceDraftReq
	if !bindDraftJSON(c, &req) {
		return
	}
	receipt, err := h.svc.Apply(c.Request.Context(), currentUserID(c), projectID, referenceID, draftID, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, receipt)
}

// Quote 校验草稿、素材和模型路线后返回单镜计划及站内积分预估。
func (h *DramaReferencePlanHandler) Quote(c *gin.Context) {
	projectID, referenceID, draftID, ok := draftPath(c)
	if !ok {
		return
	}
	var req model.QuoteDramaReferenceDraftReq
	if !bindDraftJSON(c, &req) {
		return
	}
	plan, err := h.svc.Quote(c.Request.Context(), currentUserID(c), projectID, referenceID, draftID, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, plan)
}
