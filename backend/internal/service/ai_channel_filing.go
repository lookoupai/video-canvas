package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
)

// 虚拟人像库配置的长度限制。
const (
	filingGroupIDMaxLen     = 128
	filingProjectNameMaxLen = 64
)

// ChannelFilingView 是渠道的虚拟人像库配置视图。AK/SK 只写不读：只告诉调用方有没有设置。
type ChannelFilingView struct {
	Enabled      bool   `json:"enabled"`
	GroupID      string `json:"group_id"`
	ProjectName  string `json:"project_name"`
	Site         string `json:"site"` // 由 base_url 推断的站点：cn-beijing / ap-southeast-1
	AccessKeySet bool   `json:"access_key_set"`
	SecretKeySet bool   `json:"secret_key_set"`
}

// decodeFiling 解析渠道的虚拟人像库配置；正文损坏或为空按未启用处理，不让一行坏数据拖垮列表。
func decodeFiling(raw model.JSONText) model.ChannelFiling {
	var f model.ChannelFiling
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &f)
	}
	return f
}

// filingView 组装渠道的虚拟人像库视图。
func (s *AIChannelService) filingView(ctx context.Context, ch *model.AIChannel) (ChannelFilingView, error) {
	f := decodeFiling(ch.FilingJSON)
	akSet, err := s.secrets.SecretIsSet(ctx, model.ChannelFilingAccessKeyName(ch.Key))
	if err != nil {
		return ChannelFilingView{}, err
	}
	skSet, err := s.secrets.SecretIsSet(ctx, model.ChannelFilingSecretKeyName(ch.Key))
	if err != nil {
		return ChannelFilingView{}, err
	}
	return ChannelFilingView{
		Enabled: f.Enabled, GroupID: f.GroupID, ProjectName: f.ProjectName, Site: provider.FilingSiteFor(ch.BaseURL).Name,
		AccessKeySet: akSet, SecretKeySet: skSet,
	}, nil
}

// applyFiling 校验并套用虚拟人像库配置，返回校验问题；查库失败才返回 error。
// 启用时素材组 ID 必填、渠道必须是方舟 / BytePlus 官方地址；整个平台只允许一个渠道启用——真人图片只备案到一个渠道，两个都启用就说不清该备案到哪。
func (s *AIChannelService) applyFiling(ctx context.Context, next *model.AIChannel, in model.ChannelFiling) ([]string, error) {
	f := model.ChannelFiling{
		Enabled: in.Enabled, GroupID: strings.TrimSpace(in.GroupID), ProjectName: strings.TrimSpace(in.ProjectName),
	}
	var problems []string
	if utf8.RuneCountInString(f.GroupID) > filingGroupIDMaxLen || utf8.RuneCountInString(f.ProjectName) > filingProjectNameMaxLen {
		problems = append(problems, "虚拟人像库的素材组 ID 不超过 128 字、项目名不超过 64 字")
	}
	if f.Enabled {
		if f.GroupID == "" {
			problems = append(problems, "启用虚拟人像库时必须填写素材组 ID")
		}
		if !provider.IsOfficialFilingHost(next.BaseURL) {
			problems = append(problems, "虚拟人像库只支持方舟 / BytePlus 官方渠道（Base URL 为 ark.cn-beijing.volces.com 或 ark.ap-southeast.bytepluses.com）")
		}
		other, err := s.otherFilingChannel(ctx, next.Key)
		if err != nil {
			return nil, err
		}
		if other != "" {
			problems = append(problems, "渠道「"+other+"」已启用虚拟人像库，同一时间只能有一个渠道启用")
		}
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	next.FilingJSON = model.JSONText(raw)
	return problems, nil
}

// otherFilingChannel 返回除 self 之外已启用虚拟人像库的渠道名，没有返回空串。
func (s *AIChannelService) otherFilingChannel(ctx context.Context, self string) (string, error) {
	chans, err := s.repo.ListChannels(ctx)
	if err != nil {
		return "", err
	}
	for i := range chans {
		if chans[i].Key != self && decodeFiling(chans[i].FilingJSON).Enabled {
			return chans[i].Name, nil
		}
	}
	return "", nil
}

// SetFilingSecret 设置渠道虚拟人像库的 Access Key 与 Secret Key（火山引擎 / BytePlus IAM 凭证，存 ai_secrets），只写。
// 两项必须一起填：方舟签名同时要用到它们。审计只记谁改了哪个渠道，不含值。
func (s *AIChannelService) SetFilingSecret(ctx context.Context, actorID uint64, key, accessKey, secretKey string) error {
	// 1. 渠道必须存在：否则会写出没有主人的凭证
	if _, err := s.load(ctx, key); err != nil {
		return err
	}
	// 2. 两项都要有值（凭证服务也会去首尾空白，这里先拦下“只填一项”）
	if strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return errcode.ErrInvalidParams.WithMsg("Access Key 和 Secret Key 都要填写")
	}
	// 3. 加密入库
	if err := s.secrets.SetSecret(ctx, model.ChannelFilingAccessKeyName(key), accessKey, actorID); err != nil {
		return err
	}
	if err := s.secrets.SetSecret(ctx, model.ChannelFilingSecretKeyName(key), secretKey, actorID); err != nil {
		return err
	}
	// 4. 审计：detail 故意为空
	aiAudit(ctx, s.audit, actorID, model.AuditChannelFilingSecret, model.AuditTargetChannel, key, nil)
	return nil
}

// FilingTarget 返回当前的备案目标：启用了虚拟人像库、素材组 ID 与 AK/SK 都已配齐的那个渠道。
// 没有这样的渠道返回 nil, nil（真人库据此提示“管理员尚未配置”）。凭证明文只用于签名，调用方不得写进日志或响应。
func (s *AIChannelService) FilingTarget(ctx context.Context) (*provider.FilingTarget, error) {
	chans, err := s.repo.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	for i := range chans {
		f := decodeFiling(chans[i].FilingJSON)
		if !f.Enabled || f.GroupID == "" {
			continue
		}
		ak, err := s.secretOrEmpty(ctx, model.ChannelFilingAccessKeyName(chans[i].Key))
		if err != nil {
			return nil, err
		}
		sk, err := s.secretOrEmpty(ctx, model.ChannelFilingSecretKeyName(chans[i].Key))
		if err != nil {
			return nil, err
		}
		if ak == "" || sk == "" {
			return nil, nil
		}
		return &provider.FilingTarget{
			ChannelKey: chans[i].Key, BaseURL: chans[i].BaseURL, GroupID: f.GroupID, ProjectName: f.ProjectName,
			AccessKey: ak, SecretKey: sk,
		}, nil
	}
	return nil, nil
}

// secretOrEmpty 读凭证明文，没设置返回空串。服务端没配主密钥时同样按没设置处理：
// 那样根本存不进凭证，真人库应提示“未配置”，而不是让列表整个报错。
func (s *AIChannelService) secretOrEmpty(ctx context.Context, name string) (string, error) {
	v, err := s.secrets.Get(ctx, name)
	if errors.Is(err, ErrAISecretNotSet) || errors.Is(err, ErrAISecretKeyMissing) {
		return "", nil
	}
	return v, err
}
