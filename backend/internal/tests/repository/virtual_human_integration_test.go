package repository_test

import (
	"context"
	"errors"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// 本文件测试真人库仓储：图片的唯一约束、真人删除连带图片，以及按素材找出用到它的角色卡画布（jsonpath 粗筛）。

func TestVirtualHumanRepo_CreateDeleteAndDuplicate(t *testing.T) {
	db := isolatedDB(t, &model.VirtualHuman{}, &model.VirtualHumanImage{})
	repo := repository.NewVirtualHumanRepository(db)
	ctx := context.Background()

	h := &model.VirtualHuman{UserID: 1, Name: "迅哥儿"}
	img := &model.VirtualHumanImage{UserID: 1, AssetID: 11, Status: model.FilingPending}
	if err := repo.CreateHumanWithImage(ctx, h, img); err != nil {
		t.Fatalf("CreateHumanWithImage 失败：%v", err)
	}
	if h.ID == 0 || img.ID == 0 || img.HumanID != h.ID {
		t.Fatalf("ID 没回填：human=%d image=%d image.human=%d", h.ID, img.ID, img.HumanID)
	}

	// 同一用户同一张素材不能再进真人库：新建真人时整体回滚，加到已有真人也一样
	if err := repo.CreateHumanWithImage(ctx, &model.VirtualHuman{UserID: 1, Name: "重复"}, &model.VirtualHumanImage{UserID: 1, AssetID: 11}); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("重复素材应返回 ErrDuplicate，得到 %v", err)
	}
	if n, _ := repo.CountHumans(ctx, 1); n != 1 {
		t.Fatalf("重复素材时真人应整体回滚，现有 %d 个", n)
	}
	if err := repo.CreateImageInHuman(ctx, &model.VirtualHumanImage{UserID: 1, HumanID: h.ID, AssetID: 11}); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("重复素材应返回 ErrDuplicate，得到 %v", err)
	}
	// 别的用户可以用同一个素材 ID（素材归属由服务校验）；不存在的真人返回 ErrNotFound
	if err := repo.CreateImageInHuman(ctx, &model.VirtualHumanImage{UserID: 2, HumanID: h.ID, AssetID: 11}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("别人的真人应返回 ErrNotFound，得到 %v", err)
	}
	second := &model.VirtualHumanImage{UserID: 1, HumanID: h.ID, AssetID: 12, Sort: 1}
	if err := repo.CreateImageInHuman(ctx, second); err != nil {
		t.Fatalf("CreateImageInHuman 失败：%v", err)
	}

	got, err := repo.GetImageByAsset(ctx, 1, 12)
	if err != nil || got.ID != second.ID {
		t.Fatalf("GetImageByAsset = %+v, %v", got, err)
	}
	if _, err := repo.GetImageByAsset(ctx, 2, 12); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("别人的素材应返回 ErrNotFound，得到 %v", err)
	}

	// 删除真人连带删掉它的图片
	if err := repo.DeleteHuman(ctx, 2, h.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("删别人的真人应返回 ErrNotFound，得到 %v", err)
	}
	if err := repo.DeleteHuman(ctx, 1, h.ID); err != nil {
		t.Fatalf("DeleteHuman 失败：%v", err)
	}
	if imgs, _ := repo.ListImages(ctx, 1); len(imgs) != 0 {
		t.Fatalf("删除真人后还剩 %d 张图片", len(imgs))
	}
}

func TestCanvasRepo_CanvasesUsingAssets(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{})
	repo := repository.NewCanvasProjectRepository(db)
	ctx := context.Background()

	mk := func(userID uint64, title, payload string) *model.CanvasProject {
		p := &model.CanvasProject{UserID: userID, Title: title, PayloadJSON: datatypes.JSON(payload)}
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("建画布失败：%v", err)
		}
		return p
	}
	mainHit := mk(1, "主图", `{"nodes":[{"id":"n1","data":{"kind":"character","assetId":"11","character":{"name":"迅哥儿","extras":[]}}}]}`)
	extraHit := mk(1, "补充图", `{"nodes":[{"id":"n2","data":{"kind":"character","assetId":"99","character":{"name":"闰土","extras":[{"assetId":"12","src":""}]}}}]}`)
	mk(1, "普通图片节点", `{"nodes":[{"id":"n3","data":{"kind":"image","assetId":"11"}}]}`)
	mk(1, "无关", `{"nodes":[{"id":"n4","data":{"kind":"character","assetId":"50","character":{"name":"母亲","extras":[]}}}]}`)
	mk(2, "别人的", `{"nodes":[{"id":"n5","data":{"kind":"character","assetId":"11","character":{"name":"x","extras":[]}}}]}`)
	deleted := mk(1, "已删除", `{"nodes":[{"id":"n6","data":{"kind":"character","assetId":"11","character":{"name":"y","extras":[]}}}]}`)
	if err := repo.Delete(ctx, 1, deleted.ID); err != nil {
		t.Fatalf("删画布失败：%v", err)
	}

	rows, err := repo.CanvasesUsingAssets(ctx, 1, []string{"11", "12"})
	if err != nil {
		t.Fatalf("CanvasesUsingAssets 失败：%v", err)
	}
	if len(rows) != 2 || rows[0].ID != mainHit.ID || rows[1].ID != extraHit.ID {
		t.Fatalf("应只命中「主图」和「补充图」两张画布，得到 %+v", rows)
	}
	if rows[0].Title != "主图" || len(rows[0].Payload) == 0 {
		t.Fatalf("应带回标题和画布内容，得到 %+v", rows[0])
	}
	if rows, _ := repo.CanvasesUsingAssets(ctx, 1, nil); len(rows) != 0 {
		t.Fatalf("没有素材时应返回空，得到 %d 条", len(rows))
	}
}
