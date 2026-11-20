import type {
  CreateDramaInput,
  PromptChoice,
  PromptStep,
  SourceEstimate,
  SourcePreflight,
  StartRatio,
  UnitKind,
} from "@/api/drama/type";

import type { Capabilities } from "@/api/model/type";
import { parseAspect } from "@/utils/canvas/node-frame";
import { matchOption, openParams, type ParamEntry } from "@/utils/tasks/capabilities";

import {
  CUSTOM_STYLE_KEY,
  DEFAULT_EPISODE_SECONDS,
  MAX_EPISODE_SECONDS,
  MIN_EPISODE_SECONDS,
} from "./options";
import { PROMPT_STEPS } from "./prompts";

/** 开拍设置可选的画幅（宽、高用来画比例示意框） */
export const START_RATIOS: { key: StartRatio; w: number; h: number }[] = [
  { key: "9:16", w: 12, h: 20 },
  { key: "16:9", w: 24, h: 14 },
  { key: "4:3", w: 20, h: 15 },
];

/** 默认画幅：竖屏短剧常用 */
export const DEFAULT_START_RATIO: StartRatio = "9:16";

/** 三类资产各自的图片模型 */
export interface StartImageModels {
  character: string;
  scene: string;
  prop: string;
}

/** 图片模型全部不指定 */
export const emptyImageModels = (): StartImageModels => ({ character: "", scene: "", prop: "" });

/** 三类资产 */
export type AssetSlot = keyof StartImageModels;

/** 一类资产在弹窗里手动选的比例、分辨率；空串 = 没选过 */
export interface AssetParamPicks {
  ratio: string;
  resolution: string;
}

/** 三类资产各自手动选的比例、分辨率 */
export type StartImageParams = Record<AssetSlot, AssetParamPicks>;

/** 比例、分辨率都没手动选 */
export const emptyImageParams = (): StartImageParams => ({
  character: { ratio: "", resolution: "" },
  scene: { ratio: "", resolution: "" },
  prop: { ratio: "", resolution: "" },
});

/** 没手动选比例时各类资产想要的比例：人物三视图、场景宽银幕、物品横图 */
export const ASSET_DEFAULT_RATIOS: Record<AssetSlot, string> = {
  character: "16:9",
  scene: "21:9",
  prop: "16:9",
};

/** 比例参数的两种写法，和画布节点一致 */
const RATIO_PARAM_NAMES = ["aspect_ratio", "ratio"];

/** 模型里能预设的比例、分辨率参数（开放的枚举）；模型没有的项为 undefined */
export function assetParamFields(caps: Capabilities | undefined): {
  ratio?: ParamEntry;
  resolution?: ParamEntry;
} {
  const fields = openParams(caps).filter((field) => field.type === "enum");
  return {
    ratio: fields.find((field) => RATIO_PARAM_NAMES.includes(field.name)),
    resolution: fields.find((field) => field.name === "resolution"),
  };
}

/**
 * 在选项里找和 want 最接近的比例（按宽高比的对数差）。
 * @returns 选项里有 want 就是它；都认不出（如只有 Auto）为 undefined
 */
export function closestRatio(options: readonly unknown[], want: string): string | undefined {
  const exact = options.find((option) => String(option) === want);
  if (exact !== undefined) return String(exact);
  const target = parseAspect(want);
  if (target === null) return undefined;
  let best: string | undefined;
  let gap = Infinity;
  for (const option of options) {
    const value = parseAspect(option);
    if (value === null) continue;
    const d = Math.abs(Math.log(value / target));
    if (d < gap) {
      gap = d;
      best = String(option);
    }
  }
  return best;
}

/**
 * 按模型把一类资产的比例、分辨率落到下拉里能选中的值：比例取手动选的（没选取类型默认）最接近的一项，
 * 认不出时用模型默认；分辨率手动选的模型有就用，否则用模型默认。只在读取时推导，不回写。
 * @returns 参数名 → 取值，如 { aspect_ratio: "16:9", resolution: "2K" }；模型没有的参数不出现
 */
