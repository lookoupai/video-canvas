import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  spyOn,
  test,
} from "bun:test";

import type { CreateDramaInput, SourcePreflight } from "@/api/drama/type";
import type { PromptLibrary } from "@/components/drama/prompts/use-prompt-library";
import { holdExitAnimations, installDom, type DomEnv } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";
import type { StartBody as StartBodyType } from "@/pages/drama/start-dialog";
import type { useModelsStore as useModelsStoreType } from "@/store/models";

/**
 * 开拍设置弹窗主体：用 jsdom 挂载真实组件，检查来源行、规则（剧本置灰 / 时长锁定 / 模型装不下原文时禁用 /
 * 识别不出禁用开始）以及点「开始」时提交的建剧请求体。接口用 mock。
 */

let dom: DomEnv;
let StartBody: typeof StartBodyType;
let useModelsStore: typeof useModelsStoreType;
beforeAll(async () => {
  dom = await installDom();
  StartBody = (await import("@/pages/drama/start-dialog")).StartBody;
  useModelsStore = (await import("@/store/models")).useModelsStore;
});
afterAll(() => {
  // models store 是全局单例：清掉本文件灌进去的模型，免得后面的测试（如连线规则）读到残留
  useModelsStore.setState({ byKind: {} });
  dom.restore();
});

const pre = (patch: Partial<SourcePreflight> = {}): SourcePreflight => ({
  sourceToken: "tok",
  filename: "霜刃.docx",
  chars: 4860000,
  kindGuess: "novel",
  kindSignals: "找到 1,215 个“第N章”",
  unitKind: "chapter",
  units: [],
  unitsTotal: 1215,
  issues: [{ kind: "noise_removed", count: 312, message: "清掉 312 行网文站插入语" }],
  estimate: {
    episodesMin: 900,
    episodesMax: 1400,
    episodesExact: false,
    credits: 3200,
    minutes: 40,
  },
  ...patch,
});

const library: PromptLibrary = {
  ready: true,
  templates: [],
  mine: { plan: [], scripts: [], assets: [], asset_prompts: [], video: [], board: [] },
  skills: { plan: [], scripts: [], assets: [], asset_prompts: [], video: [], board: [] },
  save: async () => {
    throw new Error("not used");
  },
  remove: async () => {},
};

const model = (key: string, window: number) =>
  ({
    key,
    kind: "agent",
    label: key,
    pricing: {},
    capabilities: { context: { window, output: 8000 } },
  }) as never;

let get: ReturnType<typeof spyOn>;
beforeEach(() => {
  get = spyOn(service, "get").mockResolvedValue({
    genres: [{ key: "xianxia", name: "玄幻修仙" }],
    styles: [{ key: "real_city_film", name: "都市电影", prompt: "" }],
  } as never);
  useModelsStore.setState({
    byKind: {
      agent: {
        status: "ready",
        loadedAt: Date.now(),
        models: [model("big", 200_000), model("small", 64_000)],
      },
    },
  });
  dom.win.localStorage.clear();
});
afterEach(() => get.mockRestore());

const mountBody = async (
  preflight: SourcePreflight,
  onSubmit: (input: CreateDramaInput) => Promise<void> = async () => {},
) => {
  const m = await dom.mount(
    <StartBody pre={preflight} library={library} onCancel={() => {}} onSubmit={onSubmit} />,
  );
  await dom.flush();
  return m;
};

const buttons = (container: HTMLElement) => [...container.querySelectorAll("button")];
const byText = (container: HTMLElement, text: string) =>
  buttons(container).find((b) => (b.textContent ?? "").trim() === text);
const startButton = (container: HTMLElement) => byText(container, "开始") as HTMLButtonElement;

