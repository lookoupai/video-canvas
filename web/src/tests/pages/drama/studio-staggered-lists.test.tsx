import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import type { Segment } from "@/api/drama/type";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { Library as LibraryType } from "@/pages/drama/studio/library";
import type { ClipBar as ClipBarType } from "@/pages/drama/studio/clip-bar";
import type { Studio, StudioContext as ContextType } from "@/pages/drama/studio/use-studio";
import type {
  PlaybackContext as PlaybackType,
  usePlayback as UsePlaybackType,
} from "@/pages/drama/studio/use-playback";

/** 资产库和片段条很多项时：第一帧只画前一批，其余分批追加，最终全部画出来（不丢项、不重复） */

let dom: DomEnv;
let Library: typeof LibraryType;
let ClipBar: typeof ClipBarType;
let StudioContext: typeof ContextType;
let PlaybackContext: typeof PlaybackType;
let usePlayback: typeof UsePlaybackType;
beforeAll(async () => {
  dom = await installDom();
  Library = (await import("@/pages/drama/studio/library")).Library;
  ClipBar = (await import("@/pages/drama/studio/clip-bar")).ClipBar;
  StudioContext = (await import("@/pages/drama/studio/use-studio")).StudioContext;
  const pb = await import("@/pages/drama/studio/use-playback");
  PlaybackContext = pb.PlaybackContext;
  usePlayback = pb.usePlayback;
}, 60_000);
afterAll(() => dom?.restore());

async function frames(n: number) {
  for (let i = 0; i < n; i++) await dom.flush(20);
}

const segments = Array.from({ length: 30 }, (_, i) => ({
  id: `s${i}`,
  episodeNo: 1,
  seq: i,
  seconds: 8,
  prompt: "p",
  takes: [],
  chosenTaskId: "",
  status: "idle",
  revision: 1,
  updatedAt: "",
  adaptations: {},
})) as Segment[];

const assets = Array.from({ length: 100 }, (_, i) => ({
  id: `a${i}`,
  type: (["character", "scene", "prop"] as const)[i % 3],
  name: `n${i}`,
  look: "",
  label: `资产${i}`,
  episodes: [],
  kind: "image" as const,
  src: null,
  generating: false,
}));

const studio = {
  no: 1,
  assets,
  assetsStatus: "ready",
  segments,
  selected: segments[0],
  select: () => {},
  submitting: new Set<string>(),
  boards: {
    statusOf: () => "empty",
    nodeOf: () => undefined,
    targets: segments.slice(0, 3),
    ready: 0,
    blocked: null,
    fill: async () => {},
  },
  actions: { reorder: async () => {}, add: async () => {}, batch: async () => {} },
} as unknown as Studio;

describe("资产库：分批追加", () => {
  test("100 个资产：第一帧只画一批，之后每帧追加，最终全部画出来", async () => {
    const m = await dom.mount(
      <StudioContext.Provider value={studio}>
        <Library />
      </StudioContext.Provider>,
    );
    const count = () => m.container.querySelectorAll('[draggable="true"]').length;
    // 本集 / 全集：默认只看本集，资产 episodes 为 [] 即全集资产，都算本集
    const first = count();
    expect(first).toBeGreaterThan(0);
    expect(first).toBeLessThan(100);
    await frames(20);
    expect(count()).toBe(100);
    await m.unmount();
  });

  test("资产不多（不到一批）：一次画完，不分批", async () => {
    const few = { ...studio, assets: assets.slice(0, 5) } as unknown as Studio;
    const m = await dom.mount(
      <StudioContext.Provider value={few}>
        <Library />
      </StudioContext.Provider>,
    );
    expect(m.container.querySelectorAll('[draggable="true"]').length).toBe(5);
    await m.unmount();
  });
});

describe("片段条：分批追加", () => {
  function WithPlayback({ children }: { children: React.ReactNode }) {
    const playback = usePlayback(segments);
    return <PlaybackContext.Provider value={playback}>{children}</PlaybackContext.Provider>;
  }

  test("30 个片段：第一帧只画一批，之后每帧追加，最终全部画出来，顺序不乱", async () => {
    const m = await dom.mount(
      <StudioContext.Provider value={studio}>
        <WithPlayback>
          <ClipBar />
        </WithPlayback>
      </StudioContext.Provider>,
    );
    const clips = () =>
      [...m.container.querySelectorAll('[role="button"][aria-label^="片段 "]')].map((el) =>
        el.getAttribute("aria-label"),
      );
    const first = clips().length;
    expect(first).toBeGreaterThan(0);
    expect(first).toBeLessThan(30);
    await frames(20);
    const all = clips();
    expect(all).toHaveLength(30);
    expect(all[0]).toContain("片段 1，");
    expect(all[29]).toContain("片段 30，");
    await m.unmount();
  });
});

describe("资产库和片段条：第一批不重复淡入", () => {
  test("第一批直接显示（区域整体的淡入由外壳负责），之后追加的项才带淡入", async () => {
    const m = await dom.mount(
      <StudioContext.Provider value={studio}>
        <Library />
      </StudioContext.Provider>,
    );
    const items = () => [...m.container.querySelectorAll('[draggable="true"]')] as HTMLElement[];
    const firstBatch = items().map((el) => el.className.includes("fade-in"));
    expect(firstBatch.every((fade) => !fade)).toBe(true);
    await frames(20);
    const all = items();
    expect(all).toHaveLength(100);
    expect(all[0].className).not.toContain("fade-in");
    expect(all[99].className).toContain("fade-in");
    await m.unmount();
  });
});

