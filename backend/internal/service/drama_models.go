package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// 分镜单段时长的合法范围与缺省，与前端 STORYBOARD_SECONDS 一致。
const (
	storyboardMinSeconds      = 4
	storyboardMaxSecondsLimit = 60
	storyboardDefaultSeconds  = 15
)

// checkAssetModel 校验短剧里指定的视频 / 图片模型：空串放行；非空必须是后台已启用且种类匹配的模型。
func (s *DramaService) checkAssetModel(ctx context.Context, key, kind, label string) error {
	if key == "" {
		return nil
	}
	invalid := errcode.ErrInvalidParams.WithMsg(label + "不可用，请重新选择")
	if s.registry == nil {
		return invalid
	}
	snap, err := s.registry.Snapshot(ctx, key)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return invalid
	}
	if err != nil {
		return err
	}
	if snap == nil || snap.Model.Kind != kind {
		return invalid
	}
	return nil
}

// checkDramaModels 校验视频模型、三类资产和故事板的图片模型，nil 表示该项不改。
func (s *DramaService) checkDramaModels(ctx context.Context, video *string, images *model.DramaImageModels) error {
	if video != nil {
		if err := s.checkAssetModel(ctx, *video, modelcfg.KindVideo, "视频模型"); err != nil {
			return err
		}
	}
	if images == nil {
		return nil
	}
	for _, item := range []struct{ key, label string }{{images.Character, "人物图片模型"}, {images.Scene, "场景图片模型"}, {images.Prop, "物品图片模型"}, {images.Board, "故事板图片模型"}} {
		if err := s.checkAssetModel(ctx, item.key, modelcfg.KindImage, item.label); err != nil {
			return err
		}
	}
	return nil
}

// dramaImageParamNames 是资产图片能预设的参数：比例（两种写法）和分辨率。
var dramaImageParamNames = map[string]bool{"aspect_ratio": true, "ratio": true, "resolution": true}

// checkDramaImageParams 校验三类资产的默认生成参数：参数名限比例、分辨率，值非空；
// 该类指定了模型时，值必须是这个模型对应枚举参数的选项。跟随默认模型的类型只查写法，由前端按默认模型选好。
func (s *DramaService) checkDramaImageParams(ctx context.Context, models model.DramaImageModels, params model.DramaImageParams) error {
	for _, item := range []struct {
		key    string
		params map[string]string
		label  string
	}{{models.Character, params.Character, "人物"}, {models.Scene, params.Scene, "场景"}, {models.Prop, params.Prop, "物品"}} {
		if err := s.checkAssetParams(ctx, item.key, item.params, item.label); err != nil {
			return err
		}
	}
	return nil
}

// checkAssetParams 校验一类资产的默认生成参数，key 为这类资产的图片模型（空串表示跟随默认）。
func (s *DramaService) checkAssetParams(ctx context.Context, key string, params map[string]string, label string) error {
	invalid := errcode.ErrInvalidParams.WithMsg(label + "图片参数不可用，请重新选择")
	for name, value := range params {
		if !dramaImageParamNames[name] || value == "" || len(value) > 32 {
			return invalid
		}
	}
	if key == "" || len(params) == 0 {
		return nil
	}
	if s.registry == nil {
		return invalid
	}
	snap, err := s.registry.Snapshot(ctx, key)
	if err != nil && !errors.Is(err, provider.ErrModelUnavailable) {
		return err
	}
	if snap == nil {
		return invalid
	}
	for name, value := range params {
		field, ok := snap.Model.Capabilities.Params.Get(name)
		if !ok || field.Type != modelcfg.ParamEnum || !hasOption(field.Options, value) {
			return invalid
		}
	}
	return nil
}

// hasOption 判断枚举选项里有没有 value（数字与字符串写法互认）。
func hasOption(options []any, value string) bool {
	for _, option := range options {
		if fmt.Sprint(option) == value {
			return true
		}
	}
	return false
}

// dramaImageParams 解码项目保存的三类图片参数；坏数据按未指定处理。
func dramaImageParams(p *model.DramaProject) model.DramaImageParams {
	var out model.DramaImageParams
	if len(p.ImageParams) > 0 {
		_ = json.Unmarshal(p.ImageParams, &out)
	}
	return out.Normalized()
}

// dramaImageModels 解码项目保存的图片模型（三类资产 + 故事板）；坏数据按未指定处理。
func dramaImageModels(p *model.DramaProject) model.DramaImageModels {
	var out model.DramaImageModels
	if len(p.ImageModels) > 0 {
		_ = json.Unmarshal(p.ImageModels, &out)
	}
	return out
}

// storyboardSeconds 分镜单段时长上限：请求显式给了以请求为准；否则取项目视频模型的时长上限（夹在 4–60），
// 项目没配视频模型、模型已不可用或没有时长参数时回落 15。估价与提交共用，保证两边一致。
func (s *DramaService) storyboardSeconds(ctx context.Context, p *model.DramaProject, req *model.DramaJobReq) (int, error) {
	if req.MaxSeconds != 0 {
		return req.MaxSeconds, nil
	}
	if p.VideoModel == "" || s.registry == nil {
		return storyboardDefaultSeconds, nil
	}
	snap, err := s.registry.Snapshot(ctx, p.VideoModel)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return storyboardDefaultSeconds, nil
	}
	if err != nil {
		return 0, err
	}
	if snap == nil {
		return storyboardDefaultSeconds, nil
	}
	maxSeconds, ok := modelMaxSeconds(snap.Model.Capabilities)
	if !ok {
		return storyboardDefaultSeconds, nil
	}
	return min(storyboardMaxSecondsLimit, max(storyboardMinSeconds, int(math.Floor(maxSeconds)))), nil
}

// modelMaxSeconds 读模型 duration 参数的上限：number 取 max，enum 取数字选项里最大的；没有时长参数返回 false。
func modelMaxSeconds(caps modelcfg.Capabilities) (float64, bool) {
	field, ok := caps.Params.Get("duration")
	if !ok {
		return 0, false
	}
	if field.Type == modelcfg.ParamNumber {
		if field.Max == nil {
			return 0, false
		}
		return float64(*field.Max), true
	}
	best, found := 0.0, false
	for _, option := range field.Options {
		n, ok := optionNumber(option)
		if ok && (!found || n > best) {
			best, found = n, true
		}
	}
	return best, found
}

// optionNumber 把枚举选项（字符串或数字）转成数字，无法转换返回 false。
func optionNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case fmt.Stringer:
		return optionNumber(n.String())
	}
	return 0, false
}