describe("开拍设置：来源与基本规则", () => {
  test("来源行：文件名、字数、单元数、识别结果、依据与处理记录", async () => {
    const m = await mountBody(pre());
    const text = m.container.textContent ?? "";
    expect(text).toContain("霜刃.docx");
    expect(text).toContain("486 万字");
    expect(text).toContain("1,215 章");
    expect(text).toContain("小说");
    expect(text).toContain("依据：找到 1,215 个“第N章”");
    expect(text).toContain("已处理 1 处");
    expect(text).toContain("清掉 312 行网文站插入语");
    await m.unmount();
  });

  test("小说：规划范围显示预估（集数、积分、耗时），画幅默认 9:16，只有三档", async () => {
    const m = await mountBody(pre());
    const text = m.container.textContent ?? "";
    expect(text).toContain("预计约 900–1,400 集");
    expect(text).toContain("3,200");
    expect(text).toContain("约 40 分钟");
    const ratios = buttons(m.container).filter((b) => b.hasAttribute("aria-pressed"));
    expect(ratios.map((b) => b.textContent)).toEqual(["9:16", "16:9", "4:3"]);
    expect(ratios[0].getAttribute("aria-pressed")).toBe("true");
    await m.unmount();
  });

  test("识别不出：胶囊显示「请选择」，开始禁用并写明原因", async () => {
    const m = await mountBody(pre({ kindGuess: "unknown" }));
    expect(byText(m.container, "请选择")).toBeTruthy();
    const start = startButton(m.container);
    expect(start.disabled).toBe(true);
    expect(start.title).toBe("先确认是剧本还是小说");
    await m.unmount();
  });

  test("剧本带「第N集」：分集规划、分集剧本行置灰、单集时长锁成「按原分集 · 80 集」、规划范围一次切完", async () => {
    const m = await mountBody(
      pre({
        kindGuess: "script",
        unitKind: "episode",
        unitsTotal: 80,
        estimate: {
          episodesMin: 80,
          episodesMax: 80,
          episodesExact: true,
          credits: 40,
          minutes: 2,
        },
      }),
    );
    const adapt = byText(m.container, "剧本直接导入") as HTMLButtonElement;
    expect(adapt.disabled).toBe(true);
    expect(adapt.title).toBe("剧本直接导入，不改编");
    const seconds = byText(m.container, "按原分集 · 80 集") as HTMLButtonElement;
    expect(seconds.disabled).toBe(true);
    expect(m.container.textContent).toContain("剧本一次切完 · 80 集");
    expect(m.container.textContent).not.toContain("规划到第");
    await m.unmount();
  });

  test("剧本没有分集标记：时长可选，只作参考；其他环节的提示词不置灰", async () => {
    const m = await mountBody(pre({ kindGuess: "script", unitKind: "scene", unitsTotal: 30 }));
    const seconds = byText(m.container, "60s/集（参考）") as HTMLButtonElement;
    expect(seconds).toBeTruthy();
    expect(seconds.disabled).toBe(false);
    const rowButton = (name: string) =>
      [...m.container.querySelectorAll("span")]
        .find((el) => el.textContent === name)
        ?.parentElement?.querySelector("button") as HTMLButtonElement;
    expect(rowButton("资产拆解").disabled).toBe(false);
    expect(rowButton("分集规划").disabled).toBe(true);
    expect(rowButton("分集剧本").disabled).toBe(true);
    await m.unmount();
  });

  test("小说可输入自定义单集时长并联动预计集数及估算参数", async () => {
    const submitted: CreateDramaInput[] = [];
    const estimate = spyOn(service, "post").mockResolvedValue({
      episodes_min: 12,
      episodes_max: 18,
      episodes_exact: false,
      credits: 20,
      minutes: 3,
    } as never);
    const m = await mountBody(pre(), async (input) => submitted.push(input));
    await dom.click(byText(m.container, "60s/集"));
    await dom.flush();
    estimate.mockClear();
    const customInput = [...dom.win.document.body.querySelectorAll("input")].find(
      (input) => input.getAttribute("aria-label") === "自定义单集时长（秒）",
    ) as HTMLInputElement;
    expect(customInput).toBeTruthy();
    await dom.type(customInput, "75");
    await dom.flush();
    expect(m.container.textContent).toContain("60s/集");
    expect(estimate).not.toHaveBeenCalled();
    await dom.click(byText(m.container, "60s/集") as HTMLButtonElement);
    await dom.flush(400);
    expect(m.container.textContent).toContain("75s/集");
    const estimateCall = estimate.mock.calls.find(
      ([path]) => path === "/drama-sources/tok/estimate",
    );
    expect(estimateCall).toBeTruthy();
    expect(estimateCall?.[1]).toMatchObject({ episode_seconds: 75 });
    expect(estimateCall?.[2]).toMatchObject({ silent: true });
    expect(m.container.textContent).toContain("预计约 12–18 集");
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(submitted[0]?.episodeSeconds).toBe(75);
    estimate.mockRestore();
    await m.unmount();
  });
});

