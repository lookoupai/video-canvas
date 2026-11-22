package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

type dramaReferenceReadArgs struct {
	Scope         string                             `json:"scope"`
	ReferenceID   idcodec.ID                         `json:"reference_id"`
	Revision      int64                              `json:"revision"`
	PreparationID idcodec.ID                         `json:"preparation_id"`
	DraftID       idcodec.ID                         `json:"draft_id"`
	Cursor        string                             `json:"cursor"`
	Limit         int                                `json:"limit"`
	ShotCursor    string                             `json:"shot_cursor"`
	ShotLimit     int                                `json:"shot_limit"`
	Quote         *model.QuoteDramaReferenceDraftReq `json:"quote"`
}

// toolDramaReferenceRead 依据当前短剧画布读取参考目录、原稿、准备状态或草稿。
func (b *AgentBridge) toolDramaReferenceRead(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a dramaReferenceReadArgs
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	value, err := b.readDramaReference(ctx, tc, p, a)
	if err != nil {
		return nil, err
	}
	content, err := jsonOut(value)
	return &toolOut{data: value, content: content, summary: "读取参考剧集 " + a.Scope}, err
}

// readDramaReference 按显式范围调用同一短剧项目内的服务。
func (b *AgentBridge) readDramaReference(ctx context.Context, tc *toolCall, p *model.DramaView, a dramaReferenceReadArgs) (any, error) {
	switch a.Scope {
	case "collection":
		if b.d.DramaReferences == nil {
			return nil, fail("参考剧集服务未启用")
		}
		return b.d.DramaReferences.Collection(ctx, tc.run.UserID, uint64(p.ID))
	case "reference":
		if a.ReferenceID == 0 || b.d.DramaReferences == nil {
			return nil, fail("需要有效 reference_id 和参考剧集服务")
		}
		if a.Revision < 0 {
			return nil, fail("revision 不能为负数")
		}
		view, err := b.d.DramaReferences.GetEpisode(ctx, tc.run.UserID, uint64(p.ID), uint64(a.ReferenceID), a.Revision)
		if err != nil {
			return nil, err
		}
		return pageDramaReferenceView(view, a.ShotCursor, dramaReferenceLimit(a.ShotLimit, 20, 50))
	case "drafts":
		return b.readDramaDrafts(ctx, tc.run.UserID, uint64(p.ID), a)
	case "preparation":
		return b.readDramaPreparation(ctx, tc.run.UserID, uint64(p.ID), a)
	case "draft":
		return b.readDramaDraft(ctx, tc.run.UserID, uint64(p.ID), a)
	case "quote":
		if a.ReferenceID == 0 || a.DraftID == 0 || a.Quote == nil || b.d.DramaPlans == nil {
			return nil, fail("逐镜报价需要 reference_id、draft_id、quote 和报价服务")
		}
		return b.d.DramaPlans.Quote(ctx, tc.run.UserID, uint64(p.ID), uint64(a.ReferenceID), uint64(a.DraftID), a.Quote)
	default:
		return nil, fail("scope 必须是 collection、reference、drafts、preparation、draft 或 quote")
	}
}

// readDramaDrafts 返回限定页大小的草稿列表。
func (b *AgentBridge) readDramaDrafts(ctx context.Context, userID, projectID uint64, a dramaReferenceReadArgs) (any, error) {
	if a.ReferenceID == 0 || b.d.DramaDrafts == nil {
		return nil, fail("需要有效 reference_id 和草稿服务")
	}
	return b.d.DramaDrafts.List(ctx, userID, projectID, uint64(a.ReferenceID), &model.ListDramaReferenceDraftsReq{Cursor: a.Cursor, Limit: dramaReferenceLimit(a.Limit, 20, 100)})
}

// readDramaPreparation 查询指定准备的持久状态摘要。
func (b *AgentBridge) readDramaPreparation(ctx context.Context, userID, projectID uint64, a dramaReferenceReadArgs) (any, error) {
	if a.ReferenceID == 0 || a.PreparationID == 0 || b.d.DramaPreparations == nil {
		return nil, fail("读取准备状态需要 reference_id、preparation_id 和准备服务")
	}
	return b.d.DramaPreparations.Get(ctx, userID, projectID, uint64(a.ReferenceID), uint64(a.PreparationID))
}

