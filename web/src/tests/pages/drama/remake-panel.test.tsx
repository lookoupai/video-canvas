import { afterAll, beforeAll, expect, spyOn, test } from "bun:test";
import service from "@/utils/requests/service";
import type { Segment } from "@/api/drama/type";
import type { Studio, StudioContext as Context } from "@/pages/drama/studio/use-studio";
import type { RemakePanel } from "@/pages/drama/studio/remake-panel";
import { installDom, type DomEnv } from "../../helpers/dom";

let env: DomEnv;
let Panel: typeof RemakePanel;
let StudioContext: typeof Context;
beforeAll(async () => {
  env = await installDom();
  StudioContext = (await import("@/pages/drama/studio/use-studio")).StudioContext;
  Panel = (await import("@/pages/drama/studio/remake-panel")).RemakePanel;
});
afterAll(() => env.restore());
const selected: Segment = {
  id: "s",
  episodeNo: 1,
  seq: 0,
  seconds: 4,
  prompt: "",
  takes: [],
  chosenTaskId: "",
  status: "idle",
  revision: 2,
  updatedAt: "",
  adaptations: {},
  remake: {
    adoptionId: "a",
    referenceId: "r",
    referenceRevision: 3,
    assetId: "8",
    sources: [{ shotId: "two", startMs: 2500, endMs: 5000 }],
    roles: [],
    targetDurationMs: 2500,
    trimStartMs: 0,
  },
};

test("原镜回放与成片窗口独立，窗口保存失败保留输入供重试", async () => {
  const get = spyOn(service, "get").mockResolvedValue({
    id: 8,
    url: "/files/reference.mp4",
    kind: "video",
    duration_ms: 5000,
  });
  const calls: number[][] = [];
  let fail = true;
  const value = {
    selected,
    canReferenceVideo: true,
    useReferenceVideo: true,
    setReferenceVideo: () => {},
    submitting: new Set(),
    plan: { input: { duration: 4 } },
    actions: {
      setWindow: async (_segment: Segment, duration: number, start: number) => {
        calls.push([duration, start]);
        if (fail) throw new Error("failed");
      },
    },
    reportSave: () => {},
    registerEditor: () => () => {},
  } as unknown as Studio;
  const { unmount } = await env.mount(
    <StudioContext.Provider value={value}>
      <Panel />
    </StudioContext.Provider>,
  );
  try {
    const buttons = () => Array.from(document.querySelectorAll("button"));
    await env.click(buttons().find((el) => el.textContent?.includes("原镜 1")));
    expect(document.querySelector("video")?.src).toContain("#t=2.5,5");
    await env.type(document.querySelector('[aria-label="成片保留秒数"]'), "2");
    await env.type(document.querySelector('[aria-label="采用素材裁剪起点秒"]'), "0.5");
    await env.click(buttons().find((el) => el.textContent === "保存裁剪"));
    expect(document.querySelector('[role="alert"]')?.textContent).toContain("未保存");
    expect(calls[0]).toEqual([2000, 500]);
    fail = false;
    await env.click(buttons().find((el) => el.textContent === "保存裁剪"));
    expect(calls[1]).toEqual([2000, 500]);
    expect(selected.remake?.sources[0].startMs).toBe(2500);
  } finally {
    await unmount();
    get.mockRestore();
  }
});
