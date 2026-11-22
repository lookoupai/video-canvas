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

test("参考页可打开助手并从当前修订直接发起分析", async () => {
  const get = spyOn(service, "get").mockImplementation(async (path) => {
    if (path === "/agent/models")
      return [{ key: "vision", name: "Vision", vision: true, context_window: 32000 }];
    if (path === "/canvas/canvas/agent/sessions") return [];
    if (path.endsWith("/drafts") || path.endsWith("/preparations"))
      return { items: [], next_cursor: "" };
    if (path.endsWith("/reference-adoptions")) return [];
    return collection;
  });
  const post = spyOn(service, "post").mockImplementation(async (path) => {
    if (path === "/canvas/canvas/agent/sessions")
      return {
        id: "session",
        canvas_id: "canvas",
        title: "",
        mode: "drama",
        model_key: "vision",
        last_seq: 0,
        created_at: "",
        updated_at: "",
      };
    return {
      id: "run",
      session_id: "session",
      canvas_id: "canvas",
      status: "running",
      mode: "drama",
      spent_credits: 0,
      context_tokens: 0,
      steps: 0,
      max_steps: 40,
      error_code: "",
      error_message: "",
      created_at: "",
      ended_at: null,
    };
  });
  const { unmount } = await env.mount(
    <MemoryRouter>
      <Workspace id="drama" />
    </MemoryRouter>,
  );
  try {
    await env.flush(20);
    await env.click(button("助手分析"));
    await env.flush(20);
    expect(document.querySelector('[aria-label="参考剧集助手"]')).not.toBeNull();
    const analyze = button("分析当前集");
    expect((analyze as HTMLButtonElement).disabled).toBe(false);
    const firstShot = document.querySelector<HTMLElement>(
      '[role="checkbox"][aria-label="分析镜头 1 · 推开门"]',
    );
    expect(firstShot?.getAttribute("aria-checked")).toBe("true");
    await env.click(firstShot);
    await env.click(analyze);
    await env.flush(20);
    const runRequest = post.mock.calls.at(-1)?.[1] as {
      message: string;
      mode: string;
      page_context: {
        kind: string;
        project_id: string;
        reference_id: string;
        reference_revision: number;
        reference_title: string;
        reference_seq: number;
        selected_shot_ids: string[];
      };
    };
    expect(runRequest.mode).toBe("drama");
    expect(runRequest.message).toContain("reference_id=ep1");
    expect(runRequest.message).toContain("reference_revision=1");
    expect(runRequest.page_context).toMatchObject({
      kind: "drama_reference",
      project_id: "drama",
      reference_id: "ep1",
      reference_revision: 1,
      reference_title: "参考1",
      reference_seq: 1,
      selected_shot_ids: ["2"],
    });
  } finally {
    await unmount();
    get.mockRestore();
    post.mockRestore();
  }
});

