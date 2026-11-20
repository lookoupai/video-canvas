package service_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/service"
)

func intPtr(n int) *int { return &n }

// 登记一个启用的视频模型与图片、文本模型；video 带可调的时长参数。
func dramaModelRegistry(registry *dramaRegistry, duration *modelcfg.ParamField) {
	video := &provider.Snapshot{ModelRevisionID: 20, Model: provider.ModelSnapshot{Key: "vid", Kind: "video"}}
	if duration != nil {
		video.Model.Capabilities.Params = modelcfg.ParamSet{{Name: "duration", ParamField: *duration}}
	}
	registry.snaps = map[string]*provider.Snapshot{
		"vid": video,
		"img": {ModelRevisionID: 21, Model: provider.ModelSnapshot{Key: "img", Kind: "image"}},
		"txt": {ModelRevisionID: 22, Model: provider.ModelSnapshot{Key: "txt", Kind: "text"}},
	}
	registry.unavailable = map[string]bool{"off": true}
}

func createWithModels(r *dramaMemory, s *service.DramaService, video string, images model.DramaImageModels) error {
	_, err := s.Create(context.Background(), 7, &model.CreateDramaReq{SourceToken: seedDramaDraft(r, "第一章\n正文"), SourceKind: "novel", StyleKey: "custom", StyleCustom: "写实", VideoModel: video, ImageModels: images})
	return err
}

func TestDramaCreateValidatesVideoAndImageModels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		video  string
		images model.DramaImageModels
		code   int
	}{
		{"全空放行", "", model.DramaImageModels{}, 0},
		{"合法模型", "vid", model.DramaImageModels{Character: "img", Scene: "img", Prop: "img"}, 0},
		{"视频模型已停用", "off", model.DramaImageModels{}, 10001},
		{"视频模型类型不符", "img", model.DramaImageModels{}, 10001},
		{"图片模型是文本类型", "", model.DramaImageModels{Scene: "txt"}, 10001},
		{"图片模型是视频类型", "", model.DramaImageModels{Prop: "vid"}, 10001},
		{"图片模型已停用", "", model.DramaImageModels{Character: "off"}, 10001},
		{"故事板模型合法", "", model.DramaImageModels{Board: "img"}, 0},
		{"故事板模型是视频类型", "", model.DramaImageModels{Board: "vid"}, 10001},
		{"故事板模型已停用", "", model.DramaImageModels{Board: "off"}, 10001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
			dramaModelRegistry(registry, nil)
			err := createWithModels(r, s, tc.video, tc.images)
			if tc.code != 0 {
				dramaCheckCode(t, err, tc.code)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.project.VideoModel != tc.video {
				t.Fatalf("video=%q", r.project.VideoModel)
			}
			var got model.DramaImageModels
			if err = json.Unmarshal(r.project.ImageModels, &got); err != nil || got != tc.images {
				t.Fatalf("images=%s err=%v", r.project.ImageModels, err)
			}
		})
	}
}

func TestDramaGetReturnsVideoAndImageModels(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaModelRegistry(registry, nil)
	if err := createWithModels(r, s, "vid", model.DramaImageModels{Character: "img", Scene: "img"}); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(context.Background(), 7, r.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.VideoModel != "vid" || view.ImageModels != (model.DramaImageModels{Character: "img", Scene: "img"}) {
		t.Fatalf("view=%+v", view)
	}
	raw, _ := json.Marshal(view)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if string(fields["video_model"]) != `"vid"` || string(fields["image_models"]) != `{"character":"img","scene":"img","prop":""}` {
		t.Fatalf("json=%s", raw)
	}
}

func TestDramaUpdateVideoAndImageModels(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaModelRegistry(registry, nil)
	vid, imgs := "vid", model.DramaImageModels{Prop: "img", Board: "img"}
	view, err := s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: 1, VideoModel: &vid, ImageModels: &imgs})
	if err != nil {
		t.Fatal(err)
	}
	if view.VideoModel != "vid" || view.ImageModels.Prop != "img" || view.ImageModels.Board != "img" {
		t.Fatalf("view=%+v", view)
	}
	// 不带字段时保持原值；带空串清除
	if view, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: view.Revision}); err != nil || view.VideoModel != "vid" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	empty := ""
	if view, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: view.Revision, VideoModel: &empty}); err != nil || view.VideoModel != "" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	wrongKind := "img"
	for _, req := range []*model.UpdateDramaReq{
		{Revision: view.Revision, VideoModel: &wrongKind},
		{Revision: view.Revision, ImageModels: &model.DramaImageModels{Scene: "vid"}},
	} {
		_, err = s.Update(context.Background(), 7, 1, req)
		dramaCheckCode(t, err, 10001)
	}
}

