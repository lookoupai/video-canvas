package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// Adopt 把所见参考修订原子采用到真实分集；已有分集仅允许显式选择原镜更新。
func (s *DramaReferenceService) Adopt(ctx context.Context, u, p, id uint64, req *model.AdoptDramaReferenceReq) (*model.DramaReferenceAdoptionView, error) {
	// 1. 固定规范化输入，提交键不参与指纹；更换键的相同修订仍复用已建立的分集。
	input, err := normalizeReferenceAdoption(req)
	if err != nil {
		return nil, err
	}
	fingerprint := referenceAdoptionFingerprint(p, id, input)
	var out *model.DramaReferenceAdoptionView
	// 2. 提交键锁防跨项目重传，项目锁与原有分集/任务写操作串行。
	err = s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		if err := tx.LockCreateKey(ctx, u, "adoption/"+input.RequestKey); err != nil {
			return err
		}
		project, err := tx.Project(ctx, u, p, true)
		if err != nil {
			return err
		}
		prior, err := priorReferenceAdoption(ctx, tx, project, id, input, fingerprint)
		if err != nil {
			return err
		}
		if prior == nil {
			prior, err = s.createReferenceAdoption(ctx, tx, project, id, input, fingerprint)
			if err != nil {
				return err
			}
		}
		out, err = referenceAdoptionView(ctx, tx.DramaTx(), prior)
		return err
	})
	return out, dramaError(err)
}

// normalizeReferenceAdoption 拒绝隐式覆盖；完整新集和已有集局部更新使用不同的必填条件。
func normalizeReferenceAdoption(req *model.AdoptDramaReferenceReq) (*model.AdoptDramaReferenceReq, error) {
	if req == nil {
		return nil, errcode.ErrInvalidParams
	}
	out := *req
	out.RequestKey, out.Instruction = strings.TrimSpace(out.RequestKey), strings.TrimSpace(out.Instruction)
	if out.Revision < 1 || out.RequestKey == "" || len(out.RequestKey) > 128 || len([]rune(out.Instruction)) > 4000 || (out.Mode != "follow" && out.Mode != "rewrite") {
		return nil, errcode.ErrInvalidParams
	}
	if out.TargetEpisodeNo < 0 || out.EpisodeRevision < 0 || len(out.ShotIDs) > 2000 {
		return nil, errcode.ErrInvalidParams
	}
	if out.TargetEpisodeNo > 0 && (out.Mode != "follow" || out.EpisodeRevision < 1 || len(out.ShotIDs) == 0) {
		return nil, errcode.ErrInvalidParams.WithMsg("更新已有集需要选择原镜并提供分集修订")
	}
	if out.TargetEpisodeNo == 0 && (out.EpisodeRevision != 0 || len(out.ShotIDs) != 0) {
		return nil, errcode.ErrInvalidParams.WithMsg("首次采用使用整集原镜")
	}
	// 空数组与未传都表示完整采用，统一为空值，避免重传的编码差异误判为不同意图。
	out.ShotIDs = nil
	if len(req.ShotIDs) > 0 {
		out.ShotIDs = slices.Clone(req.ShotIDs)
	}
	slices.Sort(out.ShotIDs)
	if len(slices.Compact(slices.Clone(out.ShotIDs))) != len(out.ShotIDs) {
		return nil, errcode.ErrInvalidParams
	}
	return &out, nil
}

