package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// normalizePreparationCreate 固定输入顺序和有限数量，镜号顺序不影响处理意图。
func normalizePreparationCreate(req *model.CreateDramaReferencePreparationReq) (*model.CreateDramaReferencePreparationReq, string, error) {
	if req == nil || req.ReferenceRevision < 1 || len(req.ShotIDs) > 200 {
		return nil, "", errcode.ErrInvalidParams
	}
	key := strings.TrimSpace(req.RequestKey)
	if key == "" || !validDraftText(key, 128) {
		return nil, "", errcode.ErrInvalidParams
	}
	input := &model.CreateDramaReferencePreparationReq{RequestKey: key, ReferenceRevision: req.ReferenceRevision,
		ShotIDs: append([]string{}, req.ShotIDs...)}
	seen := map[string]bool{}
	for _, id := range input.ShotIDs {
		if strings.TrimSpace(id) == "" || len(id) > 128 || seen[id] {
			return nil, "", errcode.ErrInvalidParams.WithMsg("选镜重复或镜号无效")
		}
		seen[id] = true
	}
	sort.Strings(input.ShotIDs)
	hash, err := preparationHash(input)
	return input, hash, err
}

// preparationHash 使用规范 JSON 指纹固定业务输入，不包含任何敏感媒体内容。
func preparationHash(input any) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

// preparationSelection 保持原片镜序，超过单次预算时要求显式缩小范围而不截断。
func preparationSelection(doc model.DramaReferenceDocument, requested []string, requireDocument bool) ([]string, error) {
	if len(doc.Shots) == 0 {
		if requireDocument {
			return nil, errcode.ErrInvalidParams.WithMsg("本地分析没有得到有效镜头")
		}
		return append([]string{}, requested...), nil
	}
	if len(requested) == 0 && len(doc.Shots) > 200 {
		return nil, errcode.ErrInvalidParams.WithMsg("本次最多准备 200 镜，请指定镜头范围")
	}
	wanted := map[string]bool{}
	for _, id := range requested {
		wanted[id] = true
	}
	selected := []string{}
	for _, shot := range doc.Shots {
		if len(requested) == 0 || wanted[shot.ID] {
			selected = append(selected, shot.ID)
			delete(wanted, shot.ID)
		}
	}
	if len(wanted) != 0 || len(selected) > 200 {
		return nil, errcode.ErrInvalidParams.WithMsg("选镜不属于当前参考稿或超过 200 镜")
	}
	return selected, nil
}

// preparationDocument 解码冻结文档和选镜范围，保持正式参考稿独立。
func preparationDocument(row *model.DramaReferencePreparation) (model.DramaReferenceDocument, []string, error) {
	var doc model.DramaReferenceDocument
	var ids []string
	if err := json.Unmarshal(row.Document, &doc); err != nil {
		return doc, nil, err
	}
	if err := json.Unmarshal(row.ShotIDs, &ids); err != nil {
		return doc, nil, err
	}
	normalizeDraftDocument(&doc)
	return doc, ids, nil
}

// planPreparationFrames 生成镜内起中尾采样计划，极短镜只取中点，末点留足解码余量。
func planPreparationFrames(ctx context.Context, tx repository.DramaReferencePreparationTx, row *model.DramaReferencePreparation) error {
	doc, ids, err := preparationDocument(row)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	frames := []model.DramaReferencePreparationFrame{}
	for _, shot := range doc.Shots {
		if !wanted[shot.ID] {
			continue
		}
		for _, at := range preparationSampleTimes(shot.StartMs, shot.EndMs) {
			frames = append(frames, model.DramaReferencePreparationFrame{UserID: row.UserID, PreparationID: row.ID,
				ShotID: shot.ID, StartMs: shot.StartMs, EndMs: shot.EndMs, AtMs: at})
		}
	}
	if err := insertPreparedFrames(ctx, tx, row, frames); err != nil {
		return err
	}
	return savePreparationCounts(ctx, tx, row)
}

