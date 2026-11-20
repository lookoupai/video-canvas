package drama_test

import (
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/drama"
)

func TestStoryboardStrictOutput(t *testing.T) {
	valid := `{"segments":[{"seconds":15,"prompt":"0-8秒全景平视固定：村民走近。8-15秒近景缓慢推进：女子抬头说「你来了」。"}]}`
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"合法", valid, true},
		{"空片段", `{"segments":[]}`, false},
		{"空提示词", strings.Replace(valid, `"prompt":"0-8秒全景平视固定：村民走近。8-15秒近景缓慢推进：女子抬头说「你来了」。"`, `"prompt":" "`, 1), false},
		{"零片段时长", strings.Replace(valid, `"seconds":15`, `"seconds":0`, 1), false},
		{"负片段时长", strings.Replace(valid, `"seconds":15`, `"seconds":-1`, 1), false},
		{"超全局上限", strings.Replace(valid, `"seconds":15`, `"seconds":61`, 1), false},
		{"不再接受分镜表", strings.Replace(valid, `"seconds":15,`, `"seconds":15,"shots":[],`, 1), false},
		{"缺提示词", `{"segments":[{"seconds":15}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := drama.DecodeOutput("storyboard_format", []byte(tc.text))
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && !errors.Is(err, drama.ErrOutputInvalid) {
				t.Fatalf("want invalid output, got %v", err)
			}
		})
	}
}

// 创作段输出纯文本分镜稿：不解码 JSON，只拒绝空白和超长。
func TestStoryboardDraftOutput(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"普通文本", "村口，女子抬头说「你来了」。", true},
		{"文本里带花括号也合法", `{"not":"json"}`, true},
		{"空白", " \n\t ", false},
		{"空串", "", false},
		{"超长", strings.Repeat("字", 20001), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := drama.DecodeOutput("storyboard", []byte(tc.text))
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if text, ok := got.(string); !ok || text != tc.text {
					t.Fatalf("草稿应原样返回字符串，got %#v", got)
				}
				return
			}
			if !errors.Is(err, drama.ErrOutputInvalid) {
				t.Fatalf("want invalid output, got %v", err)
			}
		})
	}
}

// 创作段只拿到风格与创作方法：不含格式栏名、行格式、引用语法和 Schema。
func TestStoryboardCreativePromptHasNoFormat(t *testing.T) {
	prompt, err := drama.Prompt("storyboard", map[string]any{"max_seconds": 15})
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"max_seconds", "景别", "运镜", "声音", "台词", "纯文本"} {
		if !strings.Contains(prompt, word) {
			t.Fatalf("创作提示词缺少 %s", word)
		}
	}
	for _, word := range []string{"segments", "@[", "shots", "shot_size", "## 输出 JSON Schema"} {
		if strings.Contains(prompt, word) {
			t.Fatalf("创作提示词不该出现 %s", word)
		}
	}
}

// 格式段负责切段、栏名、引用语法和 JSON 输出，输入是创作稿。
func TestStoryboardFormatPromptResource(t *testing.T) {
	prompt, err := drama.Prompt("storyboard_format", map[string]any{"max_seconds": 15, "draft_md": "女子抬头。"})
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"segments", "max_seconds", "@[名称](node_id)", "女子抬头。", "不增加"} {
		if !strings.Contains(prompt, word) {
			t.Fatalf("格式提示词缺少 %s", word)
		}
	}
	// 不再让模型输出分镜表
	for _, word := range []string{"shots", "shot_size"} {
		if strings.Contains(prompt, word) {
			t.Fatalf("提示词不该再出现 %s", word)
		}
	}
}

// 用户正文只替换创作段的风格部分；格式段与输出约定始终是系统固定的。
func TestStoryboardCustomBodyKeepsProtocol(t *testing.T) {
	vars := map[string]any{"max_seconds": 15}
	prompt, err := drama.PromptWithBody("storyboard", vars, "冷色调，手持晃动，多用侧光。")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "冷色调，手持晃动") || strings.Contains(prompt, "镜头写法") {
		t.Fatalf("用户正文没有替换内置创作段: %s", prompt)
	}
	if !strings.Contains(prompt, "## 输出约定") || !strings.Contains(prompt, "## 校验规则") {
		t.Fatal("用户正文不应替换输出约定")
	}
	// 用户照着旧内置写了栏名，也只会作为风格文字，不改变输出约定
	legacy, err := drama.PromptWithBody("storyboard", vars, "【画幅风格】一句话。【时间轴】起-止秒：中景。")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(legacy, "## 输出 JSON Schema") || !strings.Contains(legacy, "纯文本") {
		t.Fatal("旧格式正文不应改变创作段的输出约定")
	}
}

// 内置视频提示词正文就是即梦 / Seedance 分栏写法，界面上可以看到并整段替换；资产引用语法归格式段。
func TestStoryboardBuiltinBodyIsSeedance(t *testing.T) {
	body := drama.PromptBody("storyboard")
	for _, word := range []string{"【画幅风格】", "【时间轴】", "Seedance"} {
		if !strings.Contains(body, word) {
			t.Fatalf("内置正文缺少 %s", word)
		}
	}
	if strings.Contains(body, "@[名称](node_id)") {
		t.Fatal("资产引用语法应只在格式段")
	}
}

// 内置视频提示词每段末尾固定一栏【限制】：无字幕、无背景音乐、无文字水印（官方指南：模型默认可能自己加字幕和配乐，要明说）。
func TestStoryboardBuiltinBodyHasLimits(t *testing.T) {
	body := drama.PromptBody("storyboard")
	for _, word := range []string{"【限制】", "无字幕", "无背景音乐", "水印"} {
		if !strings.Contains(body, word) {
			t.Fatalf("内置正文缺少 %s", word)
		}
	}
	// 限制栏在时间轴后面，是每段的最后一栏
	if strings.Index(body, "【限制】") < strings.Index(body, "【时间轴】") {
		t.Fatal("【限制】应排在【时间轴】之后")
	}
	// 不再说「需要排除的内容一律不写不要出现」，限制栏是明确的例外
	if strings.Contains(body, "不写“不要出现”") && !strings.Contains(body, "除【限制】") {
		t.Fatal("限制栏要在“不写不要出现”的规则里写明例外")
	}
}

func TestStoryboardTimelineOverrun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seconds int
		prompt  string
		valid   bool
	}{
		{"合法区间", 15, "0-8秒全景。8-15秒近景。", true},
		{"s写法合法", 10, "0-3s: 中景。3-10s: 特写。", true},
		{"方括号合法", 10, "[0-10秒]：钟面微距。", true},
		{"波浪号与小数", 6, "0~2.5秒全景；2.5~6秒近景", true},
		{"区间终点超过seconds", 15, "0-15s: 开场。15-30s: 收尾。", false},
		{"波浪号秒写法超限", 15, "0~30秒：全程长镜头", false},
		{"全角破折号超限", 8, "0–12秒推镜", false},
		{"H3切点合法", 12, "[Shot 1] A. [Shot 2] At 00:04.500, the camera cuts to B.", true},
		{"H3切点超限", 12, "[Shot 1] A. [Shot 2] At 00:13.000, the camera cuts to B.", false},
		{"时钟写法不当区间", 5, "监控画面 00:10-00:20 的时间戳", true},
		{"帧率不是秒", 5, "24-60fps 的质感", true},
		{"无时间轴", 5, "村民走近，女子抬头。", true},
		{"倒序区间忽略", 5, "10-3秒", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := drama.StoryboardOutput{Segments: []drama.StoryboardSegment{{Seconds: tc.seconds, Prompt: tc.prompt}}}
			err := out.ValidateMaxSeconds(30)
			if tc.valid && err != nil {
				t.Fatalf("不该判无效: %v", err)
			}
			if !tc.valid && !errors.Is(err, drama.ErrOutputInvalid) {
				t.Fatalf("应判无效，got %v", err)
			}
		})
	}
}

// 内置分镜正文写 Seedance 分栏原稿（平台适配在片场转写）；格式段只管切段和 JSON。旧的平台变体不再存在。
func TestStoryboardPromptIsSeedance(t *testing.T) {
	creative, err := drama.Prompt("storyboard", map[string]any{"max_seconds": 12})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"即梦 / Seedance", "【画幅风格】", "【时间轴】"} {
		if !strings.Contains(creative, want) {
			t.Fatalf("内置分镜正文缺少 %s", want)
		}
	}
	prompt, err := drama.Prompt("storyboard_format", map[string]any{"max_seconds": 12, "draft_md": "村口。"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"required":["segments"]`, "max_seconds", "@[名称](node_id)", "放不下就拆"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("分镜格式段缺少 %s", want)
		}
	}
	for _, gone := range []string{"storyboard_seedance", "storyboard_h3"} {
		if _, err := drama.Prompt(gone, map[string]any{}); err == nil {
			t.Fatalf("%s 应已删除", gone)
		}
	}
}