// referenceAdoptionFingerprint 将目标集、原修订、镜头范围和改编要求一并绑定到提交键。
func referenceAdoptionFingerprint(p, id uint64, req *model.AdoptDramaReferenceReq) string {
	normalized := *req
	normalized.RequestKey = ""
	raw, _ := json.Marshal(struct {
		Project   uint64
		Reference uint64
		Request   model.AdoptDramaReferenceReq
	}{p, id, normalized})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// priorReferenceAdoption 先验证提交键，再查默认整集关联；重传已删除集也返回原记录。
func priorReferenceAdoption(ctx context.Context, tx repository.DramaReferenceTx, p *model.DramaProject, id uint64, req *model.AdoptDramaReferenceReq, hash string) (*model.DramaReferenceAdoption, error) {
	row, err := tx.AdoptionByKey(ctx, p.UserID, req.RequestKey)
	if err == nil {
		fingerprint, keyErr := referenceAdoptionKeyHash(row, req.RequestKey)
		if keyErr != nil {
			return nil, keyErr
		}
		if row.DramaID != p.ID || fingerprint != hash {
			return nil, errcode.ErrInvalidParams.WithMsg("提交键已经用于其他采用，请重新操作")
		}
		return row, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if req.TargetEpisodeNo != 0 {
		return nil, nil
	}
	rows, err := tx.Adoptions(ctx, p.UserID, p.ID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ReferenceID == id && row.ReferenceRevision == req.Revision && row.Mode == req.Mode {
			var basis model.DramaReferenceBasis
			if err := json.Unmarshal(row.Snapshot, &basis); err != nil {
				return nil, err
			}
			if basis.Instruction != req.Instruction {
				return nil, errcode.ErrInvalidParams.WithMsg("此参考修订已有采用记录，请进入原分集或先保存新修订")
			}
			return &row, rememberReferenceAdoptionKey(ctx, tx, &row, req.RequestKey, hash)
		}
	}
	return nil, nil
}

// rememberReferenceAdoptionKey 保存重复点击的新键，防止随后把该键误用到不同输入。
func rememberReferenceAdoptionKey(ctx context.Context, tx repository.DramaReferenceTx, row *model.DramaReferenceAdoption, key, hash string) error {
	keys := map[string]string{}
	if len(row.RequestKeys) != 0 {
		if err := json.Unmarshal(row.RequestKeys, &keys); err != nil {
			return err
		}
	}
	keys[key] = hash
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	row.RequestKeys = raw
	return tx.RememberAdoptionKey(ctx, row)
}

// referenceAdoptionKeyHash 使用该次操作的指纹，即使默认采用复用了局部更新产生的记录也能安全重传。
func referenceAdoptionKeyHash(row *model.DramaReferenceAdoption, key string) (string, error) {
	if row.RequestKey == key {
		return row.Fingerprint, nil
	}
	keys := map[string]string{}
	if err := json.Unmarshal(row.RequestKeys, &keys); err != nil {
		return "", err
	}
	return keys[key], nil
}

// Adoptions 返回采用记录与当前集号；删除分集后保留原关联并标记不可进入。
func (s *DramaReferenceService) Adoptions(ctx context.Context, u, p uint64) ([]model.DramaReferenceAdoptionView, error) {
	// 1. 项目归属统一返回不存在，避免通过采用历史枚举其他用户数据。
	if _, err := s.repo.Project(ctx, u, p, false); err != nil {
		return nil, dramaError(err)
	}
	rows, err := s.repo.Adoptions(ctx, u, p)
	if err != nil {
		return nil, err
	}
	// 2. 当前集号只从实际分集读取，重排和删除不篡改历史快照。
	out := make([]model.DramaReferenceAdoptionView, 0, len(rows))
	for _, row := range rows {
		view, err := referenceAdoptionView(ctx, s.repo.DramaTx(), &row)
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return out, nil
}

// referenceAdoptionView 以真实分集 ID 查当前集号，并保留已删除记录。
func referenceAdoptionView(ctx context.Context, tx repository.DramaTx, row *model.DramaReferenceAdoption) (*model.DramaReferenceAdoptionView, error) {
	out := &model.DramaReferenceAdoptionView{ID: idcodec.Encode(row.ID), ReferenceID: idcodec.Encode(row.ReferenceID), ReferenceRevision: row.ReferenceRevision, Mode: row.Mode, EpisodeID: idcodec.Encode(row.EpisodeID), SegmentIDs: []string{}, Deleted: true}
	if err := json.Unmarshal(row.SegmentIDs, &out.SegmentIDs); err != nil {
		return nil, err
	}
	eps, err := tx.Episodes(ctx, row.UserID, row.DramaID)
	if err != nil {
		return nil, err
	}
	for _, ep := range eps {
		if ep.ID == row.EpisodeID {
			out.EpisodeNo, out.EpisodeRevision, out.Deleted = ep.No, ep.Revision, false
		}
	}
	return out, nil
}
