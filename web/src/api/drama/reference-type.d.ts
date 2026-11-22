import type { AssetDto, BackendAssetDto } from "@/api/asset/type";

/** 一集参考视频中的角色，身份只在本集内有效。 */
export interface ReferenceCharacter {
  /** 拆片包里的稳定身份 */
  id: string;
  /** 显示名称 */
  name: string;
}
/** 原片场次。 */
export interface ReferenceScene {
  /** 稳定场次 ID */
  id: string;
  /** 场次标题 */
  title: string;
}
/** 原片分镜，使用本集原片毫秒。 */
export interface ReferenceShot {
  /** 稳定镜头 ID */
  id: string;
  /** 所属原片场次 */
  sceneId: string;
  /** 包含起点，毫秒 */
  startMs: number;
  /** 不包含终点，毫秒 */
  endMs: number;
  /** 画面动作 */
  action: string;
  /** 运镜 */
  camera: string;
  /** 本集角色 ID 列表 */
  characters: string[];
}
/** 对白独立保存，一句可跨镜、同镜可有多名说话人。 */
export interface ReferenceDialogue {
  /** 稳定对白 ID */
  id: string;
  /** 起点毫秒 */
  startMs: number;
  /** 终点毫秒 */
  endMs: number;
  /** 台词文本 */
  text: string;
  /** 本集角色 ID，空串表示待确认 */
  speaker: string;
}
/** 当前拆解或人工校订的完整文档。 */
export interface ReferenceDocument {
  /** 场次 */
  scenes: ReferenceScene[];
  /** 本集角色 */
  characters: ReferenceCharacter[];
  /** 原片镜序 */
  shots: ReferenceShot[];
  /** 独立对白 */
  dialogues: ReferenceDialogue[];
}
/** 后端原始镜头毫秒字段。 */
export type BackendReferenceShot = Omit<ReferenceShot, "sceneId" | "startMs" | "endMs"> & {
  /** 场次 ID */
  scene_id: string;
  /** 起点毫秒 */
  start_ms: number;
  /** 终点毫秒 */
  end_ms: number;
};
/** 后端对白字段。 */
export type BackendReferenceDialogue = Omit<ReferenceDialogue, "startMs" | "endMs"> & {
  /** 起点毫秒 */
  start_ms: number;
  /** 终点毫秒 */
  end_ms: number;
};
/** 后端拆解文档。 */
export interface BackendReferenceDocument {
  /** 场次 */
  scenes: ReferenceScene[];
  /** 本集角色 */
  characters: ReferenceCharacter[];
  /** 原片镜序 */
  shots: BackendReferenceShot[];
  /** 对白 */
  dialogues: BackendReferenceDialogue[];
}
/** 一集参考原片的导入与校订状态。 */
export interface DramaReference {
  /** 参考记录 ID */
  id: string;
  /** 当前集序，从 1 起 */
  seq: number;
  /** 标题 */
  title: string;
  /** 已上传原片 */
  asset: AssetDto | null;
  /** 实测时长，毫秒 */
  durationMs: number;
  /** 乐观锁修订 */
  revision: number;
  /** 导入或分析状态 */
  status: "idle" | "queued" | "ready" | "failed";
  /** 失败原因 */
  error: string;
  /** 分析来源 */
  method: string;
  /** 当前编辑稿，尚无结果时为空 */
  document: ReferenceDocument | null;
  /** 本集角色 ID 对应全剧角色 ID */
  bindings: Record<string, string>;
  /** 保存过的分析和校订修订 */
  versions: { revision: number; method: string; createdAt: string }[];
}
/** 后端参考剧集。 */
export interface BackendDramaReference extends Omit<
  DramaReference,
  "asset" | "durationMs" | "document" | "versions"
