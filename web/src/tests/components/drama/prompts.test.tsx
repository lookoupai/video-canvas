import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import type { MyPrompt, PromptChoice, PromptStep } from "@/api/drama/type";
import type { PromptLibrary } from "@/components/drama/prompts/use-prompt-library";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import { builtinPrompts } from "@/utils/drama/start-settings";
import type { PromptEditorPane as PromptEditorPaneType } from "@/components/drama/prompts/prompt-editor";
import type { PromptRows as PromptRowsType } from "@/components/drama/prompts/prompt-rows";

/**
 * 我的提示词：编辑器（新建 / 编辑 / 恢复内置 / 校验）与四行下拉（选择、新建、编辑、删除、剧本型置灰）。
 */

let dom: DomEnv;
let PromptEditorPane: typeof PromptEditorPaneType;
let PromptRows: typeof PromptRowsType;
beforeAll(async () => {
  dom = await installDom();
  PromptEditorPane = (await import("@/components/drama/prompts/prompt-editor")).PromptEditorPane;
  PromptRows = (await import("@/components/drama/prompts/prompt-rows")).PromptRows;
});
afterAll(async () => {
  // Tiptap 销毁编辑器是异步的：等它跑完再还原全局，不然会在没有 document 时抛错
  await dom.flush(50);
  dom.restore();
});

/** 故事板的内置模板（占位符是整体的块，不是文字） */
const BOARD_BUILTIN =
  "生成 {{格数}} 格分镜图，{{列数}} 列 {{行数}} 行，比例 {{比例}}\n{{画面}}\n无字幕";

const mineItem = (patch: Partial<MyPrompt> = {}): MyPrompt => ({
  id: "1f",
  step: "plan",
  name: "我的改编",
  bodies: { plan: "改过的规划" },
  revision: 3,
  updatedAt: "t",
  ...patch,
});

const makeLibrary = (items: MyPrompt[] = []) => {
  const saved: Parameters<PromptLibrary["save"]>[0][] = [];
  const removed: string[] = [];
  const library: PromptLibrary = {
    ready: true,
    templates: [
      { step: "plan", pages: [{ key: "plan", label: "分集规划", body: "内置规划" }] },
      { step: "scripts", pages: [{ key: "scripts", label: "分集剧本", body: "内置剧本" }] },
      { step: "video", pages: [{ key: "storyboard", label: "视频提示词", body: "内置视频" }] },
      { step: "board", pages: [{ key: "board", label: "故事板提示词", body: BOARD_BUILTIN }] },
    ],
    mine: {
      plan: items.filter((i) => i.step === "plan"),
      scripts: items.filter((i) => i.step === "scripts"),
      assets: [],
      asset_prompts: [],
      video: items.filter((i) => i.step === "video"),
      board: items.filter((i) => i.step === "board"),
    },
    skills: {
      plan: [],
      scripts: [],
      assets: [{ name: "cross", title: "跨集连续性", description: "保持角色一致" }],
      asset_prompts: [],
      video: [],
      board: [],
    },
    save: async (draft) => {
      saved.push(draft);
      return {
        id: draft.id ?? "new1",
        step: draft.step,
        name: draft.name.trim(),
        bodies: draft.bodies,
        revision: (draft.revision ?? 0) + 1,
        updatedAt: "t",
      };
    },
    remove: async (item) => {
      removed.push(item.id);
    },
  };
  return { library, saved, removed };
};

const textarea = (c: HTMLElement) => c.querySelector("textarea") as HTMLTextAreaElement;
const nameInput = (c: HTMLElement) =>
  c.querySelector('input[aria-label="名称"]') as HTMLInputElement;
const byText = (c: HTMLElement, text: string) =>
  [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);

