import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { useEffect, useState } from "react";

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { PlaceholderEditor as PlaceholderEditorType } from "@/components/drama/prompts/placeholder-editor";

/** 故事板模板编辑器：占位符是整体的 chip，上面的按钮往里插占位符，值始终是纯文本。 */

let dom: DomEnv;
let PlaceholderEditor: typeof PlaceholderEditorType;
beforeAll(async () => {
  dom = await installDom();
  PlaceholderEditor = (await import("@/components/drama/prompts/placeholder-editor"))
    .PlaceholderEditor;
});
afterAll(async () => {
  // Tiptap 销毁编辑器是异步的：等它跑完再还原全局，不然会在没有 document 时抛错
  await dom.flush(50);
  dom.restore();
});

/** 受控外壳：记下每次回传的文本，外部可以改值 */
let setOutside: (text: string) => void = () => {};
const seen: string[] = [];
function Harness({ initial }: { initial: string }) {
  const [value, setValue] = useState(initial);
  useEffect(() => {
    setOutside = setValue;
  }, []);
  return (
    <PlaceholderEditor
      value={value}
      ariaLabel="提示词正文"
      onChange={(text) => {
        seen.push(text);
        setValue(text);
      }}
    />
  );
}

const chips = (c: HTMLElement) => [...c.querySelectorAll("[data-placeholder]")];

describe("PlaceholderEditor：故事板模板编辑器", () => {
  test("认识的占位符画成整体的块（不可编辑），不认识的 {{xx}} 还是普通文字；上面有五个插入按钮", async () => {
    seen.length = 0;
    const m = await dom.mount(<Harness initial={"生成 {{格数}} 格\n{{画面}} {{颜色}}"} />);
    await dom.flush();
    expect(chips(m.container).map((el) => el.getAttribute("data-placeholder"))).toEqual([
      "格数",
      "画面",
    ]);
    expect(chips(m.container).every((el) => el.getAttribute("contenteditable") === "false")).toBe(
      true,
    );
    const editor = m.container.querySelector('[role="textbox"]') as HTMLElement;
    expect(editor.getAttribute("aria-label")).toBe("提示词正文");
    expect(editor.textContent).toContain("{{颜色}}");
    const buttons = [...m.container.querySelectorAll("[data-slot=placeholder-toolbar] button")];
    expect(buttons.map((b) => b.getAttribute("aria-label"))).toEqual([
      "插入占位符 格数",
      "插入占位符 列数",
      "插入占位符 行数",
      "插入占位符 比例",
      "插入占位符 画面",
    ]);
    // 没动过就不回传
    expect(seen).toEqual([]);
    await m.unmount();
  });

  test("点按钮插入占位符：多出一个块，回传的文本里是 {{名字}}", async () => {
    seen.length = 0;
    const m = await dom.mount(<Harness initial="开头" />);
    await dom.flush();
    const button = m.container.querySelector(
      'button[aria-label="插入占位符 列数"]',
    ) as HTMLButtonElement;
    await dom.click(button);
    await dom.flush();
    expect(chips(m.container).map((el) => el.getAttribute("data-placeholder"))).toEqual(["列数"]);
    expect(seen.at(-1)).toContain("{{列数}}");
    expect(seen.at(-1)).toContain("开头");
    await m.unmount();
  });

  test("外部改了正文（比如恢复内置）：编辑器跟着换内容，不再回传", async () => {
    seen.length = 0;
    const m = await dom.mount(<Harness initial="旧的 {{格数}}" />);
    await dom.flush();
    expect(chips(m.container)).toHaveLength(1);
    await dom.act(async () => {
      setOutside("新的 {{比例}} {{行数}}");
    });
    await dom.flush();
    expect(chips(m.container).map((el) => el.getAttribute("data-placeholder"))).toEqual([
      "比例",
      "行数",
    ]);
    expect(seen).toEqual([]);
    await m.unmount();
  });
});