func TestDramaStoryboardDefaultSecondsFromVideoModel(t *testing.T) {
	enumField := modelcfg.ParamField{Type: modelcfg.ParamEnum, Options: []any{"5", float64(10), "8"}}
	for _, tc := range []struct {
		name     string
		videoKey string
		field    *modelcfg.ParamField
		explicit int
		want     int
	}{
		{"number取max", "vid", &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(12)}, 0, 12},
		{"number上限夹到60", "vid", &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(120)}, 0, 60},
		{"number下限夹到4", "vid", &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(2)}, 0, 4},
		{"enum取最大选项", "vid", &enumField, 0, 10},
		{"无时长参数回落15", "vid", nil, 0, 15},
		{"未配视频模型回落15", "", &enumField, 0, 15},
		{"视频模型已停用回落15", "off", &enumField, 0, 15},
		{"显式值优先", "vid", &enumField, 8, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			storyboardReady(r)
			r.project.VideoModel = tc.videoKey
			s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
			dramaModelRegistry(registry, tc.field)
			req := storyboardRequest(t, tc.explicit, []int{1})
			// 估价与提交走同一套校验与解析
			if _, err := s.Estimate(context.Background(), 7, 1, req); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateJob(context.Background(), 7, 1, req); err != nil {
				t.Fatal(err)
			}
			var input struct {
				MaxSeconds int `json:"max_seconds"`
			}
			if err := json.Unmarshal(r.jobs[0].InputJSON, &input); err != nil || input.MaxSeconds != tc.want {
				t.Fatalf("input=%s err=%v want %d", r.jobs[0].InputJSON, err, tc.want)
			}
			if req.MaxSeconds != tc.explicit {
				t.Fatalf("不应改写调用方的请求: %d", req.MaxSeconds)
			}
		})
	}
}

// 新建资产节点按类型带上项目的默认图片模型；已有节点不动。
func TestDramaCanvasBreakdownAppliesDefaultImageModels(t *testing.T) {
	f := newDramaCanvasFake()
	f.canvas.PayloadJSON = []byte(`{"nodes":[{"id":"old","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"街道","model":"keep","drama":{"type":"scene","name":"街道","look":"","episodes":[2],"aliases":[],"description":""}}}],"edges":[]}`)
	svc := service.NewDramaCanvasService(f, &dramaCanvasBroadcast{})
	assets := append(dramaCanvasAssets(1), drama.Asset{Type: "prop", Name: "玉佩", Aliases: []string{}, Episodes: []int{1}, Description: "双鱼玉佩"})
	models := map[string]string{"character": "img-c", "scene": "img-s", "prop": "img-p"}
	created, err := svc.ApplyBreakdown(context.Background(), 1, 3, 7, 9, service.DramaBreakdownBatch{Episodes: []int{1}, Assets: assets, Models: models})
	if err != nil {
		t.Fatal(err)
	}
	g, err := canvasgraph.Parse(f.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, c := range created {
		got[c.Asset.Type] = g.Node(c.NodeID).Data()["model"]
	}
	// 街道合并进已有节点，不属于本次新增
	if !reflect.DeepEqual(got, map[string]any{"character": "img-c", "prop": "img-p"}) {
		t.Fatalf("new nodes=%v created=%+v", got, created)
	}
	if g.Node("old").Data()["model"] != "keep" {
		t.Fatalf("已有节点被改: %+v", g.Node("old").Data())
	}
}

func TestDramaCanvasBreakdownWithoutModelsLeavesModelUnset(t *testing.T) {
	f := newDramaCanvasFake()
	created, err := service.NewDramaCanvasService(f, &dramaCanvasBroadcast{}).ApplyBreakdown(context.Background(), 1, 3, 7, 9, service.DramaBreakdownBatch{Episodes: []int{1}, Assets: dramaCanvasAssets(1), Models: map[string]string{"prop": "img-p"}})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := canvasgraph.Parse(f.canvas.PayloadJSON)
	for _, c := range created {
		if _, ok := g.Node(c.NodeID).Data()["model"]; ok {
			t.Fatalf("未指定的类型不应写 model: %+v", g.Node(c.NodeID).Data())
		}
	}
}

// 拆解步骤把项目配置的图片模型交给画布服务。
func TestDramaBreakdownPassesProjectImageModels(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "剧本")
	r.project.Ratio = "9:16"
	r.project.ImageModels = []byte(`{"character":"img","scene":"","prop":"img"}`)
	canvas := &dramaPipelineCanvas{}
	raw, _ := json.Marshal(drama.BreakdownOutput{Assets: []drama.Asset{{Type: "scene", Name: "街道", Aliases: []string{}, Episodes: []int{1}, Description: "街道"}}})
	e := &dramaTextStreamer{outputs: []string{"拆解稿", string(raw)}}
	s, _ := dramaTestService(r, e, canvas)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if canvas.applied != 1 || !reflect.DeepEqual(canvas.models, map[string]string{"character": "img", "prop": "img"}) {
		t.Fatalf("applied=%d models=%v job=%+v", canvas.applied, canvas.models, r.jobs[0])
	}
}