describe("提示词编辑器", () => {
  test("新建：正文预填内置，不展示「输入变量」「输出结构」，右下角有字数", async () => {
    const { library } = makeLibrary();
    const m = await dom.mount(
      <PromptEditorPane step="video" item={null} library={library} onClose={() => {}} />,
    );
    expect(textarea(m.container).value).toBe("内置视频");
    const text = m.container.textContent ?? "";
    expect(text).toContain("视频提示词 · 新建");
    expect(text).not.toContain("输入变量");
    expect(text).not.toContain("输出结构");
    expect(text).toContain("4 / 20,000");
    await m.unmount();
  });

  test("分集规划、分集剧本是两个环节：各自只有一页，没有页签", async () => {
    const { library, saved } = makeLibrary();
    const m = await dom.mount(
      <PromptEditorPane step="scripts" item={null} library={library} onClose={() => {}} />,
    );
    expect(m.container.querySelectorAll('[role="tab"]').length).toBe(0);
    expect(m.container.textContent).toContain("分集剧本 · 新建");
    expect(textarea(m.container).value).toBe("内置剧本");
    await dom.type(nameInput(m.container), "我的改编");
    await dom.click(byText(m.container, "保存"));
    expect(saved[0]).toMatchObject({ step: "scripts", bodies: { scripts: "内置剧本" } });
    await m.unmount();
  });

  test("保存：名称为空就地报错，不调接口；填好后保存并把条目交给调用方", async () => {
    const { library, saved } = makeLibrary();
    const closed: (MyPrompt | null)[] = [];
    const m = await dom.mount(
      <PromptEditorPane
        step="video"
        item={null}
        library={library}
        onClose={(x) => closed.push(x)}
      />,
    );
    await dom.click(byText(m.container, "保存"));
    expect(m.container.querySelector('[role="alert"]')?.textContent).toBe("请填写名称");
    expect(saved).toHaveLength(0);
    await dom.type(nameInput(m.container), " 古风视频 ");
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved).toHaveLength(1);
    expect(closed[0]).toMatchObject({ id: "new1", name: "古风视频", step: "video" });
    await m.unmount();
  });

  test("正文清空或同名：校验报错；返回按钮传 null", async () => {
    const { library } = makeLibrary([mineItem({ id: "9", step: "video", name: "已有" })]);
    const closed: (MyPrompt | null)[] = [];
    const m = await dom.mount(
      <PromptEditorPane
        step="video"
        item={null}
        library={library}
        onClose={(x) => closed.push(x)}
      />,
    );
    await dom.type(nameInput(m.container), "已有");
    await dom.click(byText(m.container, "保存"));
    expect(m.container.querySelector('[role="alert"]')?.textContent).toBe(
      "这个环节里已有同名提示词",
    );
    await dom.type(nameInput(m.container), "新的");
    await dom.type(textarea(m.container), "   ");
    await dom.click(byText(m.container, "保存"));
    expect(m.container.querySelector('[role="alert"]')?.textContent).toBe("正文不能为空");
    await dom.click(m.container.querySelector('[aria-label="返回"]'));
    expect(closed).toEqual([null]);
    await m.unmount();
  });

  test("编辑：预填名称与正文；恢复内置只恢复当前页；保存带 id 与 revision", async () => {
    const item = mineItem();
    const { library, saved } = makeLibrary([item]);
    const m = await dom.mount(
      <PromptEditorPane step="plan" item={item} library={library} onClose={() => {}} />,
    );
    expect(m.container.textContent).toContain("分集规划 · 编辑");
    expect(nameInput(m.container).value).toBe("我的改编");
    expect(textarea(m.container).value).toBe("改过的规划");
    await dom.click(byText(m.container, "恢复内置"));
    expect(textarea(m.container).value).toBe("内置规划");
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved[0]).toMatchObject({
      id: "1f",
      revision: 3,
      bodies: { plan: "内置规划" },
    });
    await m.unmount();
  });
});

describe("故事板提示词编辑器", () => {
  const chipNames = (c: HTMLElement) =>
    [...c.querySelectorAll("[data-placeholder]")].map((el) => el.getAttribute("data-placeholder"));

  test("新建：预填内置模板，占位符是整体的块，上面有插入按钮；不是文本框", async () => {
    const { library, saved } = makeLibrary();
    const m = await dom.mount(
      <PromptEditorPane step="board" item={null} library={library} onClose={() => {}} />,
    );
    await dom.flush();
    expect(m.container.querySelector("textarea")).toBeNull();
    expect(chipNames(m.container)).toEqual(["格数", "列数", "行数", "比例", "画面"]);
    expect(m.container.querySelectorAll("[data-slot=placeholder-toolbar] button")).toHaveLength(5);
    expect(m.container.textContent).toContain("故事板提示词 · 新建");
    // 原样保存：文本和内置一字不差
    await dom.type(nameInput(m.container), "照抄");
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved[0]).toMatchObject({ step: "board", bodies: { board: BOARD_BUILTIN } });
    await m.unmount();
  });

  test("有不认识的占位符：保存就地报错并点名，不调接口", async () => {
    const item = mineItem({
      id: "5",
      step: "board",
      name: "写错了",
      bodies: { board: "{{画面}} {{颜色}}" },
    });
    const { library, saved } = makeLibrary([item]);
    const m = await dom.mount(
      <PromptEditorPane step="board" item={item} library={library} onClose={() => {}} />,
    );
    await dom.flush();
    await dom.click(byText(m.container, "保存"));
    expect(m.container.querySelector('[role="alert"]')?.textContent).toContain(
      "不认识的占位符 {{颜色}}",
    );
    expect(saved).toHaveLength(0);
    await m.unmount();
  });

  test("恢复内置：换回内置模板，占位符重新成块，错误随之消失后能保存", async () => {
    const item = mineItem({
      id: "5",
      step: "board",
      name: "写错了",
      bodies: { board: "{{画面}} {{颜色}}" },
    });
    const { library, saved } = makeLibrary([item]);
    const m = await dom.mount(
      <PromptEditorPane step="board" item={item} library={library} onClose={() => {}} />,
    );
    await dom.flush();
    expect(chipNames(m.container)).toEqual(["画面"]);
    await dom.click(byText(m.container, "恢复内置"));
    await dom.flush();
    expect(chipNames(m.container)).toEqual(["格数", "列数", "行数", "比例", "画面"]);
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved[0]).toMatchObject({ id: "5", bodies: { board: BOARD_BUILTIN } });
    await m.unmount();
  });
});

