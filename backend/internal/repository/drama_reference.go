package repository

import (
	"context"
	"database/sql"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// DramaReferenceTx 包含同一项目下参考集、历史版本与角色绑定的原子读写。
type DramaReferenceTx interface {
	// DramaTx 复用当前事务中的分集、片段和任务原语。
	DramaTx() DramaTx
	// Adoptions 返回本人项目的不可变采用记录。
	Adoptions(context.Context, uint64, uint64) ([]model.DramaReferenceAdoption, error)
	// AdoptionByKey 按用户提交键查询采用，不存在返回 ErrNotFound。
	AdoptionByKey(context.Context, uint64, string) (*model.DramaReferenceAdoption, error)
	// InsertAdoption 创建采用记录以获得稳定 ID。
	InsertAdoption(context.Context, *model.DramaReferenceAdoption) error
	// FinishAdoption 在创建事务内补齐片段 ID 和完整快照。
	FinishAdoption(context.Context, *model.DramaReferenceAdoption) error
	// RememberAdoptionKey 在提交键锁和项目锁内登记指向同一采用的新键。
	RememberAdoptionKey(context.Context, *model.DramaReferenceAdoption) error
	// DraftForApply 按用户、项目、参考和草稿 ID 查询草稿，按需锁行。
	DraftForApply(context.Context, uint64, uint64, uint64, uint64, bool) (*model.DramaReferenceDraft, error)
	// SavePlanDraft 保存已采用状态及批准时的草稿修订。
	SavePlanDraft(context.Context, *model.DramaReferenceDraft) error
	// DraftApplicationByKey 按用户和操作键查询 apply 回执，不存在返回 ErrNotFound。
	DraftApplicationByKey(context.Context, uint64, string) (*model.DramaReferenceDraftApplication, error)
	// DraftApplicationByID 按用户、项目和回执 ID 查询 apply 回执，不存在返回 ErrNotFound。
	DraftApplicationByID(context.Context, uint64, uint64, uint64) (*model.DramaReferenceDraftApplication, error)
	// DraftApplicationForDraft 按用户与草稿修订查询唯一采用回执，不存在返回 ErrNotFound。
	DraftApplicationForDraft(context.Context, uint64, uint64, int64) (*model.DramaReferenceDraftApplication, error)
	// SaveDraftApplication 创建 apply 回执。
	SaveDraftApplication(context.Context, *model.DramaReferenceDraftApplication) error
	// DraftGenerationByKey 按用户和批次键查询生成回执，不存在返回 ErrNotFound。
	DraftGenerationByKey(context.Context, uint64, string) (*model.DramaReferenceDraftGeneration, error)
	// DraftGeneration 按用户、项目和生成回执 ID 查询。
	DraftGeneration(context.Context, uint64, uint64, uint64, bool) (*model.DramaReferenceDraftGeneration, error)
	// SaveDraftGeneration 创建或在行锁保护下更新批次回执。
	SaveDraftGeneration(context.Context, *model.DramaReferenceDraftGeneration) error
	Project(context.Context, uint64, uint64, bool) (*model.DramaProject, error)
	CreateProject(context.Context, *model.DramaProject) error
	LockCreateKey(context.Context, uint64, string) error
	ProjectByKey(context.Context, uint64, string) (*model.DramaProject, error)
	State(context.Context, uint64, uint64) (*model.DramaReferenceState, error)
	SaveState(context.Context, *model.DramaReferenceState) error
	Episodes(context.Context, uint64, uint64) ([]model.DramaReferenceEpisode, error)
	Episode(context.Context, uint64, uint64, uint64) (*model.DramaReferenceEpisode, error)
	EpisodeByKey(context.Context, uint64, uint64, string) (*model.DramaReferenceEpisode, error)
	SaveEpisode(context.Context, *model.DramaReferenceEpisode) error
	SetEpisodeOrder(context.Context, uint64, uint64, uint64, int) error
	InsertRevision(context.Context, *model.DramaReferenceRevision) error
	Versions(context.Context, uint64, uint64) ([]model.DramaReferenceRevision, error)
	Roles(context.Context, uint64, uint64) ([]model.DramaReferenceRole, error)
	SaveRole(context.Context, *model.DramaReferenceRole) error
	Canvas(context.Context, uint64, uint64) (*model.CanvasProject, error)
	Enqueue(context.Context, uint64, uint64, uint64, int) error
}

// DraftForApply 按用户、项目、参考和 ID 查询草稿并按需加锁，缺失返回 ErrNotFound。
func (r *DramaReferenceRepository) DraftForApply(ctx context.Context, u, p, ref, id uint64, lock bool) (*model.DramaReferenceDraft, error) {
	var row model.DramaReferenceDraft
	q := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ? AND reference_id = ? AND id = ?", u, p, ref, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&row).Error
	return &row, translate(err)
}

// SavePlanDraft 在项目锁内保存本人已采用草稿。
func (r *DramaReferenceRepository) SavePlanDraft(ctx context.Context, row *model.DramaReferenceDraft) error {
	result := r.db.WithContext(ctx).Model(row).Where("user_id = ? AND drama_id = ? AND reference_id = ? AND id = ?", row.UserID, row.DramaID, row.ReferenceID, row.ID).
		Select("revision", "status", "source", "session_id", "run_id", "updated_at").Updates(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DraftApplicationByKey 按用户和幂等键查询采用回执，缺失返回 ErrNotFound。
func (r *DramaReferenceRepository) DraftApplicationByKey(ctx context.Context, u uint64, key string) (*model.DramaReferenceDraftApplication, error) {
	var row model.DramaReferenceDraftApplication
	err := r.db.WithContext(ctx).Where("user_id = ? AND request_key = ?", u, key).Take(&row).Error
	return &row, translate(err)
}

// DraftApplicationByID 按用户项目和回执 ID 查询采用回执，缺失返回 ErrNotFound。
func (r *DramaReferenceRepository) DraftApplicationByID(ctx context.Context, u, p, id uint64) (*model.DramaReferenceDraftApplication, error) {
	var row model.DramaReferenceDraftApplication
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ? AND id = ?", u, p, id).Take(&row).Error
	return &row, translate(err)
}

// SaveDraftApplication 创建不可变草稿采用回执。
func (r *DramaReferenceRepository) SaveDraftApplication(ctx context.Context, row *model.DramaReferenceDraftApplication) error {
	if row.ID == 0 {
		return r.db.WithContext(ctx).Create(row).Error
	}
	return r.db.WithContext(ctx).Model(row).Where("user_id = ? AND drama_id = ? AND id = ?", row.UserID, row.DramaID, row.ID).
		Update("response", row.Response).Error
}

// DraftApplicationForDraft 按用户与草稿修订查询唯一采用回执，缺失返回 ErrNotFound。
func (r *DramaReferenceRepository) DraftApplicationForDraft(ctx context.Context, u, draft uint64, revision int64) (*model.DramaReferenceDraftApplication, error) {
	var row model.DramaReferenceDraftApplication
	err := r.db.WithContext(ctx).Where("user_id = ? AND draft_id = ? AND draft_revision = ?", u, draft, revision).Take(&row).Error
	return &row, translate(err)
}

// DraftGenerationByKey 按用户和幂等键查询生成批次，缺失返回 ErrNotFound。
func (r *DramaReferenceRepository) DraftGenerationByKey(ctx context.Context, u uint64, key string) (*model.DramaReferenceDraftGeneration, error) {
	var row model.DramaReferenceDraftGeneration
	err := r.db.WithContext(ctx).Where("user_id = ? AND request_key = ?", u, key).Take(&row).Error
	return &row, translate(err)
}

// DraftGeneration 按用户、项目和回执 ID 查询生成批次，缺失返回 ErrNotFound。
func (r *DramaReferenceRepository) DraftGeneration(ctx context.Context, u, p, id uint64, lock bool) (*model.DramaReferenceDraftGeneration, error) {
	var row model.DramaReferenceDraftGeneration
	q := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ? AND id = ?", u, p, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&row).Error
	return &row, translate(err)
}

// SaveDraftGeneration 创建批次回执或更新在事务中加锁的进度结果。
func (r *DramaReferenceRepository) SaveDraftGeneration(ctx context.Context, row *model.DramaReferenceDraftGeneration) error {
	if row.ID == 0 {
		return r.db.WithContext(ctx).Create(row).Error
	}
	return r.db.WithContext(ctx).Model(row).Where("user_id = ? AND drama_id = ? AND id = ?", row.UserID, row.DramaID, row.ID).
		Select("response", "status", "updated_at").Updates(row).Error
}

// DramaReferenceEnqueuer 把参考集处理与输入记录在同一事务提交。
type DramaReferenceEnqueuer interface {
	EnqueueDramaReferenceTx(context.Context, *sql.Tx, uint64, uint64, uint64, int) error
}

// DramaReferenceRepository 提供以项目为事务边界的参考资料存储。
type DramaReferenceRepository struct {
	db  *gorm.DB
	enq DramaReferenceEnqueuer
}

// NewDramaReferenceRepository 创建参考集仓储。
func NewDramaReferenceRepository(db *gorm.DB) *DramaReferenceRepository {
	return &DramaReferenceRepository{db: db}
}

// SetEnqueuer 装配后台处理队列。
func (r *DramaReferenceRepository) SetEnqueuer(enq DramaReferenceEnqueuer) { r.enq = enq }

// WithTx 在单一数据库事务中提交参考集与处理队列。
func (r *DramaReferenceRepository) WithTx(ctx context.Context, fn func(DramaReferenceTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&DramaReferenceRepository{db: tx, enq: r.enq}) })
}

// Project 按用户和项目查存活项目，可在修改前加行锁；不存在返回 ErrNotFound。
func (r *DramaReferenceRepository) Project(ctx context.Context, u, id uint64, lock bool) (*model.DramaProject, error) {
	return NewDramaRepository(r.db).Project(ctx, u, id, lock)
}

// CreateProject 创建项目及其专属画布。
func (r *DramaReferenceRepository) CreateProject(ctx context.Context, p *model.DramaProject) error {
	return NewDramaRepository(r.db).InsertProject(ctx, p)
}

// LockCreateKey 对用户级创建键加事务锁，避免重传建立两个空项目。
func (r *DramaReferenceRepository) LockCreateKey(ctx context.Context, u uint64, key string) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", fmt.Sprintf("drama-reference/%d/%s", u, key)).Error
}

