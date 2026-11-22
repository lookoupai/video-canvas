package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

const maxReferenceDraftBytes = 2 << 20

// normalizeDraftRequest 深复制公开输入并统一空集合，指纹不受后续参考稿变更影响。
func normalizeDraftRequest(req *model.CreateDramaReferenceDraftReq) (*model.CreateDramaReferenceDraftReq, string, error) {
	if req == nil || req.ReferenceRevision < 1 {
		return nil, "", errcode.ErrInvalidParams
	}
	raw, err := json.Marshal(req)
	if err != nil || len(raw) > maxReferenceDraftBytes {
		return nil, "", errcode.ErrInvalidParams
	}
	var input model.CreateDramaReferenceDraftReq
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, "", err
	}
	input.RequestKey = strings.TrimSpace(input.RequestKey)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.RequestKey == "" || !validDraftText(input.RequestKey, 128) || !validDraftText(input.Instruction, 4000) {
		return nil, "", errcode.ErrInvalidParams
	}
	input.Prompts, input.Evidence = draftSlice(input.Prompts), draftSlice(input.Evidence)
	input.Observations = draftSlice(input.Observations)
	if input.Document != nil {
		normalizeDraftDocument(input.Document)
	}
	if input.Coverage != nil {
		normalizeDraftCoverage(input.Coverage)
	}
	for i := range input.Observations {
		input.Observations[i].EvidenceIDs = draftSlice(input.Observations[i].EvidenceIDs)
	}
	raw, err = json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(raw)
	return &input, hex.EncodeToString(hash[:]), nil
}

// initialDraftContent 默认只推导已有镜头边界完成，不把离线 ready 误当成视觉理解完成。
func initialDraftContent(ref *model.DramaReferenceEpisode, req *model.CreateDramaReferenceDraftReq) (model.DramaReferenceDraftContent, error) {
	content := model.DramaReferenceDraftContent{Instruction: req.Instruction, Prompts: req.Prompts,
		Evidence: req.Evidence, Observations: req.Observations}
	if req.Document != nil {
		content.Document = *req.Document
	} else {
		doc, err := referenceDocumentOf(ref)
		if err != nil {
			return content, err
		}
		content.Document = doc
	}
	if req.Coverage != nil {
		content.Coverage = *req.Coverage
	} else if len(content.Document.Shots) > 0 {
		content.Coverage.Shots = "complete"
	}
	normalizeDraftContent(&content)
	return content, nil
}

// draftResult 返回持久化内容和并发控制元数据，已有错误优先保留。
func draftResult(row *model.DramaReferenceDraft, err error) (*model.DramaReferenceDraftView, error) {
	if err != nil {
		return nil, err
	}
	content, err := draftContentOf(row)
	if err != nil {
		return nil, err
	}
	return &model.DramaReferenceDraftView{DramaReferenceDraftContent: content, ID: idcodec.Encode(row.ID),
		PreparationID: optionalDraftID(row.PreparationID), ReferenceID: idcodec.Encode(row.ReferenceID), BaseRevision: row.BaseRevision, Revision: row.Revision,
		Status: row.Status, Source: row.Source, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

// draftContentOf 解析草稿整体内容，空集合统一返回数组以便客户端直接恢复。
func draftContentOf(row *model.DramaReferenceDraft) (model.DramaReferenceDraftContent, error) {
	var content model.DramaReferenceDraftContent
	if err := json.Unmarshal(row.Content, &content); err != nil {
		return content, err
	}
	normalizeDraftContent(&content)
	return content, nil
}

// normalizeDraftContent 统一可选集合和未知覆盖状态，不推断任何语义结论。
func normalizeDraftContent(content *model.DramaReferenceDraftContent) {
	content.Instruction = strings.TrimSpace(content.Instruction)
	content.Prompts = draftSlice(content.Prompts)
	content.Evidence = draftSlice(content.Evidence)
	content.Observations = draftSlice(content.Observations)
	for i := range content.Observations {
		content.Observations[i].EvidenceIDs = draftSlice(content.Observations[i].EvidenceIDs)
	}
	normalizeDraftDocument(&content.Document)
	normalizeDraftCoverage(&content.Coverage)
}

// normalizeDraftDocument 保证空草稿和空人物列表也能用稳定 JSON 结构表达。
func normalizeDraftDocument(doc *model.DramaReferenceDocument) {
	doc.Scenes, doc.Characters = draftSlice(doc.Scenes), draftSlice(doc.Characters)
	doc.Shots, doc.Dialogues = draftSlice(doc.Shots), draftSlice(doc.Dialogues)
	for i := range doc.Shots {
		doc.Shots[i].Characters = draftSlice(doc.Shots[i].Characters)
	}
}

// normalizeDraftCoverage 缺省值表示尚未分析，只有调用者明确填写才表示已完成。
func normalizeDraftCoverage(coverage *model.DramaReferenceCoverage) {
	for _, value := range []*string{&coverage.Shots, &coverage.Visual, &coverage.Dialogue, &coverage.Speakers} {
		if *value == "" {
			*value = "not_analyzed"
		}
	}
}

// draftSlice 统一 nil 与空集合，避免网络重传因 JSON null 和 [] 不同而改变指纹。
func draftSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// validDraftText 同时限制 UTF-8 与字符数，保持中文文本与 HTTP 校验一致。
func validDraftText(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit
}

// optionalDraftID 保持未绑定准备的历史草稿字段为空。
func optionalDraftID(id uint64) string {
	if id == 0 {
		return ""
	}
	return idcodec.Encode(id)
}
