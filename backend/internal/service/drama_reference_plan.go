package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// DramaReferencePlanRepo 声明草稿报价读取所需的用户范围查询。
type DramaReferencePlanRepo interface {
	// Reference 按用户、存活项目和参考集查询，缺失返回 repository.ErrNotFound。
	Reference(context.Context, uint64, uint64, uint64, bool) (*model.DramaReferenceEpisode, error)
	// Draft 按用户、项目、参考集和草稿查询，缺失返回 repository.ErrNotFound。
	Draft(context.Context, uint64, uint64, uint64, uint64, bool) (*model.DramaReferenceDraft, error)
}

// DramaReferencePlanDeps 提供草稿、素材、模型能力及可选准备快照。
type DramaReferencePlanDeps struct {
	Repo         DramaReferencePlanRepo
	Assets       MarketingAssets
	Registry     provider.Registry
	Preparations DramaReferenceDraftPreparations
	Adoptions    *DramaReferenceService
	Segments     DramaReferencePlanSegments
}

// DramaReferencePlanSegments submits the same governed segment-generation intent used by the studio.
type DramaReferencePlanSegments interface {
	Generate(context.Context, uint64, uint64, string, *model.GenerateDramaSegmentReq) (*model.GenerationTaskView, error)
}

// DramaReferencePlanService 根据发布中的模型配置编译与报价单镜计划。
type DramaReferencePlanService struct{ deps DramaReferencePlanDeps }

// NewDramaReferencePlanService 组装只读的计划校验服务。
func NewDramaReferencePlanService(deps DramaReferencePlanDeps) *DramaReferencePlanService {
	return &DramaReferencePlanService{deps: deps}
}

type referencePlanSource struct {
	asset    *model.Asset
	draft    *model.DramaReferenceDraft
	document model.DramaReferenceDocument
	shots    map[string]model.DramaReferenceShot
}

type referencePlanCompile struct {
	projectID     string
	referenceID   string
	draftID       string
	request       *model.QuoteDramaReferenceDraftReq
	snapshot      *provider.Snapshot
	configuration string
	source        *referencePlanSource
	plan          *model.DramaReferencePlan
}

// normalizePlanQuote fixes empty optional asset lists so persisted approvals compare consistently.
func normalizePlanQuote(req model.QuoteDramaReferenceDraftReq) model.QuoteDramaReferenceDraftReq {
	out := req
	out.Units = slices.Clone(req.Units)
	for index := range out.Units {
		out.Units[index].ImageAssetIDs = slices.Clone(req.Units[index].ImageAssetIDs)
		if out.Units[index].ImageAssetIDs == nil {
			out.Units[index].ImageAssetIDs = model.DramaReferenceAssetIDs{}
		}
	}
	return out
}

// Quote 校验草稿和素材归属，返回与正式任务同一参数、模型快照和积分算法下的只读报价。
func (s *DramaReferencePlanService) Quote(ctx context.Context, u, p, ref, draftID uint64, req *model.QuoteDramaReferenceDraftReq) (*model.DramaReferencePlan, error) {
	// 1. 查询存活项目、参考集、本人草稿和引用源，越权始终按不存在处理。
	state, err := s.loadReferencePlanSource(ctx, u, p, ref, draftID, req)
	if err != nil {
		return nil, err
	}
	// 2. 对发布模型取快照，配置故障和协议不兼容作为可读计划问题返回。
	snap, modelErr := s.deps.Registry.Snapshot(ctx, req.ModelID)
	if errors.Is(modelErr, provider.ErrModelUnavailable) {
		return nil, errcode.ErrModelUnavailable
	}
	if modelErr == nil && snap == nil {
		return nil, errcode.ErrModelUnavailable
	}
	if modelErr != nil {
		return nil, modelErr
	}
	compiled := &referencePlanCompile{projectID: idcodec.Encode(p), referenceID: idcodec.Encode(ref), draftID: idcodec.Encode(draftID), request: req, snapshot: snap, source: state}
	compiled.configuration, err = referencePlanConfigurationDigest(snap)
	if err != nil {
		return nil, err
	}
	compiled.plan = newReferencePlan(compiled, req)
	// 3. 每个单镜都按将要发送的输入规范化，再计价并校验路由约束。
	seenShots := map[string]bool{}
	for _, unit := range req.Units {
		if seenShots[unit.ShotID] {
			compiled.plan.Issues = append(compiled.plan.Issues, planIssue("shot_duplicate", "units", "同一镜头不能重复计入报价", true))
		}
		seenShots[unit.ShotID] = true
		item, issues, price, err := s.compileReferencePlanUnit(ctx, u, compiled, unit)
		if err != nil {
			return nil, err
		}
		compiled.plan.Units = append(compiled.plan.Units, item)
		compiled.plan.Issues = append(compiled.plan.Issues, issues...)
		if price != nil {
			compiled.plan.TotalVideoCredits += *price
		}
	}
	return finishReferencePlan(compiled.plan, req)
}