// ProjectByKey 根据用户提交键返回存活项目，不存在返回 ErrNotFound。
func (r *DramaReferenceRepository) ProjectByKey(ctx context.Context, u uint64, key string) (*model.DramaProject, error) {
	var p model.DramaProject
	err := r.db.WithContext(ctx).Model(&model.DramaProject{}).Joins("JOIN drama_reference_states AS r ON r.drama_id = drama_projects.id AND r.user_id = drama_projects.user_id").
		Where("r.user_id = ? AND r.request_key = ?", u, key).Take(&p).Error
	return &p, translate(err)
}

// State 返回本人参考集顺序修订，首次建立前返回 ErrNotFound。
func (r *DramaReferenceRepository) State(ctx context.Context, u, id uint64) (*model.DramaReferenceState, error) {
	var row model.DramaReferenceState
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ?", u, id).Take(&row).Error
	return &row, translate(err)
}

// SaveState 在已锁项目内保存集合修订。
func (r *DramaReferenceRepository) SaveState(ctx context.Context, row *model.DramaReferenceState) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "drama_id"}}, DoUpdates: clause.AssignmentColumns([]string{"revision"})}).Create(row).Error
}

// Episodes 按用户项目及当前集序读取，无数据返回空表。
func (r *DramaReferenceRepository) Episodes(ctx context.Context, u, p uint64) ([]model.DramaReferenceEpisode, error) {
	var rows []model.DramaReferenceEpisode
	err := r.db.WithContext(ctx).Omit("input").Where("user_id = ? AND drama_id = ?", u, p).Order("seq,id").Find(&rows).Error
	return rows, err
}

