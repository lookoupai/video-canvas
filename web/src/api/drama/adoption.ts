import service from "@/utils/requests/service";
import type {
  BackendReferenceAdoption,
  ReferenceAdoption,
  ReferenceAdoptInput,
} from "./remake-type";

/** 将当前集号与历史关联转换为前端数据，已删除记录仍可展示。 */
const mapAdoption = (row: BackendReferenceAdoption): ReferenceAdoption => ({
  id: row.id,
  referenceId: row.reference_id,
  referenceRevision: row.reference_revision,
  mode: row.mode,
  episodeId: row.episode_id,
  episodeNo: row.episode_no,
  episodeRevision: row.episode_revision,
  segmentIds: row.segment_ids ?? [],
  deleted: row.deleted,
});

/**
 * 采用一个已保存的参考修订；重传由调用方保留 requestKey。
 * @param id 短剧项目 ID
 * @param reference 参考集 ID
 * @param input 制作方式、修订与更新范围
 * @returns 目标分集及本次涉及的片段
 */
export async function adoptDramaReference(
  id: string,
  reference: string,
  input: ReferenceAdoptInput,
): Promise<ReferenceAdoption> {
  const body = Object.fromEntries(
    Object.entries({
      revision: input.revision,
      request_key: input.requestKey,
      mode: input.mode,
      instruction: input.instruction,
      target_episode_no: input.targetEpisodeNo,
      episode_revision: input.episodeRevision,
      shot_ids: input.shotIds,
    }).filter(([, value]) => value !== undefined),
  );
  return mapAdoption(
    await service.post<BackendReferenceAdoption>(
      `/dramas/${id}/references/${reference}/adopt`,
      body,
      { silent: true },
    ),
  );
}

/**
 * 读取参考修订与片场的关联，刷新后可恢复入口。
 * @param id 短剧项目 ID
 * @returns 按后端顺序排列的采用记录
 */
export async function listReferenceAdoptions(id: string): Promise<ReferenceAdoption[]> {
  const result = await service.get<BackendReferenceAdoption[] | null>(
    `/dramas/${id}/reference-adoptions`,
    undefined,
    { silent: true },
  );
  return (result ?? []).map(mapAdoption);
}