describe("开拍设置：文本模型", () => {
  test("不按固定上下文门槛禁用：小窗口模型也能选，开始可点", async () => {
    const m = await mountBody(pre());
    await dom.click(byText(m.container, "big"));
    await dom.flush();
    const small = [...dom.win.document.body.querySelectorAll("button")].find(
      (b) => (b.textContent ?? "").startsWith("small") && b.className.includes("min-h-8"),
    ) as HTMLButtonElement;
    expect(small.disabled).toBe(false);
    expect(m.container.textContent).not.toContain("128k");
    expect(startButton(m.container).disabled).toBe(false);
    await m.unmount();
  });

  test("估价说这个模型装不下原文（67022）：写出后端点名的那一章，开始禁用", async () => {
    const { ApiError } = await import("@/utils/requests/request");
    const reason = "「第2章 大战」约 5012 字，超过这个模型一次能读的 3808 字，请换上下文更大的模型";
    const post = spyOn(service, "post").mockRejectedValue(new ApiError(reason, 67022, 400));
    dom.win.localStorage.setItem("drama-start-prefs", JSON.stringify({ textModel: "small" }));
    const m = await mountBody(pre());
    await dom.flush(400);
    expect(m.container.textContent).toContain(reason);
    expect(startButton(m.container).disabled).toBe(true);
    post.mockRestore();
    await m.unmount();
  });
});

describe("开拍设置：侧滑到提示词编辑器", () => {
  test("编辑器打开时开拍设置那页收起，弹窗高度跟着编辑器走；返回后恢复", async () => {
    const m = await mountBody(pre());
    const settingsPane = () =>
      [...m.container.querySelectorAll("h2")].find((h) => h.textContent === "开拍设置")!
        .parentElement as HTMLElement;
    expect(settingsPane().className).not.toContain("max-h-0");
    const chip = [...m.container.querySelectorAll("span")]
      .find((el) => el.textContent === "视频提示词")
      ?.parentElement?.querySelector("button") as HTMLButtonElement;
    await dom.click(chip);
    await dom.flush();
    await dom.click(
      [...dom.win.document.body.querySelectorAll("button")].find(
        (b) => (b.textContent ?? "").trim() === "新建",
      ),
    );
    await dom.flush();
    const editor = m.container.querySelector('[data-slot="prompt-editor"]') as HTMLElement;
    expect(editor).toBeTruthy();
    expect(settingsPane().className).toContain("max-h-0");
    expect(editor.parentElement?.className).not.toContain("max-h-0");
    await dom.click(m.container.querySelector('[data-slot="prompt-editor"] button'));
    await dom.flush();
    expect(settingsPane().className).not.toContain("max-h-0");
    await m.unmount();
  });
});