// Episode 按用户、项目、参考集读取处理输入，不匹配返回 ErrNotFound。
func (r *DramaReferenceRepository) Episode(ctx context.Context, u, p, id uint64) (*model.DramaReferenceEpisode, error) {
	var row model.DramaReferenceEpisode
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ? AND id = ?", u, p, id).Take(&row).Error
	return &row, translate(err)
}

// EpisodeByKey 按项目内创建键复用参考集，不匹配返回 ErrNotFound。
func (r *DramaReferenceRepository) EpisodeByKey(ctx context.Context, u, p uint64, key string) (*model.DramaReferenceEpisode, error) {
	var row model.DramaReferenceEpisode
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ? AND request_key = ?", u, p, key).Take(&row).Error
	return &row, translate(err)
}

// SaveEpisode 在已锁项目内保存本人参考集，ID 为零时插入。
func (r *DramaReferenceRepository) SaveEpisode(ctx context.Context, row *model.DramaReferenceEpisode) error {
	if row.ID == 0 {
		return r.db.WithContext(ctx).Create(row).Error
	}
	return r.db.WithContext(ctx).Model(row).Where("user_id = ? AND drama_id = ?", row.UserID, row.DramaID).Select("*").Updates(row).Error
}

// SetEpisodeOrder 只更新本人项目参考集的顺序，不覆盖处理输入和校订稿。
func (r *DramaReferenceRepository) SetEpisodeOrder(ctx context.Context, u, p, id uint64, seq int) error {
	return r.db.WithContext(ctx).Model(&model.DramaReferenceEpisode{}).Where("user_id = ? AND drama_id = ? AND id = ?", u, p, id).Update("seq", seq).Error
}

