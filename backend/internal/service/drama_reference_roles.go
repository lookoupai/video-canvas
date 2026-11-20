package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// Collection 返回本人项目的参考集、跨集角色以及真实画布角色候选。
func (s *DramaReferenceService) Collection(ctx context.Context, u, p uint64) (*model.DramaReferenceCollection, error) {
	// 1. 核对存活项目，读独立的参考集顺序修订。
	project, err := s.repo.Project(ctx, u, p, false)
	if err != nil {
		return nil, dramaError(err)
	}
	out := &model.DramaReferenceCollection{Episodes: []model.DramaReferenceView{}, Roles: []model.DramaReferenceRoleView{}, Targets: []model.DramaReferenceTarget{}, AnalyzerAvailable: s.analyzer != nil && s.analyzer.Available()}
	out.Title, out.CanvasID = project.Title, idcodec.Encode(project.CanvasID)
	state, err := s.repo.State(ctx, u, p)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		out.Revision = state.Revision
	}
	// 2. 各集独立读回已保存的校订稿，失败集不阻断其他集。
	rows, err := s.repo.Episodes(ctx, u, p)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		view, err := s.referenceView(ctx, &rows[i], 0)
		if err != nil {
			return nil, err
		}
		out.Episodes = append(out.Episodes, *view)
	}
	roles, err := s.repo.Roles(ctx, u, p)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		out.Roles = append(out.Roles, referenceRoleView(role))
	}
	// 3. 绑定目标限定在这个项目专属画布里的角色节点。
	canvas, err := s.repo.Canvas(ctx, u, project.CanvasID)
	if err != nil {
		return nil, dramaError(err)
	}
	out.Targets, err = referenceTargets(canvas)
	return out, err
}

// referenceTargets 仅提取项目画布标记为角色的节点身份，不信任客户端传来的素材归属。
func referenceTargets(canvas *model.CanvasProject) ([]model.DramaReferenceTarget, error) {
	var graph struct {
		Nodes []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Data struct {
				Label string `json:"label"`
				Drama struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"drama"`
			} `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(canvas.PayloadJSON, &graph); err != nil {
		return nil, err
	}
	out := []model.DramaReferenceTarget{}
	for _, node := range graph.Nodes {
		if node.Type == "group" || node.Data.Drama.Type != "character" {
			continue
		}
		name := node.Data.Drama.Name
		if name == "" {
			name = node.Data.Label
		}
		out = append(out, model.DramaReferenceTarget{NodeID: node.ID, Name: name})
	}
	return out, nil
}

// referenceRoleView 编码跨集角色 ID，映射只引用同一项目的角色。
func referenceRoleView(role model.DramaReferenceRole) model.DramaReferenceRoleView {
	return model.DramaReferenceRoleView{ID: idcodec.Encode(role.ID), Name: role.Name, TargetNodeID: role.TargetNodeID, Revision: role.Revision}
}

// CreateRole 建立用户确认的跨集身份，可稍后再指定新角色资产。
func (s *DramaReferenceService) CreateRole(ctx context.Context, u, p uint64, req *model.SaveDramaReferenceRoleReq) (*model.DramaReferenceRoleView, error) {
	// 1. 新身份使用独立主键，不按人物姓名自动合并。
	return s.saveRole(ctx, u, p, 0, req)
}

// UpdateRole 带角色修订更换名称或新角色节点，所有使用该身份的参考集同时读到更新。
func (s *DramaReferenceService) UpdateRole(ctx context.Context, u, p, id uint64, req *model.SaveDramaReferenceRoleReq) (*model.DramaReferenceRoleView, error) {
	// 1. 角色映射沿用稳定 ID，修订防止覆盖其他端的校订。
	return s.saveRole(ctx, u, p, id, req)
}

// saveRole 在项目锁内验证画布节点归属并保存跨集身份。
func (s *DramaReferenceService) saveRole(ctx context.Context, u, p, id uint64, req *model.SaveDramaReferenceRoleReq) (*model.DramaReferenceRoleView, error) {
	role := model.DramaReferenceRole{UserID: u, DramaID: p, Name: strings.TrimSpace(req.Name), TargetNodeID: req.TargetNodeID, Revision: 1}
	if role.Name == "" {
		return nil, errcode.ErrInvalidParams
	}
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		project, err := tx.Project(ctx, u, p, true)
		if err != nil {
			return err
		}
		if err := checkReferenceRoleTarget(ctx, tx, project, req.TargetNodeID); err != nil {
			return err
		}
		if id != 0 {
			if err := updateReferenceRoleRevision(ctx, tx, &role, id, req.Revision); err != nil {
				return err
			}
		}
		return tx.SaveRole(ctx, &role)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	view := referenceRoleView(role)
	return &view, nil
}

// updateReferenceRoleRevision 确认身份属于本项目且版本未过期。
func updateReferenceRoleRevision(ctx context.Context, tx repository.DramaReferenceTx, role *model.DramaReferenceRole, id uint64, revision int64) error {
	roles, err := tx.Roles(ctx, role.UserID, role.DramaID)
	if err != nil {
		return err
	}
	for _, prior := range roles {
		if prior.ID != id {
			continue
		}
		if prior.Revision != revision {
			return errcode.ErrCanvasConflict
		}
		role.ID, role.Revision = id, revision+1
		return nil
	}
	return errcode.ErrDramaNotFound
}

// checkReferenceRoleTarget 不允许把其他项目节点当作本项目的新角色资产。
func checkReferenceRoleTarget(ctx context.Context, tx repository.DramaReferenceTx, p *model.DramaProject, nodeID string) error {
	if nodeID == "" {
		return nil
	}
	canvas, err := tx.Canvas(ctx, p.UserID, p.CanvasID)
	if err != nil {
		return err
	}
	targets, err := referenceTargets(canvas)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.NodeID == nodeID {
			return nil
		}
	}
	return errcode.ErrInvalidParams.WithMsg("请选择本项目画布中的角色")
}