> {
  /** 素材视图 */
  asset: BackendAssetDto | null;
  /** 时长，毫秒 */
  duration_ms: number;
  /** 后端拆解文档 */
  document: BackendReferenceDocument | null;
  /** 修订历史 */
  versions: { revision: number; method: string; created_at: string }[];
}
/** 跨集复用的原剧角色，与新角色画布节点独立。 */
export interface ReferenceRole {
  /** 全剧角色 ID */
  id: string;
  /** 用户确认的名字 */
  name: string;
  /** 新角色所在画布节点，空串表示待选 */
  targetNodeId: string;
  /** 角色记录修订 */
  revision: number;
}
/** 项目参考剧集集合。 */
export interface DramaReferences {
  /** 项目标题 */
  title: string;
  /** 角色所在画布 */
  canvasId: string;
  /** 集序的修订 */
  revision: number;
  /** 本机分析器是否就绪 */
  analyzerAvailable: boolean;
  /** 按集序排列的原片 */
  episodes: DramaReference[];
  /** 跨集角色 */
  roles: ReferenceRole[];
  /** 可映射的新角色节点 */
  targets: { nodeId: string; name: string }[];
}
/** 后端集合响应。 */
export interface BackendDramaReferences {
  /** 标题 */
  title: string;
  /** 画布 ID */
  canvas_id: string;
  /** 修订 */
  revision: number;
  /** 分析器可用 */
  analyzer_available: boolean;
  /** 原片列表 */
  episodes: BackendDramaReference[];
  /** 跨集角色 */
  roles: { id: string; name: string; target_node_id: string; revision: number }[];
  /** 项目中的新角色 */
  targets: { node_id: string; name: string }[];
}
/** 一次校订请求携带所见的修订。 */
export interface ReferencePatch {
  /** 所见修订 */
  revision: number;
  /** 修改后的拆解文档 */
  document?: ReferenceDocument;
  /** 本集角色映射 */
  bindings?: Record<string, string>;
  /** 标题 */
  title?: string;
}

/** Agent 或外部分析写入的逐镜提示词。 */
export interface ReferenceDraftPrompt {
  /** 本参考集镜头 ID */
  shotId: string;
  /** 等待核对或采用的提示词 */
  prompt: string;
}

/** 可编辑草稿中的镜头证据引用。 */
export interface ReferenceDraftEvidence {
  /** 系统持久提帧 ID；普通外部图片没有 */
  preparationFrameId?: string;
  /** 草稿内稳定证据 ID */
  id: string;
  /** 所属参考镜头 */
  shotId: string;
  /** 当前用户素材 ID */
  assetId: number;
  /** 原片时间，毫秒 */
  atMs: number;
}

/** 草稿字段的观察来源和核对状态。 */
export interface ReferenceDraftObservation {
  /** 所属参考镜头 */
  shotId: string;
  /** 被描述字段 */
  field: "action" | "camera" | "characters" | "prompt";
  /** 字段来源 */
  source: "observation" | "imported" | "manual";
  /** 待核对程度 */
  reviewState: "observed" | "inferred" | "unknown" | "confirmed";
  /** 支持该字段的草稿证据 ID */
  evidenceIds: string[];
}

/** 分别记录镜头、视觉、对白和说话人的分析覆盖。 */
export interface ReferenceDraftCoverage {
  /** 镜头边界覆盖 */
  shots: "not_analyzed" | "partial" | "complete" | "unavailable";
  /** 视觉语义覆盖 */
  visual: "not_analyzed" | "partial" | "complete" | "unavailable";
  /** 对白覆盖 */
  dialogue: "not_analyzed" | "partial" | "complete" | "unavailable";
  /** 说话人覆盖 */
  speakers: "not_analyzed" | "partial" | "complete" | "unavailable";
}

/** 持久化的 Agent／外部分析结果，独立于正式参考稿。 */
export interface ReferenceDraft {
  /** 草稿 ID */
  id: string;
  /** 参考集 ID */
  referenceId: string;
  /** 分析依据的参考修订 */
  baseRevision: number;
  /** 草稿乐观锁修订 */
  revision: number;
  /** 编辑状态 */
  status: string;
  /** 最近编辑来源 */
  source: "agent" | "external" | "manual";
  /** 冻结的媒体准备 ID */
  preparationId?: string;
  /** Agent 与外部客户端共用的分析文档 */
  document: ReferenceDocument;
  /** 当前用户意图 */
  instruction: string;
  /** 逐镜目标提示词 */
  prompts: ReferenceDraftPrompt[];
  /** 原片画面证据 */
  evidence: ReferenceDraftEvidence[];
  /** 字段来源与状态 */
  observations: ReferenceDraftObservation[];
  /** 多维分析覆盖状态 */
  coverage: ReferenceDraftCoverage;
  /** 最近保存时间 */
  updatedAt: string;
}

