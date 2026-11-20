package service_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
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
			boards := map[string]string{idcodec.Encode(r.Rows[0].ID): drama.SourceHash(r.Rows[0].Prompt)}
			stats, err := svc.Summary(t.Context(), 1, 10, boards)
			if err != nil || stats.Count != len(tc.durations) || stats.Seconds != tc.want || stats.Done != 0 || stats.Boards != 1 {
				t.Fatalf("仿拍时长与故事板摘要 = %+v，错误 %v", stats, err)
			}
		})
	}
}
