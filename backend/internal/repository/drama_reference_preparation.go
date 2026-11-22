package repository

import (
	"context"
	"database/sql"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// DramaReferencePreparationTx 把准备、请求键和帧进度与队列放入同一事务。
type DramaReferencePreparationTx interface {
	// Reference 检查用户、项目和参考集并按需锁定项目。
	Reference(context.Context, uint64, uint64, uint64, bool) (*model.DramaReferenceEpisode, error)
	// Request 查询本人参考集的稳定指令键，不存在返回 ErrNotFound。
	Request(context.Context, uint64, uint64, uint64, string) (*model.DramaReferencePreparationRequest, error)
	// Preparation 查询本人项目参考集下的准备任务。
	Preparation(context.Context, uint64, uint64, uint64, uint64) (*model.DramaReferencePreparation, error)
	// ByRecipe 查询同一参考集内相同处理输入的准备任务。
	ByRecipe(context.Context, uint64, uint64, uint64, string) (*model.DramaReferencePreparation, error)
	// Preparations 按排他 ID 游标倒序分页读取。
	Preparations(context.Context, uint64, uint64, uint64, uint64, int) ([]model.DramaReferencePreparation, error)
	// Frames 按帧 ID 正序分页，镜号为空时读取当前准备全部镜头。
	Frames(context.Context, uint64, uint64, string, uint64, int) ([]model.DramaReferencePreparationFrame, error)
	// Frame 查询本人当前准备的一张帧图记录。
	Frame(context.Context, uint64, uint64, uint64) (*model.DramaReferencePreparationFrame, error)
	// PendingFrames 只返回未持久化素材的有限帧图。
	PendingFrames(context.Context, uint64, uint64, int) ([]model.DramaReferencePreparationFrame, error)
	// FrameCounts 计算准备的总帧数及已保存素材帧数。
	FrameCounts(context.Context, uint64, uint64) (int, int, error)
	// Save 在已锁定项目中保存准备状态，ID 为零时插入。
	Save(context.Context, *model.DramaReferencePreparation) error
	// SaveRequest 记录调用键及其输入指纹。
	SaveRequest(context.Context, *model.DramaReferencePreparationRequest) error
	// InsertFrames 按准备、镜号、时间点去重插入帧计划。
	InsertFrames(context.Context, []model.DramaReferencePreparationFrame) error
	// SaveFrame 在项目锁中保存本人准备的帧素材关联。
	SaveFrame(context.Context, *model.DramaReferencePreparationFrame) error
	// FrameCache 查询本人媒体缓存记录。
	FrameCache(context.Context, uint64, uint64) (*model.DramaReferenceFrameCache, error)
	// EnsureFrameCache 在唯一约束内登记或复用本人媒体缓存键。
	EnsureFrameCache(context.Context, *model.DramaReferenceFrameCache) error
	// SaveFrameCache 保存本人缓存记录对应的有效图片素材。
	SaveFrameCache(context.Context, *model.DramaReferenceFrameCache) error
	// Enqueue 将准备尝试写入当前事务中的持久队列。
	Enqueue(context.Context, uint64, uint64, uint64, uint64, int) error
}

// DramaReferencePreparationEnqueuer 向准备业务事务插入 River 任务。
type DramaReferencePreparationEnqueuer interface {
	EnqueueDramaReferencePreparationTx(context.Context, *sql.Tx, uint64, uint64, uint64, uint64, int) error
}

// DramaReferencePreparationRepository 提供准备任务与逐帧结果持久化。
type DramaReferencePreparationRepository struct {
	db  *gorm.DB
	enq DramaReferencePreparationEnqueuer
}

// NewDramaReferencePreparationRepository 创建媒体准备仓储。
func NewDramaReferencePreparationRepository(db *gorm.DB) *DramaReferencePreparationRepository {
	return &DramaReferencePreparationRepository{db: db}
}

// SetEnqueuer 装配同事务媒体准备队列。
func (r *DramaReferencePreparationRepository) SetEnqueuer(enq DramaReferencePreparationEnqueuer) {
	r.enq = enq
}

// WithTx 原子提交当前项目下准备、请求键和帧进度。
func (r *DramaReferencePreparationRepository) WithTx(ctx context.Context, fn func(DramaReferencePreparationTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&DramaReferencePreparationRepository{db: tx, enq: r.enq})
	})
}

// Reference 按本人存活项目读取参考集，缺失或越权返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) Reference(ctx context.Context, u, p, ref uint64, lock bool) (*model.DramaReferenceEpisode, error) {
	return NewDramaReferenceDraftRepository(r.db).Reference(ctx, u, p, ref, lock)
}

// Request 按用户、项目、参考集和键查询，缺失返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) Request(ctx context.Context, u, p, ref uint64, key string) (*model.DramaReferencePreparationRequest, error) {
	var row model.DramaReferencePreparationRequest
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ? AND reference_id = ? AND request_key = ?", u, p, ref, key).Take(&row).Error
	return &row, translate(err)
}

// Preparation 按用户、项目、参考集和 ID 查询，缺失返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) Preparation(ctx context.Context, u, p, ref, id uint64) (*model.DramaReferencePreparation, error) {
	var row model.DramaReferencePreparation
	err := r.preparationScope(ctx, u, p, ref).Where("id = ?", id).Take(&row).Error
	return &row, translate(err)
}

