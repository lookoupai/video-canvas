import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { EditorHeading as EditorHeadingType } from "@/pages/drama/studio/editor";

/** 右侧编辑区标题：第几集 · 选中的是第几段 */

let dom: DomEnv;
let EditorHeading: typeof EditorHeadingType;
beforeAll(async () => {
  dom = await installDom();
  EditorHeading = (await import("@/pages/drama/studio/editor")).EditorHeading;
}, 60_000);
afterAll(() => dom?.restore());

describe("编辑区标题", () => {
  test("第几段跟选中的片段走（seq 从 0 起，显示从 1 起）", async () => {
    const m = await dom.mount(<EditorHeading no={1} seq={6} />);
    expect(m.container.querySelector("h2")?.textContent).toBe("第 1 集 · 第 7 段");
    await m.unmount();

    const m2 = await dom.mount(<EditorHeading no={2} seq={0} />);
    expect(m2.container.querySelector("h2")?.textContent).toBe("第 2 集 · 第 1 段");
    await m2.unmount();
  });
});
