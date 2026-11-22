package service

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// createReferenceAdoption 先校验全部依据和冲突，再在同一事务创建分集、采用记录和片段。
func (s *DramaReferenceService) createReferenceAdoption(ctx context.Context, tx repository.DramaReferenceTx, p *model.DramaProject, id uint64, req *model.AdoptDramaReferenceReq, hash string) (*model.DramaReferenceAdoption, error) {
	return s.createReferenceAdoptionWithPlan(ctx, tx, p, id, req, hash, nil)
}

// createReferenceAdoptionWithPlan 原子保存采用和可选的草稿生成路线提示词与裁剪值。
func (s *DramaReferenceService) createReferenceAdoptionWithPlan(ctx context.Context, tx repository.DramaReferenceTx, p *model.DramaProject, id uint64, req *model.AdoptDramaReferenceReq, hash string, units map[string]model.DramaReferenceApplyUnit) (*model.DramaReferenceAdoption, error) {
	ref, basis, err := s.adoptionBasis(ctx, tx, p, id, req)
	if err != nil {
		return nil, err
	}
	ep, err := adoptionEpisode(ctx, tx.DramaTx(), p, ref, req)
	if err != nil {
		return nil, err
	}
	row := &model.DramaReferenceAdoption{UserID: p.UserID, DramaID: p.ID, ReferenceID: id, ReferenceRevision: req.Revision, RequestKey: req.RequestKey, Fingerprint: hash, Mode: req.Mode, EpisodeID: ep.ID, Snapshot: []byte("{}"), SegmentIDs: []byte("[]")}
	if err := tx.InsertAdoption(ctx, row); err != nil {
		return nil, err
	}
	basis.AdoptionID = idcodec.Encode(row.ID)
	row.Snapshot, err = json.Marshal(basis)
	if err != nil {
		return nil, err
	}
	ep.ReferenceBasis = row.Snapshot
	if req.Mode == "follow" {
		ids, err := saveAdoptionSegments(ctx, tx.DramaTx(), ep, basis, req.ShotIDs, units)
		if err != nil {
			return nil, err
		}
		row.SegmentIDs, err = json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		if req.TargetEpisodeNo == 0 {
			ep.ScriptMD = referenceFollowScript(ep.No, basis)
		}
	}
	if err := tx.DramaTx().SaveEpisode(ctx, ep, ep.Revision); err != nil {
		return nil, err
	}
	return row, tx.FinishAdoption(ctx, row)
}

// adoptionEpisode 新建末集或验证显式目标集；任何活动任务都不得与采用操作交错修改依据。
func adoptionEpisode(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, ref *model.DramaReferenceEpisode, req *model.AdoptDramaReferenceReq) (*model.DramaEpisode, error) {
	jobs, err := tx.Jobs(ctx, p.UserID, p.ID, true)
	if err != nil {
		return nil, err
	}
	if len(jobs) != 0 {
		return nil, errcode.ErrDramaJobBusy
	}
	eps, err := tx.Episodes(ctx, p.UserID, p.ID)
	if err != nil {
		return nil, err
	}
	next := 1
	for _, ep := range eps {
		next = max(next, ep.No+1)
		if ep.No == req.TargetEpisodeNo {
			return existingAdoptionEpisode(ctx, tx, p, ep, ref.ID, req.EpisodeRevision)
		}
	}
	if req.TargetEpisodeNo != 0 {
		return nil, errcode.ErrDramaNotFound
	}
	ep := &model.DramaEpisode{UserID: p.UserID, DramaID: p.ID, No: next, Title: ref.Title, Revision: 1, ScriptStatus: "empty", StoryboardStatus: "empty"}
	if req.Mode == "follow" {
		now := time.Now()
		ep.LockedAt, ep.ScriptStatus, ep.StoryboardStatus = &now, "ready", "ready"
	}
	if err := tx.SaveEpisode(ctx, ep, 0); err != nil {
		return nil, err
	}
	return ep, nil
}

