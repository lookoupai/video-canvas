import { useEffect } from "react";
import { motion } from "motion/react";
import { useDefaultLayout } from "react-resizable-panels";

import type { Drama } from "@/api/drama/type";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useMediaQuery } from "@/hooks/use-media-query";
import { cn } from "@/lib/utils";
import { usePhasedMount } from "@/hooks/use-staggered-mount";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { studioColumns } from "@/utils/drama/studio-layout";

import { ClipBar } from "./clip-bar";
import { Editor } from "./editor";
import { Library } from "./library";
import { Preview } from "./preview";
import { StudioBar } from "./studio-bar";
import { StudioCanvas } from "./studio-canvas";
import { PlaybackContext, usePlayback } from "./use-playback";
import { StudioContext, useStudio, useStudioState } from "./use-studio";

/**
 * 片场（工作台里的一道工序）：某一集的片段制作。
 * 顶部条 / 左资产库 / 中间大播放器 / 右检查器（参考素材、提示词）/ 底部时间线（含连播），各区之间可拖动调整大小。
 * 换一集由外层用 key 整块重来，不让上一集的状态残留。
 */
export function StudioView({
  drama,
  no,
  refresh,
}: {
  drama: Drama;
  no: number;
  refresh: () => Promise<void>;
}) {
  const state = useStudioState(drama, no, refresh);
  return (
    <StudioContext.Provider value={state}>
      <Shell />
    </StudioContext.Provider>
  );
}

/**
 * 分拍挂载的一格：on 之前什么都不画（面板本身还在，占着位置），on 之后挂上内容并淡入。
 * 只淡入、不做位移和缩放：内容是边挂边撑开的，位移会和尺寸变化叠在一起让画面抖。
 */
function Phased({
  name,
  on,
  children,
}: {
  /** 区域名，只用来标记（测试和排查看谁什么时候挂上的） */
  name: string;
  on: boolean;
  children: React.ReactNode;
}) {
  if (!on) return null;
  return (
    <motion.div
      data-phased={name}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="size-full"
    >
      {children}
    </motion.div>
  );
}

/**
 * 片场内容区的一面（列表或画布）：两面叠在一起，看不见的那面透明、不可点、对读屏隐藏（inert），
 * 切换时两面交叉淡入淡出（只动 opacity，约 180ms）。不卸载，所以切回来时滚动位置、选中片段、编辑中的提示词都还在。
 */
function Face({
  name,
  shown,
  children,
}: {
  name: "list" | "canvas";
  shown: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      data-slot={`studio-${name}`}
      data-shown={shown ? "true" : "false"}
      inert={!shown}
      className={cn(
        "absolute inset-0 transition-opacity duration-200 ease-out motion-reduce:transition-none",
        shown ? "opacity-100" : "pointer-events-none opacity-0",
      )}
    >
      {children}
    </div>
  );
}

/** 分栏线：平时是 1px 细线，悬停和拖动时亮成主题色，方便找到能拖的地方 */
const HANDLE =
  "hover:bg-primary/60 data-[separator=active]:bg-primary transition-colors duration-150";

/**
 * 连播状态（预览与时间线共用）和整体布局；顶部条的「列表 | 画布」选了画布时整块换成短剧画布：
 * 上下两块（三栏区 / 时间线）、三栏之间（资产库 / 预览 / 编辑区）都能拖动调整大小，拖好的尺寸记在本机。
 */
function Shell() {
  const { segments, select, canvasEntry } = useStudio();
  const playback = usePlayback(segments);
  // 分三拍挂载：0 骨架（顶部条 + 分栏）→ 1 预览 / 编辑区（用户马上要看的）→ 2 资产库 / 片段条（最重的两块）。
  // 分栏面板一直在（分栏库要靠它记尺寸），面板里的内容晚一点挂，挂上时淡入，不在同一帧里画完。
  const phase = usePhasedMount(2);
  // 各区域都挂完后，空闲时再把画布在后台挂上（画布编辑器很重，不和第一屏抢）；点「画布」时它已经在了
  const { mountCanvas } = useStudio();
  useEffect(() => {
    if (phase < 2) return;
    // Safari 没有 requestIdleCallback：退回一个短定时器
    const hasIdle = "requestIdleCallback" in window;
    const handle = hasIdle
      ? window.requestIdleCallback(() => mountCanvas(), { timeout: 1500 })
      : window.setTimeout(() => mountCanvas(), 300);
    return () => {
      if (hasIdle) window.cancelIdleCallback(handle);
      else window.clearTimeout(handle);
    };
  }, [phase, mountCanvas]);

  // 连播切到哪个片段，选中（时间线描边、提示词编辑区）就跟到哪个
  useEffect(() => {
    if (playback.currentId) select(playback.currentId);
  }, [playback.currentId, select]);

  const lg = useMediaQuery("(min-width: 1024px)");
  const xl = useMediaQuery("(min-width: 1280px)");
  const columns = studioColumns({ lg, xl });
  // 拖好的尺寸记在本机；三栏在不同窗口宽度下显示的栏不同，各记一份
  const rowsLayout = useDefaultLayout({ id: "drama-studio-rows", panelIds: ["main", "clips"] });
  const columnsLayout = useDefaultLayout({ id: "drama-studio-columns", panelIds: columns });

  return (
    <PlaybackContext.Provider value={playback}>
      <div className="flex h-full min-h-0 flex-col">
        <StudioBar />
        {/* 内容区：列表和画布叠在一起，都不卸载，切换只改看哪一面（交叉淡入，没有卸载 / 挂载的卡顿和闪烁） */}
        <div className="relative min-h-0 flex-1">
          <Face name="list" shown={canvasEntry.view === "list"}>
            <ResizablePanelGroup
              orientation="vertical"
              className="size-full"
              defaultLayout={rowsLayout.defaultLayout}
              onLayoutChanged={rowsLayout.onLayoutChanged}
            >
              <ResizablePanel id="main" minSize={240}>
                <ResizablePanelGroup
                  key={columns.join("|")}
                  orientation="horizontal"
                  defaultLayout={columnsLayout.defaultLayout}
                  onLayoutChanged={columnsLayout.onLayoutChanged}
                >
                  {columns.includes("library") && (
                    <>
                      <ResizablePanel id="library" defaultSize={224} minSize={180} maxSize="35%">
                        <Phased name="library" on={phase >= 2}>
                          <Library />
                        </Phased>
                      </ResizablePanel>
                      <ResizableHandle className={HANDLE} />
                    </>
                  )}
                  {columns.includes("preview") && (
                    <>
                      <ResizablePanel id="preview" minSize={280}>
                        <Phased name="preview" on={phase >= 1}>
                          <Preview />
                        </Phased>
                      </ResizablePanel>
                      <ResizableHandle className={HANDLE} />
                    </>
                  )}
                  <ResizablePanel id="editor" defaultSize={400} minSize={320}>
                    <Phased name="editor" on={phase >= 1}>
                      <Editor />
                    </Phased>
                  </ResizablePanel>
                </ResizablePanelGroup>
              </ResizablePanel>
              <ResizableHandle className={HANDLE} />
              <ResizablePanel id="clips" defaultSize={152} minSize={140} maxSize="60%">
                <Phased name="clips" on={phase >= 2}>
                  <ClipBar />
                </Phased>
              </ResizablePanel>
            </ResizablePanelGroup>
          </Face>
          {canvasEntry.mounted && (
            <Face name="canvas" shown={canvasEntry.view === "canvas"}>
              <StudioCanvas />
            </Face>
          )}
        </div>
      </div>
    </PlaybackContext.Provider>
  );
}
