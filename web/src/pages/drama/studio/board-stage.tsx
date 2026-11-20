import { useRef } from "react";
import { CircleX, Film, Grid2x2, RotateCw } from "lucide-react";

import { MediaPreview } from "@/components/canvas/media-preview";
import { TaskGenerationStatus } from "@/components/generation-status";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { variantUrl } from "@/utils/canvas/media-lod";
import { BOARD_RATIO, neighborVersion } from "@/utils/drama/board";
import { playableTake } from "@/utils/drama/segments";

import { useStudio, type PreviewView } from "./use-studio";

/** 胶囊外观：清透底 + 细描边（同工作台其他浮在内容上的控件） */
const PILL =
  "bg-chrome ring-chrome-border flex items-center gap-0.5 rounded-full p-0.5 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150";
const PILL_BUTTON =
  "text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 inline-flex h-8 items-center gap-1.5 rounded-full px-3 text-[13px] whitespace-nowrap outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-4";

/** 状态小点 / 标签：好了绿点，过期 / 失败写字，生成中转圈 */
function StateMark({ state }: { state: "ok" | "none" | "stale" | "failed" | "running" }) {
  if (state === "running") {
    return <span className="bg-primary size-1.5 animate-pulse rounded-full" />;
  }
  if (state === "stale" || state === "failed") {
    return (
      <span
        className={cn(
          "rounded-full px-1.5 text-[10.5px] leading-4",
          state === "stale" ? "bg-primary/15 text-primary" : "bg-destructive/15 text-destructive",
        )}
      >
        {state === "stale" ? "过期" : "失败"}
      </span>
    );
  }
  return (
    <span
      className={cn(
        "size-1.5 rounded-full",
        state === "ok" ? "bg-emerald-400" : "bg-muted-foreground/40",
      )}
    />
  );
}

/** 预览顶部的「故事板 | 视频」切换：一个片段先定故事板再出视频，两面各带状态 */
export function ViewSwitch() {
  const { selected, submitting, boards, view, setView } = useStudio();
  if (!selected) return null;
  const board = boards.statusOf(selected);
  const videoRunning = selected.status === "running" || submitting.has(selected.id);
  const items: { key: PreviewView; label: string; icon: React.ReactNode; mark: React.ReactNode }[] =
    [
      {
        key: "board",
        label: "故事板",
        icon: <Grid2x2 />,
        mark: (
          <StateMark
            state={
              board === "ready"
                ? "ok"
                : board === "empty"
                  ? "none"
                  : board === "stale"
                    ? "stale"
                    : board === "failed"
                      ? "failed"
                      : "running"
            }
          />
        ),
      },
      {
        key: "video",
        label: "视频",
        icon: <Film />,
        mark: (
          <StateMark state={videoRunning ? "running" : playableTake(selected) ? "ok" : "none"} />
        ),
      },
    ];
  return (
    <div role="radiogroup" aria-label="预览" className={PILL}>
      {items.map((item) => (
        <button
          key={item.key}
          type="button"
          role="radio"
          aria-checked={view === item.key}
          onClick={() => setView(item.key)}
          className={cn(
            PILL_BUTTON,
            view === item.key && "bg-chrome-hover text-foreground ring-chrome-border ring-1",
          )}
        >
          {item.icon}
          {item.label}
          {item.mark}
        </button>
      ))}
    </div>
  );
}

/** 横向滚轮 / 触控板横滑累积多少像素换一版，换完冷却多久（毫秒），免得一滑就跳好几版（同视频的版本轮播） */
const WHEEL_THRESHOLD = 60;
const WHEEL_COOLDOWN = 260;
/** 在大图上按住左右拖超过多少像素算「换版」 */
const DRAG_THRESHOLD = 40;

/**
 * 故事板一面的外壳：横向滚轮 / 触控板横滑、← → 键换到相邻版本（同视频的版本轮播），累计够了才换、换完冷却一下。
 * 换版只是回调 onStep(±1)，由调用方决定有没有相邻的版本。
 */
function BoardSwipe({
  label,
  onStep,
  children,
}: {
  label: string;
  onStep: (delta: 1 | -1) => void;
  children: React.ReactNode;
}) {
  const wheel = useRef({ total: 0, lockedUntil: 0 });
  return (
    <div
      role="group"
      aria-label={label}
      tabIndex={0}
      onWheel={(event) => {
        const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : 0;
        if (!delta) return;
        const now = performance.now();
        if (now < wheel.current.lockedUntil) return;
        wheel.current.total += delta;
        if (Math.abs(wheel.current.total) < WHEEL_THRESHOLD) return;
        onStep(wheel.current.total > 0 ? 1 : -1);
        wheel.current = { total: 0, lockedUntil: now + WHEEL_COOLDOWN };
      }}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft") onStep(-1);
        else if (event.key === "ArrowRight") onStep(1);
        else return;
        event.preventDefault();
      }}
      className="focus-visible:ring-ring/40 flex min-h-0 w-full flex-1 flex-col items-center justify-center gap-3 outline-none focus-visible:ring-2"
    >
      {children}
    </div>
  );
}