describe("分批追加时画面不抖", () => {
  test("追加的列表项只淡入，不带位移和缩放（不然会和尺寸变化叠在一起抖）", async () => {
    const m = await dom.mount(
      <StudioContext.Provider value={studio}>
        <Library />
      </StudioContext.Provider>,
    );
    await frames(20);
    const items = [...m.container.querySelectorAll('[draggable="true"]')] as HTMLElement[];
    const classes = items[items.length - 1].className;
    expect(classes).toContain("fade-in");
    expect(classes).not.toMatch(/slide-in|zoom-in/);
    await m.unmount();
  });

  test("资产库和片段条的滚动容器预留滚动条位置：滚动条出现与否都不挤内容", async () => {
    const m = await dom.mount(
      <StudioContext.Provider value={studio}>
        <Library />
      </StudioContext.Provider>,
    );
    const scroller = m.container.querySelector("aside .overflow-y-auto") as HTMLElement;
    expect(scroller.className).toContain("[scrollbar-gutter:stable]");
    await m.unmount();

    const m2 = await dom.mount(
      <StudioContext.Provider value={studio}>
        <WithPlaybackBar />
      </StudioContext.Provider>,
    );
    // 片段条整体竖向不出滚动条（高度由分栏的最小高度保证放得下），只有格子那一行能横向滚
    expect(m2.container.querySelector(".overflow-y-auto")).toBeNull();
    const row = m2.container.querySelector(".overflow-x-auto") as HTMLElement;
    expect(row.className).toContain("[scrollbar-gutter:stable]");
    // 选中片段的描边和光晕会画在格子外面，滚动行（overflow 会裁掉外面的东西）要在上、左留出空位，不然上沿和最左一格被遮住
    expect(row.className).toMatch(/\bpt-1\b/);
    expect(row.className).toMatch(/\bpx-1\b/);
    expect(row.className).toContain("overflow-y-hidden");
    await m2.unmount();
  });
});

function WithPlaybackBar() {
  const playback = usePlayback(segments);
  return (
    <PlaybackContext.Provider value={playback}>
      <ClipBar />
    </PlaybackContext.Provider>
  );
}

describe("片段条：没视频时缩略图用故事板的缩略图", () => {
  test("故事板图用 ?v=thumb 的小图，不直接加载原图", async () => {
    const withBoard = {
      ...studio,
      segments: segments.slice(0, 2),
      boards: {
        statusOf: () => "ready",
        nodeOf: () => ({ src: "/files/assets/31.png" }),
        targets: [],
        ready: 2,
        blocked: null,
        fill: async () => {},
      },
    } as unknown as Studio;
    const m = await dom.mount(
      <StudioContext.Provider value={withBoard}>
        <WithPlaybackBar />
      </StudioContext.Provider>,
    );
    await frames(4);
    const srcs = [...m.container.querySelectorAll('[role="button"] img')].map((img) =>
      img.getAttribute("src"),
    );
    expect(srcs.length).toBeGreaterThan(0);
    // 每一格的故事板图都是缩略图地址
    expect(srcs.every((src) => src === "/files/assets/31.png?v=thumb")).toBe(true);
    await m.unmount();
  });
});

describe("片段条：右上角「批量生成故事板」", () => {
  test("和「批量生成分镜视频」并排在右上角，样式一样、都不带图标，点了补齐故事板；不再有要补的就禁用", async () => {
    let filled = 0;
    const withFill = {
      ...studio,
      boards: { ...studio.boards, fill: async () => void (filled += 1) },
    } as unknown as Studio;
    const m = await dom.mount(
      <StudioContext.Provider value={withFill}>
        <WithPlaybackBar />
      </StudioContext.Provider>,
    );
    const find = (text: string) =>
      [...m.container.querySelectorAll("button")].find((b) =>
        (b.textContent ?? "").includes(text),
      ) as HTMLButtonElement;
    const fill = find("批量生成故事板");
    const batch = find("批量生成分镜视频");
    expect(fill.className).toBe(batch.className);
    expect(fill.parentElement).toBe(batch.parentElement);
    // 批量生成故事板在批量生成分镜视频左边，图标是宫格加一格
    expect(fill.compareDocumentPosition(batch) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(fill.querySelector("svg")).toBeNull();
    expect(batch.querySelector("svg")).toBeNull();
    expect(fill.textContent).toContain("· 3");
    await dom.click(fill);
    expect(filled).toBe(1);
    await m.unmount();

    const done = {
      ...studio,
      boards: { ...studio.boards, targets: [], ready: 30 },
    } as unknown as Studio;
    const m2 = await dom.mount(
      <StudioContext.Provider value={done}>
        <WithPlaybackBar />
      </StudioContext.Provider>,
    );
    const disabled = [...m2.container.querySelectorAll("button")].find((b) =>
      (b.textContent ?? "").includes("批量生成故事板"),
    ) as HTMLButtonElement;
    expect(disabled.disabled).toBe(true);
    await m2.unmount();
  });
});
