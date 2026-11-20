package marketing

import (
	"math"

	"video-canvas/internal/model"
)

// RemakeShots 按目标镜分组，保留 follow_map 里的来源顺序与独立区间。
// 旧稿缺少整张映射表时，仅回看镜头本身明确记录的 ref_shot。
func RemakeShots(shots []ScriptShot, mapping []FollowMapItem, basis Reverse) []model.MarketingRemakeShot {
	out := make([]model.MarketingRemakeShot, len(shots))
	legacy := len(mapping) == 0
	for i, shot := range shots {
		out[i] = model.MarketingRemakeShot{Scene: shot.Scene, DurationMs: int(math.Round((shot.End - shot.Start) * 1000)), Sources: []model.MarketingRemakeSource{}}
		if legacy && shot.RefShot != nil {
			mapping = append(mapping, FollowMapItem{Shot: i, RefShot: *shot.RefShot})
		}
	}
	refs := make(map[int]model.MarketingRemakeSource, len(basis.Shots))
	for _, shot := range basis.Shots {
		ref := model.MarketingRemakeSource{RefShot: shot.ID, StartMs: int(math.Round(shot.Start * 1000)), EndMs: int(math.Round(shot.End * 1000))}
		if _, duplicate := refs[shot.ID]; duplicate {
			ref = model.MarketingRemakeSource{}
		}
		refs[shot.ID] = ref
	}
	for _, m := range mapping {
		if m.Shot < 0 || m.Shot >= len(out) {
			continue
		}
		ref := refs[m.RefShot]
		if ref.RefShot <= 0 || ref.StartMs < 0 || ref.StartMs >= ref.EndMs {
			out[m.Shot].Unresolved = true
			continue
		}
		out[m.Shot].Sources = append(out[m.Shot].Sources, ref)
	}
	return out
}