// ByRecipe 按用户项目参考集和 recipe 指纹查询，缺失返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) ByRecipe(ctx context.Context, u, p, ref uint64, hash string) (*model.DramaReferencePreparation, error) {
	var row model.DramaReferencePreparation
	err := r.preparationScope(ctx, u, p, ref).Where("recipe_hash = ?", hash).Take(&row).Error
	return &row, translate(err)
}

// Preparations 按用户参考集和倒序 ID 游标分页，未找到返回空列表。
func (r *DramaReferencePreparationRepository) Preparations(ctx context.Context, u, p, ref, cursor uint64, limit int) ([]model.DramaReferencePreparation, error) {
	rows := []model.DramaReferencePreparation{}
	query := r.preparationScope(ctx, u, p, ref)
	if cursor != 0 {
		query = query.Where("id < ?", cursor)
	}
	err := query.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// Save 在本人项目锁内创建或保存准备状态，更新不存在返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) Save(ctx context.Context, row *model.DramaReferencePreparation) error {
	if row.ID == 0 {
		return r.db.WithContext(ctx).Create(row).Error
	}
	result := r.preparationScope(ctx, row.UserID, row.DramaID, row.ReferenceID).Where("id = ?", row.ID).
		Select("document", "shot_ids", "status", "attempt", "total_frames", "completed_frames", "has_audio", "error", "updated_at").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveRequest 在已锁定项目内插入用户创建键，重复返回数据库约束错误。
func (r *DramaReferencePreparationRepository) SaveRequest(ctx context.Context, row *model.DramaReferencePreparationRequest) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// Enqueue 在准备状态事务内提交指定尝试，未装配队列返回错误。
func (r *DramaReferencePreparationRepository) Enqueue(ctx context.Context, u, p, ref, id uint64, attempt int) error {
	tx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok || r.enq == nil {
		return fmt.Errorf("媒体准备队列未配置或不在事务中")
	}
	return r.enq.EnqueueDramaReferencePreparationTx(ctx, tx, u, p, ref, id, attempt)
}

// preparationScope 固定本人项目参考集边界，后续查询只能进一步缩小范围。
func (r *DramaReferencePreparationRepository) preparationScope(ctx context.Context, u, p, ref uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.DramaReferencePreparation{}).
		Where("user_id = ? AND drama_id = ? AND reference_id = ?", u, p, ref)
}

// Frames 按本人准备、可选镜号和正序游标分页，无数据返回空列表。
func (r *DramaReferencePreparationRepository) Frames(ctx context.Context, u, prep uint64, shot string, cursor uint64, limit int) ([]model.DramaReferencePreparationFrame, error) {
	rows := []model.DramaReferencePreparationFrame{}
	query := r.frameScope(ctx, u, prep).Where("id > ?", cursor)
	if shot != "" {
		query = query.Where("shot_id = ?", shot)
	}
	err := query.Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

// Frame 按本人准备和帧 ID 查询，不匹配返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) Frame(ctx context.Context, u, prep, id uint64) (*model.DramaReferencePreparationFrame, error) {
	var row model.DramaReferencePreparationFrame
	err := r.frameScope(ctx, u, prep).Where("id = ?", id).Take(&row).Error
	return &row, translate(err)
}

// PendingFrames 按本人准备查询未持久化的有限帧计划。
func (r *DramaReferencePreparationRepository) PendingFrames(ctx context.Context, u, prep uint64, limit int) ([]model.DramaReferencePreparationFrame, error) {
	rows := []model.DramaReferencePreparationFrame{}
	err := r.frameScope(ctx, u, prep).Where("asset_id = 0").Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

// FrameCounts 统计本人准备的总数和持久化图片数，不把待处理帧算作完成。
func (r *DramaReferencePreparationRepository) FrameCounts(ctx context.Context, u, prep uint64) (int, int, error) {
	var counts struct {
		Total int
		Ready int
	}
	err := r.frameScope(ctx, u, prep).Select("COUNT(*) AS total, COUNT(*) FILTER (WHERE asset_id <> 0) AS ready").Scan(&counts).Error
	return counts.Total, counts.Ready, err
}

// InsertFrames 根据本人准备、镜号、时间点唯一约束合并采样计划。
func (r *DramaReferencePreparationRepository) InsertFrames(ctx context.Context, rows []model.DramaReferencePreparationFrame) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "preparation_id"}, {Name: "shot_id"}, {Name: "at_ms"}}, DoNothing: true}).CreateInBatches(rows, 100).Error
}

// SaveFrame 在项目锁中保存本人准备的图片关联，缺失返回 ErrNotFound。
func (r *DramaReferencePreparationRepository) SaveFrame(ctx context.Context, row *model.DramaReferencePreparationFrame) error {
	result := r.frameScope(ctx, row.UserID, row.PreparationID).Where("id = ?", row.ID).Select("asset_id", "updated_at").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// frameScope 按用户和准备 ID 限制每条帧查询，准备归属由服务先行核验。
func (r *DramaReferencePreparationRepository) frameScope(ctx context.Context, u, prep uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.DramaReferencePreparationFrame{}).Where("user_id = ? AND preparation_id = ?", u, prep)
}
