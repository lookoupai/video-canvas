package plugin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// agnesVideoExecutor 使用真实 runner 和宿主文件解析，验证发出的 HTTP 正文而非占位描述。
func agnesVideoExecutor(t *testing.T, baseURL, format string) (*plugin.Executor, *provider.Snapshot) {
	t.Helper()
	src, err := plugins.Source("agnes-video")
	if err != nil {
		t.Fatal(err)
	}
	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	pre, err := runner.Precheck(t.Context(), string(src))
	if err != nil || !pre.OK {
		t.Fatalf("Agnes 预检失败：%+v %v", pre, err)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	executor := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": string(src)}, Secrets: secretResolver{value: "agnes-test"},
		Assets: &fakeAssets{userID: 9, files: map[uint64][]byte{1: []byte("PNG")}}, IPAllowed: allowLoopback,
	})
	snap := &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "agnes", Kind: "video", UpstreamModel: "agnes-video-2.5", Capabilities: agnesVideoCaps()},
		Channel: provider.ChannelSnapshot{Key: "agnes", BaseURL: baseURL, PluginKey: "agnes-video",
			PluginVersionID: 1, Settings: map[string]any{"as": format}},
		Plugin: provider.PluginSnapshot{Key: "agnes-video", Version: meta.Version, SHA256: pre.SHA256, Meta: *meta},
	}
	return executor, snap
}

func agnesVideoCaps() modelcfg.Capabilities {
	return modelcfg.Capabilities{
		Ops: []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni},
		Refs: modelcfg.Refs{
			Image: modelcfg.RefSpec{On: true, Max: 5},
			Video: modelcfg.RefSpec{On: true, Max: 1},
			Audio: modelcfg.RefSpec{On: true, Max: 3},
		},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
	}
}

func TestAgnesVideoReferenceObjects(t *testing.T) {
	for _, tc := range []struct{ format, value string }{
		{"url", "https://cdn.example.com/a.png"},
		{"base64", "UE5H"},
		{"dataUrl", "data:image/png;base64,UE5H"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			up := &fakeNewAPI{handler: func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
				if !agnesMediaObjects(body, tc.value) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"code":"invalid_request","message":"videos[0] must be an object with url"}`))
					return
				}
				_, _ = w.Write([]byte(`{"video_id":"agnes-video-1"}`))
			}}
			srv := httptest.NewServer(up)
			defer srv.Close()
			executor, snap := agnesVideoExecutor(t, srv.URL, tc.format)
			result, err := executor.Submit(t.Context(), snap, provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{
					"prompt": "参考原镜动作", "op": "omni",
					"images": []uint64{1}, "audios": []uint64{1}, "videos": []uint64{1},
				},
			})
			if err != nil || result.ProviderTaskID != "agnes-video-1" {
				t.Fatalf("参考视频提交失败：%+v %v", result, err)
			}
			got := up.last()
			if got.Path != "/v1/videos" || got.Auth != "Bearer agnes-test" || got.Body["mode"] != "reference" {
				t.Fatalf("请求路由或模式错误：%+v", got)
			}
		})
	}
}

// agnesMediaObjects 按官方契约校验：图片与音频是字符串数组，视频数组元素必须包含 url。
func agnesMediaObjects(body map[string]any, expected string) bool {
	for _, key := range []string{"images", "audios"} {
		items, ok := body[key].([]any)
		if !ok || len(items) != 1 || items[0] != expected {
			return false
		}
	}
	videos, ok := body["videos"].([]any)
	if !ok || len(videos) != 1 {
		return false
	}
	video, ok := videos[0].(map[string]any)
	return ok && video["url"] == expected && len(video) == 1
}

func TestAgnesVideoKeyframesRemainStrings(t *testing.T) {
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"video_id":"agnes-keyframe-1"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	executor, snap := agnesVideoExecutor(t, srv.URL, "url")
	_, err := executor.Submit(t.Context(), snap, provider.SubmitInput{
		Task:  provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "首尾帧动画", "op": "i2v", "images": []uint64{1, 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := up.last().Body
	if body["first_frame"] != "https://cdn.example.com/a.png" || body["last_frame"] != body["first_frame"] || body["videos"] != nil {
		t.Fatalf("首尾帧不应被包装成视频对象：%+v", body)
	}
}
