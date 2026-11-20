package service

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
)

// remakeDuration 取足以容纳采用时间窗的最短模型秒数，枚举保留原始值以兼容字符串选项。
func remakeDuration(field modelcfg.ParamField, need int) (int, any, error) {
	invalid := errcode.ErrMarketingVideoModel.WithMsg("所选模型没有足够长的可用单镜时长")
	if !field.Open {
		n, err := strconv.Atoi(fmt.Sprint(field.Default))
		if err != nil || n < need {
			return 0, nil, invalid
		}
		return n, field.Default, nil
	}
	if field.Type == modelcfg.ParamEnum {
		var best int
		var option any
		for _, candidate := range field.Options {
			n, err := strconv.Atoi(fmt.Sprint(candidate))
			if err == nil && n >= need && (best == 0 || n < best) {
				best, option = n, candidate
			}
		}
		if best == 0 {
			return 0, nil, invalid
		}
		return best, option, nil
	}
	if field.Type != modelcfg.ParamNumber {
		return 0, nil, invalid
	}
	lo, step := 0, 1
	if field.Min != nil {
		lo = *field.Min
	}
	if field.Step != nil && *field.Step > 0 {
		step = *field.Step
	}
	n := lo + (max(0, need-lo)+step-1)/step*step
	if field.Max != nil && n > *field.Max {
		return 0, nil, invalid
	}
	return n, n, nil
}

// remakeShotQuote 按当前视频模型的参数、参考素材和定价估算单镜生成秒数与冻结积分。
func (s *MarketingService) remakeShotQuote(ctx context.Context, data *model.RemakePlanData, shot *model.RemakePlanShot) (int, int, error) {
	_, seconds, credits, err := s.remakeShotInput(ctx, data, shot)
	return seconds, credits, err
}

// remakeShotInput 用单镜的画面和旁白组装输入，将原素材占位 ID 在提交前替换为独立裁片。
func (s *MarketingService) remakeShotInput(ctx context.Context, data *model.RemakePlanData, shot *model.RemakePlanShot) (map[string]any, int, int, error) {
	snap, err := s.videoSnapshot(ctx, data.Settings.VideoModel)
	if err != nil {
		return nil, 0, 0, err
	}
	caps := snap.Model.Capabilities
	images := slices.Clone(data.ProductAssets)
	if data.ModelAssetID != nil {
		images = append(images, *data.ModelAssetID)
	}
	if err := checkRemakeRefs(caps, data, shot, images); err != nil {
		return nil, 0, 0, err
	}
	facts, err := remakeShotFacts(data, shot)
	if err != nil {
		return nil, 0, 0, err
	}
	field, ok := caps.Params.Get(modelcfg.DurationParam)
	if !ok {
		return nil, 0, 0, errcode.ErrMarketingVideoModel.WithMsg("所选模型未配置单镜生成时长")
	}
	seconds, value, err := remakeDuration(field, (shot.DurationMs+shot.TrimStartMs+999)/1000)
	if err != nil {
		return nil, 0, 0, err
	}
	input := map[string]any{}
	maps.Copy(input, data.Settings.Params)
	input["op"], input["duration"], input["images"], input["generate_audio"] = modelcfg.OpOmni, value, images, true
	input["prompt"] = remakePrompt(data, shot, facts)
	if len(shot.Sources) > 0 {
		input["videos"] = []uint64{*data.RefVideoAssetID}
	}
	if name := modelcfg.FanoutParam(caps); name != "" {
		input[name] = 1
	}
	norm, errs := modelcfg.ValidateInput(modelcfg.KindVideo, caps, input)
	if len(errs) > 0 {
		return nil, 0, 0, errcode.ErrInvalidParams.WithMsg(modelcfg.JoinFieldErrors(errs))
	}
	if modelcfg.FanoutCount(caps, norm) != 1 {
		return nil, 0, 0, errcode.ErrMarketingVideoModel.WithMsg("模型必须支持单镜生成一条视频")
	}
	credits := modelcfg.Quote(snap.Model.Pricing, caps, modelcfg.SpecFromInput(norm, caps.System))
	return input, seconds, max(credits, 0), nil
}

