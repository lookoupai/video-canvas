import { describe, expect, test } from "bun:test";

import type { CanvasNodeData } from "@/types";
import {
  BOARD_LINE,
  BOARD_RATIO,
  boardLayout,
  boardPrompt,
  boardRef,
  boardsFromGraph,
  boardStatus,
  isBoardNode,
  neighborVersion,
  shotLines,
} from "@/utils/drama/board";

const PROMPT = `【画幅风格】9:16 竖屏，真人电影质感，暖黄低饱和。
【场景与光】@[周家厨房](n_kitchen)，午后侧光。
【参考与人物】@[闰土](n_runtu) 外貌与服装。
【站位】闰土画面左，靠灶台。
【时间轴】
0-3秒：全景、平视、固定。迅哥儿推门跑进厨房。台词：无
3-6秒：中景、缓推。闰土低头躲在灶台边。
6-9.5秒：近景、过肩。迅哥儿伸出手。迅哥儿（试探）：『你就是闰土？』台词：『你就是闰土？』`;

/** 和后端内置的故事板模板一致（backend/internal/drama/prompts/board.md） */
const TEMPLATE = [
  "生成一张 {{格数}} 格分镜图：{{列数}} 列 {{行数}} 行，整图比例 {{比例}}，从左到右、从上到下依次对应下面的镜头。每格右上角标出编号，格与格之间用细白线隔开，没用到的格子留白；只画画面，不写台词和说明文字。",
  "{{画面}}",
  "无字幕，无贴图，无旁白，无背景音乐，保持人物与参考图完全一致，面部特征100%相同，保持相同的骨骼结构、眼间距和下颚几何形状，禁止美化，禁止改变年龄",
].join("\n");

type GraphNode = { id: string; type?: string; data: CanvasNodeData };

/** 画布上的故事板节点：片段 s1，出图时原稿哈希 h1 */
const boardNode = (data: Partial<CanvasNodeData> = {}, id = "b1"): GraphNode => ({
  id,
  type: "canvas",
  data: {
    kind: "image",
    label: "故事板 · 第 1 集 · 01",
    src: "https://cdn/31.png",
    assetId: "31",
    drama: {
      episodes: [1],
      board: { segmentId: "s1", sourceHash: "h1", layout: "2x2", shotCount: 3 },
    },
    ...data,
  } as CanvasNodeData,
});

describe("utils/drama/board：镜头与布局", () => {
  test("只数【时间轴】里「起-止秒：」开头的行，去掉时间、台词", () => {
    expect(shotLines(PROMPT)).toEqual([
      "全景、平视、固定。迅哥儿推门跑进厨房。",
      "中景、缓推。闰土低头躲在灶台边。",
      "近景、过肩。迅哥儿伸出手。",
    ]);
    expect(shotLines("没有分栏的原稿")).toEqual([]);
    // 时间轴之后的分栏不算
    expect(shotLines("【时间轴】\n0-2秒：特写。\n【备注】\n2-4秒：不算。")).toEqual(["特写。"]);
  });

  test("故事板固定 16:9 横版：1–4 镜 2x2，5–6 镜 3x2，7 镜以上 3x3", () => {
    expect(BOARD_RATIO).toBe("16:9");
    expect(boardLayout(0)).toEqual({ layout: "2x2", cols: 2, rows: 2 });
    expect(boardLayout(4).layout).toBe("2x2");
    expect(boardLayout(5)).toEqual({ layout: "3x2", cols: 3, rows: 2 });
    expect(boardLayout(6).layout).toBe("3x2");
    expect(boardLayout(9).layout).toBe("3x3");
    expect(boardLayout(14).layout).toBe("3x3");
  });
});

