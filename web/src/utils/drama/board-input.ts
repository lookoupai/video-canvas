import type { BoardGenerateBody, Segment } from "@/api/drama/type";
import type { ModelInfo } from "@/api/model/type";
import type { NodeDrama } from "@/types";
import { parsePrompt } from "@/utils/canvas/prompt-tokens";
import { buildFreshInput } from "@/utils/tasks/fresh-input";

import { BOARD_RATIO, boardPrompt, type NodeBoardMeta } from "./board";
import { assetLinks } from "./segment-input";
import type { RefAsset } from "./segment-assets";
import { segmentParams } from "./video-models";

/** 故事板节点的初始数据：提示词、参数、模型、drama.board 标记和要连进来的资产 */
export type BoardSeed = {
  /** 节点标题，如「故事板 · 第 3 集 · 01」 */
  label: string;
  data: {
    prompt: string;
    params: Record<string, unknown>;
    model?: string;
    /** 没有 type：不进资产列表，也不能被片段 @ 引用 */
    drama: NodeDrama & { board: NodeBoardMeta };
  };
  /** 要连线到故事板节点的资产节点：原稿里 @ 的、画布上还在的，按出现顺序，不超过参考图上限 */
  refs: string[];
  /** 给用户看的提醒（没识别到时间轴、镜头过多），没有为空串 */
  notice: string;
};

/**
 * 按片段原稿拼一个故事板节点：按所选模板拼的宫格提示词、整图 16:9、所选图片模型、drama.board 标记（片段与出图时的原稿哈希），
 * 以及要连进来的资产节点（参考图靠连线带入，提示词里的 @ 由节点展开成图片编号）。
 */
export function boardNodeSeed(args: {
  segment: Pick<Segment, "id" | "seq" | "prompt">;
  episodeNo: number;
  /** 当前原稿的 SHA-256 */
  hash: string;
  model: ModelInfo | undefined;
  /** 项目设置里选的故事板提示词模板正文 */
  template: string;
  /** 画布上现有的节点 id */
  nodeIds: ReadonlySet<string>;
}): BoardSeed {
  const built = boardPrompt(args.segment.prompt, args.template);
  const caps = args.model?.capabilities;
  const max = caps?.refs?.image?.on ? caps.refs.image.max : 0;
  const refs: string[] = [];
  for (const seg of parsePrompt(built.text)) {
    if (seg.type !== "ref" || refs.includes(seg.id) || !args.nodeIds.has(seg.id)) continue;
    if (refs.length >= max) break;
    refs.push(seg.id);
  }
  const no = String(args.segment.seq + 1).padStart(2, "0");
  return {
    label: `故事板 · 第 ${args.episodeNo} 集 · ${no}`,
    data: {
      prompt: built.text,
      params: {
        prompt: built.text,
        ...segmentParams(caps, { seconds: 0, ratio: BOARD_RATIO, overrides: {} }),
      },
      ...(args.model ? { model: args.model.key } : {}),
      drama: {
        episodes: [args.episodeNo],
        board: {
          segmentId: args.segment.id,
          sourceHash: args.hash,
          layout: built.layout,
          shotCount: built.shotCount,
        },
      },
    },
    refs,
    notice: built.notice,
  };
}

/**
 * 故事板用哪个图片模型：项目设置里选了且仍在清单里就用它；否则跟随场景图片模型；都没有取第一个。
 * @param models 图片模型清单
 * @param boardKey 项目设置里的故事板模型，空串 = 没选
 * @param sceneKey 场景图片模型，空串 = 没指定
 */
export function pickBoardModel(
  models: readonly ModelInfo[],
  boardKey: string,
  sceneKey: string,
): ModelInfo | undefined {
  return (
    models.find((info) => !!boardKey && info.key === boardKey) ??
    models.find((info) => !!sceneKey && info.key === sceneKey) ??
    models[0]
  );
}

/** 片场生成一张故事板要交给后端的内容；不能提交时 error 写原因 */
export type BoardPlan = {
  body: BoardGenerateBody | null;
  /** 不能提交的原因（没有模型、参考图缺图等）；能提交为 null */
  error: string | null;
  /** 给用户看的提醒（没识别到时间轴、镜头过多），没有为空串 */
  notice: string;
};

/**
 * 组装片场「生成故事板」的请求：按模板拼宫格提示词（boardNodeSeed），原稿里 @ 的资产按图片节点的规则带成参考图，
 * 提示词里的 @ 展开成图片编号（buildFreshInput）。后端据此写画布上的故事板节点并建任务。
 * @param args.assets 画布上的资产（资产库同一份），只带画布上还在、已经出图的
 */
export function planBoardInput(args: {
  segment: Pick<Segment, "id" | "seq" | "prompt">;
  episodeNo: number;
  /** 当前原稿的 SHA-256 */
  hash: string;
  model: ModelInfo | undefined;
  /** 项目设置里选的故事板提示词模板正文 */
  template: string;
  assets: readonly RefAsset[];
}): BoardPlan {
  const seed = boardNodeSeed({ ...args, nodeIds: new Set(args.assets.map((asset) => asset.id)) });
  if (!args.model) return { body: null, error: "没有可用的图片模型", notice: seed.notice };
  if (!args.segment.prompt.trim()) {
    return { body: null, error: "片段原稿是空的", notice: seed.notice };
  }
  const links = assetLinks(seed.data.prompt, args.assets).filter((link) =>
    seed.refs.includes(link.sourceId),
  );
  const built = buildFreshInput(args.model.capabilities, seed.data.params, links);
  const error = Object.values(built.errors)[0] ?? null;
  const { board } = seed.data.drama;
  return {
    body: error
      ? null
      : {
          model_id: args.model.key,
          input: built.input,
          prompt: seed.data.prompt,
          params: seed.data.params,
          label: seed.label,
          source_hash: board.sourceHash,
          layout: board.layout,
          shot_count: board.shotCount,
          refs: seed.refs,
        },
    error,
    notice: seed.notice,
  };
}