describe("开拍设置：点「开始」建剧", () => {
  test("小说：请求体带 token、类型、范围（全部 = 0）、画幅、画风、时长、四环节提示词", async () => {
    const calls: CreateDramaInput[] = [];
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    await dom.click(byText(m.container, "16:9"));
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({
      sourceToken: "tok",
      sourceKind: "novel",
      rangeEnd: 0,
      ratio: "16:9",
      styleKey: "real_city_film",
      episodeSeconds: 60,
      textModel: "",
      prompts: {
        plan: { source: "builtin" },
        scripts: { source: "builtin" },
        assets: { source: "builtin" },
        asset_prompts: { source: "builtin" },
        video: { source: "builtin" },
        board: { source: "builtin" },
      },
    });
    await m.unmount();
  });

  test("建剧成功后记住画幅；下次弹窗预填", async () => {
    const m = await mountBody(pre());
    await dom.click(byText(m.container, "4:3"));
    await dom.click(startButton(m.container));
    await dom.flush();
    await m.unmount();
    const again = await mountBody(pre());
    const ratios = buttons(again.container).filter((b) => b.hasAttribute("aria-pressed"));
    expect(ratios.find((b) => b.textContent === "4:3")?.getAttribute("aria-pressed")).toBe("true");
    await again.unmount();
  });

  test("建剧失败：错误就地显示在弹窗里，开始恢复可点", async () => {
    const { ApiError } = await import("@/utils/requests/request");
    const m = await mountBody(pre(), async () => {
      throw new ApiError("", 67022, 400);
    });
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(m.container.querySelector('[role="alert"]')?.textContent).toContain("模型上下文不足");
    expect(startButton(m.container).disabled).toBe(false);
    await m.unmount();
  });

  test("剧本型：提交时分集规划、分集剧本强制内置，规划范围恒为全部", async () => {
    const calls: CreateDramaInput[] = [];
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({
        prompts: { plan: { source: "mine", id: "1f" }, scripts: { source: "mine", id: "2f" } },
      }),
    );
    const m = await mountBody(
      pre({ kindGuess: "script", unitKind: "episode", unitsTotal: 80 }),
      async (input) => {
        calls.push(input);
      },
    );
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].sourceKind).toBe("script");
    expect(calls[0].rangeEnd).toBe(0);
    expect(calls[0].prompts?.plan).toEqual({ source: "builtin" });
    expect(calls[0].prompts?.scripts).toEqual({ source: "builtin" });
    await m.unmount();
  });
});