describe("utils/drama/board：宫格图提示词", () => {
  test("内置模板：画面风格、场景、人物、站位和逐镜画面，保留 @ 引用，不带台词", () => {
    const out = boardPrompt(PROMPT, TEMPLATE);
    expect(out.layout).toBe("2x2");
    expect(out.shotCount).toBe(3);
    expect(out.notice).toBe("");
    expect(out.text).toContain("4 格分镜图");
    expect(out.text).toContain("2 列 2 行，整图比例 16:9");
    expect(out.text).toContain("真人电影质感");
    expect(out.text).toContain("@[周家厨房](n_kitchen)");
    expect(out.text).toContain("人物与道具：@[闰土](n_runtu) 外貌与服装。");
    expect(out.text).toContain("闰土画面左");
    expect(out.text).toContain("1. 全景、平视、固定。迅哥儿推门跑进厨房。");
    expect(out.text).toContain("3. 近景、过肩。迅哥儿伸出手。");
    expect(out.text).not.toContain("你就是闰土");
    // 固定结尾：无字幕等，人物与参考图一致
    expect(out.text.endsWith("禁止美化，禁止改变年龄")).toBe(true);
    expect(out.text).not.toContain("{{");
  });

  test("自定义模板：占位符都被替换（同一个占位符出现多次也全换），模板其余文字原样保留", () => {
    const out = boardPrompt(
      PROMPT,
      "水彩风。共 {{格数}} 格（{{列数}}×{{行数}}），比例 {{比例}}。\n{{画面}}\n再强调一次 {{格数}} 格。",
    );
    expect(out.text.startsWith("水彩风。共 4 格（2×2），比例 16:9。\n画面风格：")).toBe(true);
    expect(out.text).toContain("1. 全景、平视、固定。迅哥儿推门跑进厨房。");
    expect(out.text.endsWith("再强调一次 4 格。")).toBe(true);
    expect(out.text).not.toContain("{{");
    expect(out).toMatchObject({ layout: "2x2", shotCount: 3 });
  });

  test("模板里漏了 {{画面}}（不该发生）：画面接在最后，镜头内容不丢", () => {
    const out = boardPrompt(PROMPT, "只有开头 {{格数}} 格");
    expect(out.text.startsWith("只有开头 4 格\n")).toBe(true);
    expect(out.text).toContain("1. 全景、平视、固定。");
  });

  test("没有模板（空串）：只放画面，不写死任何版式文字", () => {
    expect(boardPrompt(PROMPT, "  ").text).toContain("1. 全景、平视、固定。");
  });

  test("没识别到时间轴：按 4 格出，并把原稿整段交给模型", () => {
    const out = boardPrompt("一段没有分栏的原稿", TEMPLATE);
    expect(out).toMatchObject({ layout: "2x2", shotCount: 0 });
    expect(out.notice).toBe("没识别到时间轴，按 4 格生成");
    expect(out.text).toContain("一段没有分栏的原稿");
  });

  test("镜头超过 9 个：第 9 格合并剩下的镜头并提示", () => {
    const lines = Array.from({ length: 11 }, (_, i) => `${i}-${i + 1}秒：镜头${i + 1}。`).join(
      "\n",
    );
    const out = boardPrompt(`【时间轴】\n${lines}`, TEMPLATE);
    expect(out).toMatchObject({ layout: "3x3", shotCount: 11 });
    expect(out.notice).toBe("镜头过多（11 个），第 9 格合并了剩下的镜头，建议拆段");
    expect(out.text).toContain("9. 镜头9。；镜头10。；镜头11。");
    expect(out.text).not.toContain("10. ");
  });
});