// CheckApproved 重新读取当前草稿和模型配置，阻止审批后沿用过期计划或报价。
func (s *DramaReferencePlanService) CheckApproved(ctx context.Context, u, p, ref, draftID uint64, req *model.QuoteDramaReferenceDraftReq, digest string) (*model.DramaReferencePlan, error) {
	// 1. 没有摘要代表旧调用不能获得对计划的默示批准。
	if len(digest) != sha256.Size*2 {
		return nil, errcode.ErrInvalidParams.WithMsg("需要最新生成计划的摘要")
	}
	// 2. 仅在重新编译仍可执行且摘要完全相同时接受旧批准范围。
	plan, err := s.Quote(ctx, u, p, ref, draftID, req)
	if err != nil {
		return nil, err
	}
	if !plan.Executable || plan.PlanDigest != digest {
		return nil, errcode.ErrCanvasConflict.WithMsg("草稿、模型配置、素材或报价已变化，请重新确认")
	}
	return plan, nil
}

// newReferencePlan 固定对用户公开的草稿与模型摘要字段。
func newReferencePlan(c *referencePlanCompile, req *model.QuoteDramaReferenceDraftReq) *model.DramaReferencePlan {
	return &model.DramaReferencePlan{DraftID: c.draftID, ProjectID: c.projectID, ReferenceID: c.referenceID, DraftRevision: c.source.draft.Revision,
		ReferenceRevision: c.source.draft.BaseRevision, SourceAssetID: strconv.FormatUint(c.source.asset.ID, 10),
		ModelID: c.snapshot.Model.Key, PromptFormat: c.snapshot.Model.Capabilities.PromptFormat,
		ConfigurationDigest: c.configuration, MaxCredits: req.MaxCredits,
		Units: []model.DramaReferencePlanUnit{}, Issues: []model.DramaReferencePlanIssue{}}
}

