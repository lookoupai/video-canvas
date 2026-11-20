import { afterAll, beforeAll, describe, expect, mock, test } from "bun:test";
import { useEffect } from "react";

import type { Drama } from "@/api/drama/type";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { StudioView as StudioViewType } from "@/pages/drama/studio/view";
import type { DramaWorkspace } from "@/pages/drama/workspace/use-workspace";

/**
 * 片场「列表 | 画布」：像资产管理那样，列表和画布都不卸载，切换只改看哪一面（交叉淡入）；
 * 画布在片场进入后的空闲时间后台挂好，点的时候已经在了，不用等。「进入画布查看」带目标节点，画布显示后飞过去。
 * 画布换成可控的假组件，只看外壳的行为。
 */

const { ViewToggleStub } = await import("@/tests/pages/drama/view-toggle-stub");
const { useStudio } = await import("@/pages/drama/studio/use-studio");
let mounts = 0;
let reportFail: () => void = () => {};
let openBoard: (id: string) => void = () => {};
const flown: string[] = [];
mock.module("@/pages/drama/workspace/assets-area", () => ({
  withNewDrama: (node: unknown) => node,
  ViewToggle: ViewToggleStub,
  DramaCanvas: ({
    commandsRef: ref,
    onFail,
  }: {
    commandsRef: { current: unknown };
    onFail?: () => void;
  }) => {
    // 数真正的挂载（不是重渲染）：空依赖的 effect 只在挂载时跑一次
    useEffect(() => {
      mounts += 1;
    }, []);
    ref.current = {
      getNodes: () => [{ id: "b1" }, { id: "b2" }],
      focusNode: (id: string) => {
        flown.push(id);
        return true;
      },
    };
    reportFail = () => onFail?.();
    const studio = useStudio();
    useEffect(() => {
      openBoard = studio.openBoardInCanvas;
    });
    return <div data-slot="fake-canvas" />;
  },
}));
mock.module("@/pages/drama/workspace/assets-overlay", () => ({ CanvasTools: () => null }));