export function settleAssetParams(
  caps: Capabilities | undefined,
  slot: AssetSlot,
  picks: AssetParamPicks,
): Record<string, string> {
  const { ratio, resolution } = assetParamFields(caps);
  const out: Record<string, string> = {};
  if (ratio) {
    const value =
      closestRatio(ratio.options ?? [], picks.ratio || ASSET_DEFAULT_RATIOS[slot]) ??
      (ratio.default === undefined ? undefined : String(ratio.default));
    if (value !== undefined) out[ratio.name] = value;
  }
  if (resolution) {
    const value = matchOption(resolution, picks.resolution) ?? resolution.default;
    if (value !== undefined) out[resolution.name] = String(value);
  }
  return out;
}

/** 用户在弹窗里确认的原文类型；识别不出且没选时为 null */
export type StartKind = "script" | "novel";

/** 五个环节的提示词选择（每个环节都有值，默认内置） */
export type StartPrompts = Record<PromptStep, PromptChoice>;

/** 开拍设置弹窗里用户可改的全部内容 */
export interface StartForm {
  /** 确认的原文类型；null 表示识别不出、用户还没选 */
  kind: StartKind | null;
  /** 规划到第几个单元，0 = 全部 */
  rangeEnd: number;
  /** 画幅 */
  ratio: StartRatio;
  /** 题材 key，空串 = 不指定 */
  genre: string;
  /** 画风 key */
  styleKey: string;
  /** 自定义画风描述 */
  styleCustom: string;
  /** 单集时长（秒） */
  seconds: number;
  /** 文本模型 key，空串 = 默认模型 */
  textModel: string;
  /** 视频模型 key，空串 = 不指定（跟随后端 / 画布默认） */
  videoModel: string;
  /** 人物 / 场景 / 物品资产各自的图片模型 key，空串 = 不指定 */
  imageModels: StartImageModels;
  /** 人物 / 场景 / 物品资产各自手动选的比例、分辨率 */
  imageParams: StartImageParams;
  /** 五个环节的提示词选择 */
  prompts: StartPrompts;
}

/** 记住的上次选择：规划范围每本书不同，不记 */
export type StartPrefs = Omit<StartForm, "kind" | "rangeEnd">;

/** 各环节全是内置 */
export const builtinPrompts = (): StartPrompts => ({
  plan: { source: "builtin" },
  scripts: { source: "builtin" },
  assets: { source: "builtin" },
  asset_prompts: { source: "builtin" },
  video: { source: "builtin" },
  board: { source: "builtin" },
});

const UNIT_WORD: Record<UnitKind, string> = {
  chapter: "章",
  episode: "集",
  scene: "场",
  chunk: "段",
};

/** 单元的量词：章 / 集 / 场 / 段 */
export const unitWord = (kind: UnitKind): string => UNIT_WORD[kind];

/**
 * 规划范围终点归一：整数、落在 1..总数，非法值回落 1
 * @param value 用户输入
 * @param total 单元总数
 */
export function normalizeRangeEnd(value: number, total: number): number {
  if (!Number.isFinite(value)) return 1;
  return Math.max(1, Math.min(Math.max(1, total), Math.floor(value)));
}

/** 预计集数：精确时一个数，否则「下限–上限」 */
export function episodesLabel(
  est: Pick<SourceEstimate, "episodesMin" | "episodesMax" | "episodesExact">,
): string {
  if (est.episodesExact || est.episodesMin === est.episodesMax) {
    return est.episodesMin.toLocaleString("en-US");
  }
  return `${est.episodesMin.toLocaleString("en-US")}–${est.episodesMax.toLocaleString("en-US")}`;
}

/** 预计耗时：不到 1 小时按分钟（最少 1 分钟），否则按小时保留一位 */
export function durationLabel(minutes: number): string {
  if (minutes < 60) return `${Math.max(1, Math.round(minutes))} 分钟`;
  return `${(minutes / 60).toFixed(1).replace(/\.0$/, "")} 小时`;
}

