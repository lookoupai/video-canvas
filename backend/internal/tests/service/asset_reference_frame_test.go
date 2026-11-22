package service_test

import (
	"testing"

	"video-canvas/internal/config"
)

func TestAssetReferenceFramePersistsOnceAndRepairsMissingObject(t *testing.T) {
	svc, repo, store := newAssetTestService(config.Storage{})
	data := assetTestPNG(t, 32, 24)
	first, err := svc.SaveReferenceFrame(t.Context(), 7, 12, 9, "001122334455667788990011223344556677889900112233", data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SaveReferenceFrame(t.Context(), 7, 12, 9, "001122334455667788990011223344556677889900112233", data)
	if err != nil || first.ID != second.ID || len(repo.rows) != 1 {
		t.Fatalf("幂等存储：%+v %v", second, err)
	}
	delete(store.objects, repo.rows[first.ID].StorageKey)
	repaired, err := svc.SaveReferenceFrame(t.Context(), 7, 12, 9, "001122334455667788990011223344556677889900112233", data)
	if err != nil || repaired.ID != first.ID {
		t.Fatalf("缺文件恢复：%+v %v", repaired, err)
	}
	if len(store.objects) != 1 {
		t.Fatal("没有恢复真实图片")
	}
	other, err := svc.SaveReferenceFrame(t.Context(), 8, 12, 9, "001122334455667788990011223344556677889900112233", data)
	if err != nil || other.ID == first.ID {
		t.Fatalf("跨用户复用：%+v %v", other, err)
	}
}
