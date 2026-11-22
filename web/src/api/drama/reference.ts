import { mapAsset } from "@/api/asset";
import service from "@/utils/requests/service";
import type {
  BackendDramaReference,
  BackendDramaReferences,
  BackendReferenceDocument,
  BackendReferenceDraft,
  BackendReferenceDraftPage,
  BackendReferencePreparationPage,
  BackendReferencePlan,
  ReferenceDraftApplyReceipt,
  ReferenceDraftApplyRequest,
  ReferenceDraftGenerateReceipt,
  ReferenceDraftGenerateRequest,
  DramaReference,
  DramaReferences,
  ReferenceDraft,
  ReferenceDraftPage,
  ReferenceDraftUpdate,
  ReferencePreparation,
  ReferencePlan,
  ReferencePlanRequest,
  ReferenceDocument,
  ReferencePatch,
  ReferenceRole,
} from "./reference-type";

function mapDocument(raw: BackendReferenceDocument): ReferenceDocument {
  return {
    scenes: raw.scenes ?? [],
    characters: raw.characters ?? [],
    shots: (raw.shots ?? []).map((s) => ({
      id: s.id,
      sceneId: s.scene_id,
      startMs: s.start_ms,
      endMs: s.end_ms,
      action: s.action,
      camera: s.camera,
      characters: s.characters ?? [],
    })),
    dialogues: (raw.dialogues ?? []).map((d) => ({
      id: d.id,
      startMs: d.start_ms,
      endMs: d.end_ms,
      text: d.text,
      speaker: d.speaker ?? "",
    })),
  };
}
function toDocument(doc: ReferenceDocument): BackendReferenceDocument {
  return {
    scenes: doc.scenes,
    characters: doc.characters,
    shots: doc.shots.map((s) => ({
      id: s.id,
      scene_id: s.sceneId,
      start_ms: s.startMs,
      end_ms: s.endMs,
      action: s.action,
      camera: s.camera,
      characters: s.characters,
    })),
    dialogues: doc.dialogues.map((d) => ({
      id: d.id,
      start_ms: d.startMs,
      end_ms: d.endMs,
      text: d.text,
      speaker: d.speaker,
    })),
  };
}
function mapEpisode(raw: BackendDramaReference): DramaReference {
  return {
    id: raw.id,
    seq: raw.seq,
    title: raw.title,
    asset: raw.asset ? mapAsset(raw.asset) : null,
    durationMs: raw.duration_ms,
    revision: raw.revision,
    status: raw.status,
    error: raw.error ?? "",
    method: raw.method ?? "",
    document: raw.document ? mapDocument(raw.document) : null,
    bindings: raw.bindings ?? {},
    versions: (raw.versions ?? []).map((v) => ({
      revision: v.revision,
      method: v.method,
      createdAt: v.created_at,
    })),
  };
}
const base = (id: string) => `/dramas/${id}/references`;
const episodePath = (id: string, ep: string) => `${base(id)}/${ep}`;

/**
 * 从参考原片创建短剧项目。
 * @param title 项目名
 * @param key 网络重传复用的请求编号
 * @returns 项目及其角色画布
 */
export async function createReferenceDrama(
  title: string,
  key: string,
): Promise<{ id: string; canvasId: string }> {
  const raw = await service.post<{ id: string; canvas_id: string }>("/drama-reference-projects", {
    title,
    request_key: key,
  });
  return { id: raw.id, canvasId: raw.canvas_id };
}
/**
 * 读取参考剧集、共用角色和项目中可映射的角色资产。
 * @param id 短剧项目 ID
 * @returns 当前集合
 */
export async function getDramaReferences(id: string): Promise<DramaReferences> {
  const raw = await service.get<BackendDramaReferences>(base(id));
  return {
    title: raw.title,
    canvasId: raw.canvas_id,
    revision: raw.revision,
    analyzerAvailable: raw.analyzer_available,
    episodes: (raw.episodes ?? []).map(mapEpisode),
    roles: (raw.roles ?? []).map((r) => ({
      id: r.id,
      name: r.name,
      targetNodeId: r.target_node_id,
      revision: r.revision,
    })),
    targets: (raw.targets ?? []).map((t) => ({ nodeId: t.node_id, name: t.name })),
  };
}
/**
 * 添加一个已上传的原片。
 * @param id 项目 ID
 * @param assetId 原片素材 ID
 * @param title 集名
 * @param key 本次添加的请求编号
 * @returns 新建或重传复用的参考集
 */
