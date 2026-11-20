package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
)

// remakeHTTPService 只负责记录登录用户、解码后的业务 ID 和请求体，业务规则另在 service 测试。
type remakeHTTPService struct {
	user, id uint64
	shot     int
	key      string
	req      any
}

func (s *remakeHTTPService) record(user, id uint64, shot int, key string, req any) error {
	s.user, s.id, s.shot, s.key, s.req = user, id, shot, key, req
	if id != 17 {
		return errcode.ErrMarketingNotFound
	}
	return nil
}
func (s *remakeHTTPService) CreateRemakePlan(_ context.Context, u, id uint64, req *model.CreateRemakePlanReq) (*model.MarketingRemakePlanView, error) {
	return &model.MarketingRemakePlanView{Revision: 2}, s.record(u, id, 0, "", req)
}
func (s *remakeHTTPService) ListRemakePlans(_ context.Context, u, id uint64) ([]model.MarketingRemakePlanSummary, error) {
	return []model.MarketingRemakePlanSummary{}, s.record(u, id, 0, "", nil)
}
func (s *remakeHTTPService) GetRemakePlan(_ context.Context, u, id uint64) (*model.MarketingRemakePlanView, error) {
	return &model.MarketingRemakePlanView{Revision: 2}, s.record(u, id, 0, "", nil)
}
func (s *remakeHTTPService) UpdateRemakeShot(_ context.Context, u, id uint64, shot int, req *model.UpdateRemakeShotReq) (*model.MarketingRemakePlanView, error) {
	return &model.MarketingRemakePlanView{Revision: 2}, s.record(u, id, shot, "", req)
}
func (s *remakeHTTPService) OrderRemakeShots(_ context.Context, u, id uint64, req *model.OrderRemakeShotsReq) (*model.MarketingRemakePlanView, error) {
	return &model.MarketingRemakePlanView{Revision: 2}, s.record(u, id, 0, "", req)
}
func (s *remakeHTTPService) GenerateRemakeShot(_ context.Context, u, id uint64, shot int, key string, req *model.GenerateRemakeShotReq) (*model.GenerationTaskView, error) {
	return &model.GenerationTaskView{Status: "queued"}, s.record(u, id, shot, key, req)
}
func (s *remakeHTTPService) ComposeRemakePlan(_ context.Context, u, id uint64, key string, req *model.ComposeRemakePlanReq) (*model.RemakeCompositionView, error) {
	return &model.RemakeCompositionView{Status: "queued"}, s.record(u, id, 0, key, req)
}
func (s *remakeHTTPService) RemakeCompositions(_ context.Context, u, id uint64) ([]model.RemakeCompositionView, error) {
	return []model.RemakeCompositionView{}, s.record(u, id, 0, "", nil)
}
func remakeHTTPRouter(t *testing.T, s *remakeHTTPService) (*gin.Engine, string) {
	t.Helper()
	const secret = "remake-local-http-test"
	token, _, err := utils.GenerateToken(7, "user", 1, secret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	return router.New(gin.TestMode, secret, router.Handlers{
		MarketingRemake: handler.NewMarketingRemakeHandler(s),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Status: "active", TokenVersion: 1}, nil
		},
	}), token
}

func TestMarketingRemakeHTTP(t *testing.T) {
	const scripts = "/api/v1/marketing-scripts/:id/remake-plans"
	const plan = "/api/v1/remake-plans/:id"
	for _, tc := range []struct {
		method, path, body, key string
		shot, status            int
		data                    string
	}{
		{"POST", scripts, `{"revision":11}`, "", 0, 201, `"revision":2`},
		{"GET", scripts, "", "", 0, 200, "[]"},
		{"GET", plan, "", "", 0, 200, `"revision":2`},
		{"PATCH", plan + "/shots/3", `{"revision":5,"scene":"推开乳霜","duration_ms":2400}`, "", 3, 200, `"revision":2`},
		{"PUT", plan + "/order", `{"revision":5,"shot_ids":[3,1,2]}`, "", 0, 200, `"revision":2`},
		{"POST", plan + "/shots/3/generate", `{"revision":8}`, "generate-3", 3, 202, `"status":"queued"`},
		{"POST", plan + "/compose", `{"revision":5,"shot_ids":[3]}`, "compose-3", 0, 202, `"status":"queued"`},
		{"GET", plan + "/compositions", "", "", 0, 200, "[]"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			svc := &remakeHTTPService{}
			routes, token := remakeHTTPRouter(t, svc)
			call := func(id, auth, body string) (int, int, string) {
				t.Helper()
				req := httptest.NewRequest(tc.method, strings.Replace(tc.path, ":id", id, 1), strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Idempotency-Key", tc.key)
				if auth != "" {
					req.Header.Set("Authorization", "Bearer "+auth)
				}
				w := httptest.NewRecorder()
				routes.ServeHTTP(w, req)
				var resp struct {
					Code int
					Data json.RawMessage
				}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatal(err)
				}
				return w.Code, resp.Code, string(resp.Data)
			}
			id := idcodec.Encode(17)
			status, code, data := call(id, token, tc.body)
			if status != tc.status || code != 0 || !strings.Contains(data, tc.data) || svc.user != 7 || svc.id != 17 || svc.shot != tc.shot || svc.key != tc.key {
				t.Fatalf("响应 %d/%d %s，传给服务的参数 %+v", status, code, data, svc)
			}
			if tc.body != "" {
				var want, got map[string]any
				raw, err := json.Marshal(svc.req)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
					t.Fatal(err)
				}
				for k, v := range want {
					if !reflect.DeepEqual(got[k], v) {
						t.Fatalf("字段 %s: %v, want %v", k, got[k], v)
					}
				}
				if status, code, _ := call(id, token, "{}"); status != 400 || code != 10001 {
					t.Fatalf("无修订 %d/%d", status, code)
				}
			}
			if status, _, _ := call(id, "", tc.body); status != http.StatusUnauthorized {
				t.Fatalf("无登录态 %d", status)
			}
			if status, code, _ := call("broken", token, tc.body); status != 400 || code != 10001 {
				t.Fatalf("非法 ID %d/%d", status, code)
			}
			if status, code, _ := call(idcodec.Encode(99), token, tc.body); status != 404 || code != errcode.ErrMarketingNotFound.Code {
				t.Fatalf("资源归属 %d/%d", status, code)
			}
		})
	}
}
