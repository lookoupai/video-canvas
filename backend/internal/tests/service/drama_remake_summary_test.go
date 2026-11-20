package service_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/service/dramasegmentfake"
)

func TestDramaRemakeSummaryUsesTargetDurations(t *testing.T) {
	for _, tc := range []struct {
		name      string
		durations []int64
		want      int
	}{
		{"两个半秒尾数相加", []int64{2500, 2500}, 5},
		{"只对总时长向上取整", []int64{2500, 2501}, 6},
		{"跟拍与普通片段混排", []int64{2500, 2500, 0}, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := dramasegmentfake.New()
			svc := segmentService(r)
			for _, duration := range tc.durations {
				seconds := 5
				if duration != 0 {
					seconds = 3
				}
				if _, err := svc.Create(t.Context(), 1, 7, 1, &model.CreateDramaSegmentReq{Seconds: &seconds}); err != nil {
					t.Fatal(err)
				}
				if duration != 0 {
					raw, err := json.Marshal(model.DramaSegmentRemake{ReferenceID: "reference", TargetDurationMs: duration})
					if err != nil {
						t.Fatal(err)
					}
					r.Rows[len(r.Rows)-1].Remake = raw
				}
			}
			count, seconds, done, err := svc.Summary(t.Context(), 1, 10)
			if err != nil || count != len(tc.durations) || seconds != tc.want || done != 0 {
				t.Fatalf("片场时长摘要 = %d镜/%d秒/%d选版，错误 %v", count, seconds, done, err)
			}
		})
	}
}
