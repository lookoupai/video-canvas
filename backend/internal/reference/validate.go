package reference

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
)

// ValidateDocument 校验镜头、场次、角色和独立对白的引用及原片毫秒边界。
func ValidateDocument(doc model.DramaReferenceDocument, durationMs int64) error {
	if len(doc.Shots) == 0 || len(doc.Shots) > 10000 || len(doc.Dialogues) > 20000 || len(doc.Characters) > 1000 || len(doc.Scenes) > 1000 {
		return ErrPackage
	}
	scenes := map[string]bool{}
	for _, s := range doc.Scenes {
		if !addID(scenes, s.ID) || !validText(s.Title, 255) {
			return fmt.Errorf("%w: 场次标识或标题无效", ErrPackage)
		}
	}
	characters := map[string]bool{}
	for _, c := range doc.Characters {
		if !addID(characters, c.ID) || strings.TrimSpace(c.Name) == "" || !validText(c.Name, 255) {
			return fmt.Errorf("%w: 角色标识或名字无效", ErrPackage)
		}
	}
	if err := validateShots(doc.Shots, scenes, characters, durationMs); err != nil {
		return err
	}
	return validateDialogues(doc.Dialogues, characters, durationMs)
}

// validateShots 检查镜序和场次、人物引用，原片的同一条时间线不允许镜头重叠。
func validateShots(input []model.DramaReferenceShot, scenes, characters map[string]bool, durationMs int64) error {
	shots := map[string]bool{}
	var prevEnd int64
	for _, s := range input {
		if !addID(shots, s.ID) || s.StartMs < prevEnd || !validRange(s.StartMs, s.EndMs, durationMs) || !scenes[s.SceneID] || !validText(s.Action, 4000) || !validText(s.Camera, 1000) {
			return fmt.Errorf("%w: 镜头 %s 的时间、场次或内容无效", ErrPackage, s.ID)
		}
		for _, id := range s.Characters {
			if !characters[id] {
				return fmt.Errorf("%w: 镜头引用未知角色", ErrPackage)
			}
		}
		prevEnd = s.EndMs
	}
	return nil
}

// validateDialogues 只检查对白自己的时间边界，允许它与多镜或另一句对白重叠。
func validateDialogues(input []model.DramaReferenceDialogue, characters map[string]bool, durationMs int64) error {
	cues := map[string]bool{}
	for _, d := range input {
		if !addID(cues, d.ID) || !validRange(d.StartMs, d.EndMs, durationMs) || !validText(d.Text, 4000) || (d.Speaker != "" && !characters[d.Speaker]) {
			return fmt.Errorf("%w: 对白时间或说话人无效", ErrPackage)
		}
	}
	return nil
}

func validRange(start, end, total int64) bool { return start >= 0 && end > start && end <= total }
func validText(text string, limit int) bool {
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= limit
}
func addID(seen map[string]bool, id string) bool {
	if strings.TrimSpace(id) == "" || len(id) > 128 || seen[id] {
		return false
	}
	seen[id] = true
	return true
}
