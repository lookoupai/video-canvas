import { mapAsset } from "@/api/asset";
import service from "@/utils/requests/service";
import type {
  BackendDramaReference,
  BackendDramaReferences,
  BackendReferenceDocument,
  DramaReference,
  DramaReferences,
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