/** 字数文案：满万按「x.x 万字」，否则「n 字」 */
export function charsLabel(chars: number): string {
  if (chars >= 10_000) return `${(chars / 10_000).toFixed(1).replace(/\.0$/, "")} 万字`;
  return `${chars.toLocaleString("en-US")} 字`;
}

/**
 * 单集时长要不要锁：剧本带「第N集」时按原分集切，时长不再可选，胶囊显示「按原分集 · N 集」；
 * 其余情况可选（剧本无分集标记时只作为按场次切集的参考长度）
 * @param kind 确认的类型
 * @param pre 预检结果
 */
export function secondsLock(
  kind: StartKind | null,
  pre: Pick<SourcePreflight, "unitKind" | "unitsTotal">,
): { locked: boolean; label: string | null } {
  if (kind === "script" && pre.unitKind === "episode") {
    return { locked: true, label: `按原分集 · ${pre.unitsTotal} 集` };
  }
  return { locked: false, label: null };
}

/**
 * 「开始」为什么不能点；可以点返回 null
 * @param state 类型是否已确认、画风是否已有、估价是否说所选模型装不下原文（67022）
 */
export function startBlockReason(state: {
  kind: StartKind | null;
  styleKey: string;
  modelBlocked: boolean;
}): string | null {
  if (!state.kind) return "先确认是剧本还是小说";
  if (!state.styleKey) return "先选一个画风";
  if (state.modelBlocked) return "所选模型装不下原文，请换一个";
  return null;
}

/** 提示词选择转成请求里的 prompts：各环节都显式带上 */
export function promptsToChoices(prompts: StartPrompts): Record<PromptStep, PromptChoice> {
  const out = {} as Record<PromptStep, PromptChoice>;
  for (const { key } of PROMPT_STEPS) out[key] = { ...prompts[key] };
  return out;
}

/** 三个图片模型里只留有值的；一个都没选返回 undefined（请求里整个不带） */
function pickedImageModels(models: StartImageModels): CreateDramaInput["imageModels"] {
  const picked = Object.fromEntries(Object.entries(models).filter(([, key]) => !!key));
  return Object.keys(picked).length > 0 ? picked : undefined;
}

/** 三类资产的参数里只留有值的；一个都没有返回 undefined（请求里整个不带） */
function pickedImageParams(
  params: Partial<Record<AssetSlot, Record<string, string>>> | undefined,
): CreateDramaInput["imageParams"] {
  const picked = Object.fromEntries(
    Object.entries(params ?? {}).filter(([, value]) => Object.keys(value ?? {}).length > 0),
  );
  return Object.keys(picked).length > 0 ? picked : undefined;
}

/**
 * 弹窗内容转成建剧请求。剧本型输入：分集规划、分集剧本强制内置，规划范围恒为全部（剧本一次切完）。
 * @param pre 预检结果
 * @param form 弹窗里的设置，kind 必须已确认
 * @param imageParams 三类资产按各自模型落好的比例、分辨率（settleAssetParams 的结果）
 */
export function buildCreateInput(
  pre: SourcePreflight,
  form: StartForm,
  imageParams?: Partial<Record<AssetSlot, Record<string, string>>>,
): CreateDramaInput {
  if (!form.kind) throw new Error("原文类型未确认");
  const script = form.kind === "script";
  const prompts = promptsToChoices(form.prompts);
  if (script) {
    prompts.plan = { source: "builtin" };
    prompts.scripts = { source: "builtin" };
  }
  return {
    sourceToken: pre.sourceToken,
    sourceKind: form.kind,
    rangeEnd: script ? 0 : form.rangeEnd,
    ratio: form.ratio,
    genre: form.genre || undefined,
    styleKey: form.styleKey,
    styleCustom: form.styleKey === CUSTOM_STYLE_KEY ? form.styleCustom.trim() : undefined,
    episodeSeconds: form.seconds,
    textModel: form.textModel,
    videoModel: form.videoModel || undefined,
    imageModels: pickedImageModels(form.imageModels),
    imageParams: pickedImageParams(imageParams),
    prompts,
  };
}

/** 记住上次选择用的 localStorage key */
const PREFS_KEY = "drama-start-prefs";

