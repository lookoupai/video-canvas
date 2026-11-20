package service

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"video-canvas/internal/model"
)

// referenceNames 为原人物建立确认过的新名字，长名称优先防止短名破坏复合姓名。
func referenceNames(basis model.DramaReferenceBasis) (*strings.Replacer, map[string]model.DramaRemakeRole) {
	roles := map[string]model.DramaRemakeRole{}
	for _, role := range basis.Roles {
		roles[role.CharacterID] = role
	}
	characters := slices.Clone(basis.Document.Characters)
	slices.SortFunc(characters, func(a, b model.DramaReferenceCharacter) int { return cmp.Compare(len(b.Name), len(a.Name)) })
	pairs := []string{}
	for _, character := range characters {
		if role, ok := roles[character.ID]; ok && character.Name != "" {
			pairs = append(pairs, character.Name, role.TargetName)
		}
	}
	return strings.NewReplacer(pairs...), roles
}

// referenceShotPrompt 保持原镜时间和动作，引用实际画布节点；跨镜对白明确只演当前交集。
func referenceShotPrompt(basis model.DramaReferenceBasis, shot model.DramaReferenceShot) string {
	replacer, roles := referenceNames(basis)
	var out strings.Builder
	fmt.Fprintf(&out, "按原镜重拍，目标时长 %.3f 秒。原片区间 %.3f–%.3f 秒。\n", float64(shot.EndMs-shot.StartMs)/1000, float64(shot.StartMs)/1000, float64(shot.EndMs)/1000)
	used := slices.Clone(shot.Characters)
	for _, dialogue := range basis.Document.Dialogues {
		if dialogue.StartMs < shot.EndMs && dialogue.EndMs > shot.StartMs && !slices.Contains(used, dialogue.Speaker) {
			used = append(used, dialogue.Speaker)
		}
	}
	for _, id := range used {
		role := roles[id]
		fmt.Fprintf(&out, "角色：@[%s](%s)。\n", role.TargetName, role.TargetNodeID)
	}
	fmt.Fprintf(&out, "画面与动作：%s\n运镜：%s\n", replacer.Replace(shot.Action), shot.Camera)
	for _, dialogue := range basis.Document.Dialogues {
		start, end := max(shot.StartMs, dialogue.StartMs), min(shot.EndMs, dialogue.EndMs)
		if start >= end {
			continue
		}
		fmt.Fprintf(&out, "本镜 %.3f–%.3f 秒，%s：%s。", float64(start-shot.StartMs)/1000, float64(end-shot.StartMs)/1000, roles[dialogue.Speaker].TargetName, replacer.Replace(dialogue.Text))
		if start != dialogue.StartMs || end != dialogue.EndMs {
			fmt.Fprint(&out, "此句跨镜，以上为完整台词；仅延续当前时间交集，勿在切镜处重新起句。")
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// referenceFollowScript 生成可审核的跟拍剧本，独立对白只写一次，不在原镜切点重复整句。
func referenceFollowScript(no int, basis model.DramaReferenceBasis) string {
	replacer, roles := referenceNames(basis)
	var out strings.Builder
	fmt.Fprintf(&out, "第%d集\n", no)
	for _, scene := range basis.Document.Scenes {
		fmt.Fprintf(&out, "\n场次：%s\n", scene.Title)
		for _, shot := range basis.Document.Shots {
			if shot.SceneID == scene.ID {
				fmt.Fprintf(&out, "△ [%.3f–%.3f] %s\n", float64(shot.StartMs)/1000, float64(shot.EndMs)/1000, replacer.Replace(shot.Action))
			}
		}
	}
	for _, dialogue := range basis.Document.Dialogues {
		fmt.Fprintf(&out, "[%.3f–%.3f] %s：%s\n", float64(dialogue.StartMs)/1000, float64(dialogue.EndMs)/1000, roles[dialogue.Speaker].TargetName, replacer.Replace(dialogue.Text))
	}
	out.WriteString("【本集完】")
	return out.String()
}