// InsertRevision 保存不会被之后编辑覆盖的参考依据。
func (r *DramaReferenceRepository) InsertRevision(ctx context.Context, row *model.DramaReferenceRevision) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// Versions 按用户与参考集读取历史，最新修订在前。
func (r *DramaReferenceRepository) Versions(ctx context.Context, u, id uint64) ([]model.DramaReferenceRevision, error) {
	var rows []model.DramaReferenceRevision
	err := r.db.WithContext(ctx).Where("user_id = ? AND reference_id = ?", u, id).Order("revision DESC").Find(&rows).Error
	return rows, err
}

// Roles 按用户和项目读取跨集身份，ID 顺序稳定。
func (r *DramaReferenceRepository) Roles(ctx context.Context, u, p uint64) ([]model.DramaReferenceRole, error) {
	var rows []model.DramaReferenceRole
	err := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ?", u, p).Order("id").Find(&rows).Error
	return rows, err
}

// SaveRole 在已锁项目内新增或保存角色身份。
func (r *DramaReferenceRepository) SaveRole(ctx context.Context, row *model.DramaReferenceRole) error {
	if row.ID == 0 {
		return r.db.WithContext(ctx).Create(row).Error
	}
	return r.db.WithContext(ctx).Model(row).Where("user_id = ? AND drama_id = ?", row.UserID, row.DramaID).Select("*").Updates(row).Error
}

// Canvas 按用户读取存活的项目画布，不匹配返回 ErrNotFound。
func (r *DramaReferenceRepository) Canvas(ctx context.Context, u, id uint64) (*model.CanvasProject, error) {
	return NewCanvasProjectRepository(r.db).GetByID(ctx, u, id)
}

// Enqueue 将参考集尝试放入当前数据库事务。
func (r *DramaReferenceRepository) Enqueue(ctx context.Context, u, p, id uint64, attempt int) error {
	tx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok || r.enq == nil {
		return fmt.Errorf("参考集队列未配置或不在事务中")
	}
	return r.enq.EnqueueDramaReferenceTx(ctx, tx, u, p, id, attempt)
}
