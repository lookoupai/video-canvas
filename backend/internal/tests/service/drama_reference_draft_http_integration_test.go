package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
)

func TestReferenceDraftRealLoginRouteRoundTrip(t *testing.T) {
	_, svc, pid, rid, revision := draftDatabase(t)
	const secret = "reference-draft-http-test-secret"
	token, _, err := utils.GenerateToken(7, "draft-owner", 1, secret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	routes := router.New("test", secret, router.Handlers{Health: &handler.HealthHandler{},
		DramaReferenceDraft: handler.NewDramaReferenceDraftHandler(svc),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Role: "user", Status: "active", TokenVersion: 1}, nil
		}})
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	base := server.URL + "/api/v1/dramas/" + idcodec.Encode(pid) + "/references/" + idcodec.Encode(rid) + "/drafts"
	status, _ := draftHTTP(t, server.Client(), "GET", base, "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("未登录必须被拒绝：%d", status)
	}
	input := fmt.Sprintf(`{"request_key":"http-create","reference_revision":%d,"instruction":"刷新仍可编辑"}`, revision)
	status, created := draftHTTP(t, server.Client(), "POST", base, token, input)
	if status != http.StatusCreated || created.Revision != 1 || created.Source != "external" {
		t.Fatalf("创建：%d %+v", status, created)
	}
	status, duplicate := draftHTTP(t, server.Client(), "POST", base, token, input)
	if status != http.StatusCreated || duplicate.ID != created.ID {
		t.Fatalf("重传：%d %+v", status, duplicate)
	}
	url := base + "/" + created.ID
	status, edited := draftHTTP(t, server.Client(), "PATCH", url, token, `{"revision":1,"instruction":"网页人工修改"}`)
	if status != http.StatusOK || edited.Revision != 2 || edited.Source != "manual" {
		t.Fatalf("修改：%d %+v", status, edited)
	}
	status, fresh := draftHTTP(t, server.Client(), "GET", url, token, "")
	if status != http.StatusOK || fresh.Instruction != "网页人工修改" || fresh.Revision != 2 {
		t.Fatalf("刷新恢复：%d %+v", status, fresh)
	}
	status, _ = draftHTTP(t, server.Client(), "PATCH", url, token, `{"revision":1,"instruction":"迟到编辑"}`)
	if status != http.StatusConflict {
		t.Fatalf("旧修订必须冲突：%d", status)
	}
}

func draftHTTP(t *testing.T, client *http.Client, method, url, token, body string) (int, model.DramaReferenceDraftView) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var result struct {
		Data model.DramaReferenceDraftView `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, result.Data
}