export async function addDramaReference(
  id: string,
  assetId: string,
  title: string,
  key: string,
): Promise<DramaReference> {
  return mapEpisode(
    await service.post<BackendDramaReference>(base(id), {
      asset_id: Number(assetId),
      title,
      request_key: key,
    }),
  );
}
/**
 * 导入指定集的拆片包。
 * @param id 项目 ID
 * @param ep 参考集 ID
 * @param revision 该集当前修订
 * @param file 标准分镜 JSON 或 VIDEO.md ZIP
 * @returns 已排队的参考集
 */
export async function importDramaReference(
  id: string,
  ep: string,
  revision: number,
  file: File,
): Promise<DramaReference> {
  const body = new FormData();
  body.set("revision", String(revision));
  body.set("package", file);
  return mapEpisode(
    await service.upload<BackendDramaReference>(`${episodePath(id, ep)}/import`, body),
  );
}
/**
 * 重新分析一个原集。
 * @param id 项目 ID
 * @param ep 参考集 ID
 * @param revision 本集当前修订
 * @returns 已排队状态
 */
export async function analyzeDramaReference(
  id: string,
  ep: string,
  revision: number,
): Promise<DramaReference> {
  return mapEpisode(
    await service.post<BackendDramaReference>(`${episodePath(id, ep)}/analyze`, { revision }),
  );
}
/**
 * 保存本集的角色、镜头和对白校订。
 * @param id 项目 ID
 * @param ep 参考集 ID
 * @param patch 修改内容及修订
 * @returns 保存后的本集文档
 */
export async function saveDramaReference(
  id: string,
  ep: string,
  patch: ReferencePatch,
): Promise<DramaReference> {
  return mapEpisode(
    await service.patch<BackendDramaReference>(episodePath(id, ep), {
      ...patch,
      document: patch.document ? toDocument(patch.document) : undefined,
    }),
  );
}
/**
 * 回看本集某次修订。
 * @param id 项目 ID
 * @param ep 参考集 ID
 * @param revision 需要回看的修订
 * @returns 保存过的文档
 */
export async function getDramaReferenceVersion(
  id: string,
  ep: string,
  revision: number,
): Promise<DramaReference> {
  return mapEpisode(
    await service.get<BackendDramaReference>(episodePath(id, ep), { params: { revision } }),
  );
}
/**
 * 保存参考集序。
 * @param id 项目 ID
 * @param revision 集合修订
 * @param ids 完整的新集序
 * @returns 保存结果
 */
export async function orderDramaReferences(
  id: string,
  revision: number,
  ids: string[],
): Promise<void> {
  await service.put(`${base(id)}/order`, { revision, ids });
}
/**
 * 创建或更新一个跨集原剧角色及其新角色对应。
 * @param id 项目 ID
 * @param roleId 更新的角色 ID，新建时为空
 * @param name 用户确认的名字
 * @param targetNodeId 项目中的新角色节点
 * @param revision 更新所见角色修订
 * @returns 保存后的角色
 */
export async function saveReferenceRole(
  id: string,
  roleId: string | null,
  name: string,
  targetNodeId: string,
  revision?: number,
): Promise<ReferenceRole> {
  const path = `/dramas/${id}/reference-roles`;
  const raw = roleId
    ? await service.patch<{ id: string; name: string; target_node_id: string; revision: number }>(
        `${path}/${roleId}`,
        { name, target_node_id: targetNodeId, revision },
      )
    : await service.post<{ id: string; name: string; target_node_id: string; revision: number }>(
        path,
        { name, target_node_id: targetNodeId },
      );
  return { id: raw.id, name: raw.name, targetNodeId: raw.target_node_id, revision: raw.revision };
}

