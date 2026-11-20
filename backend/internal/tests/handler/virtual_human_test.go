package handler_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 本文件测试真人库接口：路由取参、参数校验（400 + 10001）、业务错误码和统一响应。业务规则由 service 测试覆盖。

// hVH 是内存版真人库依赖：仓储、画布、素材、视图、备案目标和方舟客户端都由它充当，只覆盖 handler 测试用到的行为。
type hVH struct {
	humans map[uint64]*model.VirtualHuman
	images map[uint64]*model.VirtualHumanImage
	next   uint64
	usedBy string // 非空时所有图片都被这张画布的角色卡用着
}

func (v *hVH) id() uint64 { v.next++; return v.next }

func (v *hVH) ListHumans(_ context.Context, u uint64) ([]model.VirtualHuman, error) {
	var out []model.VirtualHuman
	for _, h := range v.humans {
		if h.UserID == u {
			out = append(out, *h)
		}
	}
	return out, nil
}
func (v *hVH) CountHumans(ctx context.Context, u uint64) (int64, error) {
	l, _ := v.ListHumans(ctx, u)
	return int64(len(l)), nil
}
func (v *hVH) GetHuman(_ context.Context, u, id uint64) (*model.VirtualHuman, error) {
	h, ok := v.humans[id]
	if !ok || h.UserID != u {
		return nil, repository.ErrNotFound
	}
	cp := *h
	return &cp, nil
}
func (v *hVH) RenameHuman(_ context.Context, u, id uint64, name string) error {
	h, ok := v.humans[id]
	if !ok || h.UserID != u {
		return repository.ErrNotFound
	}
	h.Name = name
	return nil
}
func (v *hVH) CreateHumanWithImage(_ context.Context, h *model.VirtualHuman, img *model.VirtualHumanImage) error {
	for _, x := range v.images {
		if x.UserID == img.UserID && x.AssetID == img.AssetID {
			return repository.ErrDuplicate
		}
	}
	h.ID = v.id()
	cp := *h
	v.humans[h.ID] = &cp
	img.ID, img.HumanID = v.id(), h.ID
	ci := *img
	v.images[img.ID] = &ci
	return nil
}
func (v *hVH) DeleteHuman(_ context.Context, u, id uint64) error {
	if _, ok := v.humans[id]; !ok {
		return repository.ErrNotFound
	}
	for k, img := range v.images {
		if img.HumanID == id {
			delete(v.images, k)
		}
	}
	delete(v.humans, id)
	return nil
}
func (v *hVH) ListImages(_ context.Context, u uint64) ([]model.VirtualHumanImage, error) {
	var out []model.VirtualHumanImage
	for _, img := range v.images {
		if img.UserID == u {
			out = append(out, *img)
		}
	}
	return out, nil
}
func (v *hVH) ListImagesByHuman(_ context.Context, u, hid uint64) ([]model.VirtualHumanImage, error) {
	var out []model.VirtualHumanImage
	for _, img := range v.images {
		if img.UserID == u && img.HumanID == hid {
			out = append(out, *img)
		}
	}
	return out, nil
}
func (v *hVH) GetImage(_ context.Context, u, id uint64) (*model.VirtualHumanImage, error) {
	img, ok := v.images[id]
	if !ok || img.UserID != u {
		return nil, repository.ErrNotFound
	}
	cp := *img
	return &cp, nil
}
func (v *hVH) GetImageByAsset(context.Context, uint64, uint64) (*model.VirtualHumanImage, error) {
	return nil, repository.ErrNotFound
}
func (v *hVH) CreateImageInHuman(_ context.Context, img *model.VirtualHumanImage) error {
	if _, ok := v.humans[img.HumanID]; !ok {
		return repository.ErrNotFound
	}
	img.ID = v.id()
	cp := *img
	v.images[img.ID] = &cp
	return nil
}
func (v *hVH) UpdateImageFiling(_ context.Context, img *model.VirtualHumanImage) error {
	cur, ok := v.images[img.ID]
	if !ok {
		return repository.ErrNotFound
	}
	cur.ArkAssetID, cur.Status, cur.Error, cur.ChannelKey = img.ArkAssetID, img.Status, img.Error, img.ChannelKey
	return nil
}
func (v *hVH) SetImageSort(_ context.Context, _, id uint64, s int) error {
	v.images[id].Sort = s
	return nil
}
func (v *hVH) DeleteImage(_ context.Context, _, id uint64) error {
	delete(v.images, id)
	return nil
}

// 画布：usedBy 非空时返回一张用了全部素材的画布
func (v *hVH) CanvasesUsingAssets(_ context.Context, _ uint64, ids []string) ([]model.CanvasPayloadRow, error) {
	if v.usedBy == "" || len(ids) == 0 {
		return nil, nil
	}
	payload := `{"nodes":[{"id":"n1","data":{"kind":"character","assetId":"` + ids[0] + `","character":{"name":"迅哥儿","extras":[]}}}]}`
	return []model.CanvasPayloadRow{{ID: 100, Title: v.usedBy, Payload: []byte(payload)}}, nil
}

