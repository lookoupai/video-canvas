import { afterAll, beforeAll, expect, spyOn, test } from "bun:test";
import { MemoryRouter } from "react-router";
import service from "@/utils/requests/service";
import type { ReferenceWorkspace } from "@/pages/drama/references";
import { installDom, type DomEnv } from "../../helpers/dom";

let env: DomEnv;
let Workspace: typeof ReferenceWorkspace;
beforeAll(async () => {
  env = await installDom();
  Workspace = (await import("@/pages/drama/references")).ReferenceWorkspace;
});
afterAll(() => env.restore());
const doc = {
  scenes: [{ id: "s", title: "门口" }],
  characters: [
    { id: "a", name: "阿澄" },
    { id: "b", name: "小舟" },
  ],
  shots: [
    {
      id: "1",
      scene_id: "s",
      start_ms: 0,
      end_ms: 2500,
      action: "推开门",
      camera: "固定",
      characters: ["a"],
    },
    {
      id: "2",
      scene_id: "s",
      start_ms: 2500,
      end_ms: 10000,
      action: "回头",
      camera: "推进",
      characters: ["b"],
    },
  ],
  dialogues: [{ id: "d", start_ms: 2000, end_ms: 4000, text: "你回来了？", speaker: "b" }],
};
function episode(id: string, seq: number) {
  return {
    id,
    seq,
    title: "参考" + seq,
    asset: { id: seq, url: "/ref" + seq + ".mp4", kind: "video", duration_ms: 10000 },
    duration_ms: 10000,
    revision: 1,
    status: "ready",
    method: "import",
    document: doc,
    bindings: { a: "shared" },
    versions: [],
  };
}
const collection = {
  title: "钥匙",
  canvas_id: "canvas",
  revision: 3,
  analyzer_available: true,
  roles: [{ id: "shared", name: "阿澄", target_node_id: "new-a", revision: 1 }],
  targets: [{ node_id: "new-a", name: "新角色阿澄" }],
  episodes: [episode("ep1", 1), episode("ep2", 2)],
};
const button = (text: string) =>
  Array.from(document.querySelectorAll("button")).find((b) => b.textContent === text)!;

test("两集共用角色，修正第二集说话人和跨集映射，仅保存这一集", async () => {
  const get = spyOn(service, "get").mockImplementation(async (path) =>
    path.endsWith("reference-adoptions") ? [] : collection,
  );
  const patch = spyOn(service, "patch").mockImplementation(async (_path, body) => ({
    ...episode("ep2", 2),
    ...(body as object),
    revision: 2,
  }));
  const { unmount } = await env.mount(
    <MemoryRouter>
      <Workspace id="drama" />
    </MemoryRouter>,
  );
  try {
    await env.click(button("第 2 集 · 参考2"));
    const who = document.querySelector<HTMLSelectElement>('[aria-label="对白 1 说话人"]')!;
    expect(who).not.toBeNull();
    await env.act(async () => {
      who.value = "a";
      who.dispatchEvent(new env.win.Event("change", { bubbles: true }));
    });
    const mapping = document.querySelector<HTMLSelectElement>('[aria-label="小舟对应的全剧角色"]')!;
    await env.act(async () => {
      mapping.value = "shared";
      mapping.dispatchEvent(new env.win.Event("change", { bubbles: true }));
    });
    await env.click(button("保存校订"));
    expect(patch.mock.calls[0][0]).toBe("/dramas/drama/references/ep2");
    expect(patch.mock.calls[0][1]).toMatchObject({
      revision: 1,
      document: { dialogues: [{ speaker: "a", start_ms: 2000, end_ms: 4000 }] },
      bindings: { a: "shared", b: "shared" },
    });
    expect(patch.mock.calls).toHaveLength(1);
    await env.click(button("原镜 1 · 0–2.5 秒"));
    expect(document.querySelector("video")?.src).toContain("/ref2.mp4#t=0,2.5");
  } finally {
    await unmount();
    get.mockRestore();
    patch.mockRestore();
  }
});

test("某集失败时保留另一集校订，只重试选中的一集", async () => {
  const get = spyOn(service, "get").mockImplementation(async (path) =>
    path.endsWith("reference-adoptions")
      ? []
      : {
          ...collection,
          episodes: [
            episode("ep1", 1),
            { ...episode("ep2", 2), status: "failed", error: "分析失败", document: null },
          ],
        },
  );
  const post = spyOn(service, "post").mockResolvedValue({
    ...episode("ep2", 2),
    status: "queued",
    document: null,
  });
  const { unmount } = await env.mount(
    <MemoryRouter>
      <Workspace id="drama" />
    </MemoryRouter>,
  );
  try {
    await env.click(button("第 2 集 · 参考2"));
    await env.click(button("重新分析本集"));
    expect(post.mock.calls[0]).toEqual(["/dramas/drama/references/ep2/analyze", { revision: 1 }]);
    await env.click(button("第 1 集 · 参考1"));
    expect(document.querySelector('textarea[aria-label="对白 1 文本"]')).not.toBeNull();
  } finally {
    await unmount();
    get.mockRestore();
    post.mockRestore();
  }
});
