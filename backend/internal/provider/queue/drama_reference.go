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

// DramaReferenceHandler 处理一集的上传包或视频分析尝试。
type DramaReferenceHandler interface {
	HandleDramaReference(context.Context, uint64, uint64, uint64, int) error
}

// DramaReferenceArgs 以参考集 ID 和尝试号区分单集重试。
type DramaReferenceArgs struct {
	UserID      uint64 `json:"user_id"`
	DramaID     uint64 `json:"drama_id"`
	ReferenceID uint64 `json:"reference_id"`
	Attempt     int    `json:"attempt"`
}

// Kind 返回参考集处理的队列类型。
func (DramaReferenceArgs) Kind() string { return "drama_reference" }

// RunRef 关联用户可见的参考集。
func (a DramaReferenceArgs) RunRef() logger.RunRef {
	return logger.RunRef{Kind: "drama_reference", ID: idcodec.Encode(a.ReferenceID), UserID: a.UserID}
}

// InsertOpts 对同一参考集尝试使用存活队列去重。
func (DramaReferenceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "drama_reference", MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

type dramaReferenceWorker struct {
	river.WorkerDefaults[DramaReferenceArgs]
	rt *Runtime
}

// Timeout 限制本地分析等待时间并给落库预留余量。
func (w *dramaReferenceWorker) Timeout(*river.Job[DramaReferenceArgs]) time.Duration {
	return 20 * time.Minute
}

// Work 在共享并发配额内处理参考集。
func (w *dramaReferenceWorker) Work(ctx context.Context, j *river.Job[DramaReferenceArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	a := j.Args
	return w.rt.cfg.DramaReference.HandleDramaReference(ctx, a.UserID, a.DramaID, a.ReferenceID, a.Attempt)
}

// EnqueueDramaReferenceTx 在参考集输入事务中提交持久队列任务。
func (rt *Runtime) EnqueueDramaReferenceTx(ctx context.Context, tx *sql.Tx, u, p, id uint64, attempt int) error {
	if rt.cfg.DramaReference == nil {
		return fmt.Errorf("参考集处理器未配置")
	}
	_, err := rt.client.InsertTx(ctx, tx, DramaReferenceArgs{UserID: u, DramaID: p, ReferenceID: id, Attempt: attempt}, withRequestMeta(ctx, nil))
	return err
}
