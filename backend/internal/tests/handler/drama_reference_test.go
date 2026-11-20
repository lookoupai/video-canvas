package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

type referenceHTTPService struct{ user, id, reference uint64 }

func (s *referenceHTTPService) CreateProject(_ context.Context, u uint64, _ *model.CreateDramaReferenceProjectReq) (*model.DramaReferenceProjectView, error) {
	s.user = u
	return &model.DramaReferenceProjectView{ID: "created"}, nil
}
func (s *referenceHTTPService) Collection(_ context.Context, u, p uint64) (*model.DramaReferenceCollection, error) {
	s.user, s.id = u, p
	if p != 7 {
		return nil, errcode.ErrDramaNotFound
	}
	return &model.DramaReferenceCollection{Revision: 4, Episodes: []model.DramaReferenceView{}}, nil
}
func (s *referenceHTTPService) AddEpisode(_ context.Context, u, p uint64, _ *model.AddDramaReferenceReq) (*model.DramaReferenceView, error) {
	s.user, s.id = u, p
	return &model.DramaReferenceView{Revision: 4}, referenceHTTPError(p)
}
func (s *referenceHTTPService) GetEpisode(_ context.Context, u, p, id uint64, _ int64) (*model.DramaReferenceView, error) {
	s.user, s.id, s.reference = u, p, id
	return &model.DramaReferenceView{Revision: 4}, referenceHTTPError(p)
}
func (s *referenceHTTPService) UpdateEpisode(_ context.Context, u, p, id uint64, _ *model.UpdateDramaReferenceReq) (*model.DramaReferenceView, error) {
	s.user, s.id, s.reference = u, p, id
	return &model.DramaReferenceView{Revision: 4}, referenceHTTPError(p)
}
func (s *referenceHTTPService) OrderEpisodes(_ context.Context, u, p uint64, _ *model.OrderDramaReferencesReq) (*model.DramaReferenceCollection, error) {
	s.user, s.id = u, p
	return &model.DramaReferenceCollection{Revision: 4}, referenceHTTPError(p)
}
func (s *referenceHTTPService) CreateRole(_ context.Context, u, p uint64, _ *model.SaveDramaReferenceRoleReq) (*model.DramaReferenceRoleView, error) {
	s.user, s.id = u, p
	return &model.DramaReferenceRoleView{Revision: 4}, referenceHTTPError(p)
}
func (s *referenceHTTPService) UpdateRole(_ context.Context, u, p, id uint64, _ *model.SaveDramaReferenceRoleReq) (*model.DramaReferenceRoleView, error) {
	s.user, s.id, s.reference = u, p, id
	return &model.DramaReferenceRoleView{Revision: 4}, referenceHTTPError(p)
}
func (s *referenceHTTPService) Import(_ context.Context, u, p, id uint64, _ int64, _ []byte) (*model.DramaReferenceView, error) {
	s.user, s.id, s.reference = u, p, id
	return &model.DramaReferenceView{Revision: 4}, referenceHTTPError(p)
}
func (s *referenceHTTPService) Analyze(_ context.Context, u, p, id uint64, _ int64) (*model.DramaReferenceView, error) {
	s.user, s.id, s.reference = u, p, id
	return &model.DramaReferenceView{Revision: 4}, referenceHTTPError(p)
}

func (s *referenceHTTPService) Adopt(_ context.Context, u, p, id uint64, _ *model.AdoptDramaReferenceReq) (*model.DramaReferenceAdoptionView, error) {
	s.user, s.id, s.reference = u, p, id
	return &model.DramaReferenceAdoptionView{ID: "adopted", EpisodeNo: 1}, referenceHTTPError(p)
}
func (s *referenceHTTPService) Adoptions(_ context.Context, u, p uint64) ([]model.DramaReferenceAdoptionView, error) {
	s.user, s.id = u, p
	return []model.DramaReferenceAdoptionView{}, referenceHTTPError(p)
}

func referenceHTTPError(p uint64) error {
	if p != 7 {
		return errcode.ErrDramaNotFound
	}
	return nil
}

