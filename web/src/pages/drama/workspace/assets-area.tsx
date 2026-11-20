import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import { Frame, LayoutGrid, Loader2, type LucideIcon } from "lucide-react";

import { getCanvas } from "@/api/canvas";
import { listSegments } from "@/api/drama";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { CanvasEditor } from "@/components/canvas-editor/canvas-editor";
import type { CanvasEditorCommands } from "@/components/canvas-editor/editor-api";
import { Button } from "@/components/ui/button";
import { useCanvasLock } from "@/hooks/use-canvas-lock";
import { cn } from "@/lib/utils";
import { buildAddNodeItems } from "@/pages/canvas/chrome/add-node-items";
import type { CanvasNode, CanvasNodeData, FlowNode, NodeDrama, NodeKind } from "@/types";
import { draftStore } from "@/utils/canvas/draft-idb";
import { loadCanvasForEditing, type OpenedCanvas } from "@/utils/canvas/open-canvas";
import { defaultDramaForNew, type AssetType, type EpisodeFilter } from "@/utils/drama/assets";
import { isBoardNode } from "@/utils/drama/board";
import { busyReason } from "@/utils/drama/jobs";
import { dramaAssetTypeOf, referenceWarning } from "@/utils/drama/segment-assets";
import { getCurrentUserId } from "@/utils/storage/user-id";

import { AssetsUiContext, type AssetsUi } from "./assets-ui";
import { AssetsOverlay } from "./assets-overlay";
import { BreakdownProgress } from "./breakdown-progress";
import { useWorkspace } from "./context";
import { NodeEpisodesButton } from "./node-episodes";

/** 短剧画布的添加菜单：没有导演台（短剧一期不做 3D 预演），其余同无限画布；引用要稳定 */
const buildMenuItems = (disabled: (kind: NodeKind) => boolean) =>
  buildAddNodeItems(disabled).filter((item) => item.value !== "director");

/** 节点功能区追加「出现集数」（故事板节点跟着片段走，不给改）；模块级函数，引用稳定 */
const renderToolbarExtra = ({ id, data }: { id: string; data: CanvasNodeData }) =>
  isBoardNode(data) ? null : <NodeEpisodesButton id={id} data={data} />;

/** 空节点（无素材无历史）也显示功能区：拆解出来的资产大多是空节点，但「出现集数」必须能用；导演台除外 */
const toolbarVisible = (data: CanvasNodeData) => data.kind !== "director";

/** 「另存为副本」产生的是普通画布，不在短剧里打开：不提供「打开」入口 */
const copyOpenPath = () => null;

/** 编辑器插槽里的元素放在模块顶层，引用不变，页面重渲染时不会牵动编辑器 */
const overlay = <AssetsOverlay />;

/**
 * 画布区：加载短剧专属画布，用可复用的编辑器组装。资产管理和故事板共用这一块，只是 overlay（列表 / 画布外壳）不同；
 * overlay 要的上下文由调用方在外面提供。
 */
