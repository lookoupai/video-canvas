// Package reference 把外部拆片数据转成原片本地毫秒时间，不执行包内指令或读取外部路径。
package reference

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"video-canvas/internal/model"
)

// ErrPackage 表示拆片包的格式、时间或引用无法可靠导入。
var ErrPackage = errors.New("拆片包格式或时间区间无效")

// MaxPackageBytes 限制上传包大小，解压后的文本另有限制。
const MaxPackageBytes = 8 << 20

// DecodePackage 读取标准分镜 JSON、capsule-cinema 参考表或 VIDEO.md ZIP，时间以实际视频为准。
func DecodePackage(data []byte, durationMs int64) (model.DramaReferenceDocument, error) {
	var doc model.DramaReferenceDocument
	if len(data) == 0 || len(data) > MaxPackageBytes || durationMs <= 0 {
		return doc, ErrPackage
	}
	var err error
	if bytes.HasPrefix(data, []byte("PK")) {
		var files map[string][]byte
		files, err = unzipText(data)
		if err == nil {
			doc, err = decodeVideoMD(files, durationMs)
		}
	} else {
		doc, err = decodeJSON(data, durationMs)
	}
	if err != nil {
		return doc, fmt.Errorf("%w: %w", ErrPackage, err)
	}
	if err = ValidateDocument(doc, durationMs); err != nil {
		return doc, err
	}
	return doc, nil
}

// unzipText 只在内存读有限的文本，拒绝重复、绝对路径、逃逸和符号链接。
func unzipText(data []byte) (map[string][]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(z.File) > 4096 {
		return nil, ErrPackage
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range z.File {
		name := f.Name
		if !safeEntry(f) {
			return nil, ErrPackage
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if _, ok := files[name]; ok {
			return nil, ErrPackage
		}
		ext := strings.ToLower(path.Ext(name))
		if !metadataExt(ext) {
			continue
		}
		if f.UncompressedSize64 > MaxPackageBytes {
			return nil, ErrPackage
		}
		text, err := readEntry(f)
		if err != nil {
			return nil, err
		}
		total += int64(len(text))
		if total > 32<<20 || len(text) > MaxPackageBytes {
			return nil, ErrPackage
		}
		files[name] = text
	}
	return files, nil
}

func safeEntry(f *zip.File) bool {
	name := f.Name
	return !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") &&
		path.Clean(name) == strings.TrimSuffix(name, "/") && f.Mode()&fs.ModeSymlink == 0 &&
		name != ".." && !strings.HasPrefix(name, "../")
}
func metadataExt(ext string) bool { return ext == ".md" || ext == ".srt" || ext == ".vtt" }
func readEntry(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(io.LimitReader(r, MaxPackageBytes+1))
}

type sheetShot struct {
	Shot                int `json:"shot"`
	Start, End          float64
	Seen, Story, Camera string
	CameraText          string `json:"camera_text"`
}
type sheetCaption struct {
	Start, End    float64
	Text, Speaker string
}

// decodeJSON 接受标准数据及已选读取器的 sheet 输出；不会使用 source_video 等本机路径。
func decodeJSON(data []byte, durationMs int64) (model.DramaReferenceDocument, error) {
	var raw struct {
		Shots    []json.RawMessage
		Duration float64
		Captions []sheetCaption
	}
	var doc model.DramaReferenceDocument
	if err := json.Unmarshal(data, &raw); err != nil {
		return doc, err
	}
	if len(raw.Shots) == 0 {
		return doc, ErrPackage
	}
	if raw.Duration > 0 && math.Abs(raw.Duration*1000-float64(durationMs)) > 100 {
		return doc, ErrPackage
	}
	var first map[string]json.RawMessage
	if err := json.Unmarshal(raw.Shots[0], &first); err != nil {
		return doc, err
	}
	if _, normal := first["start_ms"]; normal {
		err := json.Unmarshal(data, &doc)
		return doc, err
	}
	doc = emptyDocument()
	doc.Scenes = append(doc.Scenes, model.DramaReferenceScene{ID: "scene-1", Title: "场次 1"})
	for _, r := range raw.Shots {
		var shot sheetShot
		if err := json.Unmarshal(r, &shot); err != nil {
			return doc, err
		}
		camera := shot.CameraText
		if camera == "" {
			camera = shot.Camera
		}
		doc.Shots = append(doc.Shots, model.DramaReferenceShot{ID: strconv.Itoa(shot.Shot), SceneID: "scene-1", StartMs: ms(shot.Start), EndMs: ms(shot.End), Action: shot.Seen, Camera: camera, Characters: []string{}})
	}
	for _, cue := range raw.Captions {
		appendDialogue(&doc, ms(cue.Start), ms(cue.End), cue.Text, cue.Speaker)
	}
	return doc, nil
}

// emptyDocument 保持列表为空数组，便于首次校订和前端直接遍历。
func emptyDocument() model.DramaReferenceDocument {
	return model.DramaReferenceDocument{Scenes: []model.DramaReferenceScene{}, Characters: []model.DramaReferenceCharacter{}, Shots: []model.DramaReferenceShot{}, Dialogues: []model.DramaReferenceDialogue{}}
}
func ms(seconds float64) int64 { return int64(math.Round(seconds * 1000)) }

// appendDialogue 把有名字的说话人保留为本集身份，未知说话人留空供人工确认。
func appendDialogue(doc *model.DramaReferenceDocument, start, end int64, text, speaker string) {
	speaker = strings.TrimSpace(speaker)
	id := ""
	for _, c := range doc.Characters {
		if c.Name == speaker {
			id = c.ID
			break
		}
	}
	if speaker != "" && id == "" {
		id = "speaker-" + strconv.Itoa(len(doc.Characters)+1)
		doc.Characters = append(doc.Characters, model.DramaReferenceCharacter{ID: id, Name: speaker})
	}
	doc.Dialogues = append(doc.Dialogues, model.DramaReferenceDialogue{ID: "dialogue-" + strconv.Itoa(len(doc.Dialogues)+1), StartMs: start, EndMs: end, Text: strings.TrimSpace(text), Speaker: id})
}

// sortedNames 让分卷时间线与字幕的读取顺序可复现。
func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
