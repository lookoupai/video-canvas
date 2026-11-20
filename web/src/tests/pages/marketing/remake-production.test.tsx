import { afterAll, beforeAll, expect, spyOn, test } from "bun:test";
import { installDom, type DomEnv } from "../../helpers/dom";
import { round, script } from "./fixtures";
import service from "@/utils/requests/service";
import type { RemakePreviewDialog } from "@/pages/marketing/detail/remake-preview";

let env: DomEnv;
let Dialog: typeof RemakePreviewDialog;
beforeAll(async () => {
  env = await installDom();
  ({ RemakePreviewDialog: Dialog } = await import("@/pages/marketing/detail/remake-preview"));
});
afterAll(() => env.restore());

const sources = Array.from({ length: 4 }, (_, i) => ({
  ref_shot: i + 1,
  start_ms: i * 2500,
  end_ms: (i + 1) * 2500,
}));
const shots = sources.map((source, i) => ({
  id: i + 1,
  revision: 1,
  scene: "画面" + (i + 1),
  narration: "原旁白",
  duration_ms: 2500,
  trim_start_ms: 0,
  sources: [source],
  unresolved: false,
  chosen_task_id: null,
  takes: [],
  generate_seconds: 5,
  render_credits: 8,
  generate_error: "",
}));
const plan = {
  id: "p",
  script_id: "x1",
  revision: 1,
  script_revision: 1,
  video_model: "demo",
  ref_video_asset_id: 103,
  source_catalog: sources,
  shots,
};
function button(text: string) {
  const el = Array.from(
    document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button'),
  ).find((b) => b.textContent?.trim() === text || b.getAttribute("aria-label") === text);
  if (!el) throw new Error("缺少按钮 " + text);
  return el;
}

test("单镜生成响应丢失后沿用同一请求编号；失败版本旁仍可采用旧视频", async () => {
  const take = { id: 51, task_ref: "ref51", status: "succeeded", outputs: [{ url: "/old.mp4" }] };
  const withTakes = {
    ...plan,
    shots: shots.map((s) =>
      s.id === 3
        ? {
            ...s,
            takes: [
              take,
              { ...take, id: 52, status: "failed", outputs: [], error_message: "生成失败" },
            ],
          }
        : s,
    ),
  };
  const get = spyOn(service, "get").mockImplementation(async (path) => {
    if (path.endsWith("/remake-plans")) return [{ id: "p", script_revision: 1 }];
    if (path.endsWith("/remake-preview")) return { shots: [] };
    return path.endsWith("/p") ? withTakes : [];
  });
  const post = spyOn(service, "post").mockRejectedValue({ status: 504, code: "TIMEOUT" });
  const patch = spyOn(service, "patch").mockResolvedValue(withTakes);
  const { unmount } = await env.mount(
    <Dialog open script={script()} round={round()} initialShot={2} onOpenChange={() => {}} />,
  );
  try {
    await env.click(button("单镜制作"));
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain("5 秒");
    await env.click(button("生成第 3 镜"));
    expect(post.mock.calls.length).toBeGreaterThan(0);
    const deadline = Date.now() + 6500;
    while (
      !Array.from(document.querySelectorAll("button")).some(
        (el) => el.textContent?.includes("重试提交") && !el.disabled,
      ) &&
      Date.now() < deadline
    )
      await env.flush(50);
    const keys = () =>
      post.mock.calls
        .filter(([path]) => path.endsWith("/generate"))
        .map((call) => call[2]?.headers?.["Idempotency-Key"]);
    expect(new Set(keys()).size).toBe(1);
    post.mockResolvedValue({ id: 55, node_id: "shot3", status: "queued", version: 1 });
    await env.click(button("重试提交"));
    expect(new Set(keys()).size).toBe(1);
    await env.click(button("采用版本 1"));
    expect(patch.mock.calls.at(-1)?.[1]).toMatchObject({ chosen_task_id: 51, revision: 1 });
  } finally {
    await unmount();
    get.mockRestore();
    post.mockRestore();
    patch.mockRestore();
  }
}, 10000);

