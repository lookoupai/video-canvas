package handler_test

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

type referencePlanHTTPService struct {
	user, project, reference, draft uint64
}

func (s *referencePlanHTTPService) Apply(_ context.Context, user, project, reference, draft uint64, req *model.ApplyDramaReferenceDraftReq) (*model.DramaReferenceApplyResponse, error) {
	s.user, s.project, s.reference, s.draft = user, project, reference, draft
	return &model.DramaReferenceApplyResponse{DraftRevision: req.Revision}, nil
}

func (s *referencePlanHTTPService) Generate(_ context.Context, user, project, reference, draft uint64, req *model.DramaReferenceGenerateReq) (*model.DramaReferenceGenerateResponse, error) {
	s.user, s.project, s.reference, s.draft = user, project, reference, draft
	return &model.DramaReferenceGenerateResponse{ApplicationID: req.ApplicationID, Items: []model.DramaReferenceGenerateItem{}}, nil
}

func (s *referencePlanHTTPService) Quote(_ context.Context, user, project, reference, draft uint64, _ *model.QuoteDramaReferenceDraftReq) (*model.DramaReferencePlan, error) {
	s.user, s.project, s.reference, s.draft = user, project, reference, draft
	if project == 99 {
		return nil, errcode.ErrDramaNotFound
	}
	return &model.DramaReferencePlan{Executable: true, Units: []model.DramaReferencePlanUnit{}}, nil
}

func TestReferencePlanQuoteHTTPContract(t *testing.T) {
	svc := &referencePlanHTTPService{}
	h := handler.NewDramaReferencePlanHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)); c.Next() })
	r.POST("/dramas/:id/references/:reference/drafts/:draft/quote", h.Quote)
	r.POST("/dramas/:id/references/:reference/drafts/:draft/apply", h.Apply)
	r.POST("/dramas/:id/references/:reference/drafts/:draft/generate", h.Generate)
	path := "/dramas/" + idcodec.Encode(7) + "/references/" + idcodec.Encode(11) + "/drafts/" + idcodec.Encode(12) + "/quote"
	for _, tc := range []struct {
		name, path, body string
		status, code     int
	}{
		{"返回报价", path, `{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"video","duration":5}]}`, 200, 0},
		{"素材 ID 与 Asset API 一致", path, `{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"keyframe","image_asset_ids":[12]}]}`, 200, 0},
		{"兼容十进制字符串素材 ID", path, `{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"keyframe","image_asset_ids":["12"]}]}`, 200, 0},
		{"拒绝溢出素材 ID", path, `{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"keyframe","image_asset_ids":[18446744073709551616]}]}`, 400, 10001},
		{"缺少镜头", path, `{"revision":1,"model_id":"m","units":[]}`, 400, 10001},
		{"未知字段", path, `{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"video"}],"user_id":9}`, 400, 10001},
		{"草稿不存在", "/dramas/" + idcodec.Encode(99) + "/references/" + idcodec.Encode(11) + "/drafts/" + idcodec.Encode(12) + "/quote", `{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"video"}]}`, 404, 67001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := characterCall(t, r, "POST", tc.path, tc.body)
			if status != tc.status || responseCode(t, body) != tc.code {
				t.Fatalf("返回 %d %+v", status, body)
			}
		})
	}
	for _, tc := range []struct {
		path, body string
	}{
		{"/dramas/" + idcodec.Encode(7) + "/references/" + idcodec.Encode(11) + "/drafts/" + idcodec.Encode(12) + "/apply",
			`{"revision":1,"request_key":"k","plan_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","mode":"follow","quote":{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"text"}]}}`},
		{"/dramas/" + idcodec.Encode(7) + "/references/" + idcodec.Encode(11) + "/drafts/" + idcodec.Encode(12) + "/generate",
			`{"revision":1,"request_key":"k","application_id":"app","plan_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","quote":{"revision":1,"model_id":"m","units":[{"shot_id":"1","mode":"text"}]}}`},
	} {
		status, body := characterCall(t, r, "POST", tc.path, tc.body)
		if status != 200 {
			t.Fatalf("草稿采用或生成接口返回 %d %+v", status, body)
		}
	}
	if svc.user != 9 || svc.project != 7 || svc.reference != 11 || svc.draft != 12 {
		t.Fatalf("登录主体或资源范围没有透传：%+v", svc)
	}
}
