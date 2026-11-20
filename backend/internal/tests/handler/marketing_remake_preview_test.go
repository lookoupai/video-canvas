package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type remakeHandlerRepo struct{ service.MarketingRepo }

func (remakeHandlerRepo) RemakePreviewRow(_ context.Context, userID, id uint64) (*repository.MarketingPreviewRow, error) {
	if userID != 7 || id != 9 {
		return nil, repository.ErrNotFound
	}
	return &repository.MarketingPreviewRow{Kind: "follow", Revision: 4, Shots: []byte("[]")}, nil
}

func TestRemakePreviewHandler(t *testing.T) {
	h := handler.NewMarketingHandler(service.NewMarketingService(service.MarketingDeps{Repo: remakeHandlerRepo{}}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(7)); c.Next() })
	r.GET("/marketing-scripts/:id/remake-preview", h.RemakePreview)
	for _, tc := range []struct {
		id           string
		status, code int
	}{{idcodec.Encode(9), http.StatusOK, 0}, {"bad", http.StatusBadRequest, 10001}, {idcodec.Encode(10), http.StatusNotFound, errcode.ErrMarketingNotFound.Code}} {
		status, body := canvasCall(t, r, http.MethodGet, "/marketing-scripts/"+tc.id+"/remake-preview", "")
		if status != tc.status || body["code"] != float64(tc.code) {
			t.Fatalf("%s: %d %+v", tc.id, status, body)
		}
		if tc.code == 0 && body["data"].(map[string]any)["revision"] != float64(4) {
			t.Fatalf("修订: %+v", body)
		}
	}
}
