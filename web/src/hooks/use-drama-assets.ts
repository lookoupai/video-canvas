import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { getCanvas } from "@/api/canvas";
import { useTasksStore } from "@/store/tasks";
import { subscribeCanvasPatch } from "@/utils/agent/patch-bus";
import type { TaskMap } from "@/utils/tasks/merge";
import { boardsFromGraph, type BoardNode } from "@/utils/drama/board";
import { assetsFromGraph, type RefAsset } from "@/utils/drama/segment-assets";
import { followLatest } from "@/utils/latest-call";
import { reconcileTaskIds } from "@/utils/ws/task-events";

export type DramaAssetsState = {
  assets: RefAsset[];
  status: "loading" | "ready" | "error";
  /** 重新读一次画布；返回最新的资产（同时多次调用都拿最后一次的结果），读取失败为 null */
  reload: () => Promise<RefAsset[] | null>;
  /** 画布上的故事板节点，按片段 id（和资产同一次读取） */
  boards: ReadonlyMap<string, BoardNode>;
  /** 最近一次读到的故事板：reload 之后立刻取，不用等重新渲染 */
  boardsNow: () => ReadonlyMap<string, BoardNode>;
};

const NONE: RefAsset[] = [];
const NO_BOARDS: ReadonlyMap<string, BoardNode> = new Map();

/** 声音任务状态随任务快照更新，失败任务不妨碍继续使用先前已绑定的音色。 */
const withVoiceTasks = (assets: RefAsset[], tasks: TaskMap): RefAsset[] =>
  assets.map((asset) =>
    asset.voiceTaskId ? { ...asset, voiceTaskStatus: tasks[asset.voiceTaskId]?.status } : asset,
  );

/**
 * 短剧画布上的资产节点（角色 / 场景 / 道具）：分集卡片计数、片段工作区资产库和 @ 候选都读它；
 * 顺带取出故事板节点（生视频时作为第一张参考图）。
 * 只读画布，不占编辑锁；角色卡节点的形象、声音就在节点数据里，读到什么就是什么。
 * 页面回到前台时重新读一次，资产页里新生成的形象能跟上；reloadKey 变化也重读（拆解 / 写提示词任务收尾）；
 * 后端改了这张画布（片场生成的故事板写节点、出图回写）推 canvas.patch 时也重读。
 */
export function useDramaAssets(canvasId: string, reloadKey = 0): DramaAssetsState {
  const [assets, setAssets] = useState<RefAsset[]>(NONE);
  const [status, setStatus] = useState<DramaAssetsState["status"]>("loading");
  const [boards, setBoards] = useState<ReadonlyMap<string, BoardNode>>(NO_BOARDS);
  const boardsRef = useRef<ReadonlyMap<string, BoardNode>>(NO_BOARDS);
  const seq = useRef(0);
  const tasks = useTasksStore((state) => state.tasks);
  const current = useMemo(() => withVoiceTasks(assets, tasks), [assets, tasks]);

  const load = useCallback(async (): Promise<RefAsset[] | null> => {
    const mine = ++seq.current;
    try {
      const canvas = await getCanvas(canvasId);
      if (mine !== seq.current) return null;
      const next = assetsFromGraph(canvas.graph.nodes);
      const voiceIds = [
        ...new Set(next.flatMap((asset) => (asset.voiceTaskId ? [asset.voiceTaskId] : []))),
      ];
      if (voiceIds.length) await reconcileTaskIds(voiceIds);
      if (mine !== seq.current) return null;
      setAssets(next);
      const nextBoards = boardsFromGraph(canvas.graph.nodes);
      boardsRef.current = nextBoards;
      setBoards(nextBoards);
      setStatus("ready");
      return withVoiceTasks(next, useTasksStore.getState().tasks);
    } catch {
      if (mine === seq.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
      return null;
    }
  }, [canvasId]);
  // 被更晚一次读取顶掉的调用改等最新结果：批量提交时多个片段同时重读，不能有人空手而归
  const latest = useRef<Promise<RefAsset[] | null>>(null);
  const reload = useCallback(() => followLatest(latest, load()), [load]);

  useEffect(() => {
    void reload();
  }, [reload, reloadKey]);

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") void reload();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [reload]);

  useEffect(() => subscribeCanvasPatch(canvasId, () => void reload()), [canvasId, reload]);

  const boardsNow = useCallback(() => boardsRef.current, []);
  return { assets: current, status, reload, boards, boardsNow };
}
