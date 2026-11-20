import type { AssetDto, BackendAssetDto } from "@/api/asset/type";
import type { BackendRemakeSource, BackendTaskLite, RemakeSource, TaskLite } from "./type";

/** 制作计划摘要，同一脚本的不同修订分别建计划。 */
export interface RemakePlanSummary {
  /** 计划业务 ID */
  id: string;
  /** 建计划时采用的脚本修订号 */
  scriptRevision: number;
}

/** 制作计划里的一镜，重排后仍按 ID 关联任务与原片。 */
export interface RemakePlanShot {
  /** 计划内的稳定镜号，从 1 起 */
  id: number;
  /** 生成前核对的镜头修订 */
  revision: number;
  /** 动作、场景和运镜描述 */
  scene: string;
  /** 目标稿旁白 */
  narration: string;
  /** 合成时保留的成片时长，毫秒 */
  durationMs: number;
  /** 在采用视频中的剪辑入点，毫秒 */
  trimStartMs: number;
  /** 按采用顺序返回的原片区间 */
  sources: RemakeSource[];
  /** 是否有待关联的来源 */
  unresolved: boolean;
  /** 采用的成功任务 ID */
  chosenTaskId: string | null;
  /** 本镜历次生成的任务摘要 */
  takes: TaskLite[];
  /** 模型实际申请的整数秒数 */
  generateSeconds: number;
  /** 生成一次的预计积分 */
  renderCredits: number;
  /** 当前模型或来源不满足生成条件的原因 */
  generateError: string;
}

/** 计划当前的制作镜序与所用模型。 */
export interface RemakePlan extends RemakePlanSummary {
  /** 所属脚本业务 ID */
  scriptId: string;
  /** 编辑、重排及合成时核对的计划修订号 */
  revision: number;
  /** 计划固定的视频模型 key */
  videoModel: string;
  /** 原片素材 ID，缺失时为 null */
  refVideoAssetId: string | null;
  /** 本次计划可引用的原镜及其完整边界 */
  sourceCatalog: RemakeSource[];
  /** 按最新成片镜序排列 */
  shots: RemakePlanShot[];
}

/** 改镜或采用成功任务。省略字段保留当前值。 */
export type RemakeShotPatch = Partial<
  Pick<RemakePlanShot, "scene" | "narration" | "durationMs" | "trimStartMs" | "sources">
> & {
  /** 打开编辑稿时的计划修订 */
  revision: number;
  /** 要采用的本镜成功任务 ID */
  chosenTaskId?: string;
};

/** 一个合成快照与它的执行结果。 */
export interface RemakeComposition {
  /** 合成业务 ID */
  id: string;
  /** 合成采用的计划修订 */
  revision: number;
  /** 重传时复用的合成键 */
  idempotencyKey: string;
  /** 此次合成范围内的镜号，按成片镜序 */
  shotIds: number[];
  /** 排队中、合成成功或失败 */
  status: "queued" | "succeeded" | "failed";
  /** 成片素材 */
  asset: AssetDto | null;
  /** 合成失败原因 */
  error: string;
  /** 提交合成的时间 */
  createdAt: string;
}

/** 后端按脚本列出的计划摘要。 */
export interface BackendRemakePlanSummary {
  /** 计划业务 ID */
  id: string;
  /** 原脚本修订 */
  script_revision: number;
}

/** 后端返回的计划详情。 */
export interface BackendRemakePlan extends BackendRemakePlanSummary {
  /** 所属脚本业务 ID */
  script_id: string;
  /** 计划修订 */
  revision: number;
  /** 视频模型 key */
  video_model: string;
  /** 原片素材主键 */
  ref_video_asset_id: number | null;
  /** 固定于计划的原片镜号和时间边界 */
  source_catalog: BackendRemakeSource[] | null;
  /** 按展示顺序返回的全部镜头 */
  shots: Array<
    Pick<RemakePlanShot, "id" | "revision" | "scene" | "narration" | "unresolved"> & {
      /** 成片保留时长，毫秒 */
      duration_ms: number;
      /** 采用视频的入点，毫秒 */
      trim_start_ms: number;
      /** 原片毫秒区间 */
      sources: BackendRemakeSource[] | null;
      /** 采用的成功任务主键 */
      chosen_task_id: number | null;
      /** 历次单镜生成结果 */
      takes: BackendTaskLite[] | null;
      /** 预计申请的模型时长，秒 */
      generate_seconds: number;
      /** 本次预计积分 */
      render_credits: number;
      /** 无法生成的原因 */
      generate_error: string;
    }
  > | null;
}

/** 后端返回的合成快照。 */
export interface BackendRemakeComposition extends Pick<
  RemakeComposition,
  "id" | "revision" | "status" | "error"
> {
  /** 本次提交的幂等键 */
  idempotency_key: string;
  /** 被采用的镜号 */
  shot_ids: number[];
  /** 成片素材视图 */
  asset: BackendAssetDto | null;
  /** 提交时间 */
  created_at: string;
}
