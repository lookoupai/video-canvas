package skills_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/skills"
)

func TestReferenceShotAnalysisBuiltIn(t *testing.T) {
	all := skills.All()
	if len(all) != 1 || all[0].Name != "reference-shot-analysis" {
		t.Fatalf("内置逐镜分析技能缺失或意外增加：%+v", all)
	}
	if got := skills.Search("参考剧集 关键帧"); len(got) != 1 || got[0].Name != "reference-shot-analysis" {
		t.Errorf("应能按用途搜索内置技能：%+v", got)
	}
	if skill, ok := skills.Read("reference-shot-analysis"); !ok || !strings.Contains(skill.Body, "单帧只能说明画面状态") {
		t.Errorf("应能读取逐镜分析方法：%+v", skill)
	}
	for _, name := range []string{"../etc/passwd", ""} {
		if _, ok := skills.Read(name); ok {
			t.Errorf("读 %q 应返回 false，不做任何路径解析", name)
		}
	}
	if skills.Tags("script-breakdown") != nil {
		t.Error("不存在的技能没有标签")
	}
}

func TestParse(t *testing.T) {
	doc := "---\nname: demo\ntitle: 演示\ndescription: 一句话说明\ntags: [甲, 乙 ,]\n---\n\n正文第一行\n第二行\n"
	s, err := skills.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "demo" || s.Title != "演示" || s.Description != "一句话说明" {
		t.Errorf("头部解析错误: %+v", s)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "甲" || s.Tags[1] != "乙" {
		t.Errorf("标签应去掉括号、空白和空项: %v", s.Tags)
	}
	if s.Body != "正文第一行\n第二行" || strings.HasPrefix(s.Body, "---") {
		t.Errorf("正文应去掉头部和首尾空白: %q", s.Body)
	}

	t.Run("没写 title 取 name", func(t *testing.T) {
		s, err := skills.Parse("---\nname: demo\ndescription: d\n---\n正文")
		if err != nil || s.Title != "demo" {
			t.Errorf("%+v %v", s, err)
		}
	})
	t.Run("格式不对报错", func(t *testing.T) {
		for name, raw := range map[string]string{
			"没有头部":     "正文",
			"头部没有结束标记": "---\nname: a\ndescription: d\n正文",
			"缺 name":   "---\ndescription: d\n---\n正文",
			"缺说明":      "---\nname: a\n---\n正文",
		} {
			if _, err := skills.Parse(raw); err == nil {
				t.Errorf("%s 应报错", name)
			}
		}
	})
}
