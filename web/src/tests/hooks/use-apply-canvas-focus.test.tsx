import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { useApplyCanvasFocus as HookType } from "@/hooks/use-apply-canvas-focus";
import type { CanvasEditorCommands } from "@/components/canvas-editor/editor-api";

/** 片场切到画布视图后：等编辑器挂好、节点在了，再飞到要看的故事板节点 */

let dom: DomEnv;
let useApplyCanvasFocus: typeof HookType;
beforeAll(async () => {
  dom = await installDom();
  useApplyCanvasFocus = (await import("@/hooks/use-apply-canvas-focus")).useApplyCanvasFocus;
});
afterAll(() => dom.restore());

const waitFrames = (ms = 80) => dom.flush(ms);

/** 假的命令表：focusNode 记下被飞到的节点 */
function fakeCommands(nodes: string[], focused: string[]): CanvasEditorCommands {
  return {
    getNodes: () => nodes.map((id) => ({ id })),
    focusNode: (id: string) => {
      focused.push(id);
      return true;
    },
  } as unknown as CanvasEditorCommands;
}

function mountHook(
  commands: { current: CanvasEditorCommands | null },
  target: string | null,
  done: string[],
) {
  function Probe() {
    useApplyCanvasFocus(target, commands, () => done.push("done"));
    return null;
  }
  return dom.mount(<Probe />);
}

describe("useApplyCanvasFocus", () => {
  test("没有目标节点：什么都不做", async () => {
    const focused: string[] = [];
    const done: string[] = [];
    const commands = { current: fakeCommands(["b1"], focused) };
    const m = await mountHook(commands, null, done);
    await waitFrames();
    expect(focused).toEqual([]);
    expect(done).toEqual([]);
    await m.unmount();
  });

  test("节点已在：飞过去，并通知用完了（调用方据此清掉目标）", async () => {
    const focused: string[] = [];
    const done: string[] = [];
    const commands = { current: fakeCommands(["a", "b1"], focused) };
    const m = await mountHook(commands, "b1", done);
    await waitFrames();
    expect(focused).toEqual(["b1"]);
    expect(done).toEqual(["done"]);
    await m.unmount();
  });

  test("编辑器还没挂好：等它挂好再飞，不丢目标", async () => {
    const focused: string[] = [];
    const done: string[] = [];
    const commands = { current: null as CanvasEditorCommands | null };
    const m = await mountHook(commands, "b1", done);
    await waitFrames(30);
    expect(focused).toEqual([]);
    expect(done).toEqual([]);
    commands.current = fakeCommands(["b1"], focused);
    await waitFrames();
    expect(focused).toEqual(["b1"]);
    expect(done).toEqual(["done"]);
    await m.unmount();
  });
});