// 素材：11、12 是用户 1 的图片
type hVHAssets struct{}

func (hVHAssets) Get(_ context.Context, u, id uint64) (*model.Asset, error) {
	if u != 1 || (id != 11 && id != 12) {
		return nil, provider.ErrAssetNotFound
	}
	return &model.Asset{ID: id, UserID: 1, Kind: "image"}, nil
}
func (a hVHAssets) Open(ctx context.Context, u, id uint64) (*provider.AssetFile, error) {
	row, err := a.Get(ctx, u, id)
	if err != nil {
		return nil, err
	}
	return &provider.AssetFile{Asset: row, Body: io.NopCloser(strings.NewReader("")), URL: "https://cdn/x", Remote: true}, nil
}
func (a hVHAssets) ListByIDs(ctx context.Context, ids []uint64) ([]model.Asset, error) {
	var out []model.Asset
	for _, id := range ids {
		if row, err := a.Get(ctx, 1, id); err == nil {
			out = append(out, *row)
		}
	}
	return out, nil
}
func (hVHAssets) ViewOf(_ context.Context, a *model.Asset) (*model.AssetView, error) {
	return &model.AssetView{ID: a.ID, Kind: a.Kind}, nil
}

// 备案目标与方舟
type hVHFiling struct{}

func (hVHFiling) FilingTarget(context.Context) (*provider.FilingTarget, error) {
	return &provider.FilingTarget{ChannelKey: "ark", AccessKey: "ak", SecretKey: "sk", GroupID: "g"}, nil
}
func (hVHFiling) CreateAsset(context.Context, provider.FilingTarget, string, string) (string, error) {
	return "asset-1", nil
}
func (hVHFiling) GetAsset(context.Context, provider.FilingTarget, string) (string, error) {
	return provider.FilingAssetActive, nil
}
func (hVHFiling) DeleteAsset(context.Context, provider.FilingTarget, string) error { return nil }

func newVHRouter() (*gin.Engine, *hVH) {
	v := &hVH{humans: map[uint64]*model.VirtualHuman{}, images: map[uint64]*model.VirtualHumanImage{}}
	svc := service.NewVirtualHumanService(service.VirtualHumanDeps{
		Repo: v, Canvases: v, Assets: hVHAssets{}, AssetRows: hVHAssets{}, Views: hVHAssets{},
		Filing: hVHFiling{}, Client: hVHFiling{},
	})
	h := NewVirtualHumanHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.GET("/virtual-humans", h.List)
	r.POST("/virtual-humans", h.Create)
	r.PATCH("/virtual-humans/:id", h.Rename)
	r.DELETE("/virtual-humans/:id", h.Remove)
	r.GET("/virtual-humans/:id/references", h.References)
	r.POST("/virtual-humans/:id/images", h.AddImage)
	r.PUT("/virtual-humans/:id/cover", h.SetCover)
	r.POST("/virtual-human-images/:id/retry", h.RetryImage)
	r.DELETE("/virtual-human-images/:id", h.RemoveImage)
	return r, v
}

func TestVirtualHumanHandler_CreateListRename(t *testing.T) {
	r, _ := newVHRouter()

	code, resp := characterCall(t, r, http.MethodPost, "/virtual-humans", `{"name":"迅哥儿","asset_id":11}`)
	if code != http.StatusCreated || responseCode(t, resp) != 0 {
		t.Fatalf("新建应 201：%d %+v", code, resp)
	}
	data := responseData(t, resp)
	hid, _ := data["id"].(string)
	if len(hid) != idcodec.EncodedLen || data["name"] != "迅哥儿" || data["status"] != model.FilingPending {
		t.Fatalf("响应不符合预期：%+v", data)
	}

	code, resp = characterCall(t, r, http.MethodGet, "/virtual-humans?refresh=1", "")
	if code != http.StatusOK {
		t.Fatalf("列表应 200：%d %+v", code, resp)
	}
	list := responseData(t, resp)
	items, _ := list["items"].([]any)
	if len(items) != 1 || list["filing_ready"] != true {
		t.Fatalf("列表不符合预期：%+v", list)
	}
	// refresh=1 把审核中的图片刷成可用
	if items[0].(map[string]any)["status"] != model.FilingActive {
		t.Fatalf("刷新后应可用：%+v", items[0])
	}

	code, resp = characterCall(t, r, http.MethodPatch, "/virtual-humans/"+hid, `{"name":"鲁迅"}`)
	if code != http.StatusOK || responseData(t, resp)["name"] != "鲁迅" {
		t.Fatalf("改名不符合预期：%d %+v", code, resp)
	}
}