/** 缩略图地址：/files/ 下的素材走 ?v=thumb 的小图，其他地址原样 */
const thumbOf = (src: string) => variantUrl(src, "thumb").url;

/**
 * 预览的「故事板」一面：选中片段的 16:9 宫格图，按状态显示——已出图（过期时左上角提示重新生成）、生成中、失败、还没有。
 * 大图先铺缩略图、原图加载完再淡入盖上去（不做点击放大）；下方多版的小图只加载缩略图；
 * 多版时左右拖动 / 滚轮 / ← → / 点小图都能换版，换到哪版就把哪版设为当前版本（生视频带的就是它）。
 * 下面是镜头数 / 宫格、重新生成、进入画布查看；分镜时节点就建好了，没出图（包括失败）也能进画布。
 */
export function BoardStage() {
  const { selected, boards, openBoardInCanvas } = useStudio();
  /** 在大图上按下的位置；抬起时位移够大就换版 */
  const pressX = useRef<number | null>(null);
  if (!selected) return null;
  const status = boards.statusOf(selected);
  const node = boards.nodeOf(selected);
  const credits = boards.credits > 0 ? boards.credits : null;
  const blocked = boards.blocked ?? (!selected.prompt.trim() ? "片段原稿是空的" : null);
  const generate = () => void boards.generate([selected]);
  const generateLabel = (text: string) => (
    <>
      {text}
      {credits && <span className="tabular-nums opacity-60">· {credits}</span>}
    </>
  );
  const hasImage = (status === "ready" || status === "stale") && !!node?.src;
  const versions = node?.versions ?? [];
  const versionNo =
    Math.max(
      0,
      versions.findIndex((item) => item.id === node?.activeId),
    ) + 1;
  /** 往前 / 往后换一版（滑到哪版采用哪版）；只有多版的出图才能换 */
  const step = (delta: 1 | -1) => {
    if (!hasImage || versions.length < 2) return;
    const next = neighborVersion(versions, node?.activeId ?? null, delta);
    if (next) void boards.selectVersion(selected, next);
  };

  return (
    <BoardSwipe
      label={versions.length > 1 ? `故事板版本 ${versionNo} / ${versions.length}` : "故事板"}
      onStep={step}
    >
      <div
        data-slot="board-frame"
        style={{ aspectRatio: BOARD_RATIO.replace(":", "/") }}
        onPointerDown={(event) => {
          pressX.current = event.clientX;
        }}
        onPointerUp={(event) => {
          const from = pressX.current;
          pressX.current = null;
          if (from === null) return;
          const dx = event.clientX - from;
          // 向左拖看更新的一版，向右拖回前一版；挪得太少不算
          if (Math.abs(dx) >= DRAG_THRESHOLD) step(dx < 0 ? 1 : -1);
        }}
        className={cn(
          // 所有状态都带同样粗的边框（有图时是透明的），框的内外尺寸才不随状态变
          "relative grid max-h-full w-full max-w-[860px] place-items-center overflow-hidden rounded-2xl border-[1.5px] [corner-shape:squircle]",
          hasImage || status === "running"
            ? "border-transparent bg-black shadow-[0_40px_80px_-30px_rgb(0_0_0/0.9)] ring-1 ring-white/10"
            : cn(
                "border-dashed",
                status === "failed" ? "border-destructive/50" : "border-white/15",
              ),
        )}
      >
        {status === "running" ? (
          // 和画布图片节点同一套实时状态：按节点上绑的任务订阅排队 / 生成中 / 进度 / 耗时
          <TaskGenerationStatus
            taskId={node?.taskId ?? undefined}
            mediaType="image"
            className="size-full"
          />
        ) : hasImage ? (
          <>
            {/* 大图先铺缩略图，原图加载完再淡入盖上去 */}
            <MediaPreview
              src={node.src ?? ""}
              alt={`片段 ${selected.seq + 1} 的故事板`}
              mode="full"
            />
            {status === "stale" && (
              <div className="text-primary absolute top-3 left-3 flex h-7 items-center gap-1.5 rounded-full bg-black/70 py-0 pr-1 pl-3 text-[12.5px] ring-1 ring-amber-400/35 backdrop-blur-md">
                原稿已改
                <button
                  type="button"
                  disabled={!!blocked}
                  title={blocked ?? undefined}
                  onClick={generate}
                  className="bg-primary text-primary-foreground h-5 rounded-full px-2.5 text-xs font-semibold disabled:opacity-50"
                >
                  重新生成
                </button>
              </div>
            )}
          </>
        ) : (
          <div className="flex flex-col items-center gap-3 text-center">
            <span
              className={cn(
                "bg-muted grid size-14 place-items-center rounded-[18px] [&_svg]:size-6",
                status === "failed" ? "text-destructive" : "text-muted-foreground",
              )}
            >
              {status === "failed" ? <CircleX /> : <Grid2x2 />}
            </span>
            <h3
              className={cn("text-[15px] font-semibold", status === "failed" && "text-destructive")}
            >
              {status === "failed" ? "生成失败" : "还没有故事板"}
            </h3>
            {status === "failed" && node?.error && (
              <p className="text-muted-foreground -mt-1.5 text-[13px]">{node.error}</p>
            )}
          </div>
        )}
      </div>

      {/* 版本条和按钮行的位置始终占着（没有时是空的），不然它们有没有会把上面的大图框顶高 / 压矮 */}
      <div
        data-slot="board-strip"
        className="flex h-[52px] max-w-full shrink-0 items-center justify-center gap-1.5 overflow-x-auto px-1"
      >
        {hasImage &&
          versions.length > 1 &&
          versions.map((item) => (
            <button
              key={item.id}
              type="button"
              data-version={item.id}
              aria-label={`故事板版本 ${item.id}`}
              aria-current={item.id === node?.activeId ? "true" : undefined}
              onClick={() => {
                if (item.id !== node?.activeId) void boards.selectVersion(selected, item.id);
              }}
              className={cn(
                "relative h-11 shrink-0 overflow-hidden rounded-lg ring-1 outline-none transition-[box-shadow,opacity]",
                item.id === node?.activeId
                  ? "ring-primary opacity-100 ring-2"
                  : "ring-white/15 opacity-60 hover:opacity-100",
              )}
              style={{ aspectRatio: BOARD_RATIO.replace(":", "/") }}
            >
              <img src={thumbOf(item.src)} alt="" className="size-full object-cover" />
            </button>
          ))}
      </div>

      <div
        data-slot="board-actions"
        className="flex h-10 shrink-0 flex-wrap items-center justify-center gap-2"
      >
        {hasImage && node && node.shotCount > 0 && (
          <span className="text-muted-foreground text-xs tabular-nums">
            {node.shotCount} 镜 · {node.layout.replace("x", "×")} · {BOARD_RATIO}
          </span>
        )}
        {/* 生成中框里已经有进度，不再放生成按钮；其余状态都是这一个按钮：没有图写「生成」，有过图或失败写「重新生成」 */}
        {status !== "running" && (
          <div className={PILL}>
            <button
              type="button"
              className={PILL_BUTTON}
              disabled={!!blocked}
              title={blocked ?? undefined}
              onClick={generate}
            >
              {hasImage && <RotateCw />}
              {generateLabel(hasImage || status === "failed" ? "重新生成" : "生成故事板")}
            </button>
          </div>
        )}
        {node && (
          <div className={PILL}>
            <button
              type="button"
              className={PILL_BUTTON}
              onClick={() => openBoardInCanvas(node.nodeId)}
            >
              进入画布查看
            </button>
          </div>
        )}
      </div>
    </BoardSwipe>
  );
}

/** 片段条右上角的「批量生成故事板 · n」（样式同旁边的「批量生成分镜视频」）：一次补完缺的、失败的和过期的，n 是还要补的张数 */
export function BoardFillButton() {
  const { segments, boards } = useStudio();
  if (segments.length === 0) return null;
  const todo = boards.targets.length;
  const blocked = boards.blocked ?? (todo === 0 ? "每个片段都有可用的故事板" : null);
  return (
    <Button
      variant="ghost"
      size="sm"
      data-slot="board-fill"
      disabled={!!blocked}
      title={
        blocked ?? `生成缺的、失败的和已过期的故事板（已可用 ${boards.ready}/${segments.length}）`
      }
      onClick={() => void boards.fill()}
    >
      批量生成故事板
      {todo > 0 && <span className="tabular-nums opacity-60">· {todo}</span>}
    </Button>
  );
}
