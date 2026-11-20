package service_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service/dramasegmentfake"
)

func TestDramaRemakeEdit_WindowPersistsAndValidates(t *testing.T) {
	r := dramasegmentfake.New()
	s := segmentService(r)
	v, err := s.Create(t.Context(), 1, 7, 1, &model.CreateDramaSegmentReq{})
	if err != nil {
		t.Fatal(err)
	}
	setRemakeWindow(t, &r.Rows[0], 0, 2500)
	start, duration := int64(500), int64(2000)
	updated, err := s.Update(t.Context(), 1, uint64(v.ID), &model.UpdateDramaSegmentReq{Revision: 1, TrimStartMs: &start, TargetDurationMs: &duration})
	if err != nil || updated.Remake == nil || updated.Remake.TrimStartMs != start || updated.Remake.TargetDurationMs != duration {
		t.Fatalf("裁剪修改未保存：%+v %v", updated, err)
	}
	var saved model.DramaSegmentRemake
	if err := json.Unmarshal(r.Rows[0].Remake, &saved); err != nil || saved.TrimStartMs != start {
		t.Fatalf("持久化窗口不匹配：%+v %v", saved, err)
	}
	negative := int64(-1)
	_, err = s.Update(t.Context(), 1, uint64(v.ID), &model.UpdateDramaSegmentReq{Revision: updated.Revision, TrimStartMs: &negative})
	segmentCode(t, err, errcode.ErrInvalidParams.Code)
	r.Rows[0].Remake = []byte(`{}`)
	_, err = s.Update(t.Context(), 1, uint64(v.ID), &model.UpdateDramaSegmentReq{Revision: updated.Revision, TrimStartMs: &start})
	segmentCode(t, err, errcode.ErrInvalidParams.Code)
}