let dom: DomEnv;
let StudioView: typeof StudioViewType;
let Context: typeof import("@/pages/drama/workspace/context").DramaWorkspaceContext;
beforeAll(async () => {
  dom = await installDom();
  dom.win.matchMedia = (() => ({
    matches: true,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
  })) as never;
  (globalThis as { matchMedia?: unknown }).matchMedia = dom.win.matchMedia;
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
afterAll(() => dom?.restore());

const drama = {
  id: "d1",
  ratio: "9:16",
  canvasId: "c1",
  videoModel: "",
  imageModels: { character: "", scene: "", prop: "", board: "" },
  promptSettings: { board: { source: "builtin", label: "内置" } },
  episodes: [{ no: 1, title: "初见", locked: true, storyboardStatus: "ready", segmentCount: 0 }],
} as unknown as Drama;

async function frames(n: number) {
  for (let i = 0; i < n; i++) await dom.flush(30);
}
/** 等到后台挂画布的空闲回调 / 定时器（jsdom 没有 requestIdleCallback，走 300ms 定时器）也跑完 */
const settle = () => frames(20);

async function mountStudio() {
  mounts = 0;
  flown.length = 0;
  const m = await dom.mount(
    <Context.Provider
      value={{ drama, jobs: [], refresh: async () => {} } as unknown as DramaWorkspace}
    >
      <StudioView drama={drama} no={1} refresh={async () => {}} />
    </Context.Provider>,
  );
  return m;
}
const toggle = (c: HTMLElement, label: string) =>
  [...c.querySelectorAll("button")].find((b) => b.textContent?.trim() === label) as
    | HTMLButtonElement
    | undefined;
const faces = (c: HTMLElement) => ({
  list: c.querySelector('[data-slot="studio-list"]') as HTMLElement | null,
  canvas: c.querySelector('[data-slot="studio-canvas"]') as HTMLElement | null,
});

describe("片场进入画布：都不卸载，只切看哪一面", () => {
  test("片场进入、各区域挂好之后，空闲时在后台把画布挂上（列表仍然在看）", async () => {
    const m = await mountStudio();
    // 刚进来（骨架那一拍）画布还没挂，免得和列表抢第一屏
    expect(faces(m.container).canvas).toBeNull();
    await settle();
    const { list, canvas } = faces(m.container);
    expect(canvas).not.toBeNull();
    expect(canvas?.getAttribute("data-shown")).toBe("false");
    expect(list?.getAttribute("data-shown")).toBe("true");
    await m.unmount();
  });

  test("点「画布」：画布早已挂好，只是换成看画布面（列表不卸载，保留滚动 / 选中 / 编辑状态）", async () => {
    const m = await mountStudio();
    await settle();
    const before = mounts;
    await dom.click(toggle(m.container, "画布"));
    await frames(2);
    const { list, canvas } = faces(m.container);
    expect(canvas?.getAttribute("data-shown")).toBe("true");
    expect(list?.getAttribute("data-shown")).toBe("false");
    // 列表还在 DOM 里，没有被卸载
    expect(list).not.toBeNull();
    // 画布没有重新挂载（切换零成本）
    expect(mounts).toBe(before);
    await m.unmount();
  });

  test("看不见的那一面不抢事件：不可点、对读屏隐藏（inert）", async () => {
    const m = await mountStudio();
    await settle();
    const hidden = faces(m.container).canvas as HTMLElement;
    expect(hidden.className).toContain("pointer-events-none");
    expect(hidden.hasAttribute("inert")).toBe(true);
    await dom.click(toggle(m.container, "画布"));
    await frames(2);
    expect(faces(m.container).canvas?.hasAttribute("inert")).toBe(false);
    expect(faces(m.container).list?.hasAttribute("inert")).toBe(true);
    await m.unmount();
  });

  test("两面用交叉淡入淡出（只动透明度），不是硬切", async () => {
    const m = await mountStudio();
    await settle();
    const { list, canvas } = faces(m.container);
    for (const el of [list, canvas]) {
      expect(el?.className).toMatch(/transition-opacity/);
      expect(el?.className).toMatch(/duration-/);
    }
    await m.unmount();
  });

  test("切回「列表」：画布仍然挂着（下次再切零成本）", async () => {
    const m = await mountStudio();
    await settle();
    await dom.click(toggle(m.container, "画布"));
    await frames(2);
    const before = mounts;
    await dom.click(toggle(m.container, "片场"));
    await frames(2);
    const { list, canvas } = faces(m.container);
    expect(list?.getAttribute("data-shown")).toBe("true");
    expect(canvas).not.toBeNull();
    expect(canvas?.getAttribute("data-shown")).toBe("false");
    expect(mounts).toBe(before);
    await m.unmount();
  });

  test("画布加载失败：卸载画布、回列表，切换按钮仍然可点（点了会重新挂）", async () => {
    const m = await mountStudio();
    await settle();
    await dom.click(toggle(m.container, "画布"));
    await frames(2);
    await dom.act(async () => reportFail());
    await frames(2);
    expect(faces(m.container).canvas).toBeNull();
    expect(faces(m.container).list?.getAttribute("data-shown")).toBe("true");
    await dom.click(toggle(m.container, "画布"));
    await frames(2);
    expect(faces(m.container).canvas).not.toBeNull();
    await m.unmount();
  });
});

describe("进入画布查看：画布显示后过渡着飞到目标节点", () => {
  test("点「进入画布查看」：切到画布面，画布里视口飞到那个节点（有过渡，不是瞬移）；飞完不再重复飞", async () => {
    const m = await mountStudio();
    await settle();
    // 通过片场上下文触发「进入画布查看」
    await dom.act(async () => openBoard("b2"));
    await settle();
    expect(faces(m.container).canvas?.getAttribute("data-shown")).toBe("true");
    expect(flown).toEqual(["b2"]);
    // 再等一会儿：不会重复飞
    await settle();
    expect(flown).toEqual(["b2"]);
    await m.unmount();
  });

  test("画布还没显示时（后台挂着）不飞：只有切到画布面才飞，免得用户看不见", async () => {
    const m = await mountStudio();
    await settle();
    expect(faces(m.container).canvas?.getAttribute("data-shown")).toBe("false");
    expect(flown).toEqual([]);
    await m.unmount();
  });
});