// readDramaDraft 查询指定草稿的最新修订。
func (b *AgentBridge) readDramaDraft(ctx context.Context, userID, projectID uint64, a dramaReferenceReadArgs) (any, error) {
	if a.ReferenceID == 0 || a.DraftID == 0 || b.d.DramaDrafts == nil {
		return nil, fail("读取草稿需要 reference_id、draft_id 和草稿服务")
	}
	view, err := b.d.DramaDrafts.Get(ctx, userID, projectID, uint64(a.ReferenceID), uint64(a.DraftID))
	if err != nil {
		return nil, err
	}
	return pageDramaDraftView(view, a.ShotCursor, dramaReferenceLimit(a.ShotLimit, 20, 50))
}

// pageDramaReferenceView bounds a reference document before returning it to the model.
func pageDramaReferenceView(view *model.DramaReferenceView, cursor string, limit int) (any, error) {
	if view == nil {
		return nil, fail("参考剧集不存在")
	}
	document, shotIDs, next, err := pageDramaDocument(view.Document, cursor, limit)
	if err != nil {
		return nil, err
	}
	viewCopy := *view
	viewCopy.Document = document
	return map[string]any{"reference": &viewCopy, "next_shot_cursor": next, "shot_ids": sortedShotIDs(shotIDs)}, nil
}

// pageDramaDraftView bounds document and per-shot analysis fields together.
func pageDramaDraftView(view *model.DramaReferenceDraftView, cursor string, limit int) (any, error) {
	if view == nil {
		return nil, fail("参考分析草稿不存在")
	}
	document, shotIDs, next, err := pageDramaDocument(&view.Document, cursor, limit)
	if err != nil {
		return nil, err
	}
	viewCopy := *view
	viewCopy.Document = *document
	viewCopy.Prompts = filterDraftPrompts(view.Prompts, shotIDs)
	viewCopy.Evidence = filterDraftEvidence(view.Evidence, shotIDs)
	viewCopy.Observations = filterDraftObservations(view.Observations, shotIDs)
	return map[string]any{"draft": &viewCopy, "next_shot_cursor": next, "shot_ids": sortedShotIDs(shotIDs)}, nil
}

// pageDramaDocument selects shots by stable document order and retains only related fields.
func pageDramaDocument(document *model.DramaReferenceDocument, cursor string, limit int) (*model.DramaReferenceDocument, map[string]bool, string, error) {
	if document == nil {
		return nil, map[string]bool{}, "", nil
	}
	start, err := parseDramaShotCursor(cursor, len(document.Shots))
	if err != nil {
		return nil, nil, "", err
	}
	end := min(start+limit, len(document.Shots))
	page := &model.DramaReferenceDocument{Scenes: []model.DramaReferenceScene{}, Characters: []model.DramaReferenceCharacter{},
		Shots: append([]model.DramaReferenceShot{}, document.Shots[start:end]...), Dialogues: []model.DramaReferenceDialogue{}}
	shotIDs, sceneIDs, characterIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, shot := range page.Shots {
		shotIDs[shot.ID], sceneIDs[shot.SceneID] = true, true
		for _, id := range shot.Characters {
			characterIDs[id] = true
		}
	}
	for _, scene := range document.Scenes {
		if sceneIDs[scene.ID] {
			page.Scenes = append(page.Scenes, scene)
		}
	}
	for _, character := range document.Characters {
		if characterIDs[character.ID] {
			page.Characters = append(page.Characters, character)
		}
	}
	for _, dialogue := range document.Dialogues {
		if dialogueOverlapsShots(dialogue, page.Shots) {
			page.Dialogues = append(page.Dialogues, dialogue)
			if dialogue.Speaker != "" {
				characterIDs[dialogue.Speaker] = true
			}
		}
	}
	page.Characters = filterReferenceCharacters(document.Characters, characterIDs)
	next := ""
	if end < len(document.Shots) {
		next = strconv.Itoa(end)
	}
	return page, shotIDs, next, nil
}

// parseDramaShotCursor validates an offset against the frozen shot order.
func parseDramaShotCursor(cursor string, shotCount int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	start, err := strconv.Atoi(cursor)
	if err != nil || start < 0 || start > shotCount {
		return 0, fail("shot_cursor 不是有效的镜头游标")
	}
	return start, nil
}

