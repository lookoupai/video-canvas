package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
)

// RemakeComposeArgs 使用固定合成快照和尝试号。手动重试可在上个 job 即将退出时重新入队。
type RemakeComposeArgs struct {
	UserID  uint64 `json:"user_id"`
	PlanID  uint64 `json:"plan_id"`
	ID      uint64 `json:"id"`
	Attempt int    `json:"attempt"`
}

// Kind 返回独立的逐镜合成 job 类型。
func (RemakeComposeArgs) Kind() string { return "marketing_remake_compose" }

// RunRef 关联到用户看到的合成记录。
func (a RemakeComposeArgs) RunRef() logger.RunRef {
	return logger.RunRef{Kind: "mkt_compose", ID: idcodec.Encode(a.ID)}
}

// InsertOpts 沿用营销队列并发和存活 job 去重。
func (RemakeComposeArgs) InsertOpts() river.InsertOpts { return MarketingRoundArgs{}.InsertOpts() }

type remakeComposeWorker struct {
	river.WorkerDefaults[RemakeComposeArgs]
	rt *Runtime
}

// Timeout 给读取素材和转码预留处理时间。
func (w *remakeComposeWorker) Timeout(*river.Job[RemakeComposeArgs]) time.Duration {
	return marketingRoundTimeout
}

// Work 持有共享并发配额才执行，服务按尝试号校验重复投递。
func (w *remakeComposeWorker) Work(ctx context.Context, j *river.Job[RemakeComposeArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	a := j.Args
	return w.rt.cfg.Marketing.HandleRemakeComposition(ctx, a.UserID, a.PlanID, a.ID, a.Attempt)
}

// EnqueueRemakeCompositionTx 将裁片合成 job 和清单写入同一事务。
func (rt *Runtime) EnqueueRemakeCompositionTx(ctx context.Context, tx *sql.Tx, userID, planID, id uint64, attempt int) error {
	if rt.cfg.Marketing == nil {
		return errors.New("营销合成执行器未配置")
	}
	args := RemakeComposeArgs{UserID: userID, PlanID: planID, ID: id, Attempt: attempt}
	if _, err := rt.client.InsertTx(ctx, tx, args, withRequestMeta(ctx, nil)); err != nil {
		return fmt.Errorf("入队营销合成失败：%w", err)
	}
	return nil
}
