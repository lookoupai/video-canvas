package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// RemakePreview 返回本人跟拍稿的目标镜时长和全部已知原镜区间，供只读回放。
func (s *MarketingService) RemakePreview(ctx context.Context, userID, id uint64) (*model.MarketingRemakePreview, error) {
	// 1. 在同一数据库快照核对归属和脚本类型，已删除和他人的项目统一视作不存在。
	row, err := s.repo.RemakePreviewRow(ctx, userID, id)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.Kind != marketing.KindFollow {
		return nil, errcode.ErrInvalidParams.WithMsg("只有跟拍脚本支持逐镜来源预览")
	}
	// 2. 预览与建计划遵循同样的历史拆解匹配规则。
	basis, hasBasis, err := remakeBasis(row.FollowBasis, row.Reverse, row.ReverseRevision)
	if err != nil {
		return nil, err
	}
	// 3. 按落库映射分组，用脚本自身的时间计算成片镜时长。
	var shots []marketing.ScriptShot
	if err := json.Unmarshal(row.Shots, &shots); err != nil {
		return nil, fmt.Errorf("解码跟拍分镜: %w", err)
	}
	var mapping []marketing.FollowMapItem
	if len(row.FollowMap) > 0 {
		if err := json.Unmarshal(row.FollowMap, &mapping); err != nil {
			return nil, fmt.Errorf("解码跟拍映射: %w", err)
		}
	}
	return &model.MarketingRemakePreview{
		Revision: row.Revision, RefVideoAssetID: row.RefVideoAssetID,
		BasisChanged:   hasBasis && !sameJSON(row.FollowBasis, row.Reverse),
		PartialMapping: len(mapping) == 0 || !hasBasis,
		Shots:          marketing.RemakeShots(shots, mapping, basis),
	}, nil
}

// remakeBasis 优先取跟拍时保存的拆解，缺快照的旧稿仅能在拆解仍是初版时复用它。
func remakeBasis(saved, current []byte, revision int) (marketing.Reverse, bool, error) {
	hasBasis := len(saved) > 0 && !bytes.Equal(saved, []byte("null"))
	raw := saved
	if !hasBasis && revision == 1 {
		raw = current
	}
	if len(raw) == 0 {
		return marketing.Reverse{}, hasBasis, nil
	}
	basis, err := reverseOf(raw)
	return basis, hasBasis, err
}