// referencePlanConfigurationDigest hashes all effective model, channel and plugin metadata without returning secrets.
func referencePlanConfigurationDigest(snap *provider.Snapshot) (string, error) {
	raw, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// compileReferencePlanUnit checks one shot's route, media and frozen task price.
func (s *DramaReferencePlanService) compileReferencePlanUnit(ctx context.Context, userID uint64, c *referencePlanCompile, req model.DramaReferencePlanUnitReq) (model.DramaReferencePlanUnit, []model.DramaReferencePlanIssue, *int, error) {
	shot, ok := c.source.shots[req.ShotID]
	if !ok {
		return blockedReferenceUnit(req.ShotID, "shot_not_found", "shot_id", "选中的镜头不在当前草稿内"), nil, nil, nil
	}
	unit := plannedReferenceUnit(shot, req)
	issues := []model.DramaReferencePlanIssue{}
	if c.snapshot.Model.Kind != modelcfg.KindVideo {
		issues = append(issues, planIssue("model_route_unsupported", "model_id", "所选模型不是视频模型", true))
	}
	route, routeIssues := resolveReferenceRoute(req, c.snapshot)
	unit.Mode = route
	issues = append(issues, routeIssues...)
	issues = append(issues, duplicatePlanImageIssue(req.ImageAssetIDs)...)
	media, materials, mediaIssues, err := s.referencePlanMedia(ctx, userID, c, req, route)
	if err != nil {
		return unit, nil, nil, err
	}
	issues = append(issues, mediaIssues...)
	prompt := referencePlanPrompt(c.source, req.ShotID, c.request)
	unit.Prompt = prompt
	input, inputIssues := normalizeReferencePlanInput(c.snapshot, route, prompt, req, media)
	issues = append(issues, inputIssues...)
	quoteInput := input
	if route == "video" && input != nil {
		input = withoutPlanSourceVideo(input)
	}
	unit.Input = input
	unit.Duration = inputDuration(input)
	if err := validateReferencePlanDuration(unit); err != nil {
		issues = append(issues, *err)
	}
	if agnesPlugin(c.snapshot) && quoteInput != nil {
		issues = append(issues, validateAgnesPlan(c.snapshot, route, quoteInput)...)
	}
	var price *int
	if quoteInput != nil && len(inputIssues) == 0 {
		price, issues = appendReferencePlanPrice(issues, c.snapshot, quoteInput)
		unit.Credits = price
	}
	unit.ImageAssetIDs = decimalPlanAssetIDs(req.ImageAssetIDs)
	unit.Materials = materials
	unit.Issues = issues
	return unit, issues, price, nil
}

// duplicatePlanImageIssue rejects repeated assets rather than charging or forwarding the same reference twice.
func duplicatePlanImageIssue(ids model.DramaReferenceAssetIDs) []model.DramaReferencePlanIssue {
	seen := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return []model.DramaReferencePlanIssue{planIssue("image_duplicate", "image_asset_ids", "同一图片不能在单镜参考中重复提交", true)}
		}
		seen[id] = true
	}
	return nil
}

// withoutPlanSourceVideo removes the reference source placeholder because GenerateRequest receives the service-created clip.
func withoutPlanSourceVideo(input map[string]any) map[string]any {
	clean := make(map[string]any, len(input))
	for key, value := range input {
		if key != "videos" {
			clean[key] = value
		}
	}
	return clean
}

// plannedReferenceUnit derives target output trim defaults without changing the immutable source window.
func plannedReferenceUnit(shot model.DramaReferenceShot, req model.DramaReferencePlanUnitReq) model.DramaReferencePlanUnit {
	target := req.TargetDurationMs
	if target == 0 {
		target = shot.EndMs - shot.StartMs
	}
	trim := req.TrimStartMs
	if req.TrimStartMs == 0 && req.TargetDurationMs == 0 {
		trim = 0
	}
	return model.DramaReferencePlanUnit{ShotID: shot.ID, SourceStartMs: shot.StartMs, SourceEndMs: shot.EndMs,
		Duration: req.Duration, TrimStartMs: trim, TargetDurationMs: target,
		ImageAssetIDs: []string{}, Materials: []model.DramaReferencePlanAsset{}, Issues: []model.DramaReferencePlanIssue{}}
}

// referencePlanPrompt prefers a saved per-shot prompt and then uses the observed action.
func referencePlanPrompt(source *referencePlanSource, shotID string, req *model.QuoteDramaReferenceDraftReq) string {
	var content model.DramaReferenceDraftContent
	_ = json.Unmarshal(source.draft.Content, &content)
	for _, item := range content.Prompts {
		if item.ShotID == shotID && strings.TrimSpace(item.Prompt) != "" {
			return item.Prompt
		}
	}
	for _, shot := range content.Document.Shots {
		if shot.ID == shotID && strings.TrimSpace(shot.Action) != "" {
			return shot.Action
		}
	}
	return strings.TrimSpace(content.Instruction)
}

