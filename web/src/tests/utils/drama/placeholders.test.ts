import { describe, expect, test } from "bun:test";

import {
  BOARD_PLACEHOLDERS,
  checkBoardTemplate,
  docToText,
  formatPlaceholder,
  PLACEHOLDER_NODE,
  textToDoc,
} from "@/utils/drama/placeholders";

describe("故事板模板的占位符", () => {
  test("认五个：格数 / 列数 / 行数 / 比例 / 画面，写成双花括号", () => {
    expect([...BOARD_PLACEHOLDERS]).toEqual(["格数", "列数", "行数", "比例", "画面"]);
    expect(formatPlaceholder("格数")).toBe("{{格数}}");
  });

  test("检查：全用上、单个花括号都合格", () => {
    expect(
      checkBoardTemplate("{{格数}}{{列数}}{{行数}}{{比例}}\n{{画面}} { 单个 } 花括号"),
    ).toBeNull();
  });

  test("检查：不认识的占位符报错并点名，列出能用的", () => {
    const message = checkBoardTemplate("{{画面}} {{颜色}}");
    expect(message).toContain("不认识的占位符 {{颜色}}");
    expect(message).toContain("{{格数}}、{{列数}}、{{行数}}、{{比例}}、{{画面}}");
    // 多一个空格、空名字也算不认识
    expect(checkBoardTemplate("{{画面}} {{格数 }}")).toContain("{{格数 }}");
    expect(checkBoardTemplate("{{画面}} {{}}")).toContain("不认识的占位符 {{}}");
  });

  test("检查：花括号没配成对报错", () => {
    for (const body of ["{{画面}} {{", "{{画面}} }}", "{{画面}} {{格数}", "{{画面}} {格数}}"]) {
      expect(checkBoardTemplate(body)).toBe("占位符没写完整，花括号要成对写成 {{名字}}");
    }
  });

  test("检查：必须保留 {{画面}}", () => {
    expect(checkBoardTemplate("只有 {{格数}} 格")).toBe(
      "正文必须保留 {{画面}}，镜头内容会放在这里",
    );
  });

  test("文本转编辑器文档：一行一段，认识的占位符变成整体的节点，不认识的留作文字", () => {
    expect(textToDoc("生成 {{格数}} 格\n\n{{画面}}{{颜色}}")).toEqual({
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "生成 " },
            { type: PLACEHOLDER_NODE, attrs: { name: "格数" } },
            { type: "text", text: " 格" },
          ],
        },
        { type: "paragraph" },
        {
          type: "paragraph",
          content: [
            { type: PLACEHOLDER_NODE, attrs: { name: "画面" } },
            { type: "text", text: "{{颜色}}" },
          ],
        },
      ],
    });
  });

  test("文档转文本：占位符写回双花括号，段落之间换行，硬换行也是换行；转来转去不变", () => {
    expect(
      docToText({
        type: "doc",
        content: [
          {
            type: "paragraph",
            content: [
              { type: "text", text: "共 " },
              { type: PLACEHOLDER_NODE, attrs: { name: "格数" } },
              { type: "hardBreak" },
              { type: "text", text: "格" },
            ],
          },
          { type: "paragraph" },
          { type: "paragraph", content: [{ type: PLACEHOLDER_NODE, attrs: { name: "画面" } }] },
        ],
      }),
    ).toBe("共 {{格数}}\n格\n\n{{画面}}");
    const text =
      "生成一张 {{格数}} 格：{{列数}} 列 {{行数}} 行，{{比例}}\n\n{{画面}}\n结尾 {{颜色}} ";
    expect(docToText(textToDoc(text))).toBe(text);
  });
});
