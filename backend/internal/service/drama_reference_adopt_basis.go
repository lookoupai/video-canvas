package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/reference"
	"video-canvas/internal/repository"
)

// adoptionBasis 校验参考修订和素材归属，保留用户确认的原片文档及新角色身份。
func (s *DramaReferenceService) adoptionBasis(ctx context.Context, tx repository.DramaReferenceTx, p *model.DramaProject, id uint64, req *model.AdoptDramaReferenceReq) (*model.DramaReferenceEpisode, model.DramaReferenceBasis, error) {
	basis := model.DramaReferenceBasis{ReferenceID: idcodec.Encode(id), ReferenceRevision: req.Revision, Mode: req.Mode, Instruction: req.Instruction}
	row, err := tx.Episode(ctx, p.UserID, p.ID, id)
	if err != nil {
		return nil, basis, err
	}
	if row.Revision != req.Revision {
		return nil, basis, errcode.ErrCanvasConflict
	}
	if row.Status == "queued" {
		return nil, basis, errcode.ErrDramaJobBusy
	}
	basis.Document, err = referenceDocumentOf(row)
	if err != nil {
		return nil, basis, err
	}
	if err := reference.ValidateDocument(basis.Document, row.DurationMs); err != nil {
		return nil, basis, errcode.ErrInvalidParams.WithMsg("请先保存完整的参考拆解")
	}
	if err := checkAdoptionShots(basis.Document, req.ShotIDs); err != nil {
		return nil, basis, err
	}
	if req.Mode == "follow" {
		if err := checkFollowDurations(basis.Document, req.ShotIDs); err != nil {
			return nil, basis, err
		}
	}
	asset, err := s.assets.View(ctx, p.UserID, row.AssetID)
	if err != nil {
		return nil, basis, err
	}
	if asset.Kind != "video" || asset.DurationMs != row.DurationMs {
		return nil, basis, errcode.ErrInvalidParams.WithMsg("原视频已失效，请重新关联")
	}
	basis.AssetID = row.AssetID
	basis.Roles, err = adoptionRoles(ctx, tx, p, row, basis.Document)
	return row, basis, err
}

// checkFollowDurations 跟拍目标时长必须能输出至少一帧，避免建立无法生成或合成的片段。
func checkFollowDurations(doc model.DramaReferenceDocument, selected []string) error {
	for _, shot := range doc.Shots {
		if len(selected) != 0 && !slices.Contains(selected, shot.ID) {
			continue
		}
		if err := validateRemakeWindow(0, shot.EndMs-shot.StartMs); err != nil {
			return err
		}
	}
	return nil
}

// checkAdoptionShots 确认每个显式镜号都属于所见参考修订。
func checkAdoptionShots(doc model.DramaReferenceDocument, ids []string) error {
	if len(doc.Shots) == 0 {
		return errcode.ErrInvalidParams.WithMsg("参考稿没有可采用的镜头")
	}
	for _, id := range ids {
		if !slices.ContainsFunc(doc.Shots, func(shot model.DramaReferenceShot) bool { return shot.ID == id }) {
			return errcode.ErrInvalidParams.WithMsg("选择的原镜不属于此参考修订")
		}
	}
	return nil
}

// adoptionRoles 只采用已确认并仍属于本项目的角色，避免把未知说话人自动当作新身份。
func adoptionRoles(ctx context.Context, tx repository.DramaReferenceTx, p *model.DramaProject, row *model.DramaReferenceEpisode, doc model.DramaReferenceDocument) ([]model.DramaRemakeRole, error) {
	roles, err := tx.Roles(ctx, p.UserID, p.ID)
	if err != nil {
		return nil, err
	}
	canvas, err := tx.Canvas(ctx, p.UserID, p.CanvasID)
	if err != nil {
		return nil, err
	}
	targets, err := referenceTargets(canvas)
	if err != nil {
		return nil, err
	}
	var bindings map[string]string
	if err := json.Unmarshal(row.Bindings, &bindings); err != nil {
		return nil, err
	}
	nodes := map[string]string{}
	for _, target := range targets {
		nodes[target.NodeID] = target.Name
	}
	global := map[string]string{}
	for _, role := range roles {
		global[idcodec.Encode(role.ID)] = role.TargetNodeID
	}
	out := make([]model.DramaRemakeRole, 0, len(doc.Characters))
	for _, character := range doc.Characters {
		node := global[bindings[character.ID]]
		if strings.TrimSpace(nodes[node]) == "" {
			return nil, errcode.ErrInvalidParams.WithMsg("请先确认所有原角色与本项目新角色的对应")
		}
		out = append(out, model.DramaRemakeRole{CharacterID: character.ID, TargetNodeID: node, TargetName: nodes[node]})
	}
	for _, dialogue := range doc.Dialogues {
		if strings.TrimSpace(dialogue.Speaker) == "" {
			return nil, errcode.ErrInvalidParams.WithMsg("请先确认每句对白的说话人")
		}
	}
	return out, nil
}
