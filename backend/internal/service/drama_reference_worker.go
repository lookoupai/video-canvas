package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/reference"
	"video-canvas/internal/repository"
)

// Import 保存本集拆片包并提交异步解析，已有校订稿保留到新版本成功。
func (s *DramaReferenceService) Import(ctx context.Context, u, p, id uint64, revision int64, raw []byte) (*model.DramaReferenceView, error) {
	// 1. 输入限额在服务层复查，避免绕过 HTTP 入口提交超大包。
	if len(raw) == 0 || len(raw) > reference.MaxPackageBytes {
		return nil, errcode.ErrInvalidParams.WithMsg("拆片包须为 1 字节至 8 MB")
	}
	// 2. 单集版本与入队在同一事务写入，返回后允许离开页面。
	return s.queueReference(ctx, u, p, id, revision, "package", raw)
}

// Analyze 为单集原视频创建本地分析任务，不影响其他集的人工校订。
func (s *DramaReferenceService) Analyze(ctx context.Context, u, p, id uint64, revision int64) (*model.DramaReferenceView, error) {
	// 1. 分析器未安装时仍可导入包与编辑历史结果。
	if s.analyzer == nil || !s.analyzer.Available() {
		return nil, errcode.ErrInvalidParams.WithMsg("本地分析器未配置，请导入拆片包")
	}
	// 2. 后台下载已校验归属的视频并执行本地分析。
	return s.queueReference(ctx, u, p, id, revision, "rea-video-local", nil)
}

// queueReference 使用递增尝试号拒绝迟到完成，单集队列保持至处理完成。
func (s *DramaReferenceService) queueReference(ctx context.Context, u, p, id uint64, revision int64, method string, raw []byte) (*model.DramaReferenceView, error) {
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		row, err := lockedReference(ctx, tx, u, p, id, revision)
		if err != nil {
			return err
		}
		row.Attempt++
		row.Revision++
		row.Status = "queued"
		row.Error = ""
		row.Method = method
		row.Input = raw
		if err := tx.SaveEpisode(ctx, row); err != nil {
			return err
		}
		return tx.Enqueue(ctx, u, p, id, row.Attempt)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	return s.GetEpisode(ctx, u, p, id, 0)
}

// HandleDramaReference 处理当前尝试，成功与人工校订都保留不可变修订，业务失败仅更新本集状态。
func (s *DramaReferenceService) HandleDramaReference(ctx context.Context, u, p, id uint64, attempt int) error {
	// 1. 已删项目和已完成或被替换的尝试不再执行。
	if _, err := s.repo.Project(ctx, u, p, false); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	row, err := s.repo.Episode(ctx, u, p, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status != "queued" || row.Attempt != attempt {
		return nil
	}
	// 2. 网络、文件和分析命令在数据库事务外执行。
	workCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	doc, workErr := s.analyzeReference(workCtx, row)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if workErr == nil {
		workErr = reference.ValidateDocument(doc, row.DurationMs)
	}
	failure := ""
	if workErr != nil {
		logger.Warn("参考集处理失败", zap.Uint64("reference_id", id), zap.Error(workErr))
		failure = "处理失败，请检查视频与拆片包后重试"
	}
	// 3. 再次持锁核对版本，迟到工作不能覆盖新稿。
	return s.finishReference(ctx, row, doc, failure)
}

// analyzeReference 解析上传包或在受控临时目录分析本人视频。
func (s *DramaReferenceService) analyzeReference(ctx context.Context, row *model.DramaReferenceEpisode) (model.DramaReferenceDocument, error) {
	var doc model.DramaReferenceDocument
	asset, err := s.assets.View(ctx, row.UserID, row.AssetID)
	if err != nil {
		return doc, err
	}
	if asset.Kind != "video" || asset.DurationMs != row.DurationMs {
		return doc, fmt.Errorf("参考视频已失效或时长改变")
	}
	if row.Method == "package" {
		return s.decode(row.Input, row.DurationMs)
	}
	if s.analyzer == nil || !s.analyzer.Available() {
		return doc, fmt.Errorf("本地分析器不可用")
	}
	dir, err := os.MkdirTemp("", "vc-reference-")
	if err != nil {
		return doc, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "source.mp4")
	if err := s.downloadReference(ctx, row, file); err != nil {
		return doc, err
	}
	return s.analyzer.Analyze(ctx, file, row.DurationMs)
}

// downloadReference 只读取本人素材，以有界流写入服务端生成的临时路径。
func (s *DramaReferenceService) downloadReference(ctx context.Context, row *model.DramaReferenceEpisode, path string) error {
	source, err := s.assets.Open(ctx, row.UserID, row.AssetID)
	if err != nil {
		return err
	}
	defer func() { _ = source.Body.Close() }()
	if source.Asset == nil || source.Asset.UserID != row.UserID || source.Asset.Kind != "video" {
		return errcode.ErrInvalidParams
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Create(filepath.Base(path))
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(file, io.LimitReader(&assetSrcReader{ctx: ctx, r: source.Body}, marketingMaxVideoSpool+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if n == 0 || n > marketingMaxVideoSpool {
		return fmt.Errorf("参考视频为空或超过 512 MB")
	}
	return closeErr
}

// finishReference 在当前集的尝试号与修订均吻合时保存结果。
func (s *DramaReferenceService) finishReference(ctx context.Context, expected *model.DramaReferenceEpisode, doc model.DramaReferenceDocument, failure string) error {
	err := s.repo.WithTx(ctx, func(tx repository.DramaReferenceTx) error {
		if _, err := tx.Project(ctx, expected.UserID, expected.DramaID, true); err != nil {
			return err
		}
		row, err := tx.Episode(ctx, expected.UserID, expected.DramaID, expected.ID)
		if err != nil {
			return err
		}
		if row.Status != "queued" || row.Attempt != expected.Attempt || row.Revision != expected.Revision {
			return nil
		}
		return commitReferenceResult(ctx, tx, row, doc, failure)
	})
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	return err
}

// commitReferenceResult 保留失败前的可用稿，成功时登记新的不可变修订。
func commitReferenceResult(ctx context.Context, tx repository.DramaReferenceTx, row *model.DramaReferenceEpisode, doc model.DramaReferenceDocument, failure string) error {
	row.Revision++
	row.Error = failure
	row.Status = "failed"
	if failure == "" {
		bindings, err := keptReferenceBindings(row, doc)
		if err != nil {
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
		row.Status = "ready"
		row.Input = nil
		if err := saveReferenceRevision(ctx, tx, row, row.Method); err != nil {
			return err
		}
	}
	return tx.SaveEpisode(ctx, row)
}

// keptReferenceBindings 只延续人物 ID 和名称均相同的人工绑定，新人物保持待确认。
func keptReferenceBindings(row *model.DramaReferenceEpisode, next model.DramaReferenceDocument) (map[string]string, error) {
	prior, err := referenceDocumentOf(row)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, character := range prior.Characters {
		names[character.ID] = character.Name
	}
	var bindings map[string]string
	if err := json.Unmarshal(row.Bindings, &bindings); err != nil {
		return nil, err
	}
	kept := map[string]string{}
	for _, character := range next.Characters {
		if names[character.ID] == character.Name && bindings[character.ID] != "" {
			kept[character.ID] = bindings[character.ID]
		}
	}
	return kept, nil
}
