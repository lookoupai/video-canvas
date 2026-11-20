import { mapAsset } from "@/api/asset";
import type { TaskView } from "@/api/generation-task/type";
import service from "@/utils/requests/service";
import { mapRemakeSource, mapTaskLite } from "./index";
import type {
  BackendRemakeComposition,
  BackendRemakePlan,
  BackendRemakePlanSummary,
  RemakeComposition,
  RemakePlan,
  RemakePlanSummary,
  RemakeShotPatch,
} from "./remake-type";

function mapPlan(raw: BackendRemakePlan): RemakePlan {
  return {
    id: raw.id,
    scriptId: raw.script_id,
    scriptRevision: raw.script_revision,
    revision: raw.revision,
    videoModel: raw.video_model,
    refVideoAssetId: raw.ref_video_asset_id ? String(raw.ref_video_asset_id) : null,
    sourceCatalog: (raw.source_catalog ?? []).map(mapRemakeSource),
    shots: (raw.shots ?? []).map((shot) => ({
      id: shot.id,
      revision: shot.revision,
      scene: shot.scene,
      narration: shot.narration,
      durationMs: shot.duration_ms,
      trimStartMs: shot.trim_start_ms,
      unresolved: shot.unresolved,
      sources: (shot.sources ?? []).map(mapRemakeSource),
      chosenTaskId: shot.chosen_task_id ? String(shot.chosen_task_id) : null,
      takes: (shot.takes ?? []).map(mapTaskLite),
      generateSeconds: shot.generate_seconds,
      renderCredits: shot.render_credits,
      generateError: shot.generate_error,
    })),
  };
}

function mapComposition(raw: BackendRemakeComposition): RemakeComposition {
  return {
    id: raw.id,
    revision: raw.revision,
    idempotencyKey: raw.idempotency_key,
    shotIds: raw.shot_ids ?? [],
    status: raw.status,
    error: raw.error,
    asset: raw.asset ? mapAsset(raw.asset) : null,
    createdAt: raw.created_at,
  };
}

const planPath = (planId: string) => `/remake-plans/${planId}`;
const plansPath = (scriptId: string) => `/marketing-scripts/${scriptId}/remake-plans`;

/**
 * 列出一份脚本各次修订已经建立的制作计划。
 * @param scriptId 脚本 ID
 * @returns 从新到旧的计划摘要
 */
export async function listRemakePlans(scriptId: string): Promise<RemakePlanSummary[]> {
  const rows = await service.get<BackendRemakePlanSummary[]>(plansPath(scriptId));
  return (rows ?? []).map((row) => ({ id: row.id, scriptRevision: row.script_revision }));
}

/**
 * 在当前所见的脚本修订上建计划，同一修订再次提交返回已有计划。
 * @param scriptId 脚本 ID
 * @param scriptRevision 屏幕上跟拍脚本的修订
 * @returns 制作计划与镜头详情
 */
export async function createRemakePlan(
  scriptId: string,
  scriptRevision: number,
): Promise<RemakePlan> {
  return mapPlan(
    await service.post<BackendRemakePlan>(plansPath(scriptId), { revision: scriptRevision }),
  );
}

/**
 * 读取计划选版、镜序与任务状态。
 * @param planId 制作计划 ID
 * @returns 当前计划详情
 */
export async function getRemakePlan(planId: string): Promise<RemakePlan> {
  return mapPlan(await service.get<BackendRemakePlan>(planPath(planId)));
}

/**
 * 保存当前镜头改动，包括成片入点、原片区间和采用版本。
 * @param planId 制作计划 ID
 * @param shotId 计划内镜头 ID
 * @param patch 用户改过的字段与编辑时的计划修订
 * @returns 保存后的完整计划
 */
export async function updateRemakeShot(
  planId: string,
  shotId: number,
  patch: RemakeShotPatch,
): Promise<RemakePlan> {
  const { revision, scene, narration, durationMs, trimStartMs, sources, chosenTaskId } = patch;
  const data = {
    revision,
    scene,
    narration,
    duration_ms: durationMs,
    trim_start_ms: trimStartMs,
    chosen_task_id: chosenTaskId === undefined ? undefined : Number(chosenTaskId),
    sources: sources?.map((ref) => ({
      ref_shot: ref.refShot,
      start_ms: Math.round(ref.start * 1000),
      end_ms: Math.round(ref.end * 1000),
    })),
  };
  return mapPlan(
    await service.patch<BackendRemakePlan>(`${planPath(planId)}/shots/${shotId}`, data),
  );
}

/**
 * 保存整份计划的成片镜序。
 * @param planId 制作计划 ID
 * @param planRevision 改动前的计划修订
 * @param shotIds 新顺序下的全部镜号
 * @returns 保存后的完整计划
 */
export async function orderRemakeShots(
  planId: string,
  planRevision: number,
  shotIds: number[],
): Promise<RemakePlan> {
  return mapPlan(
    await service.put<BackendRemakePlan>(`${planPath(planId)}/order`, {
      revision: planRevision,
      shot_ids: shotIds,
    }),
  );
}

/**
 * 向生成网关提供单镜重拍接口。
 * @param planId 制作计划 ID
 * @param shotId 计划内镜号
 * @param shotRevision 要生成的镜头修订
 * @param key 同一操作重传时沿用的幂等键
 * @returns 平台生成任务快照
 */
export function generateRemakeShot(
  planId: string,
  shotId: number,
  shotRevision: number,
  key: string,
): Promise<TaskView> {
  return service.post<TaskView>(
    `${planPath(planId)}/shots/${shotId}/generate`,
    { revision: shotRevision },
    { headers: { "Idempotency-Key": key } },
  );
}

/**
 * 提交已选版本和剪辑窗的合成快照。
 * @param planId 制作计划 ID
 * @param planRevision 提交时的计划修订
 * @param key 对同一份合成快照重试时沿用的幂等键
 * @param shotIds 明确要求部分合成的镜号，省略则按全部镜头合成
 * @returns 新建或重用的合成记录
 */
export async function composeRemakePlan(
  planId: string,
  planRevision: number,
  key: string,
  shotIds?: number[],
): Promise<RemakeComposition> {
  const raw = await service.post<BackendRemakeComposition>(
    `${planPath(planId)}/compose`,
    { revision: planRevision, shot_ids: shotIds },
    { headers: { "Idempotency-Key": key } },
  );
  return mapComposition(raw);
}

/**
 * 读取计划的历史合成结果。
 * @param planId 制作计划 ID
 * @returns 从新到旧的合成快照与成片
 */
export async function listRemakeCompositions(planId: string): Promise<RemakeComposition[]> {
  const rows = await service.get<BackendRemakeComposition[]>(`${planPath(planId)}/compositions`);
  return (rows ?? []).map(mapComposition);
}