// dialogueOverlapsShots keeps dialogue whose original time window intersects a returned shot.
func dialogueOverlapsShots(dialogue model.DramaReferenceDialogue, shots []model.DramaReferenceShot) bool {
	for _, shot := range shots {
		if dialogue.EndMs > shot.StartMs && dialogue.StartMs < shot.EndMs {
			return true
		}
	}
	return false
}

// filterReferenceCharacters retains only characters used by the page's shots or dialogue.
func filterReferenceCharacters(characters []model.DramaReferenceCharacter, ids map[string]bool) []model.DramaReferenceCharacter {
	out := make([]model.DramaReferenceCharacter, 0, len(ids))
	for _, character := range characters {
		if ids[character.ID] {
			out = append(out, character)
		}
	}
	return out
}

// filterDraftPrompts keeps prompts for the selected shot page.
func filterDraftPrompts(rows []model.DramaReferencePrompt, ids map[string]bool) []model.DramaReferencePrompt {
	out := make([]model.DramaReferencePrompt, 0, len(rows))
	for _, row := range rows {
		if ids[row.ShotID] {
			out = append(out, row)
		}
	}
	return out
}

// filterDraftEvidence keeps evidence for the selected shot page.
func filterDraftEvidence(rows []model.DramaReferenceEvidence, ids map[string]bool) []model.DramaReferenceEvidence {
	out := make([]model.DramaReferenceEvidence, 0, len(rows))
	for _, row := range rows {
		if ids[row.ShotID] {
			out = append(out, row)
		}
	}
	return out
}

// filterDraftObservations keeps observations for the selected shot page.
func filterDraftObservations(rows []model.DramaReferenceObservation, ids map[string]bool) []model.DramaReferenceObservation {
	out := make([]model.DramaReferenceObservation, 0, len(rows))
	for _, row := range rows {
		if ids[row.ShotID] {
			out = append(out, row)
		}
	}
	return out
}

