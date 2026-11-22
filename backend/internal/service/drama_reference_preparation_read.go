package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

// Get 读取本人参考集的准备摘要，刷新不依赖 Agent 会话或原处理进程。
func (s *DramaReferencePreparationService) Get(ctx context.Context, u, p, ref, id uint64) (*model.DramaReferencePreparationView, error) {
	// 1. 同时核验存活项目、参考集和准备归属。
	row, err := s.ownedPreparation(ctx, u, p, ref, id)
	// 2. 返回状态摘要，媒体和镜头内容通过分页按需加载。
	return preparationResult(row, err)
}

// List 按 ID 倒序读取本人参考集准备，下一页游标为空表示结束。
func (s *DramaReferencePreparationService) List(ctx context.Context, u, p, ref uint64, req *model.ListDramaReferenceDraftsReq) (*model.DramaReferencePreparationList, error) {
	// 1. 校验分页和用户归属，服务调用同样受单页上限约束。
	cursor, limit, err := draftPageArgs(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.Reference(ctx, u, p, ref, false); err != nil {
		return nil, dramaError(err)
	}
	rows, err := s.repo.Preparations(ctx, u, p, ref, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	// 2. 多读一条确定后续页，不在列表中返回文档或图片数据。
	result := &model.DramaReferencePreparationList{Items: []model.DramaReferencePreparationView{}}
	if len(rows) > limit {
		rows = rows[:limit]
		result.NextCursor = idcodec.Encode(rows[len(rows)-1].ID)
	}
	for i := range rows {
		view, err := preparationResult(&rows[i], nil)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *view)
	}
	return result, nil
}

// Frames 分页读取指定镜头或整份准备的帧引用，已删除图片明确标记 missing。
func (s *DramaReferencePreparationService) Frames(ctx context.Context, u, p, ref, id uint64, req *model.ListDramaReferenceFramesReq) (*model.DramaReferenceFrameList, error) {
	// 1. 准备归属和镜号先核验，不能利用帧 ID 跨项目读取图片。
	if req == nil {
		return nil, errcode.ErrInvalidParams
	}
	cursor, limit, err := draftPageArgs(&model.ListDramaReferenceDraftsReq{Cursor: req.Cursor, Limit: req.Limit})
	if err != nil {
		return nil, err
	}
	row, err := s.ownedPreparation(ctx, u, p, ref, id)
	if err != nil {
		return nil, err
	}
	if err := checkPreparedShot(row, req.ShotID); err != nil {
		return nil, err
	}
	frames, err := s.repo.Frames(ctx, u, id, req.ShotID, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	// 2. 逐张解析素材视图，限制本次返回数量，不把媒体二进制塞进会话。
	result := &model.DramaReferenceFrameList{Items: []model.DramaReferencePreparedFrameView{}}
	if len(frames) > limit {
		frames = frames[:limit]
		result.NextCursor = idcodec.Encode(frames[len(frames)-1].ID)
	}
	for i := range frames {
		view, err := s.preparedFrameView(ctx, row, &frames[i])
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *view)
	}
	return result, nil
}

// Shots 分页读取冻结的选镜文本，准备后台分析得到的镜头也能在刷新后查看。
func (s *DramaReferencePreparationService) Shots(ctx context.Context, u, p, ref, id uint64, req *model.ListDramaReferenceDraftsReq) (*model.DramaReferencePreparedShotList, error) {
	// 1. 解析有界游标并核验准备归属。
	cursor, limit, err := draftPageArgs(req)
	if err != nil {
		return nil, err
	}
	row, err := s.ownedPreparation(ctx, u, p, ref, id)
	if err != nil {
		return nil, err
	}
	doc, ids, err := preparationDocument(row)
	if err != nil {
		return nil, err
	}
	selected := selectedPreparationShots(doc, ids)
	if cursor > uint64(len(selected)) {
		return nil, errcode.ErrInvalidParams
	}
	// 2. 固定文档的偏移是稳定游标，后续补帧不会改变镜头顺序。
	start, end := int(cursor), min(int(cursor)+limit, len(selected))
	result := &model.DramaReferencePreparedShotList{Items: append([]model.DramaReferenceShot{}, selected[start:end]...)}
	if end < len(selected) {
		result.NextCursor = idcodec.Encode(uint64(end))
	}
	return result, nil
}

// ownedPreparation 对所有公开读写入口统一执行存活项目与参考集边界核验。
func (s *DramaReferencePreparationService) ownedPreparation(ctx context.Context, u, p, ref, id uint64) (*model.DramaReferencePreparation, error) {
	if _, err := s.repo.Reference(ctx, u, p, ref, false); err != nil {
		return nil, dramaError(err)
	}
	row, err := s.repo.Preparation(ctx, u, p, ref, id)
	return row, dramaError(err)
}

// checkPreparedShot 只接受本次选择的镜号，空值表示不筛选。
func checkPreparedShot(row *model.DramaReferencePreparation, shot string) error {
	if shot == "" {
		return nil
	}
	_, ids, err := preparationDocument(row)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == shot {
			return nil
		}
	}
	return errcode.ErrInvalidParams.WithMsg("镜头不属于这次媒体准备")
}

// selectedPreparationShots 按原片顺序返回选中的镜头。
func selectedPreparationShots(doc model.DramaReferenceDocument, ids []string) []model.DramaReferenceShot {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	shots := []model.DramaReferenceShot{}
	for _, shot := range doc.Shots {
		if wanted[shot.ID] {
			shots = append(shots, shot)
		}
	}
	return shots
}

// preparedFrameView 只有本人图片仍存在时才标为 ready，已失效图片不能当作分析证据。
func (s *DramaReferencePreparationService) preparedFrameView(ctx context.Context, prep *model.DramaReferencePreparation, frame *model.DramaReferencePreparationFrame) (*model.DramaReferencePreparedFrameView, error) {
	view := &model.DramaReferencePreparedFrameView{ID: idcodec.Encode(frame.ID), PreparationID: idcodec.Encode(prep.ID),
		SourceAssetID: prep.SourceAssetID, ShotID: frame.ShotID, StartMs: frame.StartMs, EndMs: frame.EndMs,
		AtMs: frame.AtMs, ProcessorVersion: prep.ProcessorVersion, Status: "pending"}
	if frame.AssetID == 0 {
		return view, nil
	}
	asset, err := s.assets.View(ctx, frame.UserID, frame.AssetID)
	if err != nil {
		var business *errcode.Error
		if !errors.As(draftAssetError(err), &business) || business.Code != errcode.ErrInvalidParams.Code {
			return nil, err
		}
		view.Status = "missing"
		return view, nil
	}
	if asset == nil || asset.Kind != "image" {
		view.Status = "missing"
		return view, nil
	}
	valid, err := s.validPreparedAsset(ctx, frame.UserID, frame.AssetID)
	if err != nil {
		return nil, err
	}
	if !valid {
		view.Status = "missing"
		return view, nil
	}
	view.Status, view.Asset = "ready", asset
	return view, nil
}