test("合成缺镜时显示缺失镜号，只有显式选中的镜头进入部分合成", async () => {
  const ready = {
    ...plan,
    shots: shots.map((s) => ({ ...s, chosen_task_id: s.id === 3 ? 51 : null })),
  };
  const get = spyOn(service, "get").mockImplementation(async (path) =>
    path.endsWith("/remake-plans")
      ? [{ id: "p", script_revision: 1 }]
      : path.endsWith("/p")
        ? ready
        : path.endsWith("/remake-preview")
          ? { shots: [] }
          : [],
  );
  const post = spyOn(service, "post").mockResolvedValue({
    id: "c1",
    revision: 1,
    idempotency_key: "k",
    shot_ids: [3],
    status: "succeeded",
    asset: { id: 10, url: "/final.mp4", kind: "video" },
  });
  const { unmount } = await env.mount(
    <Dialog open script={script()} round={round()} initialShot={null} onOpenChange={() => {}} />,
  );
  try {
    await env.click(button("单镜制作"));
    await env.click(button("合成成片"));
    expect(button("提交合成").disabled).toBe(true);
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain("第 1、2、4 镜未选版");
    await env.click(button("部分镜头"));
    await env.click(document.querySelector('input[aria-label="合成第 3 镜"]'));
    await env.click(button("提交合成"));
    expect(post.mock.calls.at(-1)?.slice(0, 2)).toEqual([
      "/remake-plans/p/compose",
      { revision: 1, shot_ids: [3] },
    ]);
    expect(document.querySelector('a[href="/final.mp4"]')).not.toBeNull();
  } finally {
    await unmount();
    get.mockRestore();
    post.mockRestore();
  }
});

test("当前脚本创建计划，只编辑第 3 镜，重排后仍指向同一镜头", async () => {
  const get = spyOn(service, "get").mockImplementation(async (path) =>
    path.endsWith("/remake-preview") ? { shots: [] } : [],
  );
  const create = spyOn(service, "post").mockResolvedValue(plan);
  const patch = spyOn(service, "patch").mockResolvedValue({
    ...plan,
    revision: 2,
    shots: shots.map((s) => (s.id === 3 ? { ...s, scene: "新的涂抹动作", revision: 2 } : s)),
  });
  const order = spyOn(service, "put").mockResolvedValue({
    ...plan,
    revision: 3,
    shots: [shots[0], { ...shots[2], scene: "新的涂抹动作", revision: 2 }, shots[1], shots[3]],
  });
  const { unmount } = await env.mount(
    <Dialog open script={script()} round={round()} initialShot={null} onOpenChange={() => {}} />,
  );
  try {
    await env.click(button("单镜制作"));
    await env.click(button("建立制作计划"));
    expect(create.mock.calls[0]?.slice(0, 2)).toEqual([
      "/marketing-scripts/x1/remake-plans",
      { revision: 1 },
    ]);
    await env.click(button("选择第 3 镜"));
    await env.type(document.querySelector('textarea[aria-label="镜头画面"]'), "新的涂抹动作");
    await env.click(button("保存镜头"));
    expect(patch.mock.calls[0]?.[0]).toBe("/remake-plans/p/shots/3");
    expect(patch.mock.calls[0]?.[1]).toMatchObject({ revision: 1, scene: "新的涂抹动作" });
    await env.click(button("上移镜头"));
    expect(order.mock.calls[0]?.[1]).toEqual({ revision: 2, shot_ids: [1, 3, 2, 4] });
    expect(
      document.querySelector<HTMLTextAreaElement>('textarea[aria-label="镜头画面"]')?.value,
    ).toBe("新的涂抹动作");
  } finally {
    await unmount();
    get.mockRestore();
    create.mockRestore();
    patch.mockRestore();
    order.mockRestore();
  }
});

test("使用单镜制作后再次从参考镜号打开，回到该镜来源回放", async () => {
  const get = spyOn(service, "get").mockImplementation(async (path) =>
    path.endsWith("/remake-preview") ? { shots, ref_video_asset_id: 103, revision: 1 } : [],
  );
  const { useState } = await import("react");
  function Host() {
    const [open, setOpen] = useState(true);
    return (
      <>
        <button id="reopen" onClick={() => setOpen(true)}>
          参考 3
        </button>
        <Dialog
          open={open}
          script={script()}
          round={round()}
          initialShot={2}
          onOpenChange={setOpen}
        />
      </>
    );
  }
  const { container, unmount } = await env.mount(<Host />);
  try {
    await env.click(button("单镜制作"));
    await env.click(document.querySelector('[role="dialog"] [data-slot="dialog-close"]'));
    await env.click(container.querySelector("#reopen"));
    expect(document.querySelector('[role="dialog"] video')?.getAttribute("src")).toContain(
      "#t=5,7.5",
    );
  } finally {
    await unmount();
    get.mockRestore();
  }
});