// resolveReferenceRoute derives auto mode from the selected media and maps routes to existing generation ops.
func resolveReferenceRoute(req model.DramaReferencePlanUnitReq, snap *provider.Snapshot) (string, []model.DramaReferencePlanIssue) {
	mode := req.Mode
	issues := []model.DramaReferencePlanIssue{}
	if mode == "" || mode == "auto" {
		mode = autoReferenceRoute(req, snap)
		if mode == "text" && req.Duration > 0 && !modelSupportsVideoReference(snap) && len(req.ImageAssetIDs) == 0 {
			issues = append(issues, planIssue("reference_consistency_risk", "mode", "模型不支持视频参考，将只依据文字提示生成，动作一致性可能降低", false))
		}
	}
	if mode != "text" && mode != "keyframe" && mode != "image" && mode != "video" {
		return "", []model.DramaReferencePlanIssue{planIssue("route_mode_invalid", "mode", "单镜生成路线无效", true)}
	}
	op := map[string]string{"text": modelcfg.OpT2V, "keyframe": modelcfg.OpI2V, "image": modelcfg.OpOmni, "video": modelcfg.OpOmni}[mode]
	if mode == "keyframe" && (len(req.ImageAssetIDs) < 1 || len(req.ImageAssetIDs) > 2) {
		issues = append(issues, planIssue("route_media_mismatch", "image_asset_ids", "首尾帧路线需要一或两张图片", true))
	}
	if mode == "image" && len(req.ImageAssetIDs) == 0 {
		issues = append(issues, planIssue("route_media_mismatch", "image_asset_ids", "图片参考路线至少需要一张图片", true))
	}
	if mode == "video" && !snap.Model.Capabilities.Refs.Video.On {
		issues = append(issues, planIssue("model_route_unsupported", "mode", "所选模型配置未启用视频参考", true))
	}
	if (mode == "image" || mode == "keyframe") && !snap.Model.Capabilities.Refs.Image.On {
		issues = append(issues, planIssue("model_route_unsupported", "mode", "所选模型配置未启用图片参考", true))
	}
	if len(snap.Model.Capabilities.Ops) > 0 && !containsRoute(snap.Model.Capabilities.Ops, op) {
		issues = append(issues, planIssue("model_route_unsupported", "mode", "模型能力不支持该生成路线", true))
	}
	return mode, issues
}

// autoReferenceRoute selects only a reference route advertised by the current model.
func autoReferenceRoute(req model.DramaReferencePlanUnitReq, snap *provider.Snapshot) string {
	switch {
	case len(req.ImageAssetIDs) == 1:
		return "keyframe"
	case len(req.ImageAssetIDs) > 1:
		return "image"
	case req.Duration > 0 && modelSupportsVideoReference(snap):
		return "video"
	default:
		return "text"
	}
}

// modelSupportsVideoReference combines published capabilities with the resolved protocol variant.
func modelSupportsVideoReference(snap *provider.Snapshot) bool {
	return snap.Model.Capabilities.Refs.Video.On && !isAgnesFlash(snap)
}

// containsRoute tests whether the effective model advertises the chosen video operation.
func containsRoute(ops []string, want string) bool {
	for _, op := range ops {
		if op == want {
			return true
		}
	}
	return false
}

// referencePlanMedia validates selected images and checks the source video required by reference mode.
func (s *DramaReferencePlanService) referencePlanMedia(ctx context.Context, userID uint64, c *referencePlanCompile, req model.DramaReferencePlanUnitReq, mode string) (map[string]any, []model.DramaReferencePlanAsset, []model.DramaReferencePlanIssue, error) {
	media := map[string]any{}
	materials := []model.DramaReferencePlanAsset{}
	issues := []model.DramaReferencePlanIssue{}
	if mode == "video" {
		if err := validateReferenceSourceVideo(c.source.asset, c.snapshot, c.source.shots[req.ShotID]); err != nil {
			issues = append(issues, *err)
		}
		media["videos"] = []any{c.source.asset.ID}
		materials = append(materials, planAssetSnapshot(c.source.asset))
	}
	for _, id := range req.ImageAssetIDs {
		asset, err := s.deps.Assets.Get(ctx, userID, id)
		if errors.Is(err, provider.ErrAssetNotFound) {
			issues = append(issues, planIssue("asset_unavailable", "image_asset_ids", "图片素材不存在或不属于当前用户", true))
			continue
		}
		if err != nil {
			issues = append(issues, planIssue("asset_unavailable", "image_asset_ids", "无法读取图片素材", true))
			continue
		}
		if issue := validateReferenceImage(asset, c.snapshot); issue != nil {
			issues = append(issues, *issue)
			continue
		}
		if err := verifyReferencePlanAsset(ctx, s.deps.Assets, userID, id); err != nil {
			if errors.Is(err, provider.ErrAssetNotFound) {
				issues = append(issues, planIssue("asset_unavailable", "image_asset_ids", "图片文件不可读取", true))
				continue
			}
			return nil, nil, nil, err
		}
		if media["images"] == nil {
			media["images"] = []uint64{}
		}
		media["images"] = append(media["images"].([]uint64), id)
		materials = append(materials, planAssetSnapshot(asset))
	}
	return media, materials, issues, nil
}