const str = (value: unknown): string => (typeof value === "string" ? value : "");

/** 记住的一类资产比例、分辨率；类型不对的项按没选 */
const loadPicks = (value: Partial<AssetParamPicks> | undefined): AssetParamPicks => ({
  ratio: str(value?.ratio),
  resolution: str(value?.resolution),
});

/** 读上次的选择；没有、损坏或存储不可用时返回 null；画幅、时长不合法时回落默认 */
export function loadStartPrefs(): StartPrefs | null {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    if (!raw) return null;
    const data = JSON.parse(raw) as Partial<StartPrefs> | null;
    if (!data || typeof data !== "object") return null;
    const prompts = builtinPrompts();
    // 老版本把分集规划和分集剧本记成一个「剧本改编」(adapt)：同时带给两个环节，找不到的由 reconcilePrompts 回落内置
    const saved = data.prompts as (Partial<StartPrompts> & { adapt?: PromptChoice }) | undefined;
    const legacy = saved?.adapt;
    for (const { key } of PROMPT_STEPS) {
      const choice = saved?.[key] ?? (key === "plan" || key === "scripts" ? legacy : undefined);
      if (choice && ["builtin", "mine", "skill"].includes(choice.source)) prompts[key] = choice;
    }
    return {
      ratio: START_RATIOS.some((r) => r.key === data.ratio)
        ? (data.ratio as StartRatio)
        : DEFAULT_START_RATIO,
      genre: typeof data.genre === "string" ? data.genre : "",
      styleKey: typeof data.styleKey === "string" ? data.styleKey : "",
      styleCustom: typeof data.styleCustom === "string" ? data.styleCustom : "",
      seconds:
        typeof data.seconds === "number" &&
        Number.isInteger(data.seconds) &&
        data.seconds >= MIN_EPISODE_SECONDS &&
        data.seconds <= MAX_EPISODE_SECONDS
          ? data.seconds
          : DEFAULT_EPISODE_SECONDS,
      textModel: typeof data.textModel === "string" ? data.textModel : "",
      videoModel: typeof data.videoModel === "string" ? data.videoModel : "",
      imageModels: {
        character: str(data.imageModels?.character),
        scene: str(data.imageModels?.scene),
        prop: str(data.imageModels?.prop),
      },
      imageParams: {
        character: loadPicks(data.imageParams?.character),
        scene: loadPicks(data.imageParams?.scene),
        prop: loadPicks(data.imageParams?.prop),
      },
      prompts,
    };
  } catch {
    return null;
  }
}

/** 记住这次的选择（规划范围不记）；存储不可用时静默忽略 */
export function saveStartPrefs(form: StartForm): void {
  try {
    const prefs: StartPrefs = {
      ratio: form.ratio,
      genre: form.genre,
      styleKey: form.styleKey,
      styleCustom: form.styleCustom,
      seconds: form.seconds,
      textModel: form.textModel,
      videoModel: form.videoModel,
      imageModels: form.imageModels,
      imageParams: form.imageParams,
      prompts: form.prompts,
    };
    localStorage.setItem(PREFS_KEY, JSON.stringify(prefs));
  } catch {
    /** 存储被禁用或已满：记不住而已，不影响开拍 */
  }
}

/**
 * 记住的提示词选择回落：被删的「我的提示词」、已停用的技能改回内置
 * @param prompts 记住的选择
 * @param mine 各环节现有的我的提示词
 * @param skills 各环节现有的可用技能
 */
export function reconcilePrompts(
  prompts: StartPrompts,
  mine: Record<PromptStep, { id: string }[]>,
  skills: Record<PromptStep, { name: string }[]>,
): StartPrompts {
  const next = builtinPrompts();
  for (const { key } of PROMPT_STEPS) {
    const choice = prompts[key];
    if (choice.source === "mine" && mine[key].some((item) => item.id === choice.id)) {
      next[key] = choice;
    } else if (
      choice.source === "skill" &&
      skills[key].some((item) => item.name === choice.skill)
    ) {
      next[key] = choice;
    }
  }
  return next;
}
