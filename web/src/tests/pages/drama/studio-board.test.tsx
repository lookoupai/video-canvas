import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import type { Segment } from "@/api/drama/type";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { BoardNode, BoardStatus } from "@/utils/drama/board";
import type {
  BoardFillButton as BoardFillButtonType,
  BoardStage as BoardStageType,
  ViewSwitch as ViewSwitchType,
} from "@/pages/drama/studio/board-stage";
import type { StudioViewToggle as StudioViewToggleType } from "@/pages/drama/studio/view-toggle";
import type {
  PreviewView,
  Studio,
  StudioContext as StudioContextType,
} from "@/pages/drama/studio/use-studio";

/**
 * 片场里的故事板（故事板并进片场）：预览顶部「故事板 | 视频」切换、故事板一面按状态显示、
 * 「批量生成故事板」按钮（在底部片段条右上，和「批量生成分镜视频」并排）。
 */

let dom: DomEnv;
let BoardStage: typeof BoardStageType;
let ViewSwitch: typeof ViewSwitchType;
let BoardFillButton: typeof BoardFillButtonType;
let StudioViewToggle: typeof StudioViewToggleType;
let StudioContext: typeof StudioContextType;
beforeAll(async () => {
  dom = await installDom();
  const mod = await import("@/pages/drama/studio/board-stage");
  BoardStage = mod.BoardStage;
  ViewSwitch = mod.ViewSwitch;
  BoardFillButton = mod.BoardFillButton;
  StudioViewToggle = (await import("@/pages/drama/studio/view-toggle")).StudioViewToggle;
  StudioContext = (await import("@/pages/drama/studio/use-studio")).StudioContext;
});
afterAll(() => dom.restore());

const segment = (seq: number, over: Partial<Segment> = {}): Segment =>
  ({
    id: `s${seq}`,
    seq,
    seconds: 8,
    prompt: "【时间轴】\n0-3秒：全景。",
    status: "idle",
    takes: [],
    chosenTaskId: "",
    ...over,
  }) as Segment;

/** 一版出图：地址是 /files/ 下的，才有缩略图 */
const version = (id: string) => ({
  id,
  src: `/files/assets/${id}.png`,
  mediaType: "image" as const,
  assetId: id,
  createdAt: 1,
});

const node = (over: Partial<BoardNode> = {}): BoardNode => ({
  segmentId: "s0",
  sourceHash: "h",
  layout: "2x2",
  shotCount: 3,
  nodeId: "b1",
  src: "/files/assets/31.png",
  assetId: "31",
  status: "done",
  error: "",
  versions: [version("31")],
  activeId: "31",
  taskId: null,
  ...over,
});

/** 假的片场状态：statuses 是每个片段的故事板状态，记下调用 */
function studio(over: {
  segments?: Segment[];
  statuses?: Record<string, BoardStatus>;
  nodes?: Record<string, BoardNode>;
  view?: PreviewView;
  canvasEntry?: { view: "list" | "canvas"; mounted: boolean; target: string | null };
  credits?: number;
  targets?: Segment[];
  blocked?: string | null;
}) {
  const calls = {
    canvas: [] as string[],
    showCanvas: 0,
    showList: 0,
    select: [] as string[],
    generate: [] as string[][],
    fill: 0,
    video: [] as string[],
    view: [] as PreviewView[],
  };
  const segments = over.segments ?? [segment(0)];
  const value = {
    selected: segments[0],
    segments,
    submitting: new Set<string>(),
    openBoardInCanvas: (nodeId: string) => void calls.canvas.push(nodeId),
    canvasEntry: over.canvasEntry ?? { view: "list", mounted: true, target: null },
    showCanvas: () => void (calls.showCanvas += 1),
    showList: () => void (calls.showList += 1),
    view: over.view ?? "board",
    setView: (v: PreviewView) => calls.view.push(v),
    actions: { generate: async (s: Segment) => void calls.video.push(s.id) },
    boards: {
      statusOf: (s: Segment) => over.statuses?.[s.id] ?? "empty",
      nodeOf: (s: Segment) => over.nodes?.[s.id],
      model: undefined,
      credits: over.credits ?? 4,
      targets: over.targets ?? [],
      ready: Object.values(over.statuses ?? {}).filter((s) => s === "ready").length,
      blocked: over.blocked ?? null,
      generate: async (list: Segment[]) => void calls.generate.push(list.map((s) => s.id)),
      fill: async () => void (calls.fill += 1),
      selectVersion: async (_s: Segment, id: string) => void calls.select.push(id),
    },
  } as unknown as Studio;
  return { value, calls };
}

