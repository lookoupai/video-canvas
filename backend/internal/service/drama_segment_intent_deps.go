package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// DramaSegmentIntentRepo 保存不可变的生成输入并按内部键找回真实任务。
type DramaSegmentIntentRepo interface {
	// WithTx 在短剧项目锁内核对片段并保存输入。
	WithTx(context.Context, func(repository.DramaSegmentIntentTx) error) error
	// Intent 读取当前用户曾提交的同一请求。
	Intent(context.Context, uint64, string) (*model.DramaSegmentGenerationIntent, error)
	// IntentTask 读取输入对应的正式任务，允许模型下线后的结果恢复。
	IntentTask(context.Context, *model.DramaSegmentGenerationIntent) (*model.GenerationTask, error)
}

// DramaReferenceClipper 把已校验的原片区间制成无声参考视频，按素材归属复用缓存。
type DramaReferenceClipper interface {
	// Clip 返回当前用户的派生视频素材 ID，原片声音不会传给生成模型。
	Clip(context.Context, uint64, uint64, []RemakeMediaPart) (uint64, error)
}