// existingAdoptionEpisode 只允许更新同一原集的跟拍分集，并保留手工剧本和已有成片。
func existingAdoptionEpisode(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, ep model.DramaEpisode, referenceID uint64, revision int64) (*model.DramaEpisode, error) {
	if ep.Revision != revision {
		return nil, errcode.ErrCanvasConflict
	}
	var basis model.DramaReferenceBasis
	if err := json.Unmarshal(ep.ReferenceBasis, &basis); err != nil {
		return nil, err
	}
	if basis.Mode != "follow" || basis.ReferenceID != idcodec.Encode(referenceID) {
		return nil, errcode.ErrInvalidParams.WithMsg("只能更新采用自本参考集的跟拍分集")
	}
	if err := checkDramaMediaTasks(ctx, tx, p, []model.DramaEpisode{ep}, ""); err != nil {
		return nil, err
	}
	return &ep, nil
}

// saveAdoptionSegments 按稳定原镜 ID 更新所选片段，保留选版、其他片段和所有生成历史。
func saveAdoptionSegments(ctx context.Context, tx repository.DramaTx, ep *model.DramaEpisode, basis model.DramaReferenceBasis, selected []string, units map[string]model.DramaReferenceApplyUnit) ([]string, error) {
	rows, err := tx.Segments(ctx, ep.UserID, ep.ID)
	if err != nil {
		return nil, err
	}
	byShot, next, err := adoptedSegmentsByShot(rows, basis.ReferenceID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, shot := range basis.Document.Shots {
		if len(selected) != 0 && !slices.Contains(selected, shot.ID) {
			continue
		}
		row, ok := byShot[shot.ID]
		if !ok {
			next++
			row = model.DramaSegment{UserID: ep.UserID, EpisodeID: ep.ID, Seq: next, Revision: 1}
		}
		row.Prompt = referenceShotPrompt(basis, shot)
		trimStart, targetDuration := int64(0), shot.EndMs-shot.StartMs
		row.Seconds = int((targetDuration + 999) / 1000)
		if unit, ok := units[shot.ID]; ok {
			if unit.Prompt != "" {
				row.Prompt = unit.Prompt
			}
			if unit.Duration > 0 {
				row.Seconds = unit.Duration
			}
			trimStart, targetDuration = unit.TrimStartMs, unit.TargetDurationMs
		}
		row.Remake, err = json.Marshal(model.DramaSegmentRemake{AdoptionID: basis.AdoptionID, ReferenceID: basis.ReferenceID, ReferenceRevision: basis.ReferenceRevision, AssetID: basis.AssetID, Sources: []model.DramaRemakeSource{{ShotID: shot.ID, StartMs: shot.StartMs, EndMs: shot.EndMs}}, Roles: basis.Roles, TargetDurationMs: targetDuration, TrimStartMs: trimStart})
		if err != nil {
			return nil, err
		}
		if err := tx.SaveSegment(ctx, &row, row.Revision); err != nil {
			return nil, err
		}
		ids = append(ids, idcodec.Encode(row.ID))
	}
	return ids, nil
}

// adoptedSegmentsByShot 不根据可变镜序匹配，避免重排后更新错误的片段。
func adoptedSegmentsByShot(rows []model.DramaSegment, referenceID string) (map[string]model.DramaSegment, int, error) {
	out, next := map[string]model.DramaSegment{}, -1
	for _, row := range rows {
		next = max(next, row.Seq)
		var remake model.DramaSegmentRemake
		if len(row.Remake) == 0 {
			continue
		}
		if err := json.Unmarshal(row.Remake, &remake); err != nil {
			return nil, 0, err
		}
		if remake.ReferenceID == referenceID && len(remake.Sources) == 1 {
			if _, exists := out[remake.Sources[0].ShotID]; exists {
				return nil, 0, errcode.ErrInvalidParams.WithMsg("同一原镜关联了多个片段，请先整理片场")
			}
			out[remake.Sources[0].ShotID] = row
		}
	}
	return out, next, nil
}
