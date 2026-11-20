package drama

import (
	"encoding/json"
	"regexp"
	"strings"
)

// boardShotRE 是【时间轴】里一个镜头行：「0-3秒：」「6-9.5秒：」开头，要和前端 shotLines 保持一致。
var boardShotRE = regexp.MustCompile(`^\s*\d+(?:\.\d+)?\s*[-–~]\s*\d+(?:\.\d+)?\s*秒\s*[:：]`)

// BoardShotCount 数片段原稿【时间轴】里的镜头数；没有时间轴为 0。
func BoardShotCount(prompt string) int {
	in := false
	n := 0
	for _, line := range strings.Split(prompt, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "【时间轴】") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(trimmed, "【") {
			break
		}
		if boardShotRE.MatchString(line) {
			n++
		}
	}
	return n
}

// BoardLayout 按镜头数选宫格（整图 16:9 横版）：1–4 镜 2x2；5–6 镜 3x2；7 镜以上 3x3；识别不到镜头也是 2x2。要和前端 boardLayout 一致。
func BoardLayout(shots int) string {
	switch {
	case shots <= 4:
		return "2x2"
	case shots <= 6:
		return "3x2"
	}
	return "3x3"
}

// BoardHashes 从短剧画布里取出已经出图的故事板节点：片段 ID → 出图时原稿的 SHA-256。
// 故事板是画布上的图片节点，data.drama.board 记着它属于哪个片段（前端写入，字段是 camelCase）；
// 没有图、或还是本地 blob（上传中）的不算。空画布返回空表。
func BoardHashes(payload []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(payload) == 0 {
		return out, nil
	}
	var canvas struct {
		Nodes []struct {
			Data struct {
				Src   string `json:"src"`
				Drama *struct {
					Board *struct {
						SegmentID  string `json:"segmentId"`
						SourceHash string `json:"sourceHash"`
					} `json:"board"`
				} `json:"drama"`
			} `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(payload, &canvas); err != nil {
		return nil, err
	}
	for _, node := range canvas.Nodes {
		d := node.Data
		if d.Drama == nil || d.Drama.Board == nil || d.Drama.Board.SegmentID == "" {
			continue
		}
		if d.Src == "" || strings.HasPrefix(d.Src, "blob:") {
			continue
		}
		out[d.Drama.Board.SegmentID] = d.Drama.Board.SourceHash
	}
	return out, nil
}