/** 将后端草稿映射为参考工作台使用的字段。 */
function mapReferenceDraft(raw: BackendReferenceDraft): ReferenceDraft {
  return {
    id: raw.id,
    referenceId: raw.reference_id,
    baseRevision: raw.base_revision,
    revision: raw.revision,
    status: raw.status,
    source: raw.source,
    preparationId: raw.preparation_id,
    instruction: raw.instruction,
    document: mapDocument(raw.document),
    prompts: (raw.prompts ?? []).map((prompt) => ({
      shotId: prompt.shot_id,
      prompt: prompt.prompt,
    })),
    evidence: (raw.evidence ?? []).map((item) => ({
      preparationFrameId: item.preparation_frame_id,
      id: item.id,
      shotId: item.shot_id,
      assetId: item.asset_id,
      atMs: item.at_ms,
    })),
    observations: (raw.observations ?? []).map((item) => ({
      shotId: item.shot_id,
      field: item.field,
      source: item.source,
      reviewState: item.review_state,
      evidenceIds: item.evidence_ids ?? [],
    })),
    coverage: raw.coverage,
    updatedAt: raw.updated_at,
  };
}

/**
 * 分页读取参考集的 Agent 与外部分析草稿。
 * @param id 短剧项目 ID
 * @param referenceId 参考集 ID
 * @param cursor 上一页的下一游标
 * @returns 草稿页和下一游标
 */
export async function listDramaReferenceDrafts(
  id: string,
  referenceId: string,
  cursor = "",
): Promise<ReferenceDraftPage> {
  const page = await service.get<BackendReferenceDraftPage>(
    `${episodePath(id, referenceId)}/drafts`,
    { cursor, limit: 20 },
  );
  return { items: (page.items ?? []).map(mapReferenceDraft), nextCursor: page.next_cursor ?? "" };
}

/**
 * 读取一份草稿的最新修订。
 * @param id 短剧项目 ID
 * @param referenceId 参考集 ID
 * @param draftId 草稿 ID
 * @returns 草稿内容与来源状态
 */
export async function getDramaReferenceDraft(
  id: string,
  referenceId: string,
  draftId: string,
): Promise<ReferenceDraft> {
  const raw = await service.get<BackendReferenceDraft>(
    `${episodePath(id, referenceId)}/drafts/${draftId}`,
  );
  return mapReferenceDraft(raw);
}

const mapReferencePlan = (raw: BackendReferencePlan): ReferencePlan => ({
  draftId: raw.draft_id,
  projectId: raw.project_id,
  referenceId: raw.reference_id,
  draftRevision: raw.draft_revision,
  referenceRevision: raw.reference_revision,
  sourceAssetId: raw.source_asset_id,
  modelId: raw.model_id,
  promptFormat: raw.prompt_format,
  configurationDigest: raw.configuration_digest,
  planDigest: raw.plan_digest,
  executable: raw.executable,
  totalVideoCredits: raw.total_video_credits,
  maxCredits: raw.max_credits,
  units: (raw.units ?? []).map((unit) => ({
    shotId: unit.shot_id,
    mode: unit.mode,
    sourceStartMs: unit.source_start_ms,
    sourceEndMs: unit.source_end_ms,
    duration: unit.duration,
    trimStartMs: unit.trim_start_ms,
    targetDurationMs: unit.target_duration_ms,
    prompt: unit.prompt,
    imageAssetIDs: unit.image_asset_ids ?? [],
    materials: (unit.materials ?? []).map((asset) => ({
      id: asset.id,
      kind: asset.kind,
      mimeType: asset.mime_type,
      sha256: asset.sha256,
      byteSize: asset.byte_size,
      width: asset.width,
      height: asset.height,
      durationMs: asset.duration_ms,
    })),
    input: unit.input,
    credits: unit.credits,
    issues: unit.issues ?? [],
  })),
  issues: raw.issues ?? [],
});

/**
 * 按服务器实际模型配置编译只读单镜路线报价。
 * @param id 短剧项目 ID
 * @param referenceId 参考集 ID
 * @param draftId 分析草稿 ID
 * @param request 当前草稿修订、生成模型与选镜参数
 * @returns 可重验的计划摘要与站内积分报价
 */