const mount = (value: Studio, el: React.ReactElement) =>
  dom.mount(<StudioContext.Provider value={value}>{el}</StudioContext.Provider>);
const button = (c: HTMLElement, text: string) =>
  [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").includes(text));

describe("片场故事板：预览的「故事板 | 视频」切换", () => {
  test("两面各带状态：故事板过期写「过期」；点视频切过去", async () => {
    const { value, calls } = studio({ statuses: { s0: "stale" } });
    const m = await mount(value, <ViewSwitch />);
    const radios = [...m.container.querySelectorAll('[role="radio"]')];
    expect(radios.map((r) => r.textContent)).toEqual(["故事板过期", "视频"]);
    expect(radios[0].getAttribute("aria-checked")).toBe("true");
    await dom.click(radios[1]);
    expect(calls.view).toEqual(["video"]);
    await m.unmount();
  });
});

describe("片场故事板：故事板这一面", () => {
  test("还没有：虚线框里只有图标和标题，「生成 · 积分」按钮在框下面的按钮行里，点了只生成这一段", async () => {
    const { value, calls } = studio({});
    const m = await mount(value, <BoardStage />);
    const frame = m.container.querySelector('[data-slot="board-frame"]') as HTMLElement;
    const actions = m.container.querySelector('[data-slot="board-actions"]') as HTMLElement;
    expect(frame.textContent).toContain("还没有故事板");
    expect(m.container.querySelector("img")).toBeNull();
    // 按钮不在框里，在下面的按钮行里
    expect(frame.querySelector("button")).toBeNull();
    const go = button(actions, "生成故事板") as HTMLButtonElement;
    expect(go).toBeDefined();
    expect(go.textContent).toContain("· 4");
    await dom.click(go);
    expect(calls.generate).toEqual([["s0"]]);
    await m.unmount();
  });

  test("出过图：大图先铺缩略图，原图加载完再叠上去（两层都在）；镜头 / 宫格；「进入画布查看」跳到画布并定位", async () => {
    const { value, calls } = studio({ statuses: { s0: "ready" }, nodes: { s0: node() } });
    const m = await mount(value, <BoardStage />);
    const frame = m.container.querySelector('[data-slot="board-frame"]') as HTMLElement;
    const srcs = [...frame.querySelectorAll("img")].map((img) => img.getAttribute("src"));
    // 缩略图在下面垫底（先出来），原图在上面（加载完淡入）
    expect(srcs).toEqual(["/files/assets/31.png?v=thumb", "/files/assets/31.png"]);
    expect(m.container.textContent).toContain("3 镜 · 2×2 · 16:9");
    expect(m.container.textContent).not.toContain("原稿已改");
    await dom.click(button(m.container, "进入画布查看"));
    expect(calls.canvas).toEqual(["b1"]);
    await m.unmount();
  });

  test("生成中：显示和画布图片节点同一套的实时状态（进度、耗时），按节点上绑的任务订阅，不是一直「正在同步」", async () => {
    const tasks = (await import("@/store/tasks")).useTasksStore;
    const now = new Date().toISOString();
    tasks.getState().upsert({
      id: 88,
      version: 1,
      status: "running",
      progress: 42,
      created_at: now,
      submitted_at: now,
    } as never);
    const running = (taskId: string | null) =>
      studio({
        statuses: { s0: "running" },
        nodes: {
          s0: node({ src: null, status: "running", taskId, versions: [], activeId: null }),
        },
      }).value;
    const m = await mount(running("88"), <BoardStage />);
    await dom.flush();
    expect(m.container.textContent).toContain("生成中");
    expect(m.container.textContent).toContain("42%");
    expect(m.container.textContent).not.toContain("正在同步任务状态");
    await m.unmount();

    // 节点上还没绑任务（刚提交、画布还没读到）：才是「正在同步」
    const m2 = await mount(running(null), <BoardStage />);
    await dom.flush();
    expect(m2.container.textContent).toContain("正在同步任务状态");
    await m2.unmount();
  });

  test("框的大小不随状态变：有图、还没有、失败、生成中，大图框、版本条、按钮行都是同一套尺寸规则（占位始终在）", async () => {
    const layoutOf = async (over: Parameters<typeof studio>[0]) => {
      const { value } = studio(over);
      const m = await mount(value, <BoardStage />);
      const sizing = (slot: string) => {
        const el = m.container.querySelector(`[data-slot="${slot}"]`) as HTMLElement | null;
        if (!el) return null;
        return {
          aspect: el.style.aspectRatio,
          classes: el.className
            .split(/\s+/)
            .filter((c) => /^(max-|w-|h-|min-|shrink|flex-1)/.test(c))
            .sort()
            .join(" "),
        };
      };
      const out = {
        frame: sizing("board-frame"),
        strip: sizing("board-strip"),
        actions: sizing("board-actions"),
      };
      await m.unmount();
      return out;
    };
    const withImage = await layoutOf({ statuses: { s0: "ready" }, nodes: { s0: node() } });
    // 三块占位都存在
    expect(withImage.frame).not.toBeNull();
    expect(withImage.strip).not.toBeNull();
    expect(withImage.actions).not.toBeNull();
    // 没有图 / 失败 / 生成中 / 连节点都没有：三块的尺寸规则和有图时一模一样
    const cases = {
      empty: { statuses: { s0: "empty" } },
      noNode: {},
      failed: {
        statuses: { s0: "failed" },
        nodes: { s0: node({ src: null, status: "error", versions: [], activeId: null }) },
      },
      running: {
        statuses: { s0: "running" },
        nodes: { s0: node({ src: null, status: "running", versions: [], activeId: null }) },
      },
    } as const;
    for (const over of Object.values(cases)) {
      expect(await layoutOf(over as Parameters<typeof studio>[0])).toEqual(withImage);
    }
  });

  test("按钮行里的生成按钮：没有图写「生成」，有图或失败写「重新生成」，生成中不放（框里已经有进度）", async () => {
    const labelsOf = async (over: Parameters<typeof studio>[0]) => {
      const { value } = studio(over);
      const m = await mount(value, <BoardStage />);
      const actions = m.container.querySelector('[data-slot="board-actions"]') as HTMLElement;
      const labels = [...actions.querySelectorAll("button")].map((b) => b.textContent?.trim());
      await m.unmount();
      return labels;
    };
    expect(await labelsOf({ statuses: { s0: "empty" } })).toEqual(["生成故事板· 4"]);
    expect(
      await labelsOf({
        statuses: { s0: "empty" },
        nodes: { s0: node({ src: null, versions: [], activeId: null }) },
      }),
    ).toEqual(["生成故事板· 4", "进入画布查看"]);
    expect(await labelsOf({ statuses: { s0: "ready" }, nodes: { s0: node() } })).toEqual([
      "重新生成· 4",
      "进入画布查看",
    ]);
    expect(
      await labelsOf({
        statuses: { s0: "failed" },
        nodes: { s0: node({ src: null, status: "error", versions: [], activeId: null }) },
      }),
    ).toEqual(["重新生成· 4", "进入画布查看"]);
    expect(
      await labelsOf({
        statuses: { s0: "running" },
        nodes: { s0: node({ src: null, status: "running", versions: [], activeId: null }) },
      }),
    ).toEqual(["进入画布查看"]);
  });

  test("没有出图也能进画布：空节点（分镜时就建好了）、生成失败时都有「进入画布查看」", async () => {
    const empty = studio({
      statuses: { s0: "empty" },
      nodes: { s0: node({ src: null, assetId: undefined, versions: [], activeId: null }) },
    });
    const m = await mount(empty.value, <BoardStage />);
    expect(m.container.textContent).toContain("还没有故事板");
    await dom.click(button(m.container, "进入画布查看"));
    expect(empty.calls.canvas).toEqual(["b1"]);
    await m.unmount();

    const failed = studio({
      statuses: { s0: "failed" },
      nodes: {
        s0: node({ src: null, status: "error", error: "超时", versions: [], activeId: null }),
      },
    });
    const m2 = await mount(failed.value, <BoardStage />);
    await dom.click(button(m2.container, "进入画布查看"));
    expect(failed.calls.canvas).toEqual(["b1"]);
    await m2.unmount();

    // 画布上还没有这个节点（老片段、同步没成功）：不显示这个按钮
    const none = studio({ statuses: { s0: "empty" } });
    const m3 = await mount(none.value, <BoardStage />);
    expect(button(m3.container, "进入画布查看")).toBeUndefined();
    await m3.unmount();
  });

  test("多版：下方一排缩略图，当前版本高亮；点另一版就把它设为当前版本（滑到哪版采用哪版）", async () => {
    const versions = [version("30"), version("31"), version("32")];
    const { value, calls } = studio({
      statuses: { s0: "ready" },
      nodes: { s0: node({ versions, activeId: "31", src: versions[1].src }) },
    });
    const m = await mount(value, <BoardStage />);
    const thumbs = [...m.container.querySelectorAll("[data-version]")] as HTMLElement[];
    expect(thumbs.map((el) => el.dataset.version)).toEqual(["30", "31", "32"]);
    expect(thumbs.map((el) => el.getAttribute("aria-current"))).toEqual([null, "true", null]);
    // 小图也只加载缩略图
    expect(thumbs[0].querySelector("img")?.getAttribute("src")).toBe(
      "/files/assets/30.png?v=thumb",
    );
    await dom.click(thumbs[2]);
    expect(calls.select).toEqual(["32"]);
    // 点当前版本不重复提交
    await dom.click(thumbs[1]);
    expect(calls.select).toEqual(["32"]);
    await m.unmount();
  });

  test("左右滑动换版：横向滚轮、键盘 ← →、大图上左右拖动都能换到相邻版本（到头不动）", async () => {
    const versions = [version("30"), version("31"), version("32")];
    const { value, calls } = studio({
      statuses: { s0: "ready" },
      nodes: { s0: node({ versions, activeId: "31", src: versions[1].src }) },
    });
    const m = await mount(value, <BoardStage />);
    const stage = m.container.querySelector('[role="group"]') as HTMLElement;
    expect(stage.getAttribute("aria-label")).toBe("故事板版本 2 / 3");
    // 键盘
    await dom.act(async () => {
      stage.dispatchEvent(
        new dom.win.KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }),
      );
    });
    await dom.act(async () => {
      stage.dispatchEvent(
        new dom.win.KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true }),
      );
    });
    expect(calls.select).toEqual(["32", "30"]);
    // 横向滚轮：累计够了才换，竖向滚动不算
    calls.select.length = 0;
    await dom.act(async () => {
      stage.dispatchEvent(new dom.win.WheelEvent("wheel", { deltaY: 200, bubbles: true }));
    });
    expect(calls.select).toEqual([]);
    await dom.act(async () => {
      stage.dispatchEvent(new dom.win.WheelEvent("wheel", { deltaX: 90, bubbles: true }));
    });
    expect(calls.select).toEqual(["32"]);
    await m.unmount();
  });

  test("在大图上向左拖过一段距离换到下一版，向右拖回上一版；挪几个像素不算拖动", async () => {
    const versions = [version("30"), version("31"), version("32")];
    const { value, calls } = studio({
      statuses: { s0: "ready" },
      nodes: { s0: node({ versions, activeId: "31", src: versions[1].src }) },
    });
    const m = await mount(value, <BoardStage />);
    const frame = m.container.querySelector('[data-slot="board-frame"]') as HTMLElement;
    const drag = async (from: number, to: number) => {
      await dom.act(async () => {
        frame.dispatchEvent(
          new dom.win.MouseEvent("pointerdown", { clientX: from, bubbles: true }),
        );
        frame.dispatchEvent(new dom.win.MouseEvent("pointerup", { clientX: to, bubbles: true }));
      });
    };
    await drag(400, 300);
    await drag(300, 420);
    expect(calls.select).toEqual(["32", "30"]);
    // 只挪了几个像素：不换版
    calls.select.length = 0;
    await drag(300, 296);
    expect(calls.select).toEqual([]);
    await m.unmount();
  });

  test("大图不再点击放大：没有放大按钮，点一下也不弹灯箱", async () => {
    const { value } = studio({ statuses: { s0: "ready" }, nodes: { s0: node() } });
    const m = await mount(value, <BoardStage />);
    expect(m.container.querySelector('[aria-label="放大查看原图"]')).toBeNull();
    await dom.click(m.container.querySelector('[data-slot="board-frame"]'));
    await dom.flush();
    expect(dom.win.document.body.querySelector('[aria-label="媒体预览"]')).toBeNull();
    await m.unmount();
  });

  test("只有一版：不显示版本条", async () => {
    const { value } = studio({ statuses: { s0: "ready" }, nodes: { s0: node() } });
    const m = await mount(value, <BoardStage />);
    expect(m.container.querySelector("[data-version]")).toBeNull();
    await m.unmount();
  });

  test("原稿改过：左上角「原稿已改 · 重新生成」", async () => {
    const { value, calls } = studio({ statuses: { s0: "stale" }, nodes: { s0: node() } });
    const m = await mount(value, <BoardStage />);
    expect(m.container.textContent).toContain("原稿已改");
    await dom.click(
      [...m.container.querySelectorAll("button")].find((b) => b.textContent === "重新生成"),
    );
    expect(calls.generate).toEqual([["s0"]]);
    await m.unmount();
  });

  test("失败：写原因，可以重新生成；不能生成时按钮置灰并写原因", async () => {
    const failed = studio({
      statuses: { s0: "failed" },
      nodes: { s0: node({ src: null, status: "error", error: "生成超时，积分已退回" }) },
    });
    const m = await mount(failed.value, <BoardStage />);
    const frame = m.container.querySelector('[data-slot="board-frame"]') as HTMLElement;
    const actions = m.container.querySelector('[data-slot="board-actions"]') as HTMLElement;
    expect(frame.textContent).toContain("生成失败");
    expect(frame.textContent).toContain("生成超时，积分已退回");
    // 「重新生成」在下面的按钮行里，不在框里
    expect(frame.querySelector("button")).toBeNull();
    await dom.click(button(actions, "重新生成"));
    expect(failed.calls.generate).toEqual([["s0"]]);
    await m.unmount();

    const blocked = studio({ blocked: "没有可用的图片模型" });
    const m2 = await mount(blocked.value, <BoardStage />);
    const go = button(m2.container, "生成故事板") as HTMLButtonElement;
    expect(go.disabled).toBe(true);
    expect(go.title).toBe("没有可用的图片模型");
    await m2.unmount();
  });
});

