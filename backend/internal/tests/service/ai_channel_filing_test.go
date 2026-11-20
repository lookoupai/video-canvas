package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
)

// 本文件测试渠道的虚拟人像库（方舟备案）配置：保存与校验、全平台只允许一个渠道启用、只写的 AK/SK，
// 以及真人库取备案目标（FilingTarget）时的「配齐才算可用」。

// officialInput 是方舟国内站官方地址的渠道，虚拟人像库只对这类渠道开放。
func officialInput() ChannelCreateInput {
	in := createInput()
	in.BaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	return in
}

func filingOn(group string) *model.ChannelFiling {
	return &model.ChannelFiling{Enabled: true, GroupID: group, ProjectName: " zhongzuo "}
}

func TestAIChannelService_UpdateFiling(t *testing.T) {
	ctx := context.Background()

	t.Run("保存：去首尾空白，视图带站点与密钥是否已设置，审计记字段名", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.BaseURL = "https://ark.cn-beijing.volces.com/api/v3"
		_, _ = e.svc.Create(ctx, in)
		e.repo.Audits = nil
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: filingOn(" group-1 "), ActorID: 5})
		if err != nil {
			t.Fatal(err)
		}
		f := view.Filing
		if !f.Enabled || f.GroupID != "group-1" || f.ProjectName != "zhongzuo" || f.Site != "cn-beijing" || f.AccessKeySet || f.SecretKeySet {
			t.Fatalf("视图不符合预期：%+v", f)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelUpdate {
			t.Fatalf("审计不符合预期：%v", got)
		}
		// 不传 filing 不改
		view, _ = e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Name: ptr("改名"), ActorID: 5})
		if !view.Filing.Enabled || view.Filing.GroupID != "group-1" {
			t.Fatalf("不传 filing 时配置应保留：%+v", view.Filing)
		}
	})

	t.Run("BytePlus 渠道识别为国际站", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.BaseURL = "https://ark.ap-southeast.bytepluses.com/api/v3"
		_, _ = e.svc.Create(ctx, in)
		view, _ := e.svc.Get(ctx, "main")
		if view.Filing.Site != "ap-southeast-1" || view.Filing.Enabled {
			t.Fatalf("视图不符合预期：%+v", view.Filing)
		}
	})

	t.Run("启用时素材组 ID 必填；关闭时不校验", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, officialInput())
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: filingOn("  "), ActorID: 5})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: &model.ChannelFiling{}, ActorID: 5}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("只有方舟 / BytePlus 官方地址的渠道能启用，关闭不受限", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput()) // gw.example.com 是第三方中转
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: filingOn("g"), ActorID: 5})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: &model.ChannelFiling{}, ActorID: 5}); err != nil {
			t.Fatalf("关闭虚拟人像库不应被拦：%v", err)
		}
		// 同一次请求里把地址改成官方域名就能启用；反过来启用后改成非官方地址会被拦
		official := "https://ark.ap-southeast.bytepluses.com/api/v3"
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", BaseURL: &official, Filing: filingOn("g"), ActorID: 5}); err != nil {
			t.Fatal(err)
		}
		third := "https://gw.example.com"
		_, err = e.svc.Update(ctx, ChannelUpdateInput{Key: "main", BaseURL: &third, Filing: filingOn("g"), ActorID: 5})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
	})

	t.Run("全平台只允许一个渠道启用", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, officialInput())
		other := officialInput()
		other.Key, other.Name = "second", "第二个"
		_, _ = e.svc.Create(ctx, other)
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: filingOn("g"), ActorID: 5}); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "second", Filing: filingOn("g2"), ActorID: 5})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
		// 已启用的渠道自己再保存不受影响
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: filingOn("g3"), ActorID: 5}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAIChannelService_SetFilingSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("两项都存入 ai_secrets，视图显示已设置，审计不含值", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		e.repo.Audits = nil
		if err := e.svc.SetFilingSecret(ctx, 6, "main", " AK1 ", " SK1 "); err != nil {
			t.Fatal(err)
		}
		if v, _ := e.cfg.Get(ctx, model.ChannelFilingAccessKeyName("main")); v != "AK1" {
			t.Fatalf("Access Key 应能读回：%q", v)
		}
		if v, _ := e.cfg.Get(ctx, model.ChannelFilingSecretKeyName("main")); v != "SK1" {
			t.Fatalf("Secret Key 应能读回：%q", v)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelFilingSecret || len(e.repo.Audits[0].DetailJSON) != 0 {
			t.Fatalf("审计不符合预期：%v", got)
		}
		view, _ := e.svc.Get(ctx, "main")
		if !view.Filing.AccessKeySet || !view.Filing.SecretKeySet || view.SecretSet {
			t.Fatalf("视图不符合预期：%+v", view)
		}
	})

	t.Run("缺一项 400，渠道不存在 404", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		adminWantCode(t, e.svc.SetFilingSecret(ctx, 6, "main", "AK", "  "), errcode.ErrInvalidParams.Code)
		adminWantCode(t, e.svc.SetFilingSecret(ctx, 6, "nope", "AK", "SK"), errcode.ErrChannelNotFound.Code)
	})

	t.Run("删除渠道连带删掉虚拟人像库的密钥", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		_ = e.svc.SetFilingSecret(ctx, 6, "main", "AK", "SK")
		if err := e.svc.Delete(ctx, 6, "main"); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.repo.Secrets[model.ChannelFilingAccessKeyName("main")]; ok {
			t.Fatal("Access Key 应已删除")
		}
		if _, ok := e.repo.Secrets[model.ChannelFilingSecretKeyName("main")]; ok {
			t.Fatal("Secret Key 应已删除")
		}
	})
}

func TestAIChannelService_FilingTarget(t *testing.T) {
	ctx := context.Background()
	e := newAchEnv(t)
	in := createInput()
	in.BaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	_, _ = e.svc.Create(ctx, in)

	// 没有渠道启用
	if got, err := e.svc.FilingTarget(ctx); err != nil || got != nil {
		t.Fatalf("没有启用时应返回 nil：%+v %v", got, err)
	}
	// 启用了但密钥没配齐也不算可用
	_, _ = e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Filing: filingOn("group-1"), ActorID: 5})
	if got, _ := e.svc.FilingTarget(ctx); got != nil {
		t.Fatalf("密钥没配齐时应返回 nil：%+v", got)
	}
	_ = e.svc.SetFilingSecret(ctx, 6, "main", "AK", "SK")
	got, err := e.svc.FilingTarget(ctx)
	if err != nil || got == nil {
		t.Fatalf("配齐后应返回目标：%+v %v", got, err)
	}
	if got.ChannelKey != "main" || got.GroupID != "group-1" || got.ProjectName != "zhongzuo" ||
		got.AccessKey != "AK" || got.SecretKey != "SK" || got.BaseURL != in.BaseURL {
		t.Fatalf("目标不符合预期：%+v", got)
	}
}
