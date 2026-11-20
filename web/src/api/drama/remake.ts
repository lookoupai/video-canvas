import type { BackendSegmentRemake, SegmentRemake } from "./remake-type";

/**
 * 原镜区间与目标窗口分别转换，不用模型整数秒覆盖精确时码。
 * @param raw 后端跟拍依据，普通片段为空
 * @returns 跟拍依据或 null
 */
export function mapSegmentRemake(
  raw: BackendSegmentRemake | null | undefined,
): SegmentRemake | null {
  if (!raw?.reference_id) return null;
  return {
    adoptionId: raw.adoption_id,
    referenceId: raw.reference_id,
    referenceRevision: raw.reference_revision,
    assetId: String(raw.asset_id),
    sources: (raw.sources ?? []).map((source) => ({
      shotId: source.shot_id,
      startMs: source.start_ms,
      endMs: source.end_ms,
    })),
    roles: (raw.roles ?? []).map((role) => ({
      characterId: role.character_id,
      targetNodeId: role.target_node_id,
      targetName: role.target_name,
    })),
    targetDurationMs: raw.target_duration_ms,
    trimStartMs: raw.trim_start_ms,
  };
}
