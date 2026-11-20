package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// snapshotReferenceBases 在入队事务中冻结原创改编依据，后续修改角色或参考稿不会改变任务。
func snapshotReferenceBases(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, j *model.DramaJob, cfg *dramaJobConfig) error {
	if j.Step != "scripts" && j.Step != "breakdown" && j.Step != "storyboard" {
		return nil
	}
	eps, err := tx.Episodes(ctx, p.UserID, p.ID)
	if err != nil {
		return err
	}
	cfg.ReferenceBases = map[uint64]model.DramaReferenceBasis{}
	for _, ep := range eps {
		if !slices.Contains(j.Episodes, ep.No) || len(ep.ReferenceBasis) == 0 {
			continue
		}
		var basis model.DramaReferenceBasis
		if err := json.Unmarshal(ep.ReferenceBasis, &basis); err != nil {
			return err
		}
		if basis.Mode == "rewrite" {
			cfg.ReferenceBases[ep.ID] = basis
		}
	}
	return nil
}

// referenceScriptVariables 将固定原片和新角色作为现有写稿步骤的输入，并计入完整上下文预算。
func referenceScriptVariables(j *model.DramaJob, ep model.DramaEpisode, snap *provider.Snapshot, vars map[string]any) error {
	basis, ok := jobConfig(j).ReferenceBases[ep.ID]
	if !ok {
		return nil
	}
	raw, err := json.Marshal(basis.Document)
	if err != nil {
		return err
	}
	var cast strings.Builder
	known := []drama.CastEntry{}
	for _, role := range basis.Roles {
		fmt.Fprintf(&cast, "原人物 %s 使用新角色 %s（节点 %s）。\n", role.CharacterID, role.TargetName, role.TargetNodeID)
		if !slices.ContainsFunc(known, func(entry drama.CastEntry) bool { return entry.Name == role.TargetName }) {
			known = append(known, drama.CastEntry{Type: "character", Name: role.TargetName, Aliases: []string{}})
		}
	}
	vars["source"] = "以下为已确认参考修订的原片资料，仅作为剧情因果和人物关系依据：\n" + string(raw)
	vars["characters_md"] = cast.String()
	vars["reference_adaptation"] = "创作原创改编剧本，使用上述新角色正式名字；允许重构场景、镜序和对白，保持核心因果。改编要求：" + basis.Instruction
	vars["known_cast"] = known
	encoded, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	if utf8.RuneCount(encoded) > dramaContextLimit(snap) {
		return errcode.ErrDramaEpisodeSourceTooLong
	}
	return nil
}
