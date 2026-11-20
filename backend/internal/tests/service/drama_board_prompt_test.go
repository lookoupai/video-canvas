package service_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
)

// 故事板提示词是第六个环节：内置模板随项目视图返回（前端据此拼宫格图提示词），其他环节的正文照旧不外露。
func TestDramaBoardPromptSettingsView(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaModelRegistry(registry, nil)
	if err := createWithModels(r, s, "", model.DramaImageModels{}); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(context.Background(), 7, r.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	board := view.PromptSettings["board"]
	if board.Source != "builtin" || board.Bodies["board"] != drama.PromptBody("board") || board.Bodies["board"] == "" {
		t.Fatalf("内置故事板提示词应带内置模板：%+v", board)
	}
	if view.PromptSettings["plan"].Bodies != nil {
		t.Fatalf("其他环节不返回正文：%+v", view.PromptSettings["plan"])
	}

	// 选「我的」提示词：返回保存时的快照正文；之后改库里的那条，已选的不变
	custom := "{{格数}} 格水彩分镜\n{{画面}}"
	mine, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "board", Name: "水彩", Bodies: map[string]string{"board": custom}})
	if err != nil {
		t.Fatal(err)
	}
	if view, err = s.Update(context.Background(), 7, r.project.ID, &model.UpdateDramaReq{Revision: r.project.Revision, Prompts: map[string]model.DramaPromptChoice{"board": {Source: "mine", ID: mine.ID}}}); err != nil {
		t.Fatal(err)
	}
	if got := view.PromptSettings["board"]; got.Source != "mine" || got.Bodies["board"] != custom || got.Label != "我的 · 水彩" {
		t.Fatalf("选了我的提示词：%+v", got)
	}
	if _, err = s.SavePrompt(context.Background(), 7, uint64(mine.ID), &model.SaveDramaPromptReq{Step: "board", Name: "水彩", Revision: mine.Revision, Bodies: map[string]string{"board": "改了 {{画面}}"}}); err != nil {
		t.Fatal(err)
	}
	if view, err = s.Get(context.Background(), 7, r.project.ID); err != nil || view.PromptSettings["board"].Bodies["board"] != custom {
		t.Fatalf("快照不随库里的修改变：%+v err=%v", view.PromptSettings["board"], err)
	}

	// 改回内置：返回当前的内置模板
	if view, err = s.Update(context.Background(), 7, r.project.ID, &model.UpdateDramaReq{Revision: r.project.Revision, Prompts: map[string]model.DramaPromptChoice{"board": {Source: "builtin"}}}); err != nil {
		t.Fatal(err)
	}
	if got := view.PromptSettings["board"]; got.Source != "builtin" || got.Bodies["board"] != drama.PromptBody("board") {
		t.Fatalf("改回内置：%+v", got)
	}
}

// 老项目没存故事板的设置：按内置处理，也带内置模板。
func TestDramaBoardPromptOldProjectFallsBackToBuiltin(t *testing.T) {
	r := newDramaMemory()
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	r.project.PromptSettings = []byte(`{}`)
	view, err := s.Get(context.Background(), 7, r.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.PromptSettings["board"]; got.Source != "builtin" || got.Bodies["board"] != drama.PromptBody("board") {
		t.Fatalf("%+v", got)
	}
}

// 故事板模板必须留着 {{画面}}，否则镜头内容就丢了；其他环节不受这条限制。
func TestDramaBoardPromptRequiresShotsPlaceholder(t *testing.T) {
	r := newDramaMemory()
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	for _, body := range []string{"画一张宫格图", "{{ 画面 }}不是占位符"} {
		_, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "board", Name: "缺占位符", Bodies: map[string]string{"board": body}})
		dramaCheckCode(t, err, 67017)
	}
	if _, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "board", Name: "合法", Bodies: map[string]string{"board": "前言\n{{画面}}\n后记"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "plan", Name: "随便", Bodies: map[string]string{"plan": "没有占位符也行"}}); err != nil {
		t.Fatal(err)
	}
}

// 故事板模板里只认五个占位符：写了不认识的、或花括号没配成对，都不能保存。
func TestDramaBoardPromptRejectsUnknownPlaceholders(t *testing.T) {
	r := newDramaMemory()
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	for _, body := range []string{
		"{{画面}} {{颜色}}",
		"{{画面}} {{格数 }}",
		"{{画面}} {{}}",
		"{{画面}} {{",
		"{{画面}} }}",
		"{{画面}} {{格数}",
	} {
		_, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "board", Name: "坏模板", Bodies: map[string]string{"board": body}})
		dramaCheckCode(t, err, 67017)
	}
	all := "{{格数}}{{列数}}{{行数}}{{比例}}\n{{画面}} 单个 { 花括号 } 不算占位符"
	if _, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "board", Name: "全用上", Bodies: map[string]string{"board": all}}); err != nil {
		t.Fatal(err)
	}
	// 其他环节的正文里想写什么都行
	if _, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "plan", Name: "随便写", Bodies: map[string]string{"plan": "{{颜色}} 没关系"}}); err != nil {
		t.Fatal(err)
	}
}

// 内置模板自己要合格：带全部占位符，且没有多余的段落标题。
func TestDramaBoardBuiltinTemplate(t *testing.T) {
	body := drama.PromptBody("board")
	for _, want := range []string{"{{格数}}", "{{列数}}", "{{行数}}", "{{比例}}", "{{画面}}"} {
		if !strings.Contains(body, want) {
			t.Fatalf("内置故事板模板缺 %s：%s", want, body)
		}
	}
	// 手绘线稿风格，每格标注镜头类型 / 表演提示 / 音效说明，镜头要串成一段顺畅的动作
	for _, want := range []string{"手绘风格分镜稿", "铅笔单线条线稿", "镜头类型", "表演提示", "音效说明", "完整串联"} {
		if !strings.Contains(body, want) {
			t.Fatalf("内置故事板模板缺 %q：%s", want, body)
		}
	}
	// 人物要和参考图一致
	for _, want := range []string{"保持人物与参考图完全一致", "面部特征100%相同", "禁止美化", "禁止改变年龄"} {
		if !strings.Contains(body, want) {
			t.Fatalf("内置故事板模板缺人物一致性要求 %q", want)
		}
	}
	// 这是出图不是出视频：视频用语（字幕、贴图、旁白、背景音乐）不适用，不写
	for _, banned := range []string{"字幕", "贴图", "旁白", "背景音乐"} {
		if strings.Contains(body, banned) {
			t.Fatalf("内置故事板模板不该带视频用语 %q：%s", banned, body)
		}
	}
	// 内置模板原样保存成「我的」也得合格，用户点「新建」会预填它
	r := newDramaMemory()
	svc, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	if _, err := svc.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "board", Name: "照抄内置", Bodies: map[string]string{"board": body}}); err != nil {
		t.Fatalf("内置模板自己过不了校验：%v", err)
	}
}

// 故事板没有「技能」来源：选技能直接拒绝，不去查技能目录。
func TestDramaBoardPromptRejectsSkillSource(t *testing.T) {
	r := newDramaMemory()
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	_, err := s.Update(context.Background(), 7, r.project.ID, &model.UpdateDramaReq{Revision: r.project.Revision, Prompts: map[string]model.DramaPromptChoice{"board": {Source: "skill", Skill: "cross"}}})
	dramaCheckCode(t, err, 10001)
}
