package queue_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/repository"
)

type referenceQueueHandler struct {
	mu    sync.Mutex
	calls []int
}

func (s *referenceQueueHandler) HandleDramaReference(_ context.Context, _, _, _ uint64, attempt int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, attempt)
	return nil
}

func TestDramaReferenceQueueCommitAndRollback(t *testing.T) {
	db := queueDB(t)
	worker := &referenceQueueHandler{}
	rig := newRig(t, db, rigOpts{reference: worker})
	repo := repository.NewDramaReferenceRepository(db)
	repo.SetEnqueuer(rig.rt)
	err := repo.WithTx(t.Context(), func(tx repository.DramaReferenceTx) error { return tx.Enqueue(t.Context(), 7, 12, 19, 1) })
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "参考集处理被执行", func() bool {
		worker.mu.Lock()
		defer worker.mu.Unlock()
		return len(worker.calls) == 1 && worker.calls[0] == 1
	})
	_ = repo.WithTx(t.Context(), func(tx repository.DramaReferenceTx) error {
		if err := tx.Enqueue(t.Context(), 7, 12, 19, 2); err != nil {
			return err
		}
		return context.Canceled
	})
	var count int64
	if err := db.Table("river_job").Where("kind = ? AND args->>'attempt' = ?", "drama_reference", "2").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("回滚后的队列 %d %v", count, err)
	}
}
