package arkasset_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/arkasset"
)

// 本文件测试方舟虚拟人像库客户端：站点判断、请求的 Action / 签名头 / 请求体，以及响应和上游错误的解析。

func TestSiteFor(t *testing.T) {
	cases := []struct {
		baseURL, host, region string
	}{
		{"https://ark.cn-beijing.volces.com/api/v3", "ark.cn-beijing.volcengineapi.com", "cn-beijing"},
		{"https://ark.ap-southeast.bytepluses.com/api/v3", "ark.ap-southeast-1.byteplusapi.com", "ap-southeast-1"},
		{"https://ARK.AP-SOUTHEAST.BYTEPLUSES.COM", "ark.ap-southeast-1.byteplusapi.com", "ap-southeast-1"},
		{"not a url", "ark.cn-beijing.volcengineapi.com", "cn-beijing"},
	}
	for _, c := range cases {
		s := arkasset.SiteFor(c.baseURL)
		if s.Host != c.host || s.Region != c.region {
			t.Errorf("SiteFor(%q) = %s/%s，期望 %s/%s", c.baseURL, s.Host, s.Region, c.host, c.region)
		}
	}
}

func TestIsOfficialFilingHost(t *testing.T) {
	cases := []struct {
		baseURL  string
		official bool
	}{
		{"https://ark.cn-beijing.volces.com/api/v3", true},
		{"https://ARK.CN-BEIJING.VOLCES.COM", true},
		{"https://ark.ap-southeast.bytepluses.com/api/v3", true},
		{"https://ark.cn-beijing.volces.com:443/api/v3", true},
		{"https://gw.example.com", false},
		{"https://ark.cn-beijing.volces.com.evil.example.com", false},
		{"https://example.com/ark.cn-beijing.volces.com", false},
		{"https://ark.byteplus.example.com", false},
		{"not a url", false},
		{"", false},
	}
	for _, c := range cases {
		if got := provider.IsOfficialFilingHost(c.baseURL); got != c.official {
			t.Errorf("IsOfficialFilingHost(%q) = %v，期望 %v", c.baseURL, got, c.official)
		}
	}
}

// recorded 是测试服务器收到的一次请求。
type recorded struct {
	action string
	header http.Header
	body   map[string]any
}

func newServer(t *testing.T, status int, reply string) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.action = r.URL.Query().Get("Action")
		rec.header = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rec.body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func target() provider.FilingTarget {
	return provider.FilingTarget{
		ChannelKey: "ark", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", GroupID: "group-1",
		AccessKey: "AKTEST", SecretKey: "SKTEST",
	}
}

func fixedNow() time.Time { return time.Date(2026, 11, 18, 8, 30, 0, 0, time.UTC) }

func TestCreateAsset_SignsAndParsesID(t *testing.T) {
	srv, rec := newServer(t, http.StatusOK, `{"ResponseMetadata":{"RequestId":"r"},"Result":{"Id":"asset-123"}}`)
	c := arkasset.New(arkasset.Options{Endpoint: srv.URL, Now: fixedNow})

	id, err := c.CreateAsset(context.Background(), target(), "https://cdn.example.com/a.png", "迅哥儿")
	if err != nil {
		t.Fatalf("CreateAsset 失败：%v", err)
	}
	if id != "asset-123" {
		t.Fatalf("id = %q", id)
	}
	if rec.action != "CreateAsset" {
		t.Errorf("Action = %q", rec.action)
	}
	// 请求体：素材组、地址、类型、名称，项目名没填按 default
	want := map[string]any{"GroupId": "group-1", "URL": "https://cdn.example.com/a.png", "AssetType": "Image", "Name": "迅哥儿", "ProjectName": "default"}
	for k, v := range want {
		if rec.body[k] != v {
			t.Errorf("请求体 %s = %v，期望 %v", k, rec.body[k], v)
		}
	}
	// 签名头：凭证范围带日期 / region / 服务名，签名头列表固定，Host 用的是站点的管理域名
	auth := rec.header.Get("Authorization")
	if !strings.HasPrefix(auth, "HMAC-SHA256 Credential=AKTEST/20261118/cn-beijing/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=") {
		t.Errorf("Authorization = %q", auth)
	}
	if strings.Contains(auth, "SKTEST") {
		t.Error("Authorization 里不能出现 Secret Key")
	}
	if rec.header.Get("X-Date") != "20261118T083000Z" || rec.header.Get("X-Content-Sha256") == "" {
		t.Errorf("X-Date = %q，X-Content-Sha256 = %q", rec.header.Get("X-Date"), rec.header.Get("X-Content-Sha256"))
	}
}

func TestCreateAsset_MissingIDIsError(t *testing.T) {
	srv, _ := newServer(t, http.StatusOK, `{"Result":{}}`)
	c := arkasset.New(arkasset.Options{Endpoint: srv.URL, Now: fixedNow})
	if _, err := c.CreateAsset(context.Background(), target(), "u", "n"); err == nil {
		t.Fatal("响应缺少 Id 时应返回错误")
	}
}

func TestGetAsset_NormalizesStatus(t *testing.T) {
	cases := map[string]string{
		"Active":     provider.FilingAssetActive,
		"Failed":     provider.FilingAssetFailed,
		"ERROR":      provider.FilingAssetFailed,
		"Canceled":   provider.FilingAssetFailed,
		"Expired":    provider.FilingAssetFailed,
		"Processing": provider.FilingAssetProcessing,
		"":           provider.FilingAssetProcessing,
	}
	for upstream, want := range cases {
		srv, rec := newServer(t, http.StatusOK, `{"Result":{"Id":"asset-1","Status":"`+upstream+`"}}`)
		c := arkasset.New(arkasset.Options{Endpoint: srv.URL, Now: fixedNow})
		got, err := c.GetAsset(context.Background(), target(), "asset-1")
		if err != nil {
			t.Fatalf("GetAsset(%q) 失败：%v", upstream, err)
		}
		if got != want {
			t.Errorf("上游 %q → %q，期望 %q", upstream, got, want)
		}
		if rec.action != "GetAsset" || rec.body["Id"] != "asset-1" {
			t.Errorf("Action = %q，Id = %v", rec.action, rec.body["Id"])
		}
	}
}

func TestDeleteAsset_UpstreamErrorAndHTTPStatus(t *testing.T) {
	// 业务错误嵌在 ResponseMetadata.Error 里，即使 HTTP 状态不是 200 也优先报它
	srv, rec := newServer(t, http.StatusBadRequest, `{"ResponseMetadata":{"Error":{"Code":"InvalidParameter","Message":"asset not found"}}}`)
	c := arkasset.New(arkasset.Options{Endpoint: srv.URL, Now: fixedNow})
	err := c.DeleteAsset(context.Background(), target(), "asset-1")
	if err == nil || !strings.Contains(err.Error(), "InvalidParameter") {
		t.Fatalf("应返回带上游错误码的错误，得到 %v", err)
	}
	if rec.action != "DeleteAsset" {
		t.Errorf("Action = %q", rec.action)
	}

	srv2, _ := newServer(t, http.StatusInternalServerError, `oops`)
	c2 := arkasset.New(arkasset.Options{Endpoint: srv2.URL, Now: fixedNow})
	if err := c2.DeleteAsset(context.Background(), target(), "asset-1"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("HTTP 500 应返回错误，得到 %v", err)
	}
}
