import { describe, expect, test } from "bun:test";

import type { PromptTemplate } from "@/api/drama/type";
import {
  PROMPT_BODY_MAX,
  PROMPT_NAME_MAX,
  PROMPT_STEPS,
  adaptLockedReason,
  choiceLabel,
  draftFromMine,
  draftFromTemplate,
  promptRowState,
  restoreBuiltin,
  sameChoice,
  validatePromptDraft,
} from "@/utils/drama/prompts";

/** 编辑器的多页逻辑是通用的：现在每个环节只有一页，这里用一个双页模板测它 */
const adapt: PromptTemplate = {
  step: "plan",
  pages: [
    { key: "plan", label: "分集规划", body: "内置规划" },
    { key: "scripts", label: "分集剧本", body: "内置剧本" },
  ],
};
const video: PromptTemplate = {
  step: "video",
  pages: [{ key: "storyboard", label: "视频提示词", body: "内置视频" }],
};

describe("提示词环节", () => {
  test("六个环节：分集规划 / 分集剧本 / 资产拆解 / 资产图提示词 / 视频提示词 / 故事板提示词", () => {
    expect(PROMPT_STEPS.map((s) => [s.key, s.name])).toEqual([
      ["plan", "分集规划"],
      ["scripts", "分集剧本"],
      ["assets", "资产拆解"],
      ["asset_prompts", "资产图提示词"],
      ["video", "视频提示词"],
      ["board", "故事板提示词"],
    ]);
  });

  test("剧本型输入：分集规划、分集剧本两行都置灰，原因是「剧本直接导入，不改编」", () => {
    expect(adaptLockedReason("script")).toBe("剧本直接导入，不改编");
    expect(adaptLockedReason("novel")).toBeNull();
    expect(adaptLockedReason(null)).toBeNull();
    for (const step of ["plan", "scripts"] as const) {
      expect(promptRowState(step, "script", { source: "mine", id: "1" }, [], [])).toEqual({
        label: "剧本直接导入",
        disabledReason: "剧本直接导入，不改编",
        missing: false,
      });
    }
    expect(promptRowState("assets", "script", { source: "builtin" }, [], []).disabledReason).toBe(
      null,
    );
  });

  test("当前选择的展示名：内置 / 我的 · 名称 / 技能 · 标题，来源丢失时标「已删除 / 已停用」", () => {
    const mine = [{ id: "1f", name: "古风资产" }];
    const skills = [{ name: "cross", title: "跨集连续性" }];
    expect(choiceLabel({ source: "builtin" }, mine, skills)).toEqual({
      text: "内置",
      missing: false,
    });
    expect(choiceLabel({ source: "mine", id: "1f" }, mine, skills).text).toBe("我的 · 古风资产");
    expect(choiceLabel({ source: "skill", skill: "cross" }, mine, skills).text).toBe(
      "技能 · 跨集连续性",
    );
    expect(choiceLabel({ source: "mine", id: "x" }, mine, skills)).toEqual({
      text: "我的 · 已删除",
      missing: true,
    });
    expect(choiceLabel({ source: "skill", skill: "x" }, mine, skills)).toEqual({
      text: "技能 · 已停用",
      missing: true,
    });
  });

  test("sameChoice：来源 + id / 技能名相同才算同一个", () => {
    expect(sameChoice({ source: "builtin" }, { source: "builtin" })).toBe(true);
    expect(sameChoice({ source: "mine", id: "1" }, { source: "mine", id: "1" })).toBe(true);
    expect(sameChoice({ source: "mine", id: "1" }, { source: "mine", id: "2" })).toBe(false);
    expect(sameChoice({ source: "skill", skill: "a" }, { source: "builtin" })).toBe(false);
  });
});