test("分析草稿显示帧准备进度并按草稿修订保存提示词", async () => {
  const draft = {
    id: "draft",
    reference_id: "ep1",
    base_revision: 1,
    revision: 2,
    status: "editing",
    source: "agent",
    preparation_id: "prep",
    instruction: "保留动作",
    document: doc,
    prompts: [{ shot_id: "1", prompt: "手部自然转动" }],
    evidence: [
      { preparation_frame_id: "frame", id: "proof", shot_id: "1", asset_id: 12, at_ms: 1200 },
    ],
    observations: [
      {
        shot_id: "1",
        field: "action",
        source: "observation",
        review_state: "observed",
        evidence_ids: ["proof"],
      },
    ],
    coverage: {
      shots: "complete",
      visual: "partial",
      dialogue: "not_analyzed",
      speakers: "not_analyzed",
    },
    updated_at: "2026-11-21T12:00:00Z",
  };
  const get = spyOn(service, "get").mockImplementation(async (path) => {
    if (path === "/models")
      return [
        {
          key: "video",
          label: "Video",
          capabilities: { refs: { image: { on: true }, video: { on: true } } },
        },
      ];
    if (path.endsWith("/drafts")) return { items: [draft], next_cursor: "" };
    if (path.endsWith("/preparations"))
      return {
        items: [
          {
            id: "prep",
            reference_id: "ep1",
            base_revision: 1,
            source_asset_id: 12,
            duration_ms: 10000,
            processor_version: "v1",
            status: "running",
            attempt: 1,
            shot_count: 2,
            total_frames: 3,
            completed_frames: 1,
            has_audio: true,
            audio_status: "not_analyzed",
            error: "",
            created_at: "",
            updated_at: "",
          },
        ],
        next_cursor: "",
      };
    return collection;
  });
  const patch = spyOn(service, "patch").mockResolvedValue({
    ...draft,
    revision: 3,
    prompts: [{ shot_id: "1", prompt: "替换后的提示词" }],
  });
  const post = spyOn(service, "post").mockImplementation(async (path) => {
    if (path.endsWith("/quote")) {
      return {
        draft_id: "draft",
        project_id: "drama",
        reference_id: "ep1",
        draft_revision: 3,
        reference_revision: 1,
        source_asset_id: "source",
        model_id: "video",
        prompt_format: "text",
        configuration_digest: "config",
        plan_digest: "plan",
        executable: true,
        total_video_credits: 16,
        units: [
          {
            shot_id: "1",
            mode: "video",
            credits: 8,
            issues: [],
            materials: [],
            image_asset_ids: [],
          },
          {
            shot_id: "2",
            mode: "video",
            credits: 8,
            issues: [],
            materials: [],
            image_asset_ids: [],
          },
        ],
        issues: [],
      };
    }
    if (path.endsWith("/apply")) {
      return {
        application_id: "application",
        draft_id: "draft",
        draft_revision: 3,
        plan_digest: "plan",
        adoption_id: "adoption",
        episode_id: "episode",
        episode_no: 3,
        episode_revision: 1,
        segments: [
          {
            shot_id: "1",
            segment_id: "segment-1",
            revision: 1,
            trim_start_ms: 0,
            target_duration_ms: 2500,
          },
          {
            shot_id: "2",
            segment_id: "segment-2",
            revision: 1,
            trim_start_ms: 0,
            target_duration_ms: 7500,
          },
        ],
        replayed: false,
      };
    }
    if (path.endsWith("/generate")) {
      return {
        application_id: "application",
        plan_digest: "plan",
        items: [
          {
            shot_id: "1",
            segment_id: "segment-1",
            task_id: "8001",
            status: "queued",
            credits: 8,
          },
        ],
        replayed: false,
      };
    }
    return {};
  });
  const { unmount } = await env.mount(
    <MemoryRouter>
      <Workspace id="drama" />
    </MemoryRouter>,
  );
  try {
    await env.flush(30);
    expect(document.querySelector('[aria-label="分析草稿原镜 1 提示词"]')).not.toBeNull();
    expect(document.body.textContent).toContain("1/3 帧");
    const prompt = document.querySelector<HTMLTextAreaElement>(
      '[aria-label="分析草稿原镜 1 提示词"]',
    )!;
    await env.type(prompt, "替换后的提示词");
    await env.click(button("保存草稿"));
    await env.flush(20);
    expect(patch.mock.calls[0]?.[0]).toBe("/dramas/drama/references/ep1/drafts/draft");
    expect(patch.mock.calls[0]?.[1]).toMatchObject({
      revision: 2,
      prompts: [{ shot_id: "1", prompt: "替换后的提示词" }],
    });
    await env.click(button("获取报价"));
    await env.flush(10);
    expect(post.mock.calls[0]?.[0]).toBe("/dramas/drama/references/ep1/drafts/draft/quote");
    expect(post.mock.calls[0]?.[1]).toMatchObject({
      revision: 3,
      model_id: "video",
      units: [
        { shot_id: "1", mode: "auto", duration: 8, target_duration_ms: 2500 },
        { shot_id: "2", mode: "auto", duration: 8, target_duration_ms: 7500 },
      ],
    });
    expect(document.body.textContent).toContain("站内积分 16");
    const route = document.querySelector<HTMLSelectElement>('[aria-label="报价生成路线"]')!;
    await env.act(async () => {
      route.value = "keyframe";
      route.dispatchEvent(new env.win.Event("change", { bubbles: true }));
    });
    await env.click(button("获取报价"));
    await env.flush(10);
    expect(post.mock.calls[1]?.[1]).toMatchObject({
      units: [
        { shot_id: "1", image_asset_ids: ["12"] },
        { shot_id: "2", image_asset_ids: [] },
      ],
    });
    await env.click(button("采用到新分集"));
    await env.flush(5);
    expect(document.body.textContent).toContain("不会提交视频生成任务");
    await env.click(button("采用"));
    await env.flush(10);
    expect(post.mock.calls[2]?.[0]).toBe("/dramas/drama/references/ep1/drafts/draft/apply");
    expect(post.mock.calls.some(([path]) => path.endsWith("/generate"))).toBe(false);
    await env.click(button("提交生成 · 16 积分"));
    await env.flush(5);
    expect(document.body.textContent).toContain("预计冻结站内积分 16");
    await env.click(button("提交生成"));
    await env.flush(10);
    expect(post.mock.calls[3]?.[0]).toBe("/dramas/drama/references/ep1/drafts/draft/generate");
    expect(document.body.textContent).toContain("任务 8001");
  } finally {
    await unmount();
    get.mockRestore();
    patch.mockRestore();
    post.mockRestore();
  }
});

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