describe("四行提示词下拉", () => {
  const mount = async (
    items: MyPrompt[],
    over: {
      kind?: "script" | "novel" | null;
      value?: Record<PromptStep, PromptChoice>;
    } = {},
  ) => {
    const lib = makeLibrary(items);
    const calls = {
      change: [] as [PromptStep, PromptChoice][],
      edit: [] as [PromptStep, MyPrompt | null][],
    };
    const m = await dom.mount(
      <PromptRows
        kind={over.kind ?? "novel"}
        value={over.value ?? builtinPrompts()}
        library={lib.library}
        onChange={(step, choice) => calls.change.push([step, choice])}
        onEdit={(step, item) => calls.edit.push([step, item])}
      />,
    );
    return { ...lib, ...calls, m };
  };
  const rowChip = (c: HTMLElement, name: string) =>
    [...c.querySelectorAll("span")]
      .find((el) => el.textContent === name)
      ?.parentElement?.querySelector("button") as HTMLButtonElement;
  const popupButton = (text: string) =>
    [...dom.win.document.body.querySelectorAll("button")].find((b) =>
      (b.textContent ?? "").includes(text),
    ) as HTMLButtonElement | undefined;

  test("六行：分集规划 / 分集剧本 / 资产拆解 / 资产图提示词 / 视频提示词 / 故事板提示词，默认都是「内置」", async () => {
    const { m } = await mount([]);
    const names = [...m.container.querySelectorAll('[data-slot="prompt-rows"] > div > span')].map(
      (el) => el.textContent,
    );
    expect(names).toEqual([
      "分集规划",
      "分集剧本",
      "资产拆解",
      "资产图提示词",
      "视频提示词",
      "故事板提示词",
    ]);
    expect(rowChip(m.container, "故事板提示词").textContent).toBe("内置");
    expect(rowChip(m.container, "视频提示词").textContent).toBe("内置");
    await m.unmount();
  });

  test("剧本型输入：分集规划、分集剧本都置灰并写原因", async () => {
    const { m } = await mount([], { kind: "script" });
    for (const name of ["分集规划", "分集剧本"]) {
      const chip = rowChip(m.container, name);
      expect(chip.disabled).toBe(true);
      expect(chip.title).toBe("剧本直接导入，不改编");
      expect(chip.textContent).toBe("剧本直接导入");
    }
    await m.unmount();
  });

  test("下拉：内置（推荐）、我的提示词（含新建）、技能（没有时写「暂无适用技能」）", async () => {
    const { m, change, edit } = await mount([
      mineItem({ id: "7", step: "video", name: "古风视频" }),
    ]);
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    const body = dom.win.document.body.textContent ?? "";
    expect(body).toContain("推荐");
    expect(body).toContain("古风视频");
    expect(body).toContain("暂无适用技能");
    await dom.click(popupButton("古风视频"));
    expect(change).toEqual([["video", { source: "mine", id: "7" }]]);
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    await dom.click(popupButton("新建"));
    expect(edit).toEqual([["video", null]]);
    await m.unmount();
  });

  test("分集剧本行的下拉也能选技能", async () => {
    const { library, saved } = makeLibrary();
    library.skills.scripts = [{ name: "narrate", title: "解说文案", description: "第一人称" }];
    const calls: [PromptStep, PromptChoice][] = [];
    const m = await dom.mount(
      <PromptRows
        kind="novel"
        value={builtinPrompts()}
        library={library}
        onChange={(step, choice) => calls.push([step, choice])}
        onEdit={() => {}}
      />,
    );
    await dom.click(rowChip(m.container, "分集剧本"));
    await dom.flush();
    await dom.click(popupButton("解说文案"));
    expect(calls).toEqual([["scripts", { source: "skill", skill: "narrate" }]]);
    expect(saved).toEqual([]);
    await m.unmount();
  });

  test("下拉：编辑进入编辑器；有技能的环节列出技能并可选", async () => {
    const item = mineItem({ id: "7", step: "video", name: "古风视频" });
    const { m, change, edit } = await mount([item]);
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    await dom.click(dom.win.document.body.querySelector('[aria-label="编辑 古风视频"]'));
    expect(edit).toEqual([["video", item]]);
    await dom.click(rowChip(m.container, "资产拆解"));
    await dom.flush();
    expect(dom.win.document.body.textContent).toContain("保持角色一致");
    await dom.click(popupButton("跨集连续性"));
    expect(change.at(-1)).toEqual(["assets", { source: "skill", skill: "cross" }]);
    await m.unmount();
  });

  test("删除我的提示词：调库删除；正在用它的行回落为内置", async () => {
    const item = mineItem({ id: "7", step: "video", name: "古风视频" });
    const { m, removed, change } = await mount([item], {
      value: { ...builtinPrompts(), video: { source: "mine", id: "7" } },
    });
    expect(rowChip(m.container, "视频提示词").textContent).toBe("我的 · 古风视频");
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    await dom.click(dom.win.document.body.querySelector('[aria-label="删除 古风视频"]'));
    await dom.flush();
    expect(removed).toEqual(["7"]);
    expect(change).toEqual([["video", { source: "builtin" }]]);
    await m.unmount();
  });
});