export function DramaCanvas({
  overlay,
  commandsRef,
  onNodeCreate,
  onFail,
}: {
  /** 编辑器 overlay 插槽的内容；要引用稳定（模块顶层常量），否则会牵动编辑器 */
  overlay: React.ReactNode;
  commandsRef: React.RefObject<CanvasEditorCommands | null>;
  onNodeCreate: (node: CanvasNode) => CanvasNode;
  /** 画布没能加载出来（读取失败、被别的标签页占着编辑锁）：调用方据此放弃，别一直占着 */
  onFail?: () => void;
}) {
  const { drama } = useWorkspace();
  const canvasId = drama.canvasId;
  const lock = useCanvasLock(canvasId);
  const dramaRef = useRef(drama);
  useEffect(() => {
    dramaRef.current = drama;
  }, [drama]);
  /** 删资产节点前：有片段的提示词引用了它就提示一声（只提示，不阻止） */
  const confirmDeleteNodes = useCallback(async (nodes: FlowNode[]): Promise<boolean> => {
    const doomed = nodes.flatMap((node) => {
      if (node.type === "group") return [];
      const data = node.data as CanvasNodeData;
      return dramaAssetTypeOf(data) ? [{ id: node.id, label: data.label }] : [];
    });
    const current = dramaRef.current;
    const episodes = current.episodes.filter((ep) => ep.segmentCount > 0);
    if (doomed.length === 0 || episodes.length === 0) return true;
    let prompts: string[];
    try {
      const lists = await Promise.all(episodes.map((ep) => listSegments(current.id, ep.no)));
      prompts = lists.flat().map((segment) => segment.prompt);
    } catch {
      // 读不到片段就不拦着，提示只是善意提醒
      return true;
    }
    const warning = referenceWarning(doomed, prompts);
    if (!warning) return true;
    return confirm({
      title: `有 ${warning.segments} 个片段引用了它`,
      description: `「${warning.names.join("」「")}」被 ${warning.segments} 个片段的提示词引用。删除后这些引用会显示为失效，片段仍可生成，只是这部分只用文字。`,
      confirmLabel: "仍然删除",
      destructive: true,
    });
  }, []);
  const [opened, setOpened] = useState<OpenedCanvas | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (lock !== "held") return;
    let active = true;
    const userId = getCurrentUserId();
    void loadCanvasForEditing({
      canvasId,
      userId,
      getCanvas,
      loadDraft: (user, id) => draftStore.load(user, id),
      removeDraft: (id) => (userId ? draftStore.remove(userId, id) : Promise.resolve()),
    })
      .then((value) => active && setOpened(value))
      .catch(() => active && setError(true));
    return () => {
      active = false;
    };
  }, [canvasId, lock]);

  // 没能加载出来就告诉调用方（片场据此卸载画布、回列表）
  const failure = lock === "waiting" || error;
  const onFailRef = useRef(onFail);
  useEffect(() => {
    onFailRef.current = onFail;
  });
  useEffect(() => {
    if (failure) onFailRef.current?.();
  }, [failure]);

  if (lock === "waiting") {
    return (
      <div className="text-muted-foreground grid h-full place-items-center text-sm">
        这部短剧的资产画布已在另一个标签页打开，关闭那边后会自动接管。
      </div>
    );
  }
  if (error) {
    return (
      <div className="text-muted-foreground grid h-full place-items-center text-sm">
        资产画布加载失败，请刷新页面重试
      </div>
    );
  }
  const current = opened && opened.canvas.id === canvasId ? opened : null;
  if (!current) {
    return (
      <div className="text-muted-foreground grid h-full place-items-center text-sm">
        <Loader2 className="size-5 animate-spin" />
      </div>
    );
  }
  return (
    <ReactFlowProvider>
      <CanvasEditor
        key={`${canvasId}:${current.canvas.version}`}
        canvas={current.canvas}
        recovery={current.recovery}
        onConflict={(latest) => setOpened({ canvas: latest, recovery: null })}
        menuItems={buildMenuItems}
        onNodeCreate={onNodeCreate}
        toolbarExtra={renderToolbarExtra}
        toolbarVisible={toolbarVisible}
        confirmDeleteNodes={confirmDeleteNodes}
        copyOpenPath={copyOpenPath}
        containerClassName="h-full w-full"
        commandsRef={commandsRef}
        overlay={overlay}
      />
    </ReactFlowProvider>
  );
}

