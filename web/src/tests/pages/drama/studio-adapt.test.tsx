import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { Segment } from "@/api/drama/type";
import { Editor } from "@/pages/drama/studio/editor";
import { GenerateControls } from "@/pages/drama/studio/generate-controls";
import { PromptPane } from "@/pages/drama/studio/prompt-pane";
import { StudioContext, type Studio } from "@/pages/drama/studio/use-studio";

const seg = (patch: Partial<Segment> = {}): Segment => ({
  id: "s1",
  episodeNo: 1,
  seq: 0,
  seconds: 10,
  prompt: "【场景】原稿",
  takes: [],
  chosenTaskId: "",
  status: "idle",
  revision: 1,
  updatedAt: "",
  adaptations: {},
  ...patch,
});

/** 夹具的 promptView 默认「原稿」；有转写稿的用例自己传 promptView: "adapted"（真实状态是有转写稿默认看转写） */
const studio = (patch: Partial<Studio>): Studio => {
  const base = {
    promptView: "source",
    assets: [],
    assetsStatus: "ready",
    no: 1,
    format: "h3",
    setPromptView: () => {},
    adapting: new Set<string>(),
    adaptErrors: {},
    adaptStateOf: () => "none",
    setDraft: () => {},
    reportSave: () => {},
    registerEditor: () => () => {},
    submitting: new Set<string>(),
    plan: { errors: {}, input: {}, refs: [], imageCount: 0, imageMax: 9, deadLabels: [] },
    video: { models: [], status: "ready", model: { key: "h3" }, setModel: () => {} },
    actions: { patch: async () => seg(), generate: async () => {}, adapt: async () => true },
    ...patch,
  } as unknown as Studio;
  return base;
};

const render = (value: Studio, node: React.ReactNode) =>
  renderToStaticMarkup(<StudioContext.Provider value={value}>{node}</StudioContext.Provider>);

describe("片场：原稿 / 转写标签", () => {
  test("顶栏是 Seedance 模型：没有转写标签和转写按钮", () => {
    const html = render(
      studio({ format: "seedance", adaptStateOf: () => null }),
      <PromptPane segment={seg()} />,
    );
    expect(html).not.toContain("原稿 · Seedance");
    expect(html).not.toContain("转写为");
  });

  test("H3 模型、还没转写：两个标签，按钮写「转写为 H3」，默认停在原稿", () => {
    const html = render(studio({}), <PromptPane segment={seg()} />);
    expect(html).toContain("原稿 · Seedance");
    expect(html).toContain("转写 · H3");
    expect(html).toContain("转写为 H3");
    expect(html).toMatch(
      /aria-selected="true"[^>]*>(<span[^>]*><\/span>)?<span class="relative">原稿/,
    );
  });

  test("有转写稿但原稿改过：默认停在转写，标「已过期」并提示重新转写", () => {
    const segment = seg({
      adaptations: {
        h3: { prompt: "integrated", sourceHash: "old", edited: false, updatedAt: "" },
      },
    });
    const html = render(
      studio({ adaptStateOf: () => "stale", promptView: "adapted" }),
      <PromptPane segment={segment} />,
    );
    expect(html).toContain("已过期");
    expect(html).toContain("原稿已修改");
    expect(html).toContain("重新转写");
  });

  test("重新转写失败：转写标签里显示原因和重试，旧转写稿还在", () => {
    const segment = seg({
      adaptations: { h3: { prompt: "旧转写", sourceHash: "h", edited: false, updatedAt: "" } },
    });
    const html = render(
      studio({
        adaptStateOf: () => "fresh",
        promptView: "adapted",
        adaptErrors: { "s1:h3": "转写结果不合格，请重试" },
      }),
      <PromptPane segment={segment} />,
    );
    expect(html).toContain("转写结果不合格，请重试");
    expect(html).toContain(">重试<");
  });
});

describe("片场：提示词框撑满剩余高度", () => {
  test("框占右栏剩下的高度（flex-1），文字多了在框里滚，不按视口限高，也不让整个右栏滚", () => {
    for (const format of ["seedance", "h3"] as const) {
      const html = render(studio({ format }), <PromptPane segment={seg()} />);
      const box =
        html.match(/<div[^>]*class="([^"]*bg-panel[^"]*rounded-\[14px\][^"]*)"/)?.[1] ?? "";
      expect(box).toContain("flex-1");
      expect(box).toContain("min-h-48");
      expect(html).not.toContain("max-h-[50vh]");
    }
  });
});

describe("片场：生成按钮的拦截原因", () => {
  test("成片裁剪未保存时先保存窗口，避免按旧时长报价后提交新时长", () => {
    const html = render(
      studio({
        selected: seg(),
        format: "seedance",
        adaptStateOf: () => null,
        windowSavePending: true,
      }),
      <GenerateControls />,
    );
    expect(html).toContain("请先保存成片裁剪");
    expect(html).toContain("disabled");
  });

  test("选的是原稿：H3 模型还没转写也能生成（原稿直接生成，不要求先转写）", () => {
    const html = render(studio({ selected: seg(), promptView: "source" }), <GenerateControls />);
    expect(html).not.toContain("先把原稿转写为 H3");
    expect(html).not.toMatch(/disabled=""[^>]*>[^<]*立即生成/);
  });

  test("选的是转写、还没有转写稿：禁用并写明先转写", () => {
    const html = render(studio({ selected: seg(), promptView: "adapted" }), <GenerateControls />);
    expect(html).toContain("先把原稿转写为 H3");
  });

  test("选的是转写、已有转写稿：能生成", () => {
    const html = render(
      studio({
        selected: seg({
          adaptations: { h3: { prompt: "转写稿", sourceHash: "h", edited: false, updatedAt: "" } },
        }),
        promptView: "adapted",
        adaptStateOf: () => "fresh",
      }),
      <GenerateControls />,
    );
    expect(html).not.toContain("先把原稿转写为 H3");
  });

  test("正在转写：禁用并写明正在转写（选转写时）", () => {
    const html = render(
      studio({ selected: seg(), promptView: "adapted", adaptStateOf: () => "busy" }),
      <GenerateControls />,
    );
    expect(html).toContain("正在转写");
  });
});

describe("片场：历史生成失败提示", () => {
  test("历史额度不足显示在提示词之前，不把渠道 JSON 放进编辑区域", () => {
    const error = JSON.stringify({
      code: "fail_to_fetch_task",
      message: JSON.stringify({
        code: "insufficient_user_quota",
        message: "Insufficient user quota, remaining: ＄0.000000",
      }),
    });
    const selected = seg({
      status: "error",
      takes: [{ taskId: "8", status: "failed", asset: null, errorMessage: error, createdAt: "" }],
    });
    const html = render(studio({ selected, plan: null }), <Editor />);
    expect(html).toContain("上次生成失败");
    expect(html).toContain("模型渠道余额不足，请补充渠道额度或更换渠道。");
    expect(html).not.toContain("fail_to_fetch_task");
    expect(html.indexOf("上次生成失败")).toBeLessThan(html.indexOf("原稿 · Seedance"));
  });

  test("重新生成中不显示旧失败提示", () => {
    const selected = seg({
      status: "running",
      takes: [
        { taskId: "8", status: "failed", asset: null, errorMessage: "旧失败", createdAt: "" },
      ],
    });
    const html = render(studio({ selected, plan: null }), <Editor />);
    expect(html).not.toContain("上次生成失败");
    expect(html).not.toContain("旧失败");
  });
});
