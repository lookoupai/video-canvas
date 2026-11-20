// Package arkasset 调方舟（火山引擎 / BytePlus）虚拟人像库的资产管理 OpenAPI：创建、查询、删除素材。
//
// 真人照片直接当参考图传给 Seedance 会被输入风控拦下；先把图片作为素材上传到私域虚拟人像库，审核通过（Active）后，
// 生成请求里用 asset://<素材 ID> 代替图片地址即可。官方只提供轮询（GetAsset），没有回调。
// 参考：https://docs.volcengine.com/docs/ark/private-virtual-avatar-library-guide-preview
//
// 管理接口的站点由渠道 base_url 推断（provider.FilingSiteFor）；签名是火山引擎 / BytePlus 同构的 V4（HMAC-SHA256）。
package arkasset

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"video-canvas/internal/pkg/outbound"
	"video-canvas/internal/provider"
)

const (
	service    = "ark"
	apiVersion = "2024-01-01"
	// callTimeout 是一次管理接口调用的超时：接口都是同步返回的轻量操作，审核本身是异步的。
	callTimeout = 30 * time.Second
	// maxResponseBytes 是响应体上限，管理接口的响应只有几百字节。
	maxResponseBytes = 1 << 20
)

// SiteFor 按渠道 base_url 判断站点，见 provider.FilingSiteFor。
func SiteFor(baseURL string) provider.FilingSite { return provider.FilingSiteFor(baseURL) }

// Options 是客户端的可选设置，零值即生产配置。
type Options struct {
	// Endpoint 覆盖管理接口地址（含协议，如 http://127.0.0.1:8080），只给测试用；空串按站点域名走 https。
	Endpoint string
	// Now 是签名用的当前时间，测试里固定；nil 用 time.Now。
	Now func() time.Time
}

// Client 是虚拟人像库客户端。AK/SK 每次调用从 provider.FilingTarget 传入，不持有、不落日志
// （统一出站客户端记日志时会遮掉 Authorization 头）。
type Client struct {
	http     *http.Client
	endpoint string
	now      func() time.Time
}

// New 创建客户端；外部 HTTP 统一走 outbound，超时在每次调用的 context 里控制。
func New(opts Options) *Client {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Client{http: outbound.Client("filing.ark", nil), endpoint: opts.Endpoint, now: now}
}

// CreateAsset 把一张图片上传到素材组（异步审核），返回方舟素材 ID。imageURL 必须是方舟能从公网拉到的地址。
func (c *Client) CreateAsset(ctx context.Context, t provider.FilingTarget, imageURL, name string) (string, error) {
	body := map[string]any{
		"GroupId": t.GroupID, "URL": imageURL, "AssetType": "Image", "Name": name, "ProjectName": projectName(t),
	}
	resp, err := c.call(ctx, t, "CreateAsset", body)
	if err != nil {
		return "", err
	}
	id := stringField(resp, "Id")
	if id == "" {
		return "", fmt.Errorf("方舟 CreateAsset 响应缺少 Id")
	}
	return id, nil
}

// GetAsset 查询素材的审核状态，返回 provider.FilingAsset*：Active → active；Failed / Error / Canceled / Expired → failed
// （与 PR #80 实测时认的失败状态一致，免得出了结果还一直轮询）；其余按审核中。
func (c *Client) GetAsset(ctx context.Context, t provider.FilingTarget, assetID string) (string, error) {
	resp, err := c.call(ctx, t, "GetAsset", map[string]any{"Id": assetID, "ProjectName": projectName(t)})
	if err != nil {
		return "", err
	}
	switch strings.ToLower(stringField(resp, "Status")) {
	case "active":
		return provider.FilingAssetActive, nil
	case "failed", "error", "canceled", "cancelled", "expired":
		return provider.FilingAssetFailed, nil
	default:
		return provider.FilingAssetProcessing, nil
	}
}

