import { afterAll, beforeAll, describe, expect, mock, test } from "bun:test";
import { useEffect } from "react";

import type { BoardGenerateBody, Drama, Segment } from "@/api/drama/type";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { BoardNode } from "@/utils/drama/board";
import type { RefAsset } from "@/utils/drama/segment-assets";
import type {
  StudioBoards,
  useStudioBoards as HookType,
} from "@/pages/drama/studio/use-studio-boards";
import type { useModelsStore as ModelsStoreType } from "@/store/models";

/** 片场故事板 hook：按画布节点和原稿算状态，生成时把按模板拼好的请求交给后端 */

const submitted: Array<{ sid: string; body: BoardGenerateBody }> = [];
let submitError: Error | null = null;
const selected: Array<[string, string]> = [];
let selectError: Error | null = null;
// 切换故事板版本走后端接口：换成可数的假实现
const realDramaApi = { ...(await import("@/api/drama")) };
mock.module("@/api/drama", () => ({
  ...realDramaApi,
  selectBoardOutput: async (sid: string, outputId: string) => {
    if (selectError) throw selectError;
    selected.push([sid, outputId]);
  },
}));
// bun 的 mock.module 对整个测试进程生效：带上真模块的其他导出，只换提交故事板
const realGateway = { ...(await import("@/utils/tasks/gateway")) };
mock.module("@/utils/tasks/gateway", () => ({
  ...realGateway,
  submitBoardTask: async (sid: string, body: BoardGenerateBody) => {
    if (submitError) throw submitError;
    submitted.push({ sid, body });
    return { id: 1 };
  },
}));

let dom: DomEnv;
let useStudioBoards: typeof HookType;
let useModelsStore: typeof ModelsStoreType;
beforeAll(async () => {
  dom = await installDom();
  useStudioBoards = (await import("@/pages/drama/studio/use-studio-boards")).useStudioBoards;
  useModelsStore = (await import("@/store/models")).useModelsStore;
  useModelsStore.setState({
    byKind: {
      image: {
        status: "ready",
        loadedAt: Date.now(),
        models: [
          {
            key: "img-scene",
            kind: "image",
            label: "场景模型",
            pricing: {},
            capabilities: {
              ops: ["t2i", "i2i"],
              refs: { image: { on: true, max: 4, max_mb: 10 } },
              params: {},
            },
          } as never,
        ],
      },
    },
  });
});
afterAll(() => {
  useModelsStore.setState({ byKind: {} });
  dom.restore();
});

const PROMPT = "【场景与光】@[厨房](n_kitchen)\n【时间轴】\n0-3秒：全景。\n3-6秒：中景。";
const segments: Segment[] = [
  { id: "s0", seq: 0, prompt: PROMPT } as Segment,
  { id: "s1", seq: 1, prompt: PROMPT } as Segment,
  { id: "s2", seq: 2, prompt: "  " } as Segment,
];
const drama = {
  id: "d1",
  imageModels: { character: "", scene: "img-scene", prop: "", board: "" },
  promptSettings: {
    board: { source: "mine", label: "我的", bodies: { board: "水彩 {{格数}} 格\n{{画面}}" } },
  },
} as unknown as Drama;
const assets: RefAsset[] = [
  {
    id: "n_kitchen",
    type: "scene",
    name: "厨房",
    look: "",
    label: "厨房",
    episodes: [],
    kind: "image",
    src: "https://cdn/5.png",
    assetId: "5",
    generating: false,
  },
];
const board = (over: Partial<BoardNode>): BoardNode => ({
  segmentId: "s0",
  sourceHash: "fresh",
  layout: "2x2",
  shotCount: 2,
  nodeId: "b0",
  src: "https://cdn/31.png",
  assetId: "31",
  status: "done",
  error: "",
  ...over,
});

async function mountHook(boards: Map<string, BoardNode>) {
  let api: StudioBoards | null = null;
  let freshCalls = 0;
  function Probe() {
    const value = useStudioBoards({
      drama,
      no: 3,
      segments,
      boards,
      sourceOf: (segment) => segment.prompt,
      hashOf: (text) => (text === PROMPT ? "fresh" : undefined),
      flushEditors: async () => {},
      freshAssets: async () => {
        freshCalls += 1;
        return assets;
      },
      latestOf: (sid) => segments.find((segment) => segment.id === sid) ?? null,
    });
    // 每次渲染后把最新的结果交给测试读
    useEffect(() => {
      api = value;
    });
    return null;
  }
  const m = await dom.mount(<Probe />);
  await dom.flush();
  return { m, api: () => api as unknown as StudioBoards, fresh: () => freshCalls };
}

describe("useStudioBoards", () => {
  test("状态：有图且原稿没改是可用；原稿改过是过期；没节点是没有；空原稿不算要补", async () => {
    const boards = new Map([
      ["s0", board({})],
      ["s1", board({ segmentId: "s1", sourceHash: "old" })],
    ]);
    const { m, api } = await mountHook(boards);
    expect(segments.map((s) => api().statusOf(s))).toEqual(["ready", "stale", "empty"]);
    expect(api().ready).toBe(1);
    expect(api().targets.map((s) => s.id)).toEqual(["s1"]);
    // 没选故事板模型：跟随场景图片模型
    expect(api().model?.key).toBe("img-scene");
    expect(api().blocked).toBeNull();
    await m.unmount();
  });

  test("生成：按项目设置里的模板拼提示词，@ 的资产带成参考图，交给后端；提交后重读画布", async () => {
    submitted.length = 0;
    const { m, api, fresh } = await mountHook(new Map());
    await dom.act(async () => api().generate([segments[0]]));
    await dom.flush();
    expect(submitted).toHaveLength(1);
    const { sid, body } = submitted[0];
    expect(sid).toBe("s0");
    expect(body).toMatchObject({
      model_id: "img-scene",
      label: "故事板 · 第 3 集 · 01",
      source_hash: "fresh",
      layout: "2x2",
      shot_count: 2,
      refs: ["n_kitchen"],
    });
    expect(body.prompt.startsWith("水彩 4 格\n")).toBe(true);
    expect(body.input.images).toEqual([5]);
    // 提交前读一次资产、提交后再读一次画布（读到节点再撤掉「提交中」）
    expect(fresh()).toBe(2);
    expect(api().statusOf(segments[0])).toBe("empty");
    await m.unmount();
  });

  test("提交失败：不抛错，「提交中」撤掉", async () => {
    submitError = new Error("积分不足");
    const { m, api } = await mountHook(new Map());
    await dom.act(async () => api().generate([segments[0]]));
    await dom.flush();
    expect(api().statusOf(segments[0])).toBe("empty");
    submitError = null;
    await m.unmount();
  });

  test("切换版本：交给后端改节点，再重读画布；失败不抛错", async () => {
    selected.length = 0;
    const { m, api, fresh } = await mountHook(new Map());
    const before = fresh();
    await dom.act(async () => api().selectVersion(segments[0], "32"));
    expect(selected).toEqual([["s0", "32"]]);
    expect(fresh()).toBe(before + 1);
    selectError = new Error("没有这一版故事板");
    await dom.act(async () => api().selectVersion(segments[0], "999"));
    expect(selected).toHaveLength(1);
    expect(fresh()).toBe(before + 1);
    selectError = null;
    await m.unmount();
  });
});