describe("utils/drama/board：画布上的故事板节点", () => {
  test("只认带 drama.board 的节点，按片段 id 取；普通资产、组节点不算", () => {
    const nodes: GraphNode[] = [
      boardNode(),
      {
        id: "a1",
        type: "canvas",
        data: {
          kind: "image",
          label: "村口",
          drama: { type: "scene", name: "村口", episodes: [] },
        } as CanvasNodeData,
      },
      { id: "g1", type: "group", data: { kind: "image", label: "组" } as CanvasNodeData },
    ];
    const map = boardsFromGraph(nodes);
    expect([...map.keys()]).toEqual(["s1"]);
    expect(map.get("s1")).toMatchObject({
      nodeId: "b1",
      sourceHash: "h1",
      src: "https://cdn/31.png",
      assetId: "31",
      status: "done",
    });
    expect(isBoardNode(nodes[0].data)).toBe(true);
    expect(isBoardNode(nodes[1].data)).toBe(false);
  });

  test("状态：没有节点 / 没出图 → 未生成；生成中；失败；原稿哈希对不上 → 过期；否则就绪", () => {
    const of = (data: Partial<CanvasNodeData>) => boardsFromGraph([boardNode(data)]).get("s1");
    expect(boardStatus(undefined, "h1")).toBe("empty");
    expect(boardStatus(of({ src: null, assetId: undefined }), "h1")).toBe("empty");
    expect(boardStatus(of({ status: "running" }), "h1")).toBe("running");
    expect(boardStatus(of({ status: "error", error: "审核未通过" }), "h1")).toBe("failed");
    expect(of({ status: "error", error: "审核未通过" })?.error).toBe("审核未通过");
    expect(boardStatus(of({}), "h2")).toBe("stale");
    expect(boardStatus(of({}), "h1")).toBe("ready");
    // 原稿哈希还没算出来：先按就绪
    expect(boardStatus(of({}), undefined)).toBe("ready");
  });

  test("正在生成的任务 id：读节点上的 taskId，生成中的故事板才能订阅到实时进度；没有就是 null", () => {
    const of = (data: Partial<CanvasNodeData>) => boardsFromGraph([boardNode(data)]).get("s1");
    expect(of({ status: "running", taskId: "88" })?.taskId).toBe("88");
    expect(of({})?.taskId).toBeNull();
    expect(of({ taskId: "" })?.taskId).toBeNull();
  });

  test("历史版本和当前版本：读节点的 outputs / activeOutputId；旧节点只有一张图时当唯一一版；空节点没有版本", () => {
    const of = (data: Partial<CanvasNodeData>) => boardsFromGraph([boardNode(data)]).get("s1");
    const v = (id: string) => ({
      id,
      src: `https://cdn/${id}.png`,
      mediaType: "image" as const,
      assetId: id,
      createdAt: 1,
    });
    const multi = of({ outputs: [v("30"), v("31"), v("32")], activeOutputId: "31" });
    expect(multi?.versions.map((item) => item.id)).toEqual(["30", "31", "32"]);
    expect(multi?.activeId).toBe("31");
    // activeOutputId 没写或已不在历史里：取最后一版
    expect(of({ outputs: [v("30"), v("31")] })?.activeId).toBe("31");
    expect(of({ outputs: [v("30")], activeOutputId: "gone" })?.activeId).toBe("30");
    // 旧节点没有 outputs，只有 src + assetId：当唯一一版
    expect(of({})?.versions).toEqual([
      {
        id: "31",
        src: "https://cdn/31.png",
        mediaType: "image",
        assetId: "31",
        taskId: undefined,
        model: undefined,
        createdAt: 0,
      },
    ]);
    // 分镜时建好的空节点：没有图也没有版本
    const empty = of({ src: null, assetId: undefined });
    expect(empty?.versions).toEqual([]);
    expect(empty?.activeId).toBeNull();
    expect(boardStatus(empty, "h1")).toBe("empty");
  });

  test("相邻版本：往后 / 往前一版，到头不动，当前版本不在历史里时当最后一版", () => {
    const v = (id: string) => ({ id }) as never;
    const list = [v("30"), v("31"), v("32")];
    expect(neighborVersion(list, "31", 1)).toBe("32");
    expect(neighborVersion(list, "31", -1)).toBe("30");
    expect(neighborVersion(list, "32", 1)).toBeNull();
    expect(neighborVersion(list, "30", -1)).toBeNull();
    expect(neighborVersion(list, null, -1)).toBe("31");
    expect(neighborVersion([v("30")], "30", 1)).toBeNull();
    expect(neighborVersion([], null, 1)).toBeNull();
  });

  test("生视频时的引用：有图就带（生成中沿用上一版），带上是否过期", () => {
    const of = (data: Partial<CanvasNodeData>) => boardsFromGraph([boardNode(data)]).get("s1");
    expect(boardRef(undefined, "h1")).toBeNull();
    expect(boardRef(of({ src: null, assetId: undefined }), "h1")).toBeNull();
    expect(boardRef(of({}), "h2")).toEqual({
      assetId: "31",
      src: "https://cdn/31.png",
      stale: true,
    });
    expect(boardRef(of({ status: "running" }), "h1")).toEqual({
      assetId: "31",
      src: "https://cdn/31.png",
      stale: false,
    });
  });

  test("提示词说明行点名图片1是宫格图", () => {
    expect(BOARD_LINE).toContain("图片1");
    expect(BOARD_LINE).toContain("分镜宫格图");
  });

  test("提示词说明行：按格子顺序对应镜头、只参考构图；成片不带宫格线、编号、线稿和批注文字（故事板是带批注的手绘稿）", () => {
    expect(BOARD_LINE).toContain("手绘");
    expect(BOARD_LINE).toContain("编号顺序");
    expect(BOARD_LINE).toContain("只参考构图");
    for (const word of ["宫格线", "编号", "线稿", "批注文字"]) {
      expect(BOARD_LINE.slice(BOARD_LINE.indexOf("不出现"))).toContain(word);
    }
    expect(BOARD_LINE).toContain("下面写的画风");
  });
});