export async function quoteDramaReferenceDraft(
  id: string,
  referenceId: string,
  draftId: string,
  request: ReferencePlanRequest,
): Promise<ReferencePlan> {
  const raw = await service.post<BackendReferencePlan>(
    `${episodePath(id, referenceId)}/drafts/${draftId}/quote`,
    {
      revision: request.revision,
      model_id: request.modelId,
      units: request.units.map((unit) => ({
        shot_id: unit.shotId,
        mode: unit.mode,
        image_asset_ids: unit.imageAssetIDs,
        duration: unit.duration,
        trim_start_ms: unit.trimStartMs,
        target_duration_ms: unit.targetDurationMs,
        params: unit.params,
      })),
      max_credits: request.maxCredits,
    },
  );
  return mapReferencePlan(raw);
}

/**
 * 原子采用已报价的跟拍镜头计划并保存业务回执。
 * @param id 短剧项目 ID
 * @param referenceId 原片参考集 ID
 * @param draftId 独立分析草稿 ID
 * @param request 已确认的报价摘要与稳定请求键
 * @returns 新建或恢复的采用回执
 */
export async function applyDramaReferenceDraft(
  id: string,
  referenceId: string,
  draftId: string,
  request: ReferenceDraftApplyRequest,
): Promise<ReferenceDraftApplyReceipt> {
  const raw = await service.post<{
    application_id: string;
    draft_id: string;
    draft_revision: number;
    plan_digest: string;
    adoption_id: string;
    episode_id: string;
    episode_no: number;
    episode_revision: number;
    segments: {
      shot_id: string;
      segment_id: string;
      revision: number;
      trim_start_ms: number;
      target_duration_ms: number;
    }[];
    replayed: boolean;
  }>(`${episodePath(id, referenceId)}/drafts/${draftId}/apply`, {
    revision: request.revision,
    request_key: request.requestKey,
    plan_digest: request.planDigest,
    mode: request.mode,
    instruction: request.instruction,
    quote: toQuoteRequest(request.quote),
    target_episode_no: request.targetEpisodeNo,
    episode_revision: request.episodeRevision,
  });
  return {
    applicationId: raw.application_id,
    draftId: raw.draft_id,
    draftRevision: raw.draft_revision,
    planDigest: raw.plan_digest,
    adoptionId: raw.adoption_id,
    episodeId: raw.episode_id,
    episodeNo: raw.episode_no,
    episodeRevision: raw.episode_revision,
    segments: raw.segments.map((item) => ({
      shotId: item.shot_id,
      segmentId: item.segment_id,
      revision: item.revision,
      trimStartMs: item.trim_start_ms,
      targetDurationMs: item.target_duration_ms,
    })),
    replayed: raw.replayed,
  };
}

/**
 * 对已采用且重新校验的镜头计划批量提交平台生成任务。
 * @param id 短剧项目 ID
 * @param referenceId 原片参考集 ID
 * @param draftId 分析草稿 ID
 * @param request 已批准业务回执、报价摘要和稳定请求键
 * @returns 可查询的逐镜生成任务
 */
export async function generateDramaReferenceDraft(
  id: string,
  referenceId: string,
  draftId: string,
  request: ReferenceDraftGenerateRequest,
): Promise<ReferenceDraftGenerateReceipt> {
  const raw = await service.post<{
    application_id: string;
    plan_digest: string;
    status: string;
    items: {
      shot_id: string;
      segment_id: string;
      task_id?: string;
      status: string;
      error?: string;
    }[];
    replayed: boolean;
  }>(`${episodePath(id, referenceId)}/drafts/${draftId}/generate`, {
    revision: request.revision,
    request_key: request.requestKey,
    application_id: request.applicationId,
    plan_digest: request.planDigest,
    quote: toQuoteRequest(request.quote),
  });
  return {
    applicationId: raw.application_id,
    planDigest: raw.plan_digest,
    status: raw.status,
    items: raw.items.map((item) => ({
      shotId: item.shot_id,
      segmentId: item.segment_id,
      taskId: item.task_id,
      status: item.status,
      error: item.error,
    })),
    replayed: raw.replayed,
  };
}