// validateReferencePlanDuration ensures generated output can cover the independently selected trim window.
func validateReferencePlanDuration(unit model.DramaReferencePlanUnit) *model.DramaReferencePlanIssue {
	if unit.Duration > 0 && unit.TrimStartMs+unit.TargetDurationMs > int64(unit.Duration)*1000 {
		return ptrIssue(planIssue("duration_insufficient", "duration", "模型申请时长不足以覆盖成片裁剪区间", true))
	}
	return nil
}

// validateAgnesPlan applies known plugin constraints to the normalized request.
// paramString reads one effective normalized string parameter.
func paramString(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

// inputDuration returns the normalized requested output duration in seconds.
func inputDuration(input map[string]any) int {
	value, ok := input[modelcfg.DurationParam].(float64)
	if !ok {
		return 0
	}
	return int(value)
}

// appendReferencePlanPrice prices the exact input that will be persisted for the generation task.
func appendReferencePlanPrice(issues []model.DramaReferencePlanIssue, snap *provider.Snapshot, input map[string]any) (*int, []model.DramaReferencePlanIssue) {
	if snap.Model.Pricing.Pending {
		return nil, append(issues, planIssue("price_unavailable", "model_id", "模型积分报价尚未配置", true))
	}
	credits := max(modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.SpecFromInput(input, snap.Model.Capabilities.System)), 0)
	return &credits, issues
}

// finishReferencePlan applies the total budget and computes the immutable plan digest.
func finishReferencePlan(plan *model.DramaReferencePlan, req *model.QuoteDramaReferenceDraftReq) (*model.DramaReferencePlan, error) {
	if req.MaxCredits != nil && plan.TotalVideoCredits > *req.MaxCredits {
		plan.Issues = append(plan.Issues, planIssue("budget_exceeded", "max_credits", "视频生成报价超过本次积分上限", true))
	}
	plan.Executable = !hasBlockingPlanIssue(plan.Issues, plan.Units)
	raw, err := json.Marshal(struct {
		ProjectID     string `json:"project_id"`
		ReferenceID   string `json:"reference_id"`
		DraftID       string `json:"draft_id"`
		DraftRevision int64  `json:"draft_revision"`
		BaseRevision  int64  `json:"base_revision"`
		SourceAssetID string `json:"source_asset_id"`
		ModelID       string `json:"model_id"`
		Configuration string `json:"configuration"`
		Plan          any    `json:"plan"`
		Budget        *int   `json:"budget"`
	}{plan.ProjectID, plan.ReferenceID, plan.DraftID, plan.DraftRevision, plan.ReferenceRevision,
		plan.SourceAssetID, plan.ModelID, plan.ConfigurationDigest, plan.Units, plan.MaxCredits})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	plan.PlanDigest = hex.EncodeToString(sum[:])
	return plan, nil
}

// hasBlockingPlanIssue computes executability over plan-level and per-shot blockers.
func hasBlockingPlanIssue(global []model.DramaReferencePlanIssue, units []model.DramaReferencePlanUnit) bool {
	for _, issue := range global {
		if issue.Blocking {
			return true
		}
	}
	for _, unit := range units {
		for _, issue := range unit.Issues {
			if issue.Blocking {
				return true
			}
		}
	}
	return false
}