/** 后端草稿字段保持 snake_case。 */
export interface BackendReferenceDraft {
  /** 草稿 ID */
  id: string;
  /** 参考集 ID */
  reference_id: string;
  /** 分析依据的参考修订 */
  base_revision: number;
  /** 草稿乐观锁修订 */
  revision: number;
  /** 编辑状态 */
  status: string;
  /** 最近编辑来源 */
  source: "agent" | "external" | "manual";
  /** 冻结的媒体准备 ID */
  preparation_id?: string;
  /** Agent 与外部客户端共用的分析文档 */
  document: BackendReferenceDocument;
  /** 当前用户意图 */
  instruction: string;
  /** 逐镜目标提示词 */
  prompts: { shot_id: string; prompt: string }[];
  /** 原片画面证据 */
  evidence: {
    preparation_frame_id?: string;
    id: string;
    shot_id: string;
    asset_id: number;
    at_ms: number;
  }[];
  /** 字段来源与状态 */
  observations: {
    shot_id: string;
    field: ReferenceDraftObservation["field"];
    source: ReferenceDraftObservation["source"];
    review_state: ReferenceDraftObservation["reviewState"];
    evidence_ids: string[];
  }[];
  /** 多维分析覆盖状态 */
  coverage: ReferenceDraftCoverage;
  /** 最近保存时间 */
  updated_at: string;
}

/** 更新草稿只提交需要替换的字段。 */
export interface ReferenceDraftUpdate {
  /** 所见草稿修订 */
  revision: number;
  /** 更新用户意图 */
  instruction?: string;
  /** 替换分析文档 */
  document?: ReferenceDocument;
  /** 替换逐镜提示词 */
  prompts?: ReferenceDraftPrompt[];
  /** 替换画面证据 */
  evidence?: ReferenceDraftEvidence[];
  /** 替换观察状态 */
  observations?: ReferenceDraftObservation[];
  /** 替换分析覆盖 */
  coverage?: ReferenceDraftCoverage;
}

/** 参考集草稿的游标分页。 */
export interface ReferenceDraftPage {
  /** 当前页草稿 */
  items: ReferenceDraft[];
  /** 下一页游标；空串表示结束 */
  nextCursor: string;
}

/** 后端草稿分页。 */
export interface BackendReferenceDraftPage {
  /** 当前页草稿 */
  items: BackendReferenceDraft[];
  /** 下一页游标；空串表示结束 */
  next_cursor: string;
}

/** 单镜路线报价输入。 */
export interface ReferencePlanUnitRequest {
  /** 草稿中的稳定镜头 ID */
  shotId: string;
  /** 自动、文字、首帧、图片或视频参考路线 */
  mode: "auto" | "text" | "keyframe" | "image" | "video";
  /** 已上传并属于当前用户的参考图 ID */
  imageAssetIDs?: string[];
  /** 申请视频生成秒数，0 表示按模型配置 */
  duration?: number;
  /** 成片裁剪起点，毫秒 */
  trimStartMs?: number;
  /** 成片保留长度，毫秒 */
  targetDurationMs?: number;
  /** 模型允许的额外请求参数 */
  params?: Record<string, unknown>;
}

/** 参考计划报价输入。 */
export interface ReferencePlanRequest {
  /** 最新草稿修订 */
  revision: number;
  /** 已启用的视频模型 ID */
  modelId: string;
  /** 本次报价范围 */
  units: ReferencePlanUnitRequest[];
  /** 用户设定的站内积分上限 */
  maxCredits?: number;
}

/** 生成计划中的校验问题。 */
export interface ReferencePlanIssue {
  /** 稳定问题代码 */
  code: string;
  /** 对应字段 */
  field: string;
  /** 用户可读说明 */
  message: string;
  /** 是否阻止执行 */
  blocking: boolean;
}