// DeleteAsset 从素材组删除素材，删除不可恢复。
func (c *Client) DeleteAsset(ctx context.Context, t provider.FilingTarget, assetID string) error {
	_, err := c.call(ctx, t, "DeleteAsset", map[string]any{"Id": assetID, "ProjectName": projectName(t)})
	return err
}

// projectName 返回项目名，没填按方舟的默认项目 default。
func projectName(t provider.FilingTarget) string {
	if strings.TrimSpace(t.ProjectName) == "" {
		return "default"
	}
	return t.ProjectName
}

// call 发一次带 V4 签名的 POST 请求，返回解析后的 JSON；HTTP 非 200 或 ResponseMetadata.Error 都转成错误。
// 错误里只带上游的错误码和截断后的提示，不带请求内容。
func (c *Client) call(ctx context.Context, t provider.FilingTarget, action string, body map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	site := SiteFor(t.BaseURL)
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	query := url.Values{"Action": {action}, "Version": {apiVersion}}
	base := c.endpoint
	if base == "" {
		base = "https://" + site.Host
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/?"+query.Encode(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	for k, v := range sign(site, query, raw, t.AccessKey, t.SecretKey, c.now()) {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("方舟 %s 请求失败: %w", action, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("读取方舟 %s 响应失败: %w", action, err)
	}
	var obj map[string]any
	jsonErr := json.Unmarshal(data, &obj)
	if code, msg := upstreamError(obj); code != "" {
		return nil, fmt.Errorf("方舟 %s 返回错误 %s: %s", action, code, truncate(msg, 200))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("方舟 %s 返回 HTTP %d", action, resp.StatusCode)
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("方舟 %s 响应不是合法 JSON: %w", action, jsonErr)
	}
	return obj, nil
}

// upstreamError 取 ResponseMetadata.Error 的错误码与提示，没有错误返回空串。
func upstreamError(obj map[string]any) (code, msg string) {
	meta, _ := obj["ResponseMetadata"].(map[string]any)
	e, _ := meta["Error"].(map[string]any)
	if e == nil {
		return "", ""
	}
	code, _ = e["Code"].(string)
	msg, _ = e["Message"].(string)
	if code == "" {
		code = "Unknown"
	}
	return code, msg
}

// sign 生成 V4 签名需要的请求头。签名的规范化 query 和 URL 里的编码一致（url.Values.Encode：键排序、空格转 +）。
func sign(site provider.FilingSite, query url.Values, body []byte, ak, sk string, now time.Time) map[string]string {
	xDate := now.UTC().Format("20060102T150405Z")
	shortDate := xDate[:8]
	contentHash := sha256Hex(body)

	const signedHeaders = "content-type;host;x-content-sha256;x-date"
	canonicalRequest := strings.Join([]string{
		http.MethodPost,
		"/",
		canonicalQuery(query),
		"content-type:application/json\nhost:" + site.Host + "\nx-content-sha256:" + contentHash + "\nx-date:" + xDate + "\n",
		signedHeaders,
		contentHash,
	}, "\n")
	scope := shortDate + "/" + site.Region + "/" + service + "/request"
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))

	key := hmacSHA256([]byte(sk), shortDate)
	key = hmacSHA256(key, site.Region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	return map[string]string{
		"Content-Type":     "application/json",
		"Host":             site.Host,
		"X-Content-Sha256": contentHash,
		"X-Date":           xDate,
		"Authorization": fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
			ak, scope, signedHeaders, signature),
	}
}

// canonicalQuery 按键排序、逐项转义拼出规范化 query，与 url.Values.Encode 的结果一致。
func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, content string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(content))
	return h.Sum(nil)
}

// stringField 取 Result.<field>，兼容字段直接在顶层的响应。
func stringField(resp map[string]any, field string) string {
	if r, ok := resp["Result"].(map[string]any); ok {
		if s, ok := r[field].(string); ok && s != "" {
			return s
		}
	}
	s, _ := resp[field].(string)
	return s
}

// truncate 压掉多余空白并截断到 limit 个字节以内。
func truncate(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}