func TestVirtualHumanHandler_ValidationAndBusinessErrors(t *testing.T) {
	r, v := newVHRouter()

	// 参数校验：缺 asset_id、id 不是十六进制串
	code, resp := characterCall(t, r, http.MethodPost, "/virtual-humans", `{"name":"a"}`)
	if code != http.StatusBadRequest || responseCode(t, resp) != errcode.ErrInvalidParams.Code {
		t.Fatalf("缺 asset_id 应 400 + 10001：%d %+v", code, resp)
	}
	code, resp = characterCall(t, r, http.MethodPatch, "/virtual-humans/123", `{"name":"a"}`)
	if code != http.StatusBadRequest || responseCode(t, resp) != errcode.ErrInvalidParams.Code {
		t.Fatalf("id 格式错误应 400 + 10001：%d %+v", code, resp)
	}

	// 业务错误：名称为空、素材不是自己的、真人不存在
	code, resp = characterCall(t, r, http.MethodPost, "/virtual-humans", `{"name":" ","asset_id":11}`)
	if code != http.StatusBadRequest || responseCode(t, resp) != errcode.ErrVirtualHumanInvalid.Code {
		t.Fatalf("名称为空应 69003：%d %+v", code, resp)
	}
	code, resp = characterCall(t, r, http.MethodPost, "/virtual-humans", `{"name":"a","asset_id":99}`)
	if code != http.StatusBadRequest || responseCode(t, resp) != errcode.ErrVirtualHumanInvalid.Code {
		t.Fatalf("素材不存在应 69003：%d %+v", code, resp)
	}
	missing := idcodec.Encode(9999)
	code, resp = characterCall(t, r, http.MethodPost, "/virtual-humans/"+missing+"/images", `{"asset_id":12}`)
	if code != http.StatusNotFound || responseCode(t, resp) != errcode.ErrVirtualHumanNotFound.Code {
		t.Fatalf("真人不存在应 404 + 69001：%d %+v", code, resp)
	}

	// 被角色卡用着时移除 409，并能查到引用
	_, resp = characterCall(t, r, http.MethodPost, "/virtual-humans", `{"name":"迅哥儿","asset_id":11}`)
	hid := responseData(t, resp)["id"].(string)
	v.usedBy = "故乡"
	code, resp = characterCall(t, r, http.MethodDelete, "/virtual-humans/"+hid, "")
	if code != http.StatusConflict || responseCode(t, resp) != errcode.ErrVirtualHumanInUse.Code {
		t.Fatalf("被引用时移除应 409 + 69006：%d %+v", code, resp)
	}
	code, resp = characterCall(t, r, http.MethodGet, "/virtual-humans/"+hid+"/references", "")
	refs, _ := responseData(t, resp)["items"].([]any)
	if code != http.StatusOK || len(refs) != 1 || refs[0].(map[string]any)["canvas_title"] != "故乡" {
		t.Fatalf("引用不符合预期：%d %+v", code, resp)
	}
}

func TestVirtualHumanHandler_ImagesAndRemove(t *testing.T) {
	r, _ := newVHRouter()
	_, resp := characterCall(t, r, http.MethodPost, "/virtual-humans", `{"name":"迅哥儿","asset_id":11}`)
	hid := responseData(t, resp)["id"].(string)

	code, resp := characterCall(t, r, http.MethodPost, "/virtual-humans/"+hid+"/images", `{"asset_id":12}`)
	if code != http.StatusCreated {
		t.Fatalf("加图应 201：%d %+v", code, resp)
	}
	images := responseData(t, resp)["images"].([]any)
	second := images[len(images)-1].(map[string]any)["id"].(string)

	code, resp = characterCall(t, r, http.MethodPut, "/virtual-humans/"+hid+"/cover", `{"image_id":"`+second+`"}`)
	if code != http.StatusOK {
		t.Fatalf("设封面应 200：%d %+v", code, resp)
	}
	code, resp = characterCall(t, r, http.MethodPut, "/virtual-humans/"+hid+"/cover", `{"image_id":123}`)
	if code != http.StatusBadRequest || responseCode(t, resp) != errcode.ErrInvalidParams.Code {
		t.Fatalf("image_id 不是十六进制串应 400：%d %+v", code, resp)
	}

	// 审核中的图片不能重新提交
	code, resp = characterCall(t, r, http.MethodPost, "/virtual-human-images/"+second+"/retry", "")
	if code != http.StatusConflict || responseCode(t, resp) != errcode.ErrVirtualHumanRetryInvalid.Code {
		t.Fatalf("审核中重新提交应 409 + 69011：%d %+v", code, resp)
	}

	code, resp = characterCall(t, r, http.MethodDelete, "/virtual-human-images/"+second, "")
	if code != http.StatusOK || responseCode(t, resp) != 0 {
		t.Fatalf("移除图片应 200：%d %+v", code, resp)
	}
	code, resp = characterCall(t, r, http.MethodDelete, "/virtual-humans/"+hid, "")
	if code != http.StatusOK || responseCode(t, resp) != 0 {
		t.Fatalf("移除真人应 200：%d %+v", code, resp)
	}
}
