package service

import (
	"context"
	"encoding/json"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// Update 合并后校验整份草稿，旧修订不能覆盖用户已经保存的内容。
func (s *DramaReferenceDraftService) Update(ctx context.Context, u, p, ref, id uint64, req *model.UpdateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	return s.update(ctx, u, p, ref, id, req, draftActor{source: "manual"})
}

// UpdateForAgent 更新草稿并将可信 Agent 会话和运行作为最后编辑来源。
func (s *DramaReferenceDraftService) UpdateForAgent(ctx context.Context, u, p, ref, id, sessionID, runID uint64, req *model.UpdateDramaReferenceDraftReq) (*model.DramaReferenceDraftView, error) {
	if sessionID == 0 || runID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少 Agent 会话或运行标识")
	}
	return s.update(ctx, u, p, ref, id, req, draftActor{source: "agent", sessionID: sessionID, runID: runID})
}

// update 合并并校验草稿后，按当前修订原子保存内容和来源。
func (s *DramaReferenceDraftService) update(ctx context.Context, u, p, ref, id uint64, req *model.UpdateDramaReferenceDraftReq, actor draftActor) (*model.DramaReferenceDraftView, error) {
	// 1. 先读本人草稿并检查编辑状态，外部 Agent 与人工编辑共享乐观锁。
	if req == nil || req.Revision < 1 || !hasDraftEdit(req) {
		return nil, errcode.ErrInvalidParams
	}
	reference, err := s.repo.Reference(ctx, u, p, ref, false)
	if err != nil {
		return nil, dramaError(err)
	}
	row, err := s.repo.Draft(ctx, u, p, ref, id, false)
	if err != nil {
		return nil, dramaError(err)
	}
	if err := editableDraft(row, req.Revision); err != nil {
		return nil, err
	}
	content, err := draftContentOf(row)
	if err != nil {
		return nil, err
	}
	// 2. 完整校验替换后的引用关系，清空证据不能留下仍声称已观察的字段。
	mergeDraftEdit(&content, req)
	if err := s.validateDraftContent(ctx, u, reference.DurationMs, content); err != nil {
		return nil, err
	}
	if err := s.validatePreparedEvidence(ctx, u, p, ref, row.PreparationID, content); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	// 3. 锁内重查归属与修订，所有素材访问已结束，事务只提交已核验内容。
	err = s.repo.WithTx(ctx, func(tx repository.DramaReferenceDraftTx) error {
		if _, err := tx.Reference(ctx, u, p, ref, true); err != nil {
			return err
		}
		fresh, err := tx.Draft(ctx, u, p, ref, id, true)
		if err != nil {
			return err
		}
		if err := editableDraft(fresh, req.Revision); err != nil {
			return err
		}
		fresh.Content, fresh.Source, fresh.SessionID, fresh.RunID = raw, actor.source, actor.sessionID, actor.runID
		fresh.Revision++
		row = fresh
		return tx.Save(ctx, fresh)
	})
	return draftResult(row, dramaError(err))
}

// hasDraftEdit 拒绝只有修订号的空修改，避免制造无意义的新修订。
func hasDraftEdit(req *model.UpdateDramaReferenceDraftReq) bool {
	return req.Instruction != nil || req.Document != nil || req.Prompts != nil || req.Evidence != nil || req.Observations != nil || req.Coverage != nil
}

// editableDraft 将过期修订和已关闭草稿都转为可恢复的冲突响应。
func editableDraft(row *model.DramaReferenceDraft, revision int64) error {
	if row.Revision != revision {
		return errcode.ErrCanvasConflict
	}
	if row.Status != "editing" && row.Status != "ready" {
		return errcode.ErrCanvasConflict.WithMsg("草稿已采用或废弃，请重新创建草稿")
	}
	return nil
}

// mergeDraftEdit 每个提供的字段整体替换，遗漏字段保留当前值。
func mergeDraftEdit(content *model.DramaReferenceDraftContent, req *model.UpdateDramaReferenceDraftReq) {
	if req.Instruction != nil {
		content.Instruction = *req.Instruction
	}
	if req.Document != nil {
		content.Document = *req.Document
	}
	if req.Prompts != nil {
		content.Prompts = *req.Prompts
	}
	if req.Evidence != nil {
		content.Evidence = *req.Evidence
	}
	if req.Observations != nil {
		content.Observations = *req.Observations
	}
	if req.Coverage != nil {
		content.Coverage = *req.Coverage
	}
	normalizeDraftContent(content)
}