/** 「列表 ⇄ 画布」切换：资产管理和片场共用；第一项的名字和图标不同，资产管理是四方格「列表」，片场是场记板「片场」 */
export function ViewToggle({
  view,
  onChange,
  listLabel = "列表",
  listIcon = LayoutGrid,
}: {
  view: "list" | "canvas";
  onChange: (view: "list" | "canvas") => void;
  /** 第一项的名字 */
  listLabel?: string;
  /** 第一项的图标 */
  listIcon?: LucideIcon;
}) {
  return (
    <div role="group" aria-label="视图" className="bg-muted flex rounded-lg p-0.5">
      {(
        [
          ["list", listLabel, listIcon],
          ["canvas", "画布", Frame],
        ] as const
      ).map(([key, label, Icon]) => (
        <button
          key={key}
          type="button"
          aria-pressed={view === key}
          onClick={() => onChange(key)}
          className={cn(
            "focus-visible:ring-ring/50 inline-flex h-7 items-center gap-1.5 rounded-md px-3 text-[13px] outline-none focus-visible:ring-2 [&_svg]:size-4",
            view === key
              ? "bg-background font-medium shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          <Icon />
          {label}
        </button>
      ))}
    </div>
  );
}

/** 新建节点默认「全集」；资产列表新建时按列表当前的类型 / 集数归类（pending，null 表示没设） */
export function withNewDrama(
  node: CanvasNode,
  pending: { drama: NodeDrama | null } | null,
): CanvasNode {
  if (pending) {
    if (pending.drama === null) return node;
    return { ...node, data: { ...node.data, drama: node.data.drama ?? pending.drama } };
  }
  return { ...node, data: { ...node.data, drama: defaultDramaForNew(node.data) } };
}

/**
 * 资产区：顶部是「列表 ⇄ 画布」切换和全部生成；左侧分集栏选中的集驱动筛选（0 = 全集）。
 * 列表视图盖在短剧专属画布上，读的是画布里同一份节点数据。
 */
export function AssetsArea({
  ep,
  onBreakdownOpen,
}: {
  /** 左侧分集栏当前选中的集；0 = 全集 */
  ep: number;
  /** 打开资产拆解对话框：从剧本里拆出角色、场景、道具 */
  onBreakdownOpen: () => void;
}) {
  const { drama, jobs } = useWorkspace();
  const breakdownBlocked =
    busyReason(jobs) ??
    (drama.episodes.some((episode) => episode.scriptStatus === "ready" && !episode.locked)
      ? null
      : "没有可拆解的集：先写好剧本");
  const [view, setView] = useState<"list" | "canvas">("list");
  const filter = useMemo<EpisodeFilter>(
    () => (ep === 0 ? { kind: "all" } : { kind: "episode", no: ep }),
    [ep],
  );
  const [type, setType] = useState<AssetType>("character");
  const [multi, setMulti] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const commands = useRef<CanvasEditorCommands | null>(null);
  const [generateAllTarget, setGenerateAllTarget] = useState<HTMLDivElement | null>(null);
  const newDrama = useRef<{ drama: NodeDrama | null } | null>(null);
  const setNewDrama = useCallback((pending: { drama: NodeDrama | null } | null) => {
    newDrama.current = pending;
  }, []);

  const ui = useMemo<AssetsUi>(
    () => ({
      view,
      setView: (next) => {
        setView(next);
        setMulti(false);
        setSelected(new Set());
      },
      filter,
      type,
      setType,
      multi,
      setMulti: (next) => {
        setMulti(next);
        if (!next) setSelected(new Set());
      },
      selected,
      setSelected,
      commands,
      setNewDrama,
      generateAllTarget,
    }),
    [filter, generateAllTarget, multi, selected, setNewDrama, type, view],
  );

  /** 新建节点默认「全集」；从列表新建时按列表当前的类型 / 集数归类 */
  const onNodeCreate = useCallback((node: CanvasNode): CanvasNode => {
    const pending = newDrama.current;
    return withNewDrama(node, pending);
  }, []);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-border flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <ViewToggle view={view} onChange={(next) => ui.setView(next)} />
        <span className="flex-1" />
        <BreakdownProgress />
        <div ref={setGenerateAllTarget} className="flex items-center" />
        <Button
          variant="outline"
          size="sm"
          disabled={!!breakdownBlocked}
          title={breakdownBlocked ?? "从已写好的剧本里拆出角色、场景和道具"}
          onClick={onBreakdownOpen}
        >
          资产拆解
        </Button>
      </div>
      <div className="relative min-h-0 flex-1">
        {/* 绝对定位铺满：编辑器的 h-full 才有确定的高度可依 */}
        <div className="absolute inset-0">
          <AssetsUiContext.Provider value={ui}>
            <DramaCanvas overlay={overlay} commandsRef={commands} onNodeCreate={onNodeCreate} />
          </AssetsUiContext.Provider>
        </div>
      </div>
    </div>
  );
}