/** 报价绑定的参考素材快照。 */
export interface ReferencePlanAsset {
  /** 素材 ID */
  id: string;
  /** image 或 video */
  kind: string;
  /** 媒体格式 */
  mimeType: string;
  /** 素材内容摘要 */
  sha256: string;
  /** 文件大小，字节 */
  byteSize: number;
  /** 图片或视频宽度 */
  width: number;
  /** 图片或视频高度 */
  height: number;
  /** 媒体时长，毫秒 */
  durationMs: number;
}

/** 一个原镜对应的规范化生成路线与独立输出裁剪窗口。 */
export interface ReferencePlanUnit {
  /** 草稿镜头 ID */
  shotId: string;
  /** text、keyframe、image 或 video */
  mode: string;
  /** 原片窗口起点，毫秒 */
  sourceStartMs: number;
  /** 原片窗口终点，毫秒 */
  sourceEndMs: number;
  /** 规范化模型申请秒数 */
  duration: number;
  /** 成片裁剪起点，毫秒 */
  trimStartMs: number;
  /** 成片保留长度，毫秒 */
  targetDurationMs: number;
  /** 已保存的目标提示词 */
  prompt: string;
  /** 本镜所用图片素材 ID */
  imageAssetIDs: string[];
  /** 报价采用的媒体快照 */
  materials: ReferencePlanAsset[];
  /** 实际规范化的上游请求 */
  input?: Record<string, unknown>;
  /** 站内积分，null 表示不可报价 */
  credits: number | null;
  /** 本镜路线限制 */
  issues: ReferencePlanIssue[];
}

/** 只读报价和后续审批重验摘要。 */
export interface ReferencePlan {
  /** 参考分析草稿 ID */
  draftId: string;
  /** 项目 ID */
  projectId: string;
  /** 参考集 ID */
  referenceId: string;
  /** 所见草稿修订 */
  draftRevision: number;
  /** 计划依据的参考修订 */
  referenceRevision: number;
  /** 原片素材 ID */
  sourceAssetId: string;
  /** 生成模型 ID */
  modelId: string;
  /** 模型提示词格式 */
  promptFormat: string;
  /** 模型、渠道和插件配置摘要 */
  configurationDigest: string;
  /** 完整计划摘要，提交前必须复核 */
  planDigest: string;
  /** 本计划是否可执行 */
  executable: boolean;
  /** 所有镜头预计站内积分 */
  totalVideoCredits: number;
  /** 用户设置的最高积分 */
  maxCredits?: number;
  /** 逐镜生成计划 */
  units: ReferencePlanUnit[];
  /** 计划级问题 */
  issues: ReferencePlanIssue[];
}

/** 报价接口的后端 snake_case 请求与响应。 */
export interface BackendReferencePlan {
  draft_id: string;
  project_id: string;
  reference_id: string;
  draft_revision: number;
  reference_revision: number;
  source_asset_id: string;
  model_id: string;
  prompt_format: string;
  configuration_digest: string;
  plan_digest: string;
  executable: boolean;
  total_video_credits: number;
  max_credits?: number;
  units: {
    shot_id: string;
    mode: string;
    source_start_ms: number;
    source_end_ms: number;
    duration: number;
    trim_start_ms: number;
    target_duration_ms: number;
    prompt: string;
    image_asset_ids: string[];
    materials: {
      id: string;
      kind: string;
      mime_type: string;
      sha256: string;
      byte_size: number;
      width: number;
      height: number;
      duration_ms: number;
    }[];
    input?: Record<string, unknown>;
    credits: number | null;
    issues: { code: string; field: string; message: string; blocking: boolean }[];
  }[];
  issues: { code: string; field: string; message: string; blocking: boolean }[];
}

/** 采用一份已报价的跟拍计划。 */
export interface ReferenceDraftApplyRequest {
  /** 所见草稿修订 */
  revision: number;
  /** 网络重试时保持不变的操作键 */
  requestKey: string;
  /** 报价时返回的稳定摘要 */
  planDigest: string;
  /** 参考跟拍方式 */
  mode: "follow";
  /** 对原片的整体创作意图 */
  instruction?: string;
  /** 需要按相同配置重验的完整报价输入 */
  quote: ReferencePlanRequest;
  /** 可选的现有目标分集号 */
  targetEpisodeNo?: number;
  /** 更新既有分集时的所见修订 */
  episodeRevision?: number;
}

