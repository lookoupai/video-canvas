package service_test

import (
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

func TestReferenceDraftCreateRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		edit func(*model.CreateDramaReferenceDraftReq)
	}{
		{"缺少创建键", func(r *model.CreateDramaReferenceDraftReq) { r.RequestKey = "" }},
		{"缺少参考修订", func(r *model.CreateDramaReferenceDraftReq) { r.ReferenceRevision = 0 }},
		{"创建键过长", func(r *model.CreateDramaReferenceDraftReq) { r.RequestKey = strings.Repeat("a", 129) }},
		{"要求过长", func(r *model.CreateDramaReferenceDraftReq) { r.Instruction = strings.Repeat("字", 4001) }},
		{"提示词过长", func(r *model.CreateDramaReferenceDraftReq) { r.Prompts[0].Prompt = strings.Repeat("字", 40001) }},
		{"跨用户图片", func(r *model.CreateDramaReferenceDraftReq) { r.Evidence[0].AssetID = 3 }},
		{"视频不是帧图", func(r *model.CreateDramaReferenceDraftReq) { r.Evidence[0].AssetID = 1 }},
		{"缺失素材", func(r *model.CreateDramaReferenceDraftReq) { r.Evidence[0].AssetID = 999 }},
		{"未知镜号", func(r *model.CreateDramaReferenceDraftReq) { r.Evidence[0].ShotID = "missing" }},
		{"结束时刻不在本镜", func(r *model.CreateDramaReferenceDraftReq) { r.Evidence[0].AtMs = 2500 }},
		{"负时间", func(r *model.CreateDramaReferenceDraftReq) { r.Evidence[0].AtMs = -1 }},
		{"重复证据", func(r *model.CreateDramaReferenceDraftReq) { r.Evidence = append(r.Evidence, r.Evidence[0]) }},
		{"未知提示词镜号", func(r *model.CreateDramaReferenceDraftReq) { r.Prompts[0].ShotID = "unknown" }},
		{"重复提示词", func(r *model.CreateDramaReferenceDraftReq) { r.Prompts = append(r.Prompts, r.Prompts[0]) }},
		{"非法观察字段", func(r *model.CreateDramaReferenceDraftReq) { r.Observations[0].Field = "password" }},
		{"重复观察字段", func(r *model.CreateDramaReferenceDraftReq) {
			r.Observations = append(r.Observations, r.Observations[0])
		}},
		{"未知核对状态", func(r *model.CreateDramaReferenceDraftReq) { r.Observations[0].ReviewState = "certain" }},
		{"重复观察证据", func(r *model.CreateDramaReferenceDraftReq) { r.Observations[0].EvidenceIDs = []string{"f1", "f1"} }},
		{"缺少观察证据", func(r *model.CreateDramaReferenceDraftReq) { r.Observations[0].EvidenceIDs = nil }},
		{"跨镜观察证据", func(r *model.CreateDramaReferenceDraftReq) { r.Observations[0].ShotID = "2" }},
		{"伪造观察来源", func(r *model.CreateDramaReferenceDraftReq) { r.Observations[0].Source = "admin" }},
		{"非法覆盖状态", func(r *model.CreateDramaReferenceDraftReq) {
			r.Coverage = &model.DramaReferenceCoverage{Visual: "certain"}
		}},
		{"空文档不能声称完整", func(r *model.CreateDramaReferenceDraftReq) {
			r.Document = &model.DramaReferenceDocument{}
			r.Prompts, r.Evidence, r.Observations = nil, nil, nil
			r.Coverage = &model.DramaReferenceCoverage{Visual: "complete"}
		}},
		{"非法镜头区间", func(r *model.CreateDramaReferenceDraftReq) {
			doc := referenceDocument()
			doc.Shots[0].EndMs = 6000
			r.Document = &doc
		}},
		{"空文档孤立人物", func(r *model.CreateDramaReferenceDraftReq) {
			r.Document = &model.DramaReferenceDocument{Characters: []model.DramaReferenceCharacter{{ID: "a", Name: "路人"}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newDraftService(t)
			req := draftRequest()
			tc.edit(req)
			_, err := svc.Create(t.Context(), 7, 4, 3, req)
			wantMktCode(t, err, errcode.ErrInvalidParams.Code)
			if len(repo.rows) != 0 {
				t.Fatal("无效草稿不应落库")
			}
		})
	}
}

func TestReferenceDraftOwnerAndReferenceRevision(t *testing.T) {
	svc, _ := newDraftService(t)
	req := draftRequest()
	req.ReferenceRevision = 1
	_, err := svc.Create(t.Context(), 7, 4, 3, req)
	wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
	for _, ids := range [][3]uint64{{8, 4, 3}, {7, 44, 3}, {7, 4, 33}} {
		_, err = svc.Create(t.Context(), ids[0], ids[1], ids[2], draftRequest())
		wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
	}
	got, err := svc.Create(t.Context(), 7, 4, 3, draftRequest())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(got.ID)
	for _, ids := range [][3]uint64{{8, 4, 3}, {7, 44, 3}, {7, 4, 33}} {
		_, err = svc.Get(t.Context(), ids[0], ids[1], ids[2], id)
		wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
		_, err = svc.List(t.Context(), ids[0], ids[1], ids[2], &model.ListDramaReferenceDraftsReq{Limit: 20})
		wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
		text := "越权"
		_, err = svc.Update(t.Context(), ids[0], ids[1], ids[2], id, &model.UpdateDramaReferenceDraftReq{Revision: 1, Instruction: &text})
		wantMktCode(t, err, errcode.ErrDramaNotFound.Code)
	}
}

func TestReferenceDraftClosedAndInvalidUpdate(t *testing.T) {
	svc, repo := newDraftService(t)
	got, err := svc.Create(t.Context(), 7, 4, 3, draftRequest())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(got.ID)
	for _, req := range []*model.UpdateDramaReferenceDraftReq{nil, {}, {Revision: 1}} {
		_, err := svc.Update(t.Context(), 7, 4, 3, id, req)
		wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	}
	for _, status := range []string{"applied", "discarded"} {
		row := repo.rows[id]
		row.Status = status
		repo.rows[id] = row
		text := "过期修改"
		_, err := svc.Update(t.Context(), 7, 4, 3, id, &model.UpdateDramaReferenceDraftReq{Revision: 1, Instruction: &text})
		wantMktCode(t, err, errcode.ErrCanvasConflict.Code)
	}
	for _, req := range []*model.ListDramaReferenceDraftsReq{nil, {Limit: -1}, {Limit: 101}, {Cursor: "invalid"}} {
		_, err := svc.List(t.Context(), 7, 4, 3, req)
		wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	}
}
