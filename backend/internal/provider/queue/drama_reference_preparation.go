package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
)

// DramaReferencePreparationHandler 处理一个独立媒体准备尝试。
type DramaReferencePreparationHandler interface {
	HandlePreparation(context.Context, uint64, uint64, uint64, uint64, int) error
}

// DramaReferencePreparationArgs 以准备 ID 和尝试号区分恢复任务。
type DramaReferencePreparationArgs struct {
	UserID        uint64 `json:"user_id"`
	DramaID       uint64 `json:"drama_id"`
	PreparationID uint64 `json:"preparation_id"`
	ReferenceID   uint64 `json:"reference_id"`
	Attempt       int    `json:"attempt"`
}

// Kind 返回参考集处理的队列类型。
func (DramaReferencePreparationArgs) Kind() string { return "drama_reference_preparation" }

// RunRef 关联用户可见的参考集。
func (a DramaReferencePreparationArgs) RunRef() logger.RunRef {
	return logger.RunRef{Kind: "drama_reference_preparation", ID: idcodec.Encode(a.PreparationID), UserID: a.UserID}
}

// InsertOpts 对同一参考集尝试使用存活队列去重。
func (DramaReferencePreparationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "drama_reference_preparation", MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

type dramaReferencePreparationWorker struct {
	river.WorkerDefaults[DramaReferencePreparationArgs]
	rt *Runtime
}

// Timeout 限制本地分析等待时间并给落库预留余量。
func (w *dramaReferencePreparationWorker) Timeout(*river.Job[DramaReferencePreparationArgs]) time.Duration {
	return 20 * time.Minute
}

// Work 在共享并发配额内处理参考集。
func (w *dramaReferencePreparationWorker) Work(ctx context.Context, j *river.Job[DramaReferencePreparationArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	a := j.Args
	return w.rt.cfg.DramaReferencePreparation.HandlePreparation(ctx, a.UserID, a.DramaID, a.ReferenceID, a.PreparationID, a.Attempt)
}

// EnqueueDramaReferencePreparationTx 在参考集输入事务中提交持久队列任务。
func (rt *Runtime) EnqueueDramaReferencePreparationTx(ctx context.Context, tx *sql.Tx, u, p, ref, id uint64, attempt int) error {
	if rt.cfg.DramaReferencePreparation == nil {
		return fmt.Errorf("参考集处理器未配置")
	}
	_, err := rt.client.InsertTx(ctx, tx, DramaReferencePreparationArgs{UserID: u, DramaID: p, ReferenceID: ref, PreparationID: id, Attempt: attempt}, withRequestMeta(ctx, nil))
	return err
}
