import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { StudioView as StudioViewType } from "@/pages/drama/studio/view";
import type { Drama } from "@/api/drama/type";
import type { DramaWorkspace } from "@/pages/drama/workspace/use-workspace";

/**
 * 进入片场分节奏挂载：第 0 拍只有分栏骨架（面板在、内容还没挂），之后预览 / 编辑区、再之后资产库 / 片段条逐步出现，
 * 不在同一帧把所有区域都挂完。直接渲染真的片场外壳，靠每个区域外层的 data-phased 标记看谁什么时候挂上。
 */

let dom: DomEnv;
let StudioView: typeof StudioViewType;
let Context: typeof import("@/pages/drama/workspace/context").DramaWorkspaceContext;
const pendingFrames = new Map<number, FrameRequestCallback>();
let frameId = 0;
beforeAll(async () => {
  dom = await installDom();
  /** 挂载耗时可能超过真实帧间隔，显式推进帧才能稳定检查每一拍。 */
  globalThis.requestAnimationFrame = dom.win.requestAnimationFrame = (callback) => {
    pendingFrames.set(++frameId, callback);
    return frameId;
  };
  globalThis.cancelAnimationFrame = dom.win.cancelAnimationFrame = (id) => {
    pendingFrames.delete(id);
  };
  // 当成宽屏：三栏（资产库 / 预览 / 编辑）都显示
  dom.win.matchMedia = (() => ({
    matches: true,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
  })) as never;
  (globalThis as { matchMedia?: unknown }).matchMedia = dom.win.matchMedia;
  // jsdom 没有 DOMRect，分栏库量尺寸时要用
  (globalThis as { DOMRect?: unknown }).DOMRect ??= class {
    x = 0;
    y = 0;
    width = 0;
    height = 0;
    top = 0;
    left = 0;
    right = 0;
    bottom = 0;
  };
  StudioView = (await import("@/pages/drama/studio/view")).StudioView;
  Context = (await import("@/pages/drama/workspace/context")).DramaWorkspaceContext;
}, 60_000);
afterAll(() => {
  pendingFrames.clear();
  dom?.restore();
});

const drama = {
  id: "d1",
  ratio: "9:16",
  canvasId: "c1",
  videoModel: "",
  imageModels: { character: "", scene: "", prop: "", board: "" },
  promptSettings: { board: { source: "builtin", label: "内置" } },
  episodes: [{ no: 1, title: "初见", locked: true, storyboardStatus: "ready", segmentCount: 0 }],
} as unknown as Drama;

const phased = (c: HTMLElement) =>
  [...c.querySelectorAll("[data-phased]")].map((el) => el.getAttribute("data-phased")).sort();

async function frames(n: number) {
  for (let i = 0; i < n; i++) {
    await dom.act(async () => {
      const callbacks = [...pendingFrames.values()];
      pendingFrames.clear();
      for (const callback of callbacks) callback(performance.now());
    });
  }
}

describe("片场外壳：分拍挂载", () => {
  test("第一帧所有分区都还没挂；之后先出预览 + 编辑区，再出资产库 + 片段条", async () => {
    const m = await dom.mount(
      <Context.Provider
        value={{ drama, jobs: [], refresh: async () => {} } as unknown as DramaWorkspace}
      >
        <StudioView drama={drama} no={1} refresh={async () => {}} />
      </Context.Provider>,
    );
    try {
      // 第 0 拍：骨架（分栏面板在，内容都还没挂）
      expect(phased(m.container)).toEqual([]);
      await frames(1);
      // 第 1 拍：用户马上要看的主体
      expect(phased(m.container)).toEqual(["editor", "preview"]);
      await frames(2);
      // 第 2 拍：最重的两块
      expect(phased(m.container)).toEqual(["clips", "editor", "library", "preview"]);
    } finally {
      await m.unmount();
    }
  });
});
