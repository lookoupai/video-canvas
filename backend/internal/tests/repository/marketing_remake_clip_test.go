package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestMarketingRemakeClip_RacingUploadsAndOwnership(t *testing.T) {
	db := isolatedDB(t, &model.Asset{}, &model.MarketingRemakeClip{})
	repo := repository.NewMarketingRepository(db)
	ctx := t.Context()
	source := &model.Asset{ID: 17, UserID: 9, Kind: "video", StorageKey: "reference", MimeType: "video/mp4", Source: "upload"}
	if err := db.Create(source).Error; err != nil {
		t.Fatal(err)
	}
	results := make(chan struct {
		id  uint64
		err error
	}, 2)
	save := func(ctx context.Context, n int) {
		asset := &model.Asset{UserID: 9, Kind: "video", StorageKey: fmt.Sprintf("clip/%d", n), MimeType: "video/mp4", Source: "upload"}
		id, err := repo.KeepRemakeClip(ctx, 9, 17, "windows", asset)
		results <- struct {
			id  uint64
			err error
		}{id, err}
	}
	for n := range 2 {
		go save(ctx, n)
	}
	one, two := <-results, <-results
	if one.err != nil || two.err != nil || one.id != two.id || one.id == 0 {
		t.Fatalf("并发采用: %+v %+v", one, two)
	}
	var count int64
	if err := db.Model(&model.Asset{}).Where("id <> ?", source.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("应仅登记一份派生素材: %d %v", count, err)
	}
	got, err := repo.FindRemakeClip(ctx, 9, 17, "windows")
	if err != nil || got != one.id {
		t.Fatalf("缓存: %d %v", got, err)
	}
	for _, tc := range []struct {
		user, source uint64
		hash         string
	}{{8, 17, "windows"}, {9, 18, "windows"}, {9, 17, "different"}} {
		if _, err := repo.FindRemakeClip(ctx, tc.user, tc.source, tc.hash); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("不应复用 %+v: %v", tc, err)
		}
	}
	if err := db.Delete(&model.Asset{}, got).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindRemakeClip(ctx, 9, 17, "windows"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("派生对象已删除: %v", err)
	}
	save(ctx, 3)
	three := <-results
	if three.err != nil || three.id == got {
		t.Fatalf("重新裁片: %+v", three)
	}
}

func TestMarketingRemakeClip_RejectsUnavailableSource(t *testing.T) {
	for _, change := range []string{"deleted", "foreign", "image"} {
		t.Run(change, func(t *testing.T) {
			db := isolatedDB(t, &model.Asset{}, &model.MarketingRemakeClip{})
			repo := repository.NewMarketingRepository(db)
			source := &model.Asset{ID: 17, UserID: 9, Kind: "video", StorageKey: "reference", MimeType: "video/mp4", Source: "upload"}
			if err := db.Create(source).Error; err != nil {
				t.Fatal(err)
			}
			first := &model.Asset{UserID: 9, Kind: "video", StorageKey: "clip/first", MimeType: "video/mp4", Source: "upload"}
			if _, err := repo.KeepRemakeClip(t.Context(), 9, 17, "windows", first); err != nil {
				t.Fatal(err)
			}
			var err error
			switch change {
			case "deleted":
				err = db.Delete(source).Error
			case "foreign":
				err = db.Model(source).Update("user_id", 10).Error
			case "image":
				err = db.Model(source).Update("kind", "image").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.FindRemakeClip(t.Context(), 9, 17, "windows"); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("失效原片不能通过裁片缓存绕过归属校验: %v", err)
			}
			next := &model.Asset{UserID: 9, Kind: "video", StorageKey: "clip/next", MimeType: "video/mp4", Source: "upload"}
			if _, err := repo.KeepRemakeClip(t.Context(), 9, 17, "new-windows", next); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("原片失效后不能登记新裁片: %v", err)
			}
			var count int64
			if err := db.Model(&model.Asset{}).Where("storage_key = ?", next.StorageKey).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("拒绝后不得留下派生素材行: %d %v", count, err)
			}
		})
	}
}