// 用指定提示词写法的视频模型跑一次分镜，返回创作段与格式段发给文本模型的提示词。
func runStoryboardWithFormat(t *testing.T, format string, promptSettings string) (creative, formatPrompt string) {
	t.Helper()
	r := newDramaMemory()
	storyboardReady(r)
	e := &dramaTextStreamer{outputs: []string{"村口，村民走近。", `{"segments":[{"seconds":12,"prompt":"@[村口](n_scene) 0-12秒：中景，村民走近。"}]}`}}
	s, registry := dramaTestService(r, e, nil)
	dramaModelRegistry(registry, &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(12)})
	registry.snaps["vid"].Model.Capabilities.PromptFormat = format
	r.project.VideoModel = "vid"
	if promptSettings != "" {
		r.project.PromptSettings = []byte(promptSettings)
	}
	job, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 0, []int{1}))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	if r.jobs[0].Status != "succeeded" || len(e.prompts) != 2 {
		t.Fatalf("status=%s code=%d prompts=%d", r.jobs[0].Status, r.jobs[0].ErrorCode, len(e.prompts))
	}
	return e.prompts[0], e.prompts[1]
}

// 内置分镜正文固定写 Seedance 分栏原稿：不管项目视频模型声明哪种写法都一样，平台适配交给片场转写。
// 格式段只切段、补资产引用、输出 segments JSON，不规定写法。
func TestDramaStoryboardAlwaysSeedance(t *testing.T) {
	const seedanceMark, h3Mark = "即梦 / Seedance", "integrated_multimodal_description"
	for _, format := range []string{"seedance", "h3", "", "sora"} {
		t.Run("模型写法="+format, func(t *testing.T) {
			creative, prompt := runStoryboardWithFormat(t, format, "")
			if !strings.Contains(creative, seedanceMark) || strings.Contains(creative, h3Mark) || strings.Contains(creative, `"required":["segments"]`) {
				t.Fatalf("内置创作正文应是 Seedance 分栏写法、不带 JSON:\n%s", creative)
			}
			if strings.Contains(prompt, seedanceMark) || strings.Contains(prompt, h3Mark) {
				t.Fatalf("格式段不应规定平台写法:\n%s", prompt)
			}
			for _, want := range []string{`"max_seconds":12`, "一段的时长不超过 `max_seconds`", `"required":["segments"]`} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("格式段提示词缺少 %s", want)
				}
			}
		})
	}
}

// 用户换成自己的分镜正文后，提示词里不再出现内置的 Seedance 分栏写法。
func TestDramaStoryboardCustomBodyDropsSeedance(t *testing.T) {
	custom := `{"video":{"source":"mine","label":"我的","bodies":{"storyboard":"每段一句话描述画面，不分栏。"}}}`
	creative, prompt := runStoryboardWithFormat(t, "", custom)
	if !strings.Contains(creative, "每段一句话描述画面，不分栏。") {
		t.Fatalf("应使用用户正文:\n%s", creative)
	}
	for _, text := range []string{creative, prompt} {
		for _, gone := range []string{"即梦 / Seedance", "【画幅风格】", "【时间轴】"} {
			if strings.Contains(text, gone) {
				t.Fatalf("自定义正文后不应再出现 %s:\n%s", gone, text)
			}
		}
	}
}

