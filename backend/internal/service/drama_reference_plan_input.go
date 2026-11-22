package service

import (
	"context"
	"fmt"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// normalizeReferencePlanInput validates and normalizes the exact model request for one route.
func normalizeReferencePlanInput(snap *provider.Snapshot, mode, prompt string, req model.DramaReferencePlanUnitReq, media map[string]any) (map[string]any, []model.DramaReferencePlanIssue) {
	raw := map[string]any{"prompt": prompt, "op": routeOperation(mode)}
	params, issues := normalizedRequestParams(snap.Model.Capabilities, req)
	for key, value := range params {
		raw[key] = value
	}
	for key, value := range media {
		raw[key] = value
	}
	input, errs := modelcfg.ValidateInput(snap.Model.Kind, snap.Model.Capabilities, raw)
	issues = append(issues, normalizedInputIssues(errs)...)
	if len(errs) > 0 {
		return nil, issues
	}
	issues = append(issues, omittedParamsIssues(req, input)...)
	issues = append(issues, omittedMediaIssues(media, input)...)
	return input, issues
}

// normalizedRequestParams validates caller-selected settings and keeps application fields separate.
func normalizedRequestParams(caps modelcfg.Capabilities, req model.DramaReferencePlanUnitReq) (map[string]any, []model.DramaReferencePlanIssue) {
	params := map[string]any{}
	issues := []model.DramaReferencePlanIssue{}
	for key, value := range req.Params {
		field, exists := caps.Params.Get(key)
		if key == "prompt" || key == "op" || key == "images" || key == "videos" || key == "audios" || !exists || key != "mode" && !field.Open {
			issues = append(issues, planIssue("model_input_unavailable", key, "参数不属于当前模型可配置字段", true))
			continue
		}
		params[key] = value
	}
	if req.Duration > 0 {
		if configured, exists := req.Params[modelcfg.DurationParam]; exists && fmt.Sprint(configured) != fmt.Sprint(req.Duration) {
			issues = append(issues, planIssue("duration_conflict", modelcfg.DurationParam, "请只设置一个申请生成时长", true))
		}
		params[modelcfg.DurationParam] = req.Duration
	}
	return params, issues
}

// normalizedInputIssues translates model validator failures into quote issues.
func normalizedInputIssues(errs []modelcfg.FieldError) []model.DramaReferencePlanIssue {
	issues := make([]model.DramaReferencePlanIssue, 0, len(errs))
	for _, fieldErr := range errs {
		issues = append(issues, planIssue("model_input_invalid", fieldErr.Field, fieldErr.Message, true))
	}
	return issues
}

// omittedParamsIssues detects parameters discarded by model capability normalization.
func omittedParamsIssues(req model.DramaReferencePlanUnitReq, input map[string]any) []model.DramaReferencePlanIssue {
	issues := []model.DramaReferencePlanIssue{}
	for key := range req.Params {
		if _, exists := input[key]; !exists {
			issues = append(issues, planIssue("model_input_unavailable", key, "参数没有进入规范化的上游请求", true))
		}
	}
	return issues
}

// omittedMediaIssues rejects media references dropped from normalized model input.
func omittedMediaIssues(media map[string]any, input map[string]any) []model.DramaReferencePlanIssue {
	actual := modelcfg.MediaRefs(input)
	issues := []model.DramaReferencePlanIssue{}
	for _, key := range []string{"images", "videos"} {
		requested := mediaReferencesByKey(modelcfg.MediaRefs(media), key)
		retained := mediaReferencesByKey(actual, key)
		if len(requested) > 0 && len(requested) != len(retained) {
			issues = append(issues, planIssue("route_media_mismatch", key, "模型规范化时移除了所选参考素材", true))
		}
	}
	return issues
}

// mediaReferencesByKey selects normalized references of one media kind.
func mediaReferencesByKey(refs []modelcfg.MediaRef, key string) []modelcfg.MediaRef {
	selected := make([]modelcfg.MediaRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Key == key {
			selected = append(selected, ref)
		}
	}
	return selected
}

// routeOperation maps user routes to supported generation operations.
func routeOperation(mode string) string {
	return map[string]string{"text": modelcfg.OpT2V, "keyframe": modelcfg.OpI2V, "image": modelcfg.OpOmni, "video": modelcfg.OpOmni}[mode]
}

// decimalPlanAssetIDs keeps the standard decimal asset ID representation in quote responses.
func decimalPlanAssetIDs(ids model.DramaReferenceAssetIDs) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, strconv.FormatUint(id, 10))
	}
	return result
}

// planAssetSnapshot records non-sensitive material identity and technical metadata for approval revalidation.
func planAssetSnapshot(asset *model.Asset) model.DramaReferencePlanAsset {
	return model.DramaReferencePlanAsset{ID: strconv.FormatUint(asset.ID, 10), Kind: asset.Kind, MimeType: asset.MimeType,
		Sha256: asset.Sha256, ByteSize: asset.ByteSize, Width: asset.Width, Height: asset.Height, DurationMs: asset.DurationMs}
}

// verifyReferencePlanAsset opens and closes a user's asset to confirm its backing object exists.
func verifyReferencePlanAsset(ctx context.Context, assets MarketingAssets, userID, assetID uint64) error {
	file, err := assets.Open(ctx, userID, assetID)
	if err != nil {
		return err
	}
	if file == nil || file.Body == nil {
		return provider.ErrAssetNotFound
	}
	return file.Body.Close()
}