/** 提交已采用镜头的批量生成任务。 */
export interface ReferenceDraftGenerateRequest {
  /** 所见草稿修订 */
  revision: number;
  /** 网络重试时保持不变的操作键 */
  requestKey: string;
  /** 已原子采用的业务回执 */
  applicationId: string;
  /** 采用时确认的报价摘要 */
  planDigest: string;
  /** 与已采用计划完全相同的报价输入 */
  quote: ReferencePlanRequest;
}

/** 已采用镜头与目标片段修订。 */
export interface ReferenceDraftAppliedSegment {
  /** 原片镜头 ID */
  shotId: string;
  /** 短剧片段 ID */
  segmentId: string;
  /** 新片段修订 */
  revision: number;
  /** 成片裁剪起点，毫秒 */
  trimStartMs: number;
  /** 成片保留时长，毫秒 */
  targetDurationMs: number;
}

/** 原子采用回执。 */
export interface ReferenceDraftApplyReceipt {
  /** 采用业务回执 ID */
  applicationId: string;
  /** 分析草稿 ID */
  draftId: string;
  /** 采用依据的草稿修订 */
  draftRevision: number;
  /** 报价摘要 */
  planDigest: string;
  /** 正式采用记录 ID */
  adoptionId: string;
  /** 目标短剧分集 ID */
  episodeId: string;
  /** 当前集号 */
  episodeNo: number;
  /** 新分集修订 */
  episodeRevision: number;
  /** 当前计划对应的所有镜头 */
  segments: ReferenceDraftAppliedSegment[];
  /** 是否复用了已经提交的同一操作 */
  replayed: boolean;
}

/** 批量生成任务。 */
export interface ReferenceDraftGenerateTask {
  /** 原片镜头 ID */
  shotId: string;
  /** 目标片段 ID */
  segmentId: string;
  /** 平台生成任务 ID */
  taskId?: string;
  /** 当前任务状态 */
  status: string;
  /** 提交该镜时的业务错误 */
  error?: string;
}

/** 批量生成回执。 */
export interface ReferenceDraftGenerateReceipt {
  /** 原子采用回执 */
  applicationId: string;
  /** 执行计划摘要 */
  planDigest: string;
  /** 批次提交状态 */
  status: string;
  /** 逐镜结果 */
  items: ReferenceDraftGenerateTask[];
  /** 是否复用了相同业务键 */
  replayed: boolean;
}

/** 持久媒体准备任务摘要。 */
export interface ReferencePreparation {
  /** 准备任务 ID */
  id: string;
  /** 参考集 ID */
  referenceId: string;
  /** 冻结的参考修订 */
  baseRevision: number;
  /** 原片素材 ID */
  sourceAssetId: number;
  /** 原片时长，毫秒 */
  durationMs: number;
  /** 处理器版本 */
  processorVersion: string;
  /** 持久任务状态 */
  status: "queued" | "running" | "succeeded" | "failed" | "canceled";
  /** 当前尝试次数 */
  attempt: number;
  /** 选镜数 */
  shotCount: number;
  /** 计划帧数 */
  totalFrames: number;
  /** 已完成帧数 */
  completedFrames: number;
  /** null 表示尚未探测音轨 */
  hasAudio: boolean | null;
  /** 语义分析状态，音轨存在不代表对白已分析 */
  audioStatus: "not_analyzed" | "unavailable";
  /** 可恢复的失败原因 */
  error: string;
  /** 创建时间 */
  createdAt: string;
  /** 最近更新 */
  updatedAt: string;
}

/** 后端媒体准备分页。 */
export interface BackendReferencePreparationPage {
  /** 当前页准备任务 */
  items: {
    id: string;
    reference_id: string;
    base_revision: number;
    source_asset_id: number;
    duration_ms: number;
    processor_version: string;
    status: ReferencePreparation["status"];
    attempt: number;
    shot_count: number;
    total_frames: number;
    completed_frames: number;
    has_audio: boolean | null;
    audio_status: ReferencePreparation["audioStatus"];
    error: string;
    created_at: string;
    updated_at: string;
  }[];
  /** 下一页游标；空串表示结束 */
  next_cursor: string;
}