// 内置来源的项目快照里存的是创建当时的内置正文副本；发版后内置提示词更新了，
// 已有项目也要用最新的内置提示词，而不是继续用旧副本。
func TestDramaBuiltinPromptIgnoresStaleSnapshotBody(t *testing.T) {
	stale := `{"video":{"source":"builtin","label":"内置","bodies":{"storyboard":"旧版内置正文：已过期"}}}`
	for _, format := range []string{"", "seedance", "h3"} {
		creative, _ := runStoryboardWithFormat(t, format, stale)
		if strings.Contains(creative, "旧版内置正文：已过期") {
			t.Fatalf("format=%q 仍在用快照里的旧内置正文:\n%s", format, creative)
		}
		if !strings.Contains(creative, "## 输出约定") || !strings.Contains(creative, "镜头写法") {
			t.Fatalf("format=%q 没有用当前内置创作正文", format)
		}
	}
}

func TestDramaStoryboardCustomPromptNotOverriddenByFormat(t *testing.T) {
	custom := `{"video":{"source":"mine","id":3,"label":"我的 · 自写","bodies":{"storyboard":"自写正文：只写一句话"}}}`
	for _, format := range []string{"seedance", "h3"} {
		creative, prompt := runStoryboardWithFormat(t, format, custom)
		if !strings.Contains(creative, "自写正文：只写一句话") || strings.Contains(creative, "目标平台是即梦 / Seedance") || strings.Contains(creative, "integrated_multimodal_description") {
			t.Fatalf("用户正文被平台模板覆盖或平台模板泄漏进创作段:\n%s", creative)
		}
		// 用户正文只替换创作段；格式段系统固定、不带平台写法，并拿到时长上限
		if strings.Contains(prompt, "自写正文：只写一句话") || strings.Contains(prompt, "即梦 / Seedance") {
			t.Fatalf("格式段被用户正文改动:\n%s", prompt)
		}
		if !strings.Contains(prompt, `"max_seconds":12`) {
			t.Fatal("格式段变量缺少 max_seconds")
		}
	}
}

// 图片模型 img 带比例和分辨率两个枚举参数，用来校验资产图片参数。
func dramaImageParamRegistry(registry *dramaRegistry) {
	dramaModelRegistry(registry, nil)
	registry.snaps["img"].Model.Capabilities.Params = modelcfg.ParamSet{
		{Name: "aspect_ratio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Options: []any{"1:1", "16:9", "21:9"}}},
		{Name: "resolution", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Options: []any{"1K", "2K"}}},
	}
}

func TestDramaCreateValidatesImageParams(t *testing.T) {
	models := model.DramaImageModels{Character: "img", Scene: "img"}
	for _, tc := range []struct {
		name   string
		params model.DramaImageParams
		ok     bool
	}{
		{"全空放行", model.DramaImageParams{}, true},
		{"在模型选项里", model.DramaImageParams{Character: map[string]string{"aspect_ratio": "16:9", "resolution": "2K"}, Scene: map[string]string{"aspect_ratio": "21:9"}}, true},
		{"跟随默认模型只查写法", model.DramaImageParams{Prop: map[string]string{"aspect_ratio": "3:2", "resolution": "4K"}}, true},
		{"比例不在选项里", model.DramaImageParams{Character: map[string]string{"aspect_ratio": "9:16"}}, false},
		{"分辨率不在选项里", model.DramaImageParams{Scene: map[string]string{"resolution": "4K"}}, false},
		{"模型没有这个参数", model.DramaImageParams{Character: map[string]string{"ratio": "16:9"}}, false},
		{"不认识的参数名", model.DramaImageParams{Prop: map[string]string{"seed": "1"}}, false},
		{"空值", model.DramaImageParams{Prop: map[string]string{"resolution": ""}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
			dramaImageParamRegistry(registry)
			_, err := s.Create(context.Background(), 7, &model.CreateDramaReq{SourceToken: seedDramaDraft(r, "第一章\n正文"), SourceKind: "novel", StyleKey: "custom", StyleCustom: "写实", ImageModels: models, ImageParams: tc.params})
			if !tc.ok {
				dramaCheckCode(t, err, 10001)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got model.DramaImageParams
			if err = json.Unmarshal(r.project.ImageParams, &got); err != nil || !reflect.DeepEqual(got.ByType(), tc.params.ByType()) {
				t.Fatalf("params=%s err=%v", r.project.ImageParams, err)
			}
		})
	}
}

func TestDramaGetAndUpdateImageParams(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaImageParamRegistry(registry)
	imgs := model.DramaImageModels{Scene: "img"}
	params := model.DramaImageParams{Scene: map[string]string{"aspect_ratio": "21:9"}}
	view, err := s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: 1, ImageModels: &imgs, ImageParams: &params})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.ImageParams.ByType(), params.ByType()) {
		t.Fatalf("view=%+v", view.ImageParams)
	}
	raw, _ := json.Marshal(view)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if string(fields["image_params"]) != `{"character":{},"scene":{"aspect_ratio":"21:9"},"prop":{}}` {
		t.Fatalf("json=%s", fields["image_params"])
	}
	// 只改参数时按项目已存的模型校验
	bad := model.DramaImageParams{Scene: map[string]string{"aspect_ratio": "9:16"}}
	_, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: view.Revision, ImageParams: &bad})
	dramaCheckCode(t, err, 10001)
}

