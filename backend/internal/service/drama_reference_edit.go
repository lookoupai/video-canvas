package service

import (
	"context"
	"encoding/json"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/reference"
	"video-canvas/internal/repository"
)

// UpdateEpisode 只保存当前参考集的修订，保留其他集及旧版原片依据。
func (s *DramaReferenceService) UpdateEpisode(ctx context.Context, u, p, id uint64, req *model.UpdateDramaReferenceReq) (*model.DramaReferenceView, error) {
	// 1. 锁项目与参考集，排队中的同集暂时禁止修改，避免分析覆盖人工校订。
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		row, err := lockedReference(ctx, tx, u, p, id, req.Revision)
		if err != nil {
			return err
		}
		return applyReferenceEdit(ctx, tx, row, req)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	// 2. 每次人工校订固定成独立修订，后续重新分析可回看原来的依据。
	return s.GetEpisode(ctx, u, p, id, 0)
}

// applyReferenceEdit 校验完整编辑稿及绑定后保存该集的独立修订。
func applyReferenceEdit(ctx context.Context, tx repository.DramaReferenceTx, row *model.DramaReferenceEpisode, req *model.UpdateDramaReferenceReq) error {
	if req.Document == nil && req.Bindings == nil && req.Title == nil {
		return errcode.ErrInvalidParams
	}
	if req.Title != nil {
		row.Title = strings.TrimSpace(*req.Title)
	}
	if req.Document == nil && len(row.Document) == 0 {
		if len(req.Bindings) > 0 {
			return errcode.ErrInvalidParams.WithMsg("请先导入本集人物再建立对应关系")
		}
		row.Revision++
		return tx.SaveEpisode(ctx, row)
	}
	doc, err := referenceDocumentOf(row)
	if err != nil {
		return err
	}
	if req.Document != nil {
		doc = *req.Document
		if err := reference.ValidateDocument(doc, row.DurationMs); err != nil {
			return errcode.ErrInvalidParams.WithMsg("镜头、对白时间或人物引用无效")
		}
	}
	var bindings map[string]string
	if err := json.Unmarshal(row.Bindings, &bindings); err != nil {
		return err
	}
	if req.Bindings != nil {
		bindings = req.Bindings
	}
	if err := validateReferenceBindings(ctx, tx, row.UserID, row.DramaID, doc, bindings); err != nil {
		return err
	}
	row.Document, err = json.Marshal(doc)
	if err != nil {
		return err
	}
	row.Bindings, err = json.Marshal(bindings)
	if err != nil {
		return err
	}
	row.Revision++
	if err := tx.SaveEpisode(ctx, row); err != nil {
		return err
	}
	return saveReferenceRevision(ctx, tx, row, "manual")
}

// lockedReference 在项目锁内读取参考集并确认当前版本可编辑。
func lockedReference(ctx context.Context, tx repository.DramaReferenceTx, u, p, id uint64, revision int64) (*model.DramaReferenceEpisode, error) {
	if _, err := tx.Project(ctx, u, p, true); err != nil {
		return nil, err
	}
	row, err := tx.Episode(ctx, u, p, id)
	if err != nil {
		return nil, err
	}
	if row.Revision != revision {
		return nil, errcode.ErrCanvasConflict
	}
	if row.Status == "queued" {
		return nil, errcode.ErrDramaJobBusy
	}
	return row, nil
}

// referenceDocumentOf 解析当前参考集文档，未导入时返回空文档。
func referenceDocumentOf(row *model.DramaReferenceEpisode) (model.DramaReferenceDocument, error) {
	var doc model.DramaReferenceDocument
	if len(row.Document) == 0 {
		return doc, nil
	}
	err := json.Unmarshal(row.Document, &doc)
	return doc, err
}

// validateReferenceBindings 确认本集身份到全局角色的映射只引用当前项目。
func validateReferenceBindings(ctx context.Context, tx repository.DramaReferenceTx, u, p uint64, doc model.DramaReferenceDocument, bindings map[string]string) error {
	locals := map[string]bool{}
	for _, character := range doc.Characters {
		locals[character.ID] = true
	}
	roles, err := tx.Roles(ctx, u, p)
	if err != nil {
		return err
	}
	valid := map[string]bool{}
	for _, role := range roles {
		valid[idcodec.Encode(role.ID)] = true
	}
	for local, role := range bindings {
		if !locals[local] || (role != "" && !valid[role]) {
			return errcode.ErrInvalidParams.WithMsg("角色对应关系不属于本集或本项目")
		}
	}
	return nil
}

// saveReferenceRevision 记录不可变拆解与当时的角色对应，当前状态独立保存。
func saveReferenceRevision(ctx context.Context, tx repository.DramaReferenceTx, row *model.DramaReferenceEpisode, method string) error {
	return tx.InsertRevision(ctx, &model.DramaReferenceRevision{UserID: row.UserID, ReferenceID: row.ID, Revision: row.Revision, Method: method, Document: row.Document, Bindings: row.Bindings})
}

// OrderEpisodes 带集合修订更新完整集序，不改变任何单集的校订稿。
func (s *DramaReferenceService) OrderEpisodes(ctx context.Context, u, p uint64, req *model.OrderDramaReferencesReq) (*model.DramaReferenceCollection, error) {
	// 1. 项目锁串行化新增集和重排，验证本次排列包含全部参考集。
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		if _, err := tx.Project(ctx, u, p, true); err != nil {
			return err
		}
		state, err := referenceState(ctx, tx, u, p)
		if err != nil {
			return err
		}
		if state.Revision != req.Revision {
			return errcode.ErrCanvasConflict
		}
		episodes, err := tx.Episodes(ctx, u, p)
		if err != nil {
			return err
		}
		if err := saveReferenceOrder(ctx, tx, episodes, req.IDs); err != nil {
			return err
		}
		state.Revision++
		return tx.SaveState(ctx, state)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	// 2. 重读最新顺序，保留正在导入和已经校订的各集。
	return s.Collection(ctx, u, p)
}

// saveReferenceOrder 验证完整排列，只更新顺序字段。
func saveReferenceOrder(ctx context.Context, tx repository.DramaReferenceTx, episodes []model.DramaReferenceEpisode, ids []string) error {
	if len(episodes) != len(ids) {
		return errcode.ErrInvalidParams.WithMsg("排序须包含全部参考集")
	}
	available := map[string]*model.DramaReferenceEpisode{}
	for i := range episodes {
		available[idcodec.Encode(episodes[i].ID)] = &episodes[i]
	}
	for index, id := range ids {
		row, ok := available[id]
		if !ok {
			return errcode.ErrInvalidParams.WithMsg("参考集重复或不属于当前项目")
		}
		delete(available, id)
		if err := tx.SetEpisodeOrder(ctx, row.UserID, row.DramaID, row.ID, index+1); err != nil {
			return err
		}
	}
	return nil
}
