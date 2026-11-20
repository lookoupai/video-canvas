package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

// remakePlans 是逐镜制作工作台用到的计划、生成和合成操作。
type remakePlans interface {
	ListRemakePlans(ctx context.Context, userID, scriptID uint64) ([]model.MarketingRemakePlanSummary, error)
	CreateRemakePlan(ctx context.Context, userID, scriptID uint64, req *model.CreateRemakePlanReq) (*model.MarketingRemakePlanView, error)
	GetRemakePlan(ctx context.Context, userID, planID uint64) (*model.MarketingRemakePlanView, error)
	UpdateRemakeShot(ctx context.Context, userID, planID uint64, shotID int, req *model.UpdateRemakeShotReq) (*model.MarketingRemakePlanView, error)
	OrderRemakeShots(ctx context.Context, userID, planID uint64, req *model.OrderRemakeShotsReq) (*model.MarketingRemakePlanView, error)
	GenerateRemakeShot(ctx context.Context, userID, planID uint64, shotID int, key string, req *model.GenerateRemakeShotReq) (*model.GenerationTaskView, error)
	ComposeRemakePlan(ctx context.Context, userID, planID uint64, key string, req *model.ComposeRemakePlanReq) (*model.RemakeCompositionView, error)
	RemakeCompositions(ctx context.Context, userID, planID uint64) ([]model.RemakeCompositionView, error)
}

// MarketingRemakeHandler 提供营销逐镜重拍的计划、单镜任务和合成接口。
type MarketingRemakeHandler struct{ svc remakePlans }

// NewMarketingRemakeHandler 创建逐镜制作 HTTP 接口。
func NewMarketingRemakeHandler(svc remakePlans) *MarketingRemakeHandler {
	return &MarketingRemakeHandler{svc: svc}
}

// ListPlans 返回本人脚本历次修订的制作计划。
func (h *MarketingRemakeHandler) ListPlans(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	plans, err := h.svc.ListRemakePlans(c.Request.Context(), currentUserID(c), id)
	reply(c, plans, err)
}

// CreatePlan 按脚本修订取得或创建制作计划。
func (h *MarketingRemakeHandler) CreatePlan(c *gin.Context) {
	id, ok := CanvasPathID(c)
	var req model.CreateRemakePlanReq
	if !ok || !BindJSON(c, &req) {
		return
	}
	plan, err := h.svc.CreateRemakePlan(c.Request.Context(), currentUserID(c), id, &req)
	created(c, plan, err)
}

// GetPlan 返回有序分镜、生成版本与选版。
func (h *MarketingRemakeHandler) GetPlan(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	plan, err := h.svc.GetRemakePlan(c.Request.Context(), currentUserID(c), id)
	reply(c, plan, err)
}

// UpdateShot 校验计划修订并更新指定镜头的编辑稿或选版。
func (h *MarketingRemakeHandler) UpdateShot(c *gin.Context) {
	plan, shot, ok := remakeShotPath(c)
	var req model.UpdateRemakeShotReq
	if !ok || !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateRemakeShot(c.Request.Context(), currentUserID(c), plan, shot, &req)
	reply(c, v, err)
}

// OrderShots 按稳定的镜头 ID 列表重排制作计划。
func (h *MarketingRemakeHandler) OrderShots(c *gin.Context) {
	id, ok := CanvasPathID(c)
	var req model.OrderRemakeShotsReq
	if !ok || !BindJSON(c, &req) {
		return
	}
	plan, err := h.svc.OrderRemakeShots(c.Request.Context(), currentUserID(c), id, &req)
	reply(c, plan, err)
}

// GenerateShot 将单镜修订和生成意图幂等键交给任务服务，返回任务快照。
func (h *MarketingRemakeHandler) GenerateShot(c *gin.Context) {
	plan, shot, ok := remakeShotPath(c)
	var req model.GenerateRemakeShotReq
	if !ok || !BindJSON(c, &req) {
		return
	}
	task, err := h.svc.GenerateRemakeShot(c.Request.Context(), currentUserID(c), plan, shot, c.GetHeader("Idempotency-Key"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, task)
}

// Compose 将选定范围与提交时的计划修订交给合成服务。
func (h *MarketingRemakeHandler) Compose(c *gin.Context) {
	id, ok := CanvasPathID(c)
	var req model.ComposeRemakePlanReq
	if !ok || !BindJSON(c, &req) {
		return
	}
	job, err := h.svc.ComposeRemakePlan(c.Request.Context(), currentUserID(c), id, c.GetHeader("Idempotency-Key"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, job)
}

// Compositions 返回计划的历史合成及成片素材。
func (h *MarketingRemakeHandler) Compositions(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	jobs, err := h.svc.RemakeCompositions(c.Request.Context(), currentUserID(c), id)
	reply(c, jobs, err)
}

// remakeShotPath 计划 ID 使用十六进制编码，计划内的镜头 ID 是从 1 起的稳定整数。
func remakeShotPath(c *gin.Context) (uint64, int, bool) {
	id, ok := CanvasPathID(c)
	if !ok {
		return 0, 0, false
	}
	var path struct {
		Shot int `uri:"shot" binding:"min=1" label:"镜头 ID"`
	}
	if !bindURI(c, &path) {
		return 0, 0, false
	}
	return id, path.Shot, true
}
