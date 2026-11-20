package reference

import (
	"html"
	"path"
	"regexp"
	"strconv"
	"strings"

	"video-canvas/internal/model"
)

var voiceTag = regexp.MustCompile(`^<v(?:\.[^ >]+)*\s+([^>]+)>([\s\S]*)`)
var subtitleTags = regexp.MustCompile(`<[^>]*>`)

// subtitleTrack 先选有说话人标签的 VTT，其次 SRT，避免同一转写导入两次。
func subtitleTrack(files map[string][]byte, root string, doc *model.DramaReferenceDocument) error {
	names := sortedNames(files)
	for _, ext := range []string{".vtt", ".srt"} {
		for _, name := range names {
			if !strings.HasPrefix(name, root) || strings.ToLower(path.Ext(name)) != ext {
				continue
			}
			if err := parseSubtitles(string(files[name]), doc); err != nil {
				return err
			}
			if len(doc.Dialogues) > 0 {
				return nil
			}
		}
	}
	return nil
}

// parseSubtitles 保留整句的时间，允许它跨越切点。没有说话人标签时留待人工确认。
func parseSubtitles(text string, doc *model.DramaReferenceDocument) error {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.Contains(lines[i], "-->") {
			continue
		}
		a, b, _ := strings.Cut(lines[i], "-->")
		start, err := subtitleTime(strings.TrimSpace(a))
		if err != nil {
			return err
		}
		endText := strings.Fields(b)
		if len(endText) == 0 {
			return ErrPackage
		}
		end, err := subtitleTime(endText[0])
		if err != nil {
			return err
		}
		var body []string
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
			body = append(body, lines[i])
		}
		value := strings.TrimSpace(strings.Join(body, "\n"))
		speaker := ""
		if match := voiceTag.FindStringSubmatch(value); match != nil {
			speaker = match[1]
			value = match[2]
		}
		value = html.UnescapeString(subtitleTags.ReplaceAllString(value, ""))
		if strings.TrimSpace(value) != "" {
			appendDialogue(doc, start, end, value, speaker)
		}
	}
	return nil
}

func subtitleTime(value string) (int64, error) {
	parts := strings.Split(strings.ReplaceAll(value, ",", "."), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, ErrPackage
	}
	var sec float64
	for i, p := range parts {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil || n < 0 || (i > 0 && n >= 60) {
			return 0, ErrPackage
		}
		sec = sec*60 + n
	}
	return ms(sec), nil
}