func TestDramaReferenceHTTP(t *testing.T) {
	svc := &referenceHTTPService{}
	h := handler.NewDramaReferenceHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)); c.Next() })
	r.POST("/drama-reference-projects", h.CreateProject)
	r.POST("/dramas/:id/references/:reference/adopt", h.Adopt)
	r.GET("/dramas/:id/reference-adoptions", h.Adoptions)
	r.GET("/dramas/:id/references", h.List)
	r.POST("/dramas/:id/references", h.Add)
	r.GET("/dramas/:id/references/:reference", h.Get)
	r.PUT("/dramas/:id/references/order", h.Order)
	r.POST("/dramas/:id/reference-roles", h.CreateRole)
	r.PATCH("/dramas/:id/reference-roles/:role", h.UpdateRole)
	r.PATCH("/dramas/:id/references/:reference", h.Update)
	r.POST("/dramas/:id/references/:reference/analyze", h.Analyze)
	pid, rid := idcodec.Encode(7), idcodec.Encode(11)
	for _, tc := range []struct {
		method, path, body string
		status, code       int
	}{
		{"POST", "/drama-reference-projects", `{"title":"参考剧","request_key":"create"}`, 201, 0},
		{"POST", "/drama-reference-projects", `{}`, 400, 10001},
		{"POST", "/dramas/" + pid + "/references/" + rid + "/adopt", `{"revision":3,"request_key":"adopt","mode":"follow"}`, 200, 0},
		{"POST", "/dramas/" + pid + "/references/" + rid + "/adopt", `{}`, 400, 10001},
		{"POST", "/dramas/" + pid + "/references/" + rid + "/adopt", `{"revision":3,"request_key":"adopt","mode":"bad"}`, 400, 10001},
		{"GET", "/dramas/" + pid + "/reference-adoptions", "", 200, 0},
		{"GET", "/dramas/" + pid + "/references/" + rid + "?revision=3", "", 200, 0},
		{"GET", "/dramas/" + pid + "/references/" + rid + "?revision=-1", "", 400, 10001},
		{"PUT", "/dramas/" + pid + "/references/order", `{"revision":1,"ids":["` + rid + `"]}`, 200, 0},
		{"PUT", "/dramas/" + pid + "/references/order", `{}`, 400, 10001},
		{"POST", "/dramas/" + pid + "/reference-roles", `{"name":"新角色"}`, 201, 0},
		{"POST", "/dramas/" + pid + "/reference-roles", `{}`, 400, 10001},
		{"PATCH", "/dramas/" + pid + "/reference-roles/" + rid, `{"revision":3,"name":"新角色"}`, 200, 0},
		{"PATCH", "/dramas/" + pid + "/reference-roles/" + rid, `{"name":"新角色"}`, 400, 10001},
		{"GET", "/dramas/" + pid + "/references", "", 200, 0},
		{"GET", "/dramas/broken/references", "", 400, 10001},
		{"GET", "/dramas/" + idcodec.Encode(99) + "/references", "", 404, 67001},
		{"POST", "/dramas/" + pid + "/references", `{"asset_id":12,"request_key":"episode"}`, 201, 0},
		{"POST", "/dramas/" + pid + "/references", `{}`, 400, 10001},
		{"PATCH", "/dramas/" + pid + "/references/" + rid, `{"revision":3,"title":"新集名"}`, 200, 0},
		{"PATCH", "/dramas/" + pid + "/references/" + rid, `{}`, 400, 10001},
		{"POST", "/dramas/" + pid + "/references/" + rid + "/analyze", `{"revision":3}`, 202, 0},
		{"POST", "/dramas/" + pid + "/references/broken/analyze", `{"revision":3}`, 400, 10001},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			status, body := characterCall(t, r, tc.method, tc.path, tc.body)
			if status != tc.status || responseCode(t, body) != tc.code {
				t.Fatalf("%d %+v", status, body)
			}
			if tc.code == 0 && strings.Contains(tc.path, pid) {
				status, body = characterCall(t, r, tc.method, strings.Replace(tc.path, pid, idcodec.Encode(99), 1), tc.body)
				if status != 404 || responseCode(t, body) != 67001 {
					t.Fatalf("业务错误 %d %+v", status, body)
				}
			}

		})
	}
	if svc.user != 9 {
		t.Fatalf("用户隔离 %d", svc.user)
	}
}

func TestDramaReferencePackageHTTP(t *testing.T) {
	svc := &referenceHTTPService{}
	h := handler.NewDramaReferenceHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)); c.Next() })
	r.POST("/dramas/:id/references/:reference/import", h.Import)
	for _, tc := range []struct {
		project      uint64
		revision     string
		file         bool
		status, code int
	}{
		{7, "3", true, 202, 0}, {7, "", true, 400, 10001}, {7, "3", false, 400, 10001}, {99, "3", true, 404, 67001},
	} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("revision", tc.revision); err != nil {
			t.Fatal(err)
		}
		if tc.file {
			part, err := writer.CreateFormFile("package", "reference.json")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte("{}")); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/dramas/"+idcodec.Encode(tc.project)+"/references/"+idcodec.Encode(11)+"/import", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var response struct{ Code int }
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || response.Code != tc.code {
			t.Fatalf("导入响应 %d %s", w.Code, w.Body)
		}
	}
}