// checkRemakeRefs 对照模型实际接受的图片与视频处理数量，防止未开启的引用被输入规范化静默丢弃。
func checkRemakeRefs(caps modelcfg.Capabilities, data *model.RemakePlanData, shot *model.RemakePlanShot, images []uint64) error {
	if !hasOp(caps, modelcfg.OpOmni) || !caps.Refs.Image.On || len(images) == 0 || len(images) > caps.Refs.Image.Max {
		return errcode.ErrMarketingVideoModel.WithMsg("模型不支持本镜的商品图或模特图数量")
	}
	if shot.Unresolved {
		return errcode.ErrInvalidParams.WithMsg("请先确认本镜的原片来源")
	}
	if len(shot.Sources) == 0 {
		return nil
	}
	if data.RefVideoAssetID == nil {
		return errcode.ErrMarketingAssets.WithMsg("原片素材不存在")
	}
	if !caps.Refs.Video.On || caps.Refs.Video.Max < 1 {
		return errcode.ErrMarketingVideoModel.WithMsg("模型不支持单镜参考裁片")
	}
	var total int
	for _, src := range shot.Sources {
		total += src.EndMs - src.StartMs
	}
	if total > MarketingRefMaxSeconds*1000 {
		return errcode.ErrMarketingRefTooLong
	}
	return nil
}

// remakePrompt 保留目标镜画面、商品依据及用户旁白，在需要更长模型时长时说明要保留的剪辑窗口。
func remakePrompt(data *model.RemakePlanData, shot *model.RemakePlanShot, facts marketing.ProductFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "只生成一个商品镜头。按商品参考图保持商品外观。目标画面：%s。", shot.Scene)
	if len(shot.Sources) > 0 {
		b.WriteString("按裁片的动作和运镜跟拍，原片声音不作参考。")
	}
	fmt.Fprintf(&b, "主要动作落在 %.3f 至 %.3f 秒。", float64(shot.TrimStartMs)/1000, float64(shot.TrimStartMs+shot.DurationMs)/1000)
	if shot.Narration != "" {
		fmt.Fprintf(&b, "旁白（%s）：%s。", data.Settings.Language, shot.Narration)
	} else {
		b.WriteString("无旁白。")
	}
	fmt.Fprintf(&b, "商品：%s。外观：%s。", facts.Name, strings.Join(facts.Anchors, "、"))
	for _, point := range facts.SellingPoints {
		fmt.Fprintf(&b, "卖点：%s（%s）。", point.Text, point.Proof)
	}
	if len(facts.Prohibited) > 0 {
		fmt.Fprintf(&b, "禁用：%s。", strings.Join(facts.Prohibited, "、"))
	}
	return b.String()
}

// remakeShotFacts 在本镜当前稿上沿用营销脚本的品牌、背书和极限词检查。
func remakeShotFacts(data *model.RemakePlanData, shot *model.RemakePlanShot) (marketing.ProductFacts, error) {
	var facts marketing.ProductFacts
	if len(data.Product) > 0 {
		if err := json.Unmarshal(data.Product, &facts); err != nil {
			return facts, fmt.Errorf("解码商品事实卡：%w", err)
		}
	}
	reverse := &marketing.Reverse{Brands: data.Brands, Claims: data.Claims}
	flags := marketing.ScanScript([]marketing.ScriptShot{{Scene: shot.Scene, Narration: shot.Narration}}, data.Settings.Platform, facts, reverse)
	if len(flags) > 0 {
		return facts, errcode.ErrMarketingScriptBusy.WithMsg("本镜稿含有" + flagSummary(scriptFlags(flags)) + "，请先修改")
	}
	return facts, nil
}