describe("片场故事板：「批量生成故事板」按钮", () => {
  test("有要补的才能点，按钮上是还要补的张数，悬停写已可用张数 / 片段数；没有进度条文字", async () => {
    const segments = [segment(0), segment(1), segment(2)];
    const { value, calls } = studio({
      segments,
      statuses: { s0: "ready", s1: "stale", s2: "empty" },
      targets: [segments[1], segments[2]],
    });
    const m = await mount(value, <BoardFillButton />);
    expect(m.container.textContent).not.toContain("1/3");
    const fill = button(m.container, "批量生成故事板") as HTMLButtonElement;
    // 不带图标（和「批量生成分镜视频」一样只有文字）
    expect(fill.querySelector("svg")).toBeNull();
    expect(fill.textContent).toContain("· 2");
    expect(fill.title).toContain("已可用 1/3");
    await dom.click(fill);
    expect(calls.fill).toBe(1);
    await m.unmount();

    const done = studio({ segments: [segment(0)], statuses: { s0: "ready" } });
    const m2 = await mount(done.value, <BoardFillButton />);
    expect((button(m2.container, "批量生成故事板") as HTMLButtonElement).disabled).toBe(true);
    await m2.unmount();
  });
});

describe("片场：列表 | 画布 切换（和资产管理同款）", () => {
  test("两个选项「片场 | 画布」，当前视图高亮；点「画布」切到画布面，点「片场」回片场面（都是直接切，不用等）", async () => {
    const { value, calls } = studio({});
    const m = await mount(value, <StudioViewToggle />);
    const buttons = [...m.container.querySelectorAll("button")] as HTMLButtonElement[];
    expect(buttons.map((b) => b.textContent)).toEqual(["片场", "画布"]);
    // 片场用电影场记板图标，不是资产管理那个四方格
    expect(buttons[0].querySelector("svg")?.getAttribute("class")).toContain("lucide-clapperboard");
    expect(buttons.map((b) => b.getAttribute("aria-pressed"))).toEqual(["true", "false"]);
    // 不转圈、不禁用：画布早在后台挂好了
    expect(buttons.every((b) => !b.disabled && !b.querySelector(".animate-spin"))).toBe(true);
    await dom.click(buttons[1]);
    expect(calls.showCanvas).toBe(1);
    await m.unmount();

    const inCanvas = studio({ canvasEntry: { view: "canvas", mounted: true, target: null } });
    const m2 = await mount(inCanvas.value, <StudioViewToggle />);
    const buttons2 = [...m2.container.querySelectorAll("button")];
    expect(buttons2.map((b) => b.getAttribute("aria-pressed"))).toEqual(["false", "true"]);
    await dom.click(buttons2[0]);
    expect(inCanvas.calls.showList).toBe(1);
    await m2.unmount();
  });
});

describe("片场故事板：「进入画布查看」", () => {
  test("点了直接切到画布并飞到这个节点，按钮不转圈、不禁用", async () => {
    const { value, calls } = studio({ statuses: { s0: "ready" }, nodes: { s0: node() } });
    const m = await mount(value, <BoardStage />);
    const go = button(m.container, "进入画布查看") as HTMLButtonElement;
    expect(go.disabled).toBe(false);
    expect(go.querySelector(".animate-spin")).toBeNull();
    await dom.click(go);
    expect(calls.canvas).toEqual(["b1"]);
    await m.unmount();
  });
});