// sortedShotIDs returns stable IDs for concise page metadata.
func sortedShotIDs(ids map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

type dramaReferencePrepareArgs struct {
	ReferenceID idcodec.ID `json:"reference_id"`
	Revision    int64      `json:"revision"`
	ShotIDs     []string   `json:"shot_ids"`
}

// toolDramaReferencePrepare 创建或复用指定参考修订的媒体准备任务。
func (b *AgentBridge) toolDramaReferencePrepare(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a dramaReferencePrepareArgs
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	if a.ReferenceID == 0 || a.Revision < 1 || b.d.DramaPreparations == nil {
		return nil, fail("需要有效参考集、revision 和媒体准备服务")
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	view, err := b.d.DramaPreparations.Create(ctx, tc.run.UserID, uint64(p.ID), uint64(a.ReferenceID), &model.CreateDramaReferencePreparationReq{
		RequestKey: dramaAgentRequestKey(ctx, tc, "prepare"), ReferenceRevision: a.Revision, ShotIDs: a.ShotIDs,
	})
	return dramaReferenceOut(view, err, "已创建或复用媒体准备")
}

type dramaReferenceStatusArgs struct {
	ReferenceID   idcodec.ID `json:"reference_id"`
	PreparationID idcodec.ID `json:"preparation_id"`
	DraftID       idcodec.ID `json:"draft_id"`
}

// toolDramaReferenceStatus 恢复读取指定准备或草稿的最新状态。
func (b *AgentBridge) toolDramaReferenceStatus(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a dramaReferenceStatusArgs
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	var value any
	switch {
	case a.PreparationID != 0 && a.DraftID == 0 && b.d.DramaPreparations != nil:
		if a.ReferenceID == 0 {
			return nil, fail("查询准备状态必须提供 reference_id")
		}
		value, err = b.d.DramaPreparations.Get(ctx, tc.run.UserID, uint64(p.ID), uint64(a.ReferenceID), uint64(a.PreparationID))
	case a.DraftID != 0 && a.PreparationID == 0 && b.d.DramaDrafts != nil:
		if a.ReferenceID == 0 {
			return nil, fail("查询草稿状态必须提供 reference_id")
		}
		value, err = b.d.DramaDrafts.Get(ctx, tc.run.UserID, uint64(p.ID), uint64(a.ReferenceID), uint64(a.DraftID))
	default:
		return nil, fail("只能查询一个有效 preparation_id 或 draft_id，并提供 reference_id")
	}
	return dramaReferenceOut(value, err, "查询参考分析状态")
}

type dramaReferenceInspectArgs struct {
	ReferenceID   idcodec.ID `json:"reference_id"`
	PreparationID idcodec.ID `json:"preparation_id"`
	ShotID        string     `json:"shot_id"`
	Cursor        string     `json:"cursor"`
	Limit         int        `json:"limit"`
}

// toolDramaReferenceInspect 将指定镜头的系统帧图和时间证据附到模型本轮输入。
func (b *AgentBridge) toolDramaReferenceInspect(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a dramaReferenceInspectArgs
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	if a.ReferenceID == 0 || a.PreparationID == 0 || a.ShotID == "" || b.d.DramaPreparations == nil {
		return nil, fail("查看帧图需要 reference_id、preparation_id、shot_id 和准备服务")
	}
	if err := b.requireReferenceVision(ctx, tc); err != nil {
		return nil, err
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	frames, err := b.d.DramaPreparations.Frames(ctx, tc.run.UserID, uint64(p.ID), uint64(a.ReferenceID), uint64(a.PreparationID), &model.ListDramaReferenceFramesReq{
		Cursor: a.Cursor, Limit: dramaReferenceLimit(a.Limit, 4, 4), ShotID: a.ShotID,
	})
	if err != nil {
		return nil, err
	}
	return b.attachDramaReferenceFrames(ctx, tc, frames)
}

// requireReferenceVision 防止通过未声明为视觉模型的运行上下文触发图片分析。
func (b *AgentBridge) requireReferenceVision(ctx context.Context, tc *toolCall) error {
	snapshot, _, err := b.resolveModel(ctx, tc.sess.modelKey)
	if err != nil {
		return err
	}
	if !snapshot.Model.Capabilities.Vision {
		return fail("当前 Agent 模型不能看图，请先切换到支持视觉理解的模型。")
	}
	return nil
}

// attachDramaReferenceFrames 只把仍有效的系统帧图交给模型，并公开可回写的真实证据 ID。
func (b *AgentBridge) attachDramaReferenceFrames(ctx context.Context, tc *toolCall, frames *model.DramaReferenceFrameList) (*toolOut, error) {
	if frames == nil || len(frames.Items) == 0 {
		return nil, fail("该镜头还没有帧图，请稍后查询准备状态或补充采样点。")
	}
	out := &toolOut{summary: fmt.Sprintf("查看镜头 %s 的原片帧图", frames.Items[0].ShotID)}
	items := make([]map[string]any, 0, len(frames.Items))
	for _, frame := range frames.Items {
		item, err := b.attachDramaReferenceFrame(ctx, tc, frame, out)
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, fail("所选帧图已失效或尚未生成，请查询准备状态后重试。")
	}
	value := map[string]any{"frames": items, "next_cursor": frames.NextCursor}
	content, err := jsonOut(value)
	out.data, out.content = value, content
	return out, err
}

// attachDramaReferenceFrame 检查图片素材归属后，把一张帧图和证据字段交给模型。
func (b *AgentBridge) attachDramaReferenceFrame(ctx context.Context, tc *toolCall, frame model.DramaReferencePreparedFrameView, out *toolOut) (map[string]any, error) {
	if frame.Status != "ready" || frame.Asset == nil {
		return nil, nil
	}
	assetID := strconv.FormatUint(frame.Asset.ID, 10)
	image, err := b.loadImage(ctx, tc.run.UserID, assetID)
	if err != nil {
		return nil, fail("帧图素材无法读取：%v", err)
	}
	out.images = append(out.images, image)
	return map[string]any{"preparation_frame_id": frame.ID, "shot_id": frame.ShotID, "asset_id": frame.Asset.ID,
		"at_ms": frame.AtMs, "start_ms": frame.StartMs, "end_ms": frame.EndMs}, nil
}

type dramaReferenceDraftArgs struct {
	Action            string                             `json:"action"`
	ReferenceID       idcodec.ID                         `json:"reference_id"`
	PreparationID     idcodec.ID                         `json:"preparation_id"`
	ReferenceRevision int64                              `json:"reference_revision"`
	DraftID           idcodec.ID                         `json:"draft_id"`
	Revision          int64                              `json:"revision"`
	Instruction       *string                            `json:"instruction"`
	Document          *model.DramaReferenceDocument      `json:"document"`
	Prompts           *[]model.DramaReferencePrompt      `json:"prompts"`
	Evidence          *[]model.DramaReferenceEvidence    `json:"evidence"`
	Observations      *[]model.DramaReferenceObservation `json:"observations"`
	Coverage          *model.DramaReferenceCoverage      `json:"coverage"`
}

// toolDramaReferenceDraft 创建或更新分析草稿，服务端按修订完整校验结构、来源和帧证据。
func (b *AgentBridge) toolDramaReferenceDraft(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a dramaReferenceDraftArgs
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	if a.ReferenceID == 0 || b.d.DramaDrafts == nil {
		return nil, fail("需要有效 reference_id 和草稿服务")
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	view, err := b.saveDramaReferenceDraft(ctx, tc, uint64(p.ID), a)
	return dramaReferenceOut(view, err, "保存参考分析草稿")
}

// saveDramaReferenceDraft 把模型参数转换为入口专属的 Agent 来源服务请求。
func (b *AgentBridge) saveDramaReferenceDraft(ctx context.Context, tc *toolCall, projectID uint64, a dramaReferenceDraftArgs) (*model.DramaReferenceDraftView, error) {
	switch a.Action {
	case "create":
		if a.ReferenceRevision < 1 {
			return nil, fail("创建草稿必须提供 reference_revision")
		}
		preparationID := ""
		if a.PreparationID != 0 {
			preparationID = idcodec.Encode(uint64(a.PreparationID))
		}
		return b.d.DramaDrafts.CreateForAgent(ctx, tc.run.UserID, projectID, uint64(a.ReferenceID), tc.run.SessionID, tc.run.ID, &model.CreateDramaReferenceDraftReq{
			PreparationID: preparationID, RequestKey: dramaAgentRequestKey(ctx, tc, "draft"), ReferenceRevision: a.ReferenceRevision,
			Instruction: dramaStringValue(a.Instruction), Document: a.Document, Prompts: valueOrEmpty(a.Prompts), Evidence: valueOrEmpty(a.Evidence),
			Observations: valueOrEmpty(a.Observations), Coverage: a.Coverage,
		})
	case "update":
		if a.DraftID == 0 || a.Revision < 1 {
			return nil, fail("更新草稿必须提供 draft_id 和 revision")
		}
		return b.d.DramaDrafts.UpdateForAgent(ctx, tc.run.UserID, projectID, uint64(a.ReferenceID), uint64(a.DraftID), tc.run.SessionID, tc.run.ID, &model.UpdateDramaReferenceDraftReq{
			Revision: a.Revision, Instruction: a.Instruction, Document: a.Document, Prompts: a.Prompts, Evidence: a.Evidence,
			Observations: a.Observations, Coverage: a.Coverage,
		})
	default:
		return nil, fail("action 必须是 create 或 update")
	}
}

// dramaAgentRequestKey derives a stable business key from the durable tool task id.
func dramaAgentRequestKey(ctx context.Context, tc *toolCall, kind string) string {
	key := IdempotencyKeyFrom(ctx)
	if key == "" {
		key = fmt.Sprintf("%d:%s", tc.run.ID, tc.id)
	}
	sum := sha256.Sum256([]byte(key))
	return kind + "-" + hex.EncodeToString(sum[:24])
}

// dramaReferenceLimit applies a safe default and caps tool-controlled pagination.
func dramaReferenceLimit(value, fallback, pageMax int) int {
	if value < 1 {
		return fallback
	}
	return min(value, pageMax)
}

// dramaReferenceOut converts a service result to the standard model-facing JSON response.
func dramaReferenceOut(value any, err error, summary string) (*toolOut, error) {
	if err != nil {
		return nil, err
	}
	content, err := jsonOut(value)
	return &toolOut{data: value, content: content, summary: summary}, err
}

// valueOrEmpty preserves omitted arrays as explicit empty arrays at draft creation.
func valueOrEmpty[T any](value *[]T) []T {
	if value == nil {
		return nil
	}
	return *value
}

// dramaStringValue converts an optional instruction into the model's empty-string default.
func dramaStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
