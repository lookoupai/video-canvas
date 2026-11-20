package reference

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"

	"video-canvas/internal/model"
)

var timelineLine = regexp.MustCompile(`^#([0-9]+)\s+([0-9]+(?:\.[0-9]+)?)-([0-9]+(?:\.[0-9]+)?)\s*(?:\||$)`)

type videoHead struct {
	Version int       `yaml:"videomd"`
	Scene   int       `yaml:"scene"`
	Shot    int       `yaml:"shot"`
	Time    []float64 `yaml:"time"`
	Source  struct {
		Duration float64 `yaml:"duration"`
	} `yaml:"source"`
}

// frontmatter 只解码数据头，正文仍按声明的时间线语法读取。
func frontmatter(raw []byte) (videoHead, string, error) {
	text := strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff")
	var head videoHead
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return head, "", ErrPackage
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "---" {
			continue
		}
		err := yaml.Unmarshal([]byte(strings.Join(lines[1:i], "\n")), &head)
		return head, strings.Join(lines[i+1:], "\n"), err
	}
	return head, "", ErrPackage
}

// packageRoot 要求恰好一个入口，支持用户将整个包目录压缩后的一级前缀。
func packageRoot(files map[string][]byte) (string, error) {
	root := ""
	count := 0
	for name := range files {
		if path.Base(name) == "VIDEO.md" {
			root = strings.TrimSuffix(name, "VIDEO.md")
			count++
		}
	}
	if count != 1 {
		return "", ErrPackage
	}
	return root, nil
}

// decodeVideoMD 以逐镜文件的精确毫秒覆盖时间线展示秒数，再独立导入字幕。
func decodeVideoMD(files map[string][]byte, durationMs int64) (model.DramaReferenceDocument, error) {
	doc := emptyDocument()
	root, err := packageRoot(files)
	if err != nil {
		return doc, err
	}
	card, _, err := frontmatter(files[root+"VIDEO.md"])
	if err != nil || card.Version != 1 {
		return doc, ErrPackage
	}
	if card.Source.Duration > 0 && math.Abs(card.Source.Duration*1000-float64(durationMs)) > 100 {
		return doc, fmt.Errorf("%w: 原片时长与拆片包不符", ErrPackage)
	}
	scenes := map[string]bool{}
	for _, name := range sortedNames(files) {
		rel := strings.TrimPrefix(name, root)
		if !strings.HasPrefix(name, root) || (rel != "video/timeline.md" && !strings.HasPrefix(rel, "video/timeline/")) {
			continue
		}
		head, body, err := frontmatter(files[name])
		if err != nil {
			return doc, err
		}
		sceneID := "scene-" + strconv.Itoa(max(head.Scene, 1))
		shots, err := timelineShots(body, sceneID)
		if err != nil {
			return doc, err
		}
		if len(shots) == 0 {
			continue
		}
		if !scenes[sceneID] {
			doc.Scenes = append(doc.Scenes, model.DramaReferenceScene{ID: sceneID, Title: fmt.Sprintf("场次 %d", max(head.Scene, 1))})
			scenes[sceneID] = true
		}
		doc.Shots = append(doc.Shots, shots...)
	}
	if err := exactTimes(files, root, &doc); err != nil {
		return doc, err
	}
	sort.SliceStable(doc.Shots, func(i, j int) bool { return doc.Shots[i].StartMs < doc.Shots[j].StartMs })
	if err := subtitleTrack(files, root, &doc); err != nil {
		return doc, err
	}
	return doc, nil
}

// timelineShots 只解析标记为 videomd-timeline 的代码块，普通正文不会成为执行指令。
func timelineShots(body, scene string) ([]model.DramaReferenceShot, error) {
	out := []model.DramaReferenceShot{}
	in := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "```videomd-timeline" {
			in = true
			continue
		}
		if strings.HasPrefix(line, "```") {
			in = false
			continue
		}
		if !in || line == "" {
			continue
		}
		match := timelineLine.FindStringSubmatch(line)
		if match == nil {
			return nil, ErrPackage
		}
		n, _ := strconv.Atoi(match[1])
		a, _ := strconv.ParseFloat(match[2], 64)
		b, _ := strconv.ParseFloat(match[3], 64)
		shot := model.DramaReferenceShot{ID: strconv.Itoa(n), SceneID: scene, StartMs: ms(a), EndMs: ms(b), Characters: []string{}}
		for _, field := range strings.Split(line, "|")[1:] {
			key, value, _ := strings.Cut(strings.TrimSpace(field), " ")
			switch key {
			case "seen":
				shot.Action = value
			case "camera":
				shot.Camera = value
			}
		}
		out = append(out, shot)
	}
	return out, nil
}

// exactTimes 逐镜文件保存的时间精度高于摘要，引用错误或重复时拒绝导入。
func exactTimes(files map[string][]byte, root string, doc *model.DramaReferenceDocument) error {
	byID := map[string]int{}
	for i, s := range doc.Shots {
		if _, ok := byID[s.ID]; ok {
			return ErrPackage
		}
		byID[s.ID] = i
	}
	seen := map[int]bool{}
	for _, name := range sortedNames(files) {
		if !strings.HasPrefix(name, root+"video/shots/") || path.Ext(name) != ".md" {
			continue
		}
		head, _, err := frontmatter(files[name])
		if err != nil {
			return err
		}
		i, ok := byID[strconv.Itoa(head.Shot)]
		if !ok || seen[head.Shot] || len(head.Time) != 2 {
			return ErrPackage
		}
		seen[head.Shot] = true
		doc.Shots[i].StartMs, doc.Shots[i].EndMs = ms(head.Time[0]), ms(head.Time[1])
	}
	return nil
}
