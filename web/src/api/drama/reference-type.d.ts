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
