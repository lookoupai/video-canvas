/** 跟拍沿原镜重拍；原创改编进入已有剧本流程。 */
export type ReferenceMode = "follow" | "rewrite";

/** 采用到片场所需的版本与更新范围。 */
export interface ReferenceAdoptInput {
  /** 已保存的参考修订。 */
  revision: number;
  /** 网络重传必须复用。 */
  requestKey: string;
  /** 制作方式。 */
  mode: ReferenceMode;
  /** 原创改编要求。 */
  instruction?: string;
  /** 省略时创建新集或复用同修订的已有采用。 */
  targetEpisodeNo?: number;
  /** 更新已有集时所见的修订。 */
  episodeRevision?: number;
  /** 更新已有集时显式选择的原镜 ID。 */
  shotIds?: string[];
}

/** 已采用的参考修订与真实分集关联。 */
export interface ReferenceAdoption {
  /** 采用记录 ID。 */
  id: string;
  /** 原参考集 ID。 */
  referenceId: string;
  /** 采用时的参考修订。 */
  referenceRevision: number;
  /** 制作方式。 */
  mode: ReferenceMode;
  /** 目标分集稳定 ID。 */
  episodeId: string;
  /** 当前集号，删除后为零。 */
  episodeNo: number;
  /** 当前目标集修订。 */
  episodeRevision: number;
  /** 本次采用建立或更新的片段 ID。 */
  segmentIds: string[];
  /** 目标集已删除，仍保留记录。 */
  deleted: boolean;
}

/** 后端返回的采用记录。 */
export interface BackendReferenceAdoption {
  /** 采用记录 ID。 */
  id: string;
  /** 原参考集 ID。 */
  reference_id: string;
  /** 采用时的参考修订。 */
  reference_revision: number;
  /** 制作方式。 */
  mode: ReferenceMode;
  /** 目标分集稳定 ID。 */
  episode_id: string;
  /** 当前集号。 */
  episode_no: number;
  /** 目标集修订。 */
  episode_revision: number;
  /** 本次采用涉及的片段 ID。 */
  segment_ids: string[] | null;
  /** 目标集是否已删除。 */
  deleted: boolean;
}

/** 前端保留原片时码与成片裁剪窗口，两者独立。 */
export interface SegmentRemake {
  /** 来源采用记录。 */
  adoptionId: string;
  /** 来源参考集。 */
  referenceId: string;
  /** 固定的原片修订。 */
  referenceRevision: number;
  /** 原视频素材 ID。 */
  assetId: string;
  /** 按顺序裁取的独立原镜区间。 */
  sources: {
    /** 原镜稳定 ID。 */
    shotId: string;
    /** 原片起点毫秒。 */
    startMs: number;
    /** 原片终点毫秒，不包含该帧。 */
    endMs: number;
  }[];
  /** 采用时确认的人物对应。 */
  roles: {
    /** 本集人物 ID。 */
    characterId: string;
    /** 项目目标角色节点。 */
    targetNodeId: string;
    /** 采用时的目标角色名。 */
    targetName: string;
  }[];
  /** 成片保留长度，毫秒。 */
  targetDurationMs: number;
  /** 从生成素材中开始裁取的位置，毫秒。 */
  trimStartMs: number;
}

/** 后端保存的跟拍片段依据。 */
export interface BackendSegmentRemake {
  /** 采用记录。 */
  adoption_id: string;
  /** 参考集。 */
  reference_id: string;
  /** 参考修订。 */
  reference_revision: number;
  /** 原片素材。 */
  asset_id: number | string;
  /** 原镜区间。 */
  sources:
    | {
        /** 原镜 ID。 */
        shot_id: string;
        /** 原片起点毫秒。 */
        start_ms: number;
        /** 原片终点毫秒。 */
        end_ms: number;
      }[]
    | null;
  /** 已确认的角色对应。 */
  roles:
    | {
        /** 本集人物 ID。 */
        character_id: string;
        /** 新角色节点 ID。 */
        target_node_id: string;
        /** 新角色名称。 */
        target_name: string;
      }[]
    | null;
  /** 成片保留毫秒。 */
  target_duration_ms: number;
  /** 采用素材裁剪起点毫秒。 */
  trim_start_ms: number;
}
