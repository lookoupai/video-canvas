package service_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// boardCanvas 是只给故事板计数用的画布读取：返回固定 payload 或错误，记下读了几次。
type boardCanvas struct {
	payload []byte
	err     error
	reads   int
}

func (c *boardCanvas) GetByID(_ context.Context, _, _ uint64) (*model.CanvasProject, error) {
	c.reads++
	if c.err != nil {
		return nil, c.err
	}
	return &model.CanvasProject{PayloadJSON: c.payload}, nil
}

// boardSegments 记下每次收到的故事板表，故事板数就是表的大小。
type boardSegments struct{ got []map[string]string }

func (s *boardSegments) Summary(_ context.Context, _, _ uint64, boards map[string]string) (model.DramaSegmentStats, error) {
	s.got = append(s.got, boards)
	return model.DramaSegmentStats{Count: 2, Boards: len(boards)}, nil
}

func TestDramaService_EpisodeBoardsFromCanvas(t *testing.T) {
	ctx := context.Background()
	r := newDramaMemory()
	r.episodes = []model.DramaEpisode{{BaseModel: model.BaseModel{ID: 31}, UserID: 7, DramaID: 1, No: 1}, {BaseModel: model.BaseModel{ID: 32}, UserID: 7, DramaID: 1, No: 2}}
	canvas := &boardCanvas{payload: []byte(`{"nodes":[{"id":"b","data":{"src":"/files/b.png","drama":{"episodes":[1],"board":{"segmentId":"5d2a","sourceHash":"h"}}}}]}`)}
	segments := &boardSegments{}
	s := service.NewDramaService(service.DramaDeps{Repo: r, Segments: segments, CanvasAssets: canvas})

	view, err := s.Get(ctx, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if view.Episodes[0].DoneBoardCount != 1 || view.Episodes[1].DoneBoardCount != 1 {
		t.Fatalf("集摘要带故事板数：%+v", view.Episodes)
	}
	if canvas.reads != 1 || segments.got[0]["5d2a"] != "h" {
		t.Fatalf("整部剧只读一次画布，故事板表交给片段统计：reads=%d got=%v", canvas.reads, segments.got)
	}

	// 画布不存在：不挡集摘要，按没有故事板算
	canvas.err = repository.ErrNotFound
	if view, err = s.Get(ctx, 7, 1); err != nil || view.Episodes[0].DoneBoardCount != 0 {
		t.Fatalf("画布不存在：%+v %v", view, err)
	}
	// 其他读取错误照常上报
	canvas.err = errors.New("数据库不可用")
	if _, err = s.Get(ctx, 7, 1); err == nil {
		t.Fatal("画布读取失败要上报")
	}
	// 坏的画布 JSON 也上报
	canvas.err, canvas.payload = nil, []byte(`{`)
	if _, err = s.Get(ctx, 7, 1); err == nil {
		t.Fatal("画布 JSON 坏了要上报")
	}
}
