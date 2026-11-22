package handler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

type referenceDraftHTTPService struct{ user uint64 }

func (s *referenceDraftHTTPService) Create(_ context.Context, u, p, _ uint64, _ *model.CreateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	s.user = u
	return &model.DramaReferenceDraftView{ID: idcodec.Encode(12), Revision: 1}, referenceHTTPError(p)
}
func (s *referenceDraftHTTPService) Get(_ context.Context, u, p, _, _ uint64) (*model.DramaReferenceDraftView, error) {
	s.user = u
	return &model.DramaReferenceDraftView{ID: idcodec.Encode(12), Revision: 1}, referenceHTTPError(p)
}
func (s *referenceDraftHTTPService) List(_ context.Context, u, p, _ uint64, _ *model.ListDramaReferenceDraftsReq) (*model.DramaReferenceDraftList, error) {
	s.user = u
	return &model.DramaReferenceDraftList{Items: []model.DramaReferenceDraftView{}}, referenceHTTPError(p)
}
func (s *referenceDraftHTTPService) Update(_ context.Context, u, p, _, _ uint64, req *model.UpdateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	s.user = u
	if req.Revision == 99 {
		return nil, errcode.ErrCanvasConflict
	}
	return &model.DramaReferenceDraftView{ID: idcodec.Encode(12), Revision: 2}, referenceHTTPError(p)
}

func TestReferenceDraftHTTPContract(t *testing.T) {
	svc := &referenceDraftHTTPService{}
	h := handler.NewDramaReferenceDraftHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)); c.Next() })
	base := "/dramas/:id/references/:reference/drafts"
	r.POST(base, h.Create)
	r.GET(base, h.List)
	r.GET(base+"/:draft", h.Get)
	r.PATCH(base+"/:draft", h.Update)
	path := "/dramas/" + idcodec.Encode(7) + "/references/" + idcodec.Encode(11) + "/drafts"
	one := path + "/" + idcodec.Encode(12)
	for _, tc := range []struct {
		name, method, path, body string
		status, code             int
	}{
		{"创建", "POST", path, `{"reference_revision":3,"request_key":"new"}`, 201, 0},
		{"创建缺参数", "POST", path, `{}`, 400, 10001},
		{"禁止伪造来源", "POST", path, `{"reference_revision":3,"request_key":"new","source":"agent"}`, 400, 10001},
		{"多个JSON对象", "POST", path, `{"reference_revision":3,"request_key":"new"}{}`, 400, 10001},
		{"创建越权", "POST", strings.Replace(path, idcodec.Encode(7), idcodec.Encode(99), 1), `{"reference_revision":3,"request_key":"new"}`, 404, 67001},
		{"读取", "GET", one, "", 200, 0},
		{"读取非法ID", "GET", path + "/invalid", "", 400, 10001},
		{"读取越权", "GET", strings.Replace(one, idcodec.Encode(7), idcodec.Encode(99), 1), "", 404, 67001},
		{"列表", "GET", path + "?limit=20", "", 200, 0},
		{"列表超过上限", "GET", path + "?limit=101", "", 400, 10001},
		{"列表非法游标", "GET", path + "?cursor=invalid", "", 400, 10001},
		{"列表越权", "GET", strings.Replace(path, idcodec.Encode(7), idcodec.Encode(99), 1), "", 404, 67001},
		{"更新", "PATCH", one, `{"revision":1,"instruction":"新要求"}`, 200, 0},
		{"更新缺修订", "PATCH", one, `{"instruction":"新要求"}`, 400, 10001},
		{"更新禁止改基准", "PATCH", one, `{"revision":1,"reference_revision":99}`, 400, 10001},
		{"旧修订", "PATCH", one, `{"revision":99,"instruction":"旧要求"}`, 409, 30002},
		{"体积超过限制", "POST", path, `{"reference_revision":3,"request_key":"new","instruction":"` + strings.Repeat("a", 2<<20) + `"}`, 400, 10001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := characterCall(t, r, tc.method, tc.path, tc.body)
			if status != tc.status || responseCode(t, body) != tc.code {
				t.Fatalf("返回 %d %+v", status, body)
			}
		})
	}
	if svc.user != 9 {
		t.Fatalf("登录用户没有透传：%d", svc.user)
	}
}
