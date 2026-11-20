import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";

import type { Drama, Segment } from "@/api/drama/type";
import type { ModelInfo } from "@/api/model/type";
import { selectBoardOutput } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { useRemoteModels } from "@/hooks/use-models";
import { sha256Hex } from "@/utils/drama/adaptation";
import { boardStatus, type BoardNode, type BoardStatus } from "@/utils/drama/board";
import { pickBoardModel, planBoardInput } from "@/utils/drama/board-input";
import type { RefAsset } from "@/utils/drama/segment-assets";
import { lowestUnit } from "@/utils/pricing/quote";
import { submitBoardTask } from "@/utils/tasks/gateway";
import { describeSubmitError } from "@/utils/tasks/submit";

/** 片场里的故事板：每个片段一张宫格图，是脚本到视频之间的一步 */
export type StudioBoards = {
  /** 片段的故事板状态；刚提交、还没读到画布上的任务时也算生成中 */
  statusOf: (segment: Segment) => BoardStatus;
  /** 片段在画布上的故事板节点；没有为 undefined */
  nodeOf: (segment: Segment) => BoardNode | undefined;
  /** 生成故事板用的图片模型（项目设置里选的，没选跟随场景图片模型） */
  model: ModelInfo | undefined;
  /** 一张的最低积分；估不出来为 0 */
  credits: number;
  /** 要补的片段：没有、失败或已过期，且原稿不空 */
  targets: Segment[];
  /** 可用（出过图且没过期）的张数 */
  ready: number;
  /** 不能生成的原因（没有图片模型、正在提交）；能生成为 null */
  blocked: string | null;
  /** 生成这些片段的故事板（单张直接开始，批量由调用方先确认） */
  generate: (segments: Segment[]) => Promise<void>;
  /** 补齐本集：缺的、失败的、过期的，先确认再一起提交 */
  fill: () => Promise<void>;
  /** 把片段故事板的当前版本切到某一版（滑到哪版，生视频就带哪版）；后端改节点后推 canvas.patch，读画布的 hook 据此刷新 */
  selectVersion: (segment: Segment, outputId: string) => Promise<void>;
};

const seqLabel = (segment: Segment) => String(segment.seq + 1).padStart(2, "0");

/**
 * 片场的故事板状态与动作：读画布上的故事板节点算状态，生成时把按模板拼好的请求交给后端，
 * 后端写画布节点、建图片任务，出图后回写节点并推 canvas.patch，读画布的 hook 据此刷新。
 */
export function useStudioBoards(args: {
  drama: Drama;
  no: number;
  segments: Segment[];
  boards: ReadonlyMap<string, BoardNode>;
  /** 片段此刻的原稿（含还没存下的编辑） */
  sourceOf: (segment: Segment) => string;
  /** 文字的 SHA-256；还没算出来为 undefined */
  hashOf: (text: string) => string | undefined;
  /** 编辑中的提示词先存盘 */
  flushEditors: () => Promise<void>;
  /** 重读画布，返回最新的资产；读取失败为 null */
  freshAssets: () => Promise<RefAsset[] | null>;
  /** store 里最新的片段 */
  latestOf: (sid: string) => Segment | null;
}): StudioBoards {
  const { drama, no, segments, boards, sourceOf, hashOf, flushEditors, freshAssets, latestOf } =
    args;
  const remote = useRemoteModels("image");
  const model = useMemo(
    () => pickBoardModel(remote.models, drama.imageModels.board, drama.imageModels.scene),
    [drama.imageModels.board, drama.imageModels.scene, remote.models],
  );
  const template = drama.promptSettings.board.bodies?.board ?? "";
  const credits = lowestUnit(model?.pricing);
  const [submitting, setSubmitting] = useState<ReadonlySet<string>>(new Set());

  const nodeOf = useCallback((segment: Segment) => boards.get(segment.id), [boards]);
  const statusOf = useCallback(
    (segment: Segment): BoardStatus =>
      submitting.has(segment.id)
        ? "running"
        : boardStatus(boards.get(segment.id), hashOf(sourceOf(segment))),
    [boards, hashOf, sourceOf, submitting],
  );
  const targets = useMemo(
    () =>
      segments.filter((segment) => {
        const status = statusOf(segment);
        return (
          (status === "empty" || status === "failed" || status === "stale") &&
          sourceOf(segment).trim() !== ""
        );
      }),
    [segments, sourceOf, statusOf],
  );
  const ready = segments.filter((segment) => statusOf(segment) === "ready").length;
  const blocked = !model
    ? remote.status === "ready"
      ? "没有可用的图片模型"
      : "正在加载图片模型"
    : null;

  const generate = useCallback(
    async (list: Segment[]) => {
      if (!model || list.length === 0) return;
      const ids = list.map((segment) => segment.id);
      setSubmitting((prev) => new Set([...prev, ...ids]));
      try {
        // 编辑中的原稿先存盘，故事板按屏幕上看到的拼
        await flushEditors();
        const assets = await freshAssets();
        if (!assets) {
          toast.error("资产信息读取失败，请稍后重试");
          return;
        }
        const reasons = (
          await Promise.all(
            list.map(async (segment) => {
              const current = latestOf(segment.id) ?? segment;
              const plan = planBoardInput({
                segment: current,
                episodeNo: no,
                hash: hashOf(current.prompt) ?? (await sha256Hex(current.prompt)),
                model,
                template,
                assets,
              });
              if (!plan.body) return plan.error ?? "没能组装故事板";
              try {
                await submitBoardTask(current.id, plan.body);
                return null;
              } catch (e) {
                return describeSubmitError(e).message;
              }
            }),
          )
        ).filter((reason): reason is string => !!reason);
        if (reasons.length > 0) {
          toast.error(
            list.length === 1
              ? `没能开始生成：${reasons[0]}`
              : `${reasons.length} 张没能开始生成：${reasons[0]}`,
          );
        }
        // 节点和任务已经写进画布：读到之后再撤掉「提交中」，状态不会闪回去
        await freshAssets();
      } finally {
        setSubmitting((prev) => new Set([...prev].filter((id) => !ids.includes(id))));
      }
    },
    [flushEditors, freshAssets, hashOf, latestOf, model, no, template],
  );

  const fill = useCallback(async () => {
    if (targets.length === 0) return;
    const go = await confirm({
      title: `生成 ${targets.length} 张故事板？`,
      description: `片段 ${targets.map(seqLabel).join("、")}${credits > 0 ? `，约 ${credits * targets.length} 积分起` : ""}`,
      confirmLabel: "开始生成",
    });
    if (go) await generate(targets);
  }, [credits, generate, targets]);

  const selectVersion = useCallback(
    async (segment: Segment, outputId: string) => {
      try {
        await selectBoardOutput(segment.id, outputId);
        await freshAssets();
      } catch (e) {
        toast.error(dramaErrorMessage(e, "切换失败，请稍后重试"));
      }
    },
    [freshAssets],
  );

  return {
    selectVersion,
    statusOf,
    nodeOf,
    model,
    credits,
    targets,
    ready,
    blocked: blocked ?? (submitting.size > 0 && targets.length === 0 ? "正在提交" : null),
    generate,
    fill,
  };
}