function toQuoteRequest(request: ReferencePlanRequest) {
  return {
    revision: request.revision,
    model_id: request.modelId,
    units: request.units.map((unit) => ({
      shot_id: unit.shotId,
      mode: unit.mode,
      image_asset_ids: unit.imageAssetIDs,
      duration: unit.duration,
      trim_start_ms: unit.trimStartMs,
      target_duration_ms: unit.targetDurationMs,
      params: unit.params,
    })),
    max_credits: request.maxCredits,
  };
}

/**
 * 分页读取媒体准备状态，刷新后可恢复镜头和帧图进度。
 * @param id 短剧项目 ID
 * @param referenceId 参考集 ID
 * @returns 当前页准备任务
 */
export async function listDramaReferencePreparations(
  id: string,
  referenceId: string,
): Promise<ReferencePreparation[]> {
  const page = await service.get<BackendReferencePreparationPage>(
    `${episodePath(id, referenceId)}/preparations`,
    { limit: 20 },
  );
  return (page.items ?? []).map((item) => ({
    id: item.id,
    referenceId: item.reference_id,
    baseRevision: item.base_revision,
    sourceAssetId: item.source_asset_id,
    durationMs: item.duration_ms,
    processorVersion: item.processor_version,
    status: item.status,
    attempt: item.attempt,
    shotCount: item.shot_count,
    totalFrames: item.total_frames,
    completedFrames: item.completed_frames,
    hasAudio: item.has_audio,
    audioStatus: item.audio_status,
    error: item.error,
    createdAt: item.created_at,
    updatedAt: item.updated_at,
  }));
}

/**
 * 取消参考集的媒体准备并保留已经完成的帧图。
 * @param id 短剧项目 ID
 * @param referenceId 参考集 ID
 * @param preparationId 媒体准备 ID
 */
export async function cancelDramaReferencePreparation(
  id: string,
  referenceId: string,
  preparationId: string,
): Promise<void> {
  await service.post(`${episodePath(id, referenceId)}/preparations/${preparationId}/cancel`);
}

/**
 * 使用稳定请求键恢复未完成的媒体准备。
 * @param id 短剧项目 ID
 * @param referenceId 参考集 ID
 * @param preparationId 媒体准备 ID
 * @param requestKey 本次恢复操作的幂等键
 */
export async function resumeDramaReferencePreparation(
  id: string,
  referenceId: string,
  preparationId: string,
  requestKey: string,
): Promise<void> {
  await service.post(`${episodePath(id, referenceId)}/preparations/${preparationId}/resume`, {
    request_key: requestKey,
  });
}

/**
 * 按草稿修订保存人工核对结果。
 * @param id 短剧项目 ID
 * @param referenceId 参考集 ID
 * @param draftId 草稿 ID
 * @param update 本次要替换的字段
 * @returns 保存后的草稿修订
 */
export async function updateDramaReferenceDraft(
  id: string,
  referenceId: string,
  draftId: string,
  update: ReferenceDraftUpdate,
): Promise<ReferenceDraft> {
  const raw = await service.patch<BackendReferenceDraft>(
    `${episodePath(id, referenceId)}/drafts/${draftId}`,
    {
      revision: update.revision,
      instruction: update.instruction,
      document: update.document ? toDocument(update.document) : undefined,
      prompts: update.prompts?.map((prompt) => ({ shot_id: prompt.shotId, prompt: prompt.prompt })),
      evidence: update.evidence?.map((item) => ({
        preparation_frame_id: item.preparationFrameId,
        id: item.id,
        shot_id: item.shotId,
        asset_id: item.assetId,
        at_ms: item.atMs,
      })),
      observations: update.observations?.map((item) => ({
        shot_id: item.shotId,
        field: item.field,
        source: item.source,
        review_state: item.reviewState,
        evidence_ids: item.evidenceIds,
      })),
      coverage: update.coverage,
    },
  );
  return mapReferenceDraft(raw);
}