// 新建资产节点按类型带上项目的默认图片参数，写进 data.params；已有节点不动。
func TestDramaCanvasBreakdownAppliesDefaultImageParams(t *testing.T) {
	f := newDramaCanvasFake()
	f.canvas.PayloadJSON = []byte(`{"nodes":[{"id":"old","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"街道","params":{"aspect_ratio":"1:1"},"drama":{"type":"scene","name":"街道","look":"","episodes":[2],"aliases":[],"description":""}}}],"edges":[]}`)
	svc := service.NewDramaCanvasService(f, &dramaCanvasBroadcast{})
	assets := append(dramaCanvasAssets(1), drama.Asset{Type: "prop", Name: "玉佩", Aliases: []string{}, Episodes: []int{1}, Description: "双鱼玉佩"})
	params := map[string]map[string]string{"character": {"aspect_ratio": "16:9", "resolution": "2K"}, "scene": {"aspect_ratio": "21:9"}}
	created, err := svc.ApplyBreakdown(context.Background(), 1, 3, 7, 9, service.DramaBreakdownBatch{Episodes: []int{1}, Assets: assets, Params: params})
	if err != nil {
		t.Fatal(err)
	}
	g, err := canvasgraph.Parse(f.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, c := range created {
		got[c.Asset.Type] = g.Node(c.NodeID).Data()["params"]
	}
	want := map[string]any{"character": map[string]any{"aspect_ratio": "16:9", "resolution": "2K"}, "prop": nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("new nodes=%v", got)
	}
	if !reflect.DeepEqual(g.Node("old").Data()["params"], map[string]any{"aspect_ratio": "1:1"}) {
		t.Fatalf("已有节点被改: %+v", g.Node("old").Data())
	}
}

// 拆解步骤把项目配置的图片参数交给画布服务。
func TestDramaBreakdownPassesProjectImageParams(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "剧本")
	r.project.Ratio = "9:16"
	r.project.ImageParams = []byte(`{"character":{"aspect_ratio":"16:9"},"scene":{},"prop":{"resolution":"2K"}}`)
	canvas := &dramaPipelineCanvas{}
	raw, _ := json.Marshal(drama.BreakdownOutput{Assets: []drama.Asset{{Type: "scene", Name: "街道", Aliases: []string{}, Episodes: []int{1}, Description: "街道"}}})
	e := &dramaTextStreamer{outputs: []string{"拆解稿", string(raw)}}
	s, _ := dramaTestService(r, e, canvas)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{"character": {"aspect_ratio": "16:9"}, "prop": {"resolution": "2K"}}
	if canvas.applied != 1 || !reflect.DeepEqual(canvas.params, want) {
		t.Fatalf("applied=%d params=%v", canvas.applied, canvas.params)
	}
}

// 故事板的图片模型不属于任何资产类型，不能混进资产拆解按类型取模型的映射。
func TestDramaImageModelsByTypeIgnoresBoard(t *testing.T) {
	got := model.DramaImageModels{Scene: "s", Board: "b"}.ByType()
	if !reflect.DeepEqual(got, map[string]string{"scene": "s"}) {
		t.Fatalf("ByType=%v", got)
	}
}