describe("开拍设置：视频 / 图片模型", () => {
  const videoModel = (key: string, withImageRef: boolean) =>
    ({
      key,
      kind: "video",
      label: key,
      pricing: {},
      capabilities: {
        ops: withImageRef ? ["t2v", "i2v"] : ["t2v"],
        refs: { image: { on: withImageRef, max: withImageRef ? 9 : 0, max_mb: 10 } },
        params: { duration: { type: "number", label: "时长", open: true, min: 4, max: 15 } },
      },
    }) as never;
  const imageModel = (key: string) =>
    ({ key, kind: "image", label: key, pricing: {}, capabilities: {} }) as never;

  beforeEach(() => {
    const byKind = useModelsStore.getState().byKind;
    useModelsStore.setState({
      byKind: {
        ...byKind,
        video: {
          status: "ready",
          loadedAt: Date.now(),
          models: [videoModel("vid-a", true), videoModel("vid-text", false)],
        },
        image: {
          status: "ready",
          loadedAt: Date.now(),
          models: [imageModel("img-a"), imageModel("img-b")],
        },
      },
    });
  });

  const menuRows = () =>
    [...dom.win.document.body.querySelectorAll("button")].filter((b) =>
      b.className.includes("min-h-10"),
    ) as HTMLButtonElement[];

  const pickFromPopover = async (trigger: HTMLElement | undefined, name: string) => {
    await dom.click(trigger);
    await dom.flush();
    await dom.click(menuRows().find((b) => (b.textContent ?? "").startsWith(name)));
    await dom.flush();
  };

  test("图片模型跟随默认：胶囊显示默认模型，下拉里仍是「跟随默认」打勾", async () => {
    const m = await mountBody(pre());
    await dom.click(byText(m.container, "人物 · img-a"));
    await dom.flush();
    const active = (b: HTMLElement | undefined) => b?.className.split(/\s+/).includes("bg-muted");
    expect(active(menuRows().find((b) => (b.textContent ?? "").trim() === "跟随默认"))).toBe(true);
    expect(active(menuRows().find((b) => (b.textContent ?? "").startsWith("img-a")))).toBe(false);
    await m.unmount();
  });

  test("视频模型跟随默认：胶囊显示第一个支持参考图的模型，下拉里仍是「跟随默认」打勾", async () => {
    const byKind = useModelsStore.getState().byKind;
    useModelsStore.setState({
      byKind: {
        ...byKind,
        video: {
          status: "ready",
          loadedAt: Date.now(),
          models: [videoModel("vid-text", false), videoModel("vid-a", true)],
        },
      },
    });
    const m = await mountBody(pre());
    expect(byText(m.container, "vid-text")).toBeUndefined();
    await dom.click(byText(m.container, "vid-a"));
    await dom.flush();
    const active = (b: HTMLElement | undefined) => b?.className.split(/\s+/).includes("bg-muted");
    expect(active(menuRows().find((b) => (b.textContent ?? "").trim() === "跟随默认"))).toBe(true);
    expect(active(menuRows().find((b) => (b.textContent ?? "").startsWith("vid-a")))).toBe(false);
    await m.unmount();
  });

  test("文本模型下面依次是视频模型、资产图片（人物 / 场景 / 物品）", async () => {
    const m = await mountBody(pre());
    const text = m.container.textContent ?? "";
    expect(text.indexOf("文本模型")).toBeLessThan(text.indexOf("视频模型"));
    expect(text.indexOf("视频模型")).toBeLessThan(text.indexOf("资产图片"));
    // 跟随默认时胶囊上直接显示默认模型（清单第一个）
    expect(byText(m.container, "人物 · img-a")).toBeTruthy();
    expect(byText(m.container, "场景 · img-a")).toBeTruthy();
    expect(byText(m.container, "物品 · img-a")).toBeTruthy();
    await m.unmount();
  });

  test("选视频模型和图片模型后点开始：请求体带上它们", async () => {
    const calls: CreateDramaInput[] = [];
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({ styleKey: "real_city_film" }),
    );
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    await pickFromPopover(byText(m.container, "vid-a"), "vid-a");
    await pickFromPopover(byText(m.container, "人物 · img-a"), "img-a");
    await pickFromPopover(byText(m.container, "物品 · img-a"), "img-b");
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].videoModel).toBe("vid-a");
    expect(calls[0].imageModels).toMatchObject({ character: "img-a", prop: "img-b" });
    expect(calls[0].imageModels?.scene).toBeUndefined();
    await m.unmount();
  });

  test("不支持参考图的视频模型置灰并写原因，可用的副文案带时长上限", async () => {
    const m = await mountBody(pre());
    await dom.click(byText(m.container, "vid-a"));
    await dom.flush();
    const rows = menuRows();
    const ok = rows.find((b) => (b.textContent ?? "").startsWith("vid-a"))!;
    const bad = rows.find((b) => (b.textContent ?? "").startsWith("vid-text"))!;
    expect(ok.disabled).toBe(false);
    expect(ok.textContent).toContain("≤15s");
    expect(bad.disabled).toBe(true);
    expect(bad.textContent).toContain("不支持参考图");
    expect(bad.title).toContain("不支持参考图");
    await m.unmount();
  });

  test("记住的模型已下线：读取时回落为不指定，提交不带它", async () => {
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({
        styleKey: "real_city_film",
        videoModel: "gone",
        imageModels: { character: "gone", scene: "img-a" },
      }),
    );
    const calls: CreateDramaInput[] = [];
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    expect(byText(m.container, "人物 · img-a")).toBeTruthy();
    expect(byText(m.container, "场景 · img-a")).toBeTruthy();
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].videoModel).toBeUndefined();
    expect(calls[0].imageModels?.character).toBeUndefined();
    expect(calls[0].imageModels?.scene).toBe("img-a");
    await m.unmount();
  });
});

