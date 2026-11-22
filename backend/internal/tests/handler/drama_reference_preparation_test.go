package handler_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

type preparationHTTP struct{ err error }

func (f *preparationHTTP) Create(context.Context, uint64, uint64, uint64, *model.CreateDramaReferencePreparationReq) (*model.DramaReferencePreparationView, error) {
	return &model.DramaReferencePreparationView{ID: "7", Status: "queued"}, f.err
}
func (f *preparationHTTP) Get(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferencePreparationView, error) {
	return &model.DramaReferencePreparationView{ID: "7"}, f.err
}
func (f *preparationHTTP) List(context.Context, uint64, uint64, uint64, *model.ListDramaReferenceDraftsReq) (*model.DramaReferencePreparationList, error) {
	return &model.DramaReferencePreparationList{Items: []model.DramaReferencePreparationView{}}, f.err
}
func (f *preparationHTTP) Shots(context.Context, uint64, uint64, uint64, uint64, *model.ListDramaReferenceDraftsReq) (*model.DramaReferencePreparedShotList, error) {
	return &model.DramaReferencePreparedShotList{Items: []model.DramaReferenceShot{}}, f.err
}
func (f *preparationHTTP) Frames(context.Context, uint64, uint64, uint64, uint64, *model.ListDramaReferenceFramesReq) (*model.DramaReferenceFrameList, error) {
	return &model.DramaReferenceFrameList{Items: []model.DramaReferencePreparedFrameView{}}, f.err
}
func (f *preparationHTTP) Cancel(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferencePreparationView, error) {
	return &model.DramaReferencePreparationView{ID: "7", Status: "canceled"}, f.err
}
func (f *preparationHTTP) Resume(context.Context, uint64, uint64, uint64, uint64, *model.ResumeDramaReferencePreparationReq) (*model.DramaReferencePreparationView, error) {
	return &model.DramaReferencePreparationView{ID: "7", Status: "queued"}, f.err
}
func (f *preparationHTTP) AddFrames(context.Context, uint64, uint64, uint64, uint64, *model.AddDramaReferenceFramesReq) (*model.DramaReferencePreparationView, error) {
	return &model.DramaReferencePreparationView{ID: "7", Status: "queued"}, f.err
}

func preparationRouter(f *preparationHTTP) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uint64(7)); c.Next() })
	h := handler.NewDramaReferencePreparationHandler(f)
	base := "/dramas/:id/references/:reference/preparations"
	r.POST(base, h.Create)
	r.GET(base, h.List)
	r.GET(base+"/:preparation", h.Get)
	r.POST(base+"/:preparation/cancel", h.Cancel)
	r.POST(base+"/:preparation/resume", h.Resume)
	r.POST(base+"/:preparation/frames", h.AddFrames)
	r.GET(base+"/:preparation/frames", h.Frames)
	r.GET(base+"/:preparation/shots", h.Shots)
	return r
}

func TestReferencePreparationHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		err                      error
		status, code             int
	}{
		{"创建", "POST", "", `{"request_key":"x","reference_revision":2}`, nil, 201, 0},
		{"列表", "GET", "", "", nil, 200, 0}, {"查询", "GET", "/7", "", nil, 200, 0},
		{"镜头", "GET", "/7/shots?limit=1", "", nil, 200, 0}, {"帧", "GET", "/7/frames?shot_id=1", "", nil, 200, 0},
		{"取消", "POST", "/7/cancel", "", nil, 200, 0}, {"恢复", "POST", "/7/resume", `{"request_key":"r"}`, nil, 200, 0},
		{"补帧", "POST", "/7/frames", `{"request_key":"f","frames":[{"shot_id":"1","at_ms":100}]}`, nil, 200, 0},
		{"缺键", "POST", "", `{"reference_revision":2}`, nil, 400, 10001}, {"超页", "GET", "?limit=101", "", nil, 400, 10001},
		{"无点", "POST", "/7/frames", `{"request_key":"f","frames":[]}`, nil, 400, 10001}, {"伪造", "POST", "", `{"request_key":"x","reference_revision":2,"status":"succeeded"}`, nil, 400, 10001},
		{"越权", "GET", "/7", "", errcode.ErrDramaNotFound, 404, 67001}, {"冲突", "POST", "/7/resume", `{"request_key":"r"}`, errcode.ErrCanvasConflict, 409, 30002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := preparationRouter(&preparationHTTP{err: tc.err})
			req := httptest.NewRequest(tc.method, "/dramas/"+idcodec.Encode(4)+"/references/"+idcodec.Encode(3)+"/preparations"+strings.Replace(tc.path, "/7", "/"+idcodec.Encode(7), 1), strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			var body struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body.Code != tc.code {
				t.Fatalf("响应 %d %s", w.Code, w.Body.String())
			}
		})
	}
}
