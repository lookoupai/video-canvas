package service

import (
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// validateAgnesPlan applies known upstream restrictions to the normalized request.
func validateAgnesPlan(snap *provider.Snapshot, route string, input map[string]any) []model.DramaReferencePlanIssue {
	issues := []model.DramaReferencePlanIssue{}
	issues = append(issues, validateAgnesVideoMode(snap, input)...)
	issues = append(issues, validateAgnesOutput(snap, input)...)
	issues = append(issues, validateAgnesExplicitMode(route, input)...)
	issues = append(issues, validateAgnesVideoWindow(route, input)...)
	issues = append(issues, validateAgnesImageLimits(snap, input)...)
	return issues
}

// validateAgnesVideoMode blocks Flash video references and enforces its fixed output size.
func validateAgnesVideoMode(snap *provider.Snapshot, input map[string]any) []model.DramaReferencePlanIssue {
	if !isAgnesFlash(snap) {
		return nil
	}
	issues := []model.DramaReferencePlanIssue{}
	if len(mediaReferencesByKey(modelcfg.MediaRefs(input), "videos")) > 0 {
		issues = append(issues, planIssue("protocol_video_unsupported", "mode", "Agnes Video 2.5 Flash 不接受视频参考", true))
	}
	if paramString(input, "resolution") != "720P" {
		issues = append(issues, planIssue("protocol_resolution_unsupported", "resolution", "Agnes Flash 仅支持 720P", true))
	}
	return issues
}

// validateAgnesOutput checks the protocol's supported duration, output count, and resolution.
func validateAgnesOutput(snap *provider.Snapshot, input map[string]any) []model.DramaReferencePlanIssue {
	issues := []model.DramaReferencePlanIssue{}
	duration := inputDuration(input)
	if duration < 4 || duration > 12 {
		issues = append(issues, planIssue("protocol_output_duration", "duration", "Agnes 视频申请时长须为 4–12 秒", true))
	}
	if countOutput(input) != 1 {
		issues = append(issues, planIssue("protocol_output_count", "n", "Agnes 首期仿拍每镜只支持生成一个结果", true))
	}
	resolution := paramString(input, "resolution")
	if resolution != "" && !containsRoute([]string{"720P", "1080P", "1K", "2K"}, resolution) {
		issues = append(issues, planIssue("protocol_resolution_unsupported", "resolution", "Agnes 不支持当前输出分辨率", true))
	}
	return issues
}

// validateAgnesExplicitMode prevents a model parameter from overriding the selected media route.
func validateAgnesExplicitMode(route string, input map[string]any) []model.DramaReferencePlanIssue {
	mode := paramString(input, "mode")
	if mode != "" && mode != agnesRouteMode(route) {
		return []model.DramaReferencePlanIssue{planIssue("protocol_mode_conflict", "mode", "模型参数会覆盖单镜选择的 Agnes 生成路线", true)}
	}
	return nil
}

// validateAgnesVideoWindow records that the created silent clip must be checked before submission.
func validateAgnesVideoWindow(route string, input map[string]any) []model.DramaReferencePlanIssue {
	if route != "video" || len(mediaReferencesByKey(modelcfg.MediaRefs(input), "videos")) == 0 {
		return nil
	}
	return []model.DramaReferencePlanIssue{planIssue("protocol_video_window_review", "mode", "视频片段需在任务提交时复核 2–12 秒、50 MB 与 24–60 FPS 协议上限", false)}
}

// validateAgnesImageLimits applies the upstream image-count restriction for each model variant.
func validateAgnesImageLimits(snap *provider.Snapshot, input map[string]any) []model.DramaReferencePlanIssue {
	refs := mediaReferencesByKey(modelcfg.MediaRefs(input), "images")
	maxCount := 8
	if isAgnesFlash(snap) {
		maxCount = 5
	}
	if len(refs) > maxCount {
		return []model.DramaReferencePlanIssue{planIssue("protocol_image_count", "images", fmt.Sprintf("当前 Agnes 模型每次最多接受 %d 张图片", maxCount), true)}
	}
	return nil
}

// validateReferenceSourceVideo checks source ownership metadata and the selected window for this model.
func validateReferenceSourceVideo(asset *model.Asset, snap *provider.Snapshot, shot model.DramaReferenceShot) *model.DramaReferencePlanIssue {
	if asset == nil || asset.Kind != "video" || asset.ID == 0 {
		return ptrIssue(planIssue("asset_unavailable", "source_asset_id", "原片视频素材不可用", true))
	}
	if !snap.Model.Capabilities.Refs.Video.On {
		return ptrIssue(planIssue("model_route_unsupported", "mode", "所选模型配置不接受视频参考", true))
	}
	maxMB := snap.Model.Capabilities.Refs.Video.MaxMB
	if maxMB > 0 && asset.ByteSize > int64(maxMB)<<20 {
		return ptrIssue(planIssue("protocol_video_size_review", "mode", "原片超过模型单文件视频上限，裁片会在提交前重新校验", false))
	}
	length := shot.EndMs - shot.StartMs
	if agnesPlugin(snap) && (length < 2000 || length > 12000) {
		return ptrIssue(planIssue("protocol_video_duration", "mode", "Agnes 单镜视频参考长度须为 2–12 秒，请选择图片路线", true))
	}
	return nil
}

// validateReferenceImage checks image metadata and Agnes dimensions before task submission.
func validateReferenceImage(asset *model.Asset, snap *provider.Snapshot) *model.DramaReferencePlanIssue {
	if asset.Kind != "image" || asset.ByteSize < 1 || asset.ByteSize > int64(snap.Model.Capabilities.Refs.Image.MaxMB)<<20 {
		return ptrIssue(planIssue("asset_unavailable", "image_asset_ids", "图片类型或文件大小不符合模型配置", true))
	}
	if agnesPlugin(snap) && (asset.Width < 256 || asset.Width > 5760 || asset.Height < 256 || asset.Height > 5760) {
		return ptrIssue(planIssue("protocol_image_dimensions", "image_asset_ids", "Agnes 图片宽高必须在 256 至 5760 像素之间", true))
	}
	return nil
}

// isAgnesFlash resolves the explicit upstream variant used by the selected Agnes plugin.
func isAgnesFlash(snap *provider.Snapshot) bool {
	return agnesPlugin(snap) && strings.HasSuffix(strings.ToLower(snap.Model.UpstreamModel), "-flash")
}

// countOutput reads Agnes' normalized single-output count parameter.
func countOutput(input map[string]any) int {
	value, ok := input["n"].(float64)
	if !ok || value == 0 {
		return 1
	}
	return int(value)
}

// agnesPlugin identifies protocol rules by resolved plugin identity.
func agnesPlugin(snap *provider.Snapshot) bool { return snap.Plugin.Key == "agnes-video" }

// agnesRouteMode translates the selected route into Agnes' explicit mode vocabulary.
func agnesRouteMode(mode string) string {
	if mode == "text" {
		return "text"
	}
	if mode == "keyframe" {
		return "keyframe"
	}
	return "reference"
}

// planIssue creates one stable, client-readable validation result.
func planIssue(code, field, message string, blocking bool) model.DramaReferencePlanIssue {
	return model.DramaReferencePlanIssue{Code: code, Field: field, Message: message, Blocking: blocking}
}

// ptrIssue returns a copy suitable for optional helper results.
func ptrIssue(issue model.DramaReferencePlanIssue) *model.DramaReferencePlanIssue { return &issue }

// blockedReferenceUnit returns an empty quote row when the selected shot cannot be compiled.
func blockedReferenceUnit(id, code, field, message string) model.DramaReferencePlanUnit {
	unit := model.DramaReferencePlanUnit{ShotID: id, ImageAssetIDs: []string{}, Materials: []model.DramaReferencePlanAsset{}, Issues: []model.DramaReferencePlanIssue{}}
	unit.Issues = append(unit.Issues, planIssue(code, field, message, true))
	return unit
}
