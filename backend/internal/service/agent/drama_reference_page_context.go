package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

type dramaReferencePageContext struct {
	Kind              string   `json:"kind"`
	ProjectID         string   `json:"project_id"`
	ReferenceID       string   `json:"reference_id"`
	ReferenceRevision int64    `json:"reference_revision"`
	ReferenceTitle    string   `json:"reference_title,omitempty"`
	ReferenceSeq      int      `json:"reference_seq,omitempty"`
	SelectedShotIDs   []string `json:"selected_shot_ids,omitempty"`
}

func (b *AgentBridge) validateDramaReferencePageContext(
	ctx context.Context,
	run RunContext,
	raw json.RawMessage,
) (*dramaReferencePageContext, error) {
	var page dramaReferencePageContext
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&page); err != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧参考页面上下文格式错误")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧参考页面上下文格式错误")
	}
	if page.Kind != "drama_reference" || page.ReferenceRevision < 1 ||
		len(page.SelectedShotIDs) > 200 {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧参考页面上下文格式错误")
	}
	projectID, projectOK := idcodec.Decode(page.ProjectID)
	referenceID, referenceOK := idcodec.Decode(page.ReferenceID)
	if !projectOK || !referenceOK {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧参考页面 ID 格式错误")
	}
	project, err := b.dramaProject(ctx, run.UserID, run.CanvasID)
	if err != nil {
		return nil, err
	}
	if uint64(project.ID) != projectID || b.d.DramaReferences == nil {
		return nil, errcode.ErrDramaNotFound
	}
	reference, err := b.d.DramaReferences.GetEpisode(
		ctx, run.UserID, projectID, referenceID, page.ReferenceRevision,
	)
	if err != nil {
		return nil, err
	}
	if reference == nil || reference.Document == nil ||
		(page.ReferenceTitle != "" && page.ReferenceTitle != reference.Title) ||
		(page.ReferenceSeq != 0 && page.ReferenceSeq != reference.Seq) {
		return nil, errcode.ErrDramaNotFound
	}
	shotIDs := make(map[string]bool, len(reference.Document.Shots))
	for _, shot := range reference.Document.Shots {
		shotIDs[shot.ID] = true
	}
	selected := make(map[string]bool, len(page.SelectedShotIDs))
	for _, id := range page.SelectedShotIDs {
		if id == "" || len(id) > 128 || selected[id] || !shotIDs[id] {
			return nil, errcode.ErrInvalidParams.WithMsg("短剧参考上下文包含无效镜头")
		}
		selected[id] = true
	}
	page.ReferenceTitle = reference.Title
	page.ReferenceSeq = reference.Seq
	return &page, nil
}
