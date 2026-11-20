package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// DramaSegmentIntentTx 在已有短剧项目锁内保存生成意图，不在事务内执行裁片或模型请求。
type DramaSegmentIntentTx interface {
	DramaTx
	// Intent 按用户与请求键读取固定输入，不存在返回 ErrNotFound。
	Intent(context.Context, uint64, string) (*model.DramaSegmentGenerationIntent, error)
	// InsertIntent 插入固定输入；同键并发返回已有记录供服务校验指纹。
	InsertIntent(context.Context, *model.DramaSegmentGenerationIntent) (*model.DramaSegmentGenerationIntent, error)
}

// DramaSegmentIntentRepository 复用短剧原语和项目锁，独立管理单镜生成的重传输入。
type DramaSegmentIntentRepository struct{ *DramaRepository }

// NewDramaSegmentIntentRepository 创建生成意图仓储。
func NewDramaSegmentIntentRepository(db *gorm.DB) *DramaSegmentIntentRepository {
	return &DramaSegmentIntentRepository{DramaRepository: NewDramaRepository(db)}
}

// WithTx 将片段修订检查与意图插入放在同一事务中。
func (r *DramaSegmentIntentRepository) WithTx(ctx context.Context, fn func(DramaSegmentIntentTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(NewDramaSegmentIntentRepository(tx))
	})
}

// Intent 仅查询当前用户的请求键，不存在返回 ErrNotFound。
func (r *DramaSegmentIntentRepository) Intent(ctx context.Context, userID uint64, key string) (*model.DramaSegmentGenerationIntent, error) {
	var row model.DramaSegmentGenerationIntent
	err := r.db.WithContext(ctx).Where("user_id = ? AND request_key = ?", userID, key).Take(&row).Error
	return &row, translate(err)
}

// InsertIntent 利用用户级唯一键处理跨项目的并发请求，始终返回最终保存的固定输入。
func (r *DramaSegmentIntentRepository) InsertIntent(ctx context.Context, row *model.DramaSegmentGenerationIntent) (*model.DramaSegmentGenerationIntent, error) {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
	if err != nil {
		return nil, err
	}
	return r.Intent(ctx, row.UserID, row.RequestKey)
}

// IntentTask 按用户、画布、节点和内部键恢复正式视频任务，不存在返回 ErrNotFound。
func (r *DramaSegmentIntentRepository) IntentTask(ctx context.Context, in *model.DramaSegmentGenerationIntent) (*model.GenerationTask, error) {
	var task model.GenerationTask
	err := r.db.WithContext(ctx).Where("user_id = ? AND canvas_project_id = ? AND node_id = ? AND idempotency_key = ? AND kind = ? AND is_test = false", in.UserID, in.CanvasID, "seg:"+idcodec.Encode(in.SegmentID), in.TaskKey, model.KindVideo).Take(&task).Error
	return &task, translate(err)
}
