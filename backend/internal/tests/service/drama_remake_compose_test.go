package service_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
)

type remakeComposeFiles struct{ *composeFiles }

func (a *remakeComposeFiles) Open(_ context.Context, user, id uint64) (*provider.AssetFile, error) {
	return &provider.AssetFile{Asset: &model.Asset{ID: id, UserID: user, Kind: "video", DurationMs: 5000}, Body: io.NopCloser(strings.NewReader(strconv.FormatUint(id, 10)))}, nil
}

func (a *remakeComposeFiles) View(_ context.Context, user, id uint64) (*model.AssetView, error) {
	return &model.AssetView{ID: id, Kind: "video", DurationMs: 5000}, nil
}

type remakeComposeProcessor struct {
	composeProcessor
	windows []videoframe.Window
	ids     []string
	mute    bool
}

func (p *remakeComposeProcessor) ComposeRanges(_ context.Context, windows []videoframe.Window, dst string, mute bool) error {
	p.windows, p.mute = append([]videoframe.Window{}, windows...), mute
	for _, window := range windows {
		body, err := os.ReadFile(window.Path)
		if err != nil {
			return err
		}
		p.ids = append(p.ids, string(body))
	}
	return os.WriteFile(dst, []byte("composed"), 0600)
}

func setRemakeWindow(t *testing.T, row *model.DramaSegment, start, duration int64) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"Remake": map[string]any{
		"adoption_id": "adopt", "reference_id": "reference", "reference_revision": 3, "asset_id": 40,
		"sources":            []map[string]any{{"shot_id": "one", "start_ms": 2500, "end_ms": 5000}},
		"target_duration_ms": duration, "trim_start_ms": start,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, row); err != nil {
		t.Fatal(err)
	}
}

func TestDramaRemakeCompose_FreezesWindowsAndKeepsOtherSegments(t *testing.T) {
	r := newComposeMemory()
	setRemakeWindow(t, &r.segments[0], 500, 2500)
	other := uint64(21)
	r.segments[1].ChosenTaskID = &other
	canvas := uint64(2)
	r.tasks = append(r.tasks, model.GenerationTask{ID: other, UserID: 7, CanvasProjectID: &canvas, NodeID: "seg:" + idcodec.Encode(6), Kind: "video", Status: model.TaskSucceeded, OutputJSON: []byte(`[{"asset_id":31,"media_type":"video"}]`)})
	files := &remakeComposeFiles{&composeFiles{r: r}}
	processor := &remakeComposeProcessor{}
	s := service.NewDramaService(service.DramaDeps{Repo: r, Segments: r, Assets: files, Compose: processor})
	ctx := context.Background()
	job, err := s.CreateJob(ctx, 7, 1, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	// 提交之后改裁剪窗口与选版，后台仍须使用提交时的素材和窗口。
	setRemakeWindow(t, &r.segments[0], 1500, 1000)
	r.tasks[0].OutputJSON = []byte(`[{"asset_id":32,"media_type":"video"}]`)
	if err := s.HandleDrama(ctx, 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	if len(processor.windows) != 2 || processor.calls != 0 || processor.mute {
		t.Fatalf("仿拍应按窗口合成并保留音轨：windows=%+v calls=%d mute=%v", processor.windows, processor.calls, processor.mute)
	}
	first, second := processor.windows[0], processor.windows[1]
	if first.StartMs != 500 || first.EndMs != 3000 || second.StartMs != 0 || second.EndMs != 5000 || strings.Join(processor.ids, ",") != "30,31" {
		t.Fatalf("窗口与选版必须固定，普通片段使用全长：%+v ids=%v", processor.windows, processor.ids)
	}
	if len(r.assets) != 1 || r.creditCalls != 0 || r.jobs[0].Status != "succeeded" {
		t.Fatalf("合成未成功或错误扣费：assets=%d credits=%d jobs=%+v", len(r.assets), r.creditCalls, r.jobs)
	}
}

func TestDramaRemakeCompose_RejectsInvalidWindowBeforeEnqueue(t *testing.T) {
	for _, tc := range []struct {
		name            string
		start, duration int64
	}{{"负起点", -1, 2500}, {"空窗口", 0, 0}, {"少于一帧", 0, 20}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newComposeMemory()
			setRemakeWindow(t, &r.segments[0], tc.start, tc.duration)
			s := composeService(r, &composeFiles{r: r}, &composeProcessor{})
			_, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
			if err == nil || len(r.jobs) != 0 {
				t.Fatalf("非法裁剪不应入队：err=%v jobs=%+v", err, r.jobs)
			}
		})
	}
}