describe("提示词编辑器草稿", () => {
  test("新建：名称为空，正文预填内置；多页模板按页预填", () => {
    const draft = draftFromTemplate(adapt);
    expect(draft).toMatchObject({
      step: "plan",
      id: null,
      name: "",
      page: "plan",
      bodies: { plan: "内置规划", scripts: "内置剧本" },
    });
    expect(draft.pages.map((p) => p.label)).toEqual(["分集规划", "分集剧本"]);
    expect(draftFromTemplate(video).page).toBe("storyboard");
  });

  test("编辑：用条目的名称和正文，缺的页用内置补齐", () => {
    const draft = draftFromMine(adapt, {
      id: "1f",
      step: "plan",
      name: "我的改编",
      bodies: { plan: "改过的规划" },
      revision: 3,
      updatedAt: "t",
    });
    expect(draft).toMatchObject({
      id: "1f",
      revision: 3,
      name: "我的改编",
      bodies: { plan: "改过的规划", scripts: "内置剧本" },
    });
  });

  test("恢复内置：只恢复当前页，别的页保留用户改动", () => {
    const draft = { ...draftFromTemplate(adapt), bodies: { plan: "乱改", scripts: "也改了" } };
    expect(restoreBuiltin(draft, adapt, "plan").bodies).toEqual({
      plan: "内置规划",
      scripts: "也改了",
    });
  });

  test("校验：名称必填、≤ 40 字、同环节不重名；正文不能空、不超 20,000 字", () => {
    const base = draftFromTemplate(video);
    const names = [{ id: "x", name: "已有" }];
    expect(validatePromptDraft({ ...base, name: " " }, names)).toBe("请填写名称");
    expect(
      validatePromptDraft({ ...base, name: "字".repeat(PROMPT_NAME_MAX + 1) }, names),
    ).toContain("40");
    expect(validatePromptDraft({ ...base, name: "已有" }, names)).toBe("这个环节里已有同名提示词");
    expect(
      validatePromptDraft({ ...base, name: "新", bodies: { storyboard: "  " } }, names),
    ).toContain("不能为空");
    expect(
      validatePromptDraft(
        { ...base, name: "新", bodies: { storyboard: "字".repeat(PROMPT_BODY_MAX + 1) } },
        names,
      ),
    ).toContain("20,000");
    expect(validatePromptDraft({ ...base, name: "新" }, names)).toBeNull();
  });

  test("故事板提示词：正文必须保留 {{画面}}，其他环节没有这条", () => {
    const board = draftFromTemplate({
      step: "board",
      pages: [{ key: "board", label: "故事板提示词", body: "内置 {{画面}}" }],
    });
    expect(validatePromptDraft({ ...board, name: "新" }, [])).toBeNull();
    expect(
      validatePromptDraft({ ...board, name: "新", bodies: { board: "水彩风宫格图" } }, []),
    ).toBe("正文必须保留 {{画面}}，镜头内容会放在这里");
    expect(validatePromptDraft({ ...draftFromTemplate(video), name: "新" }, [])).toBeNull();
  });

  test("故事板提示词：写了不认识的占位符、花括号没成对都不能保存；其他环节不查", () => {
    const board = draftFromTemplate({
      step: "board",
      pages: [{ key: "board", label: "故事板提示词", body: "{{画面}}" }],
    });
    const check = (body: string) =>
      validatePromptDraft({ ...board, name: "新", bodies: { board: body } }, []);
    expect(check("{{格数}} 格\n{{画面}}")).toBeNull();
    expect(check("{{画面}} {{颜色}}")).toContain("不认识的占位符 {{颜色}}");
    expect(check("{{画面}} {{")).toBe("占位符没写完整，花括号要成对写成 {{名字}}");
    const other = { ...draftFromTemplate(video), name: "新", bodies: { storyboard: "{{颜色}}" } };
    expect(validatePromptDraft(other, [])).toBeNull();
  });

  test("编辑自己时同名不算重复；报错带上是哪一页", () => {
    const draft = draftFromMine(adapt, {
      id: "1f",
      step: "plan",
      name: "我的改编",
      bodies: { plan: "p", scripts: "" },
      revision: 1,
      updatedAt: "t",
    });
    // scripts 为空串：不是缺页（缺页用内置补），而是用户清空了
    expect(validatePromptDraft(draft, [{ id: "1f", name: "我的改编" }])).toBe(
      "「分集剧本」正文不能为空",
    );
    expect(
      validatePromptDraft({ ...draft, bodies: { plan: "p", scripts: "s" } }, [
        { id: "1f", name: "我的改编" },
      ]),
    ).toBeNull();
  });
});