// preparationRecipe 用实际选镜和镜窗统一省略全镜与显式全镜，修订仍固定准备快照。
func preparationRecipe(ref *model.DramaReferenceEpisode, doc model.DramaReferenceDocument, selected []string) (string, error) {
	window := []model.DramaRemakeSource{}
	for _, shot := range selectedPreparationShots(doc, selected) {
		window = append(window, model.DramaRemakeSource{ShotID: shot.ID, StartMs: shot.StartMs, EndMs: shot.EndMs})
	}
	return preparationHash(struct {
		Asset              uint64
		Revision, Duration int64
		Processor          string
		IDs                []string
		Windows            []model.DramaRemakeSource
	}{ref.AssetID, ref.Revision, ref.DurationMs, referenceProcessorVersion, draftSlice(selected), window})
}

// insertPreparedFrames 先登记跨准备复用的媒体键，再保存当前修订的独立帧依据。
func insertPreparedFrames(ctx context.Context, tx repository.DramaReferencePreparationTx, prep *model.DramaReferencePreparation, frames []model.DramaReferencePreparationFrame) error {
	for i := range frames {
		frame := &frames[i]
		cache := &model.DramaReferenceFrameCache{UserID: prep.UserID, SourceAssetID: prep.SourceAssetID,
			StartMs: frame.StartMs, EndMs: frame.EndMs, AtMs: frame.AtMs, ProcessorVersion: prep.ProcessorVersion}
		hash, err := preparationHash(struct {
			Source         uint64
			Start, End, At int64
			Processor      string
		}{
			cache.SourceAssetID, cache.StartMs, cache.EndMs, cache.AtMs, cache.ProcessorVersion})
		if err != nil {
			return err
		}
		token := make([]byte, 24)
		if _, err := rand.Read(token); err != nil {
			return err
		}
		cache.StorageToken = hex.EncodeToString(token)
		cache.CacheKey = hash
		if err := tx.EnsureFrameCache(ctx, cache); err != nil {
			return err
		}
		frame.CacheID = cache.ID
	}
	return tx.InsertFrames(ctx, frames)
}

// preparationSampleTimes 对长镜留 100ms 尾帧余量，避免解码器取到下一个镜头。
func preparationSampleTimes(start, end int64) []int64 {
	if end <= start {
		return nil
	}
	mid := start + (end-start)/2
	if end-start <= 200 {
		return []int64{mid}
	}
	return []int64{start, mid, end - 100}
}

// savePreparationCounts 只统计已经关联持久素材的帧，状态与计数在同一事务保存。
func savePreparationCounts(ctx context.Context, tx repository.DramaReferencePreparationTx, row *model.DramaReferencePreparation) error {
	total, ready, err := tx.FrameCounts(ctx, row.UserID, row.ID)
	if err != nil {
		return err
	}
	row.TotalFrames, row.CompletedFrames = total, ready
	return tx.Save(ctx, row)
}

// preparationResult 返回有限状态摘要；无音轨只有明确探测后才能标记 unavailable。
func preparationResult(row *model.DramaReferencePreparation, err error) (*model.DramaReferencePreparationView, error) {
	if err != nil {
		return nil, err
	}
	_, ids, err := preparationDocument(row)
	if err != nil {
		return nil, err
	}
	audioStatus := "not_analyzed"
	if row.HasAudio != nil && !*row.HasAudio {
		audioStatus = "unavailable"
	}
	return &model.DramaReferencePreparationView{ID: idcodec.Encode(row.ID), ReferenceID: idcodec.Encode(row.ReferenceID),
		BaseRevision: row.BaseRevision, SourceAssetID: row.SourceAssetID, DurationMs: row.DurationMs,
		ProcessorVersion: row.ProcessorVersion, Status: row.Status, Attempt: row.Attempt, ShotCount: len(ids),
		TotalFrames: row.TotalFrames, CompletedFrames: row.CompletedFrames, HasAudio: row.HasAudio, AudioStatus: audioStatus,
		Error: row.Error, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}