describe("开拍设置：资产图片的比例与分辨率", () => {
  const enumParam = (label: string, options: string[]) => ({
    type: "enum",
    label,
    open: true,
    options,
    default: options[0],
  });
  const imageModel = (key: string, ratios: string[], resolutions?: string[]) =>
    ({
      key,
      kind: "image",
      label: key,
      pricing: {},
      capabilities: {
        params: {
          aspect_ratio: enumParam("比例", ratios),
          ...(resolutions ? { resolution: enumParam("分辨率", resolutions) } : {}),
        },
      },
    }) as never;

  beforeEach(() => {
    const byKind = useModelsStore.getState().byKind;
    useModelsStore.setState({
      byKind: {
        ...byKind,
        image: {
          status: "ready",
          loadedAt: Date.now(),
          models: [
            imageModel("wide", ["1:1", "16:9", "21:9"], ["1K", "2K", "4K"]),
            imageModel("narrow", ["1:1", "3:2", "2:3"]),
          ],
        },
      },
    });
  });

  const paramChip = (container: HTMLElement, index: number) =>
    buttons(container).filter((b) => b.getAttribute("aria-label") === "图片参数")[index];

  test("跟随默认模型：比例按类型默认、分辨率用模型默认，开始时一起提交", async () => {
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({ styleKey: "real_city_film" }),
    );
    const calls: CreateDramaInput[] = [];
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    expect(paramChip(m.container, 0).textContent).toContain("16:9 · 1K");
    expect(paramChip(m.container, 1).textContent).toContain("21:9 · 1K");
    expect(paramChip(m.container, 2).textContent).toContain("16:9 · 1K");
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].imageParams).toEqual({
      character: { aspect_ratio: "16:9", resolution: "1K" },
      scene: { aspect_ratio: "21:9", resolution: "1K" },
      prop: { aspect_ratio: "16:9", resolution: "1K" },
    });
    await m.unmount();
  });

  test("模型没有默认比例时选中最接近的；在浮层里改比例、分辨率后提交改过的值", async () => {
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({ styleKey: "real_city_film", imageModels: { scene: "narrow" } }),
    );
    const calls: CreateDramaInput[] = [];
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    // narrow 没有 21:9 也没有分辨率：选中最接近的 3:2
    expect(paramChip(m.container, 1).textContent?.trim()).toBe("3:2");

    await dom.click(paramChip(m.container, 0));
    await dom.flush();
    const radios = [...dom.win.document.body.querySelectorAll("[role='radio']")] as HTMLElement[];
    const ratio21 = radios.find((r) => r.textContent === "21:9");
    expect(radios.find((r) => r.textContent === "16:9")?.getAttribute("aria-checked")).toBe("true");
    await dom.click(ratio21);
    await dom.click(radios.find((r) => r.textContent === "4K"));
    await dom.flush();
    expect(paramChip(m.container, 0).textContent).toContain("21:9 · 4K");

    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].imageParams?.character).toEqual({ aspect_ratio: "21:9", resolution: "4K" });
    expect(calls[0].imageParams?.scene).toEqual({ aspect_ratio: "3:2" });
    await m.unmount();
  });
});

describe("开拍设置：弹窗开关", () => {
  test("草稿过期时关弹窗并清掉预检：退出动画期间弹窗里仍是原来的内容", async () => {
    const { StartDialog } = await import("@/pages/drama/start-dialog");
    const { useState } = await import("react");
    const Host = () => {
      const [state, setState] = useState<{ open: boolean; pre: SourcePreflight | null }>({
        open: true,
        pre: pre(),
      });
      return (
        <>
          <button type="button" onClick={() => setState({ open: false, pre: null })}>
            过期关闭
          </button>
          <StartDialog
            open={state.open}
            onOpenChange={(open) => setState((s) => ({ ...s, open }))}
            pre={state.pre}
            onSubmit={async () => {}}
          />
        </>
      );
    };
    const release = holdExitAnimations(dom);
    try {
      const m = await dom.mount(<Host />);
      await dom.flush();
      await dom.click(byText(m.container, "过期关闭"));
      const popup = dom.win.document.body.querySelector('[data-slot="dialog-content"]');
      expect(popup?.hasAttribute("data-ending-style")).toBe(true);
      expect(popup?.textContent).toContain("霜刃.docx");
      await m.unmount();
    } finally {
      release();
    }
  });
});
