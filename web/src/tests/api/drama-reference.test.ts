import { afterAll, afterEach, expect, spyOn, test } from "bun:test";
import service from "@/utils/requests/service";
import {
  cancelDramaReferencePreparation,
  applyDramaReferenceDraft,
  generateDramaReferenceDraft,
  getDramaReferenceDraft,
  getDramaReferences,
  importDramaReference,
  listDramaReferenceDrafts,
  listDramaReferencePreparations,
  quoteDramaReferenceDraft,
  resumeDramaReferencePreparation,
  saveDramaReference,
  updateDramaReferenceDraft,
} from "@/api/drama/reference";

const get = spyOn(service, "get");
const upload = spyOn(service, "upload");
const patch = spyOn(service, "patch");
const post = spyOn(service, "post");
afterAll(() => {
  get.mockRestore();
  upload.mockRestore();
  patch.mockRestore();
  post.mockRestore();
});
afterEach(() => {
  get.mockReset();
  upload.mockReset();
  patch.mockReset();
  post.mockReset();
});

test("参考剧集将毫秒与角色身份保留，空列表安全显示", async () => {
  get.mockResolvedValue({
    revision: 4,
    title: "归还钥匙",
    canvas_id: "canvas",
    analyzer_available: true,
    episodes: [
      {
        id: "ep",
        seq: 2,
        title: "门口",
        duration_ms: 10000,
        revision: 3,
        status: "ready",
        bindings: { a: "role" },
        versions: [],
        document: {
          scenes: [],
          characters: [{ id: "a", name: "阿澄" }],
          shots: [],
          dialogues: [{ id: "d", start_ms: 1800, end_ms: 3200, text: "回来啦？", speaker: "a" }],
        },
      },
    ],
    roles: [{ id: "role", name: "阿澄", target_node_id: "node", revision: 2 }],
    targets: null,
  });
  const res = await getDramaReferences("drama");
  expect(get.mock.calls[0][0]).toBe("/dramas/drama/references");
  expect(res.episodes[0].document?.dialogues[0]).toMatchObject({
    startMs: 1800,
    endMs: 3200,
    speaker: "a",
  });
  expect(res.roles[0].targetNodeId).toBe("node");
  expect(res.targets).toEqual([]);
});

test("导入按单集提交版本，校订保留跨镜对白的原片毫秒", async () => {
  const row = { id: "ep", seq: 1, revision: 4, status: "ready", document: null };
  upload.mockResolvedValue(row);
  patch.mockResolvedValue(row);
  await importDramaReference(
    "drama",
    "ep",
    3,
    new File(["{}"], "reference.json", { type: "application/json" }),
  );
  const body = upload.mock.calls[0][1] as FormData;
  expect(body.get("revision")).toBe("3");
  expect((body.get("package") as File).name).toBe("reference.json");
  await saveDramaReference("drama", "ep", {
    revision: 4,
    document: {
      scenes: [{ id: "s", title: "门口" }],
      characters: [{ id: "a", name: "阿澄" }],
      shots: [
        {
          id: "1",
          sceneId: "s",
          startMs: 0,
          endMs: 2500,
          action: "敲门",
          camera: "固定",
          characters: ["a"],
        },
      ],
      dialogues: [{ id: "d", startMs: 2000, endMs: 3500, text: "是我。", speaker: "a" }],
    },
    bindings: { a: "role" },
  });
  expect(patch.mock.calls[0][1]).toMatchObject({
    revision: 4,
    document: { dialogues: [{ start_ms: 2000, end_ms: 3500, speaker: "a" }] },
    bindings: { a: "role" },
  });
});

test("分析草稿映射系统帧证据、覆盖状态与修订更新", async () => {
  const row = {
    id: "draft",
    reference_id: "reference",
    base_revision: 3,
    revision: 2,
    status: "editing",
    source: "agent",
    preparation_id: "preparation",
    instruction: "保留动作",
    document: {
      scenes: [],
      characters: [],
      shots: [
        {
          id: "shot",
          scene_id: "scene",
          start_ms: 0,
          end_ms: 2500,
          action: "转身",
          camera: "固定",
          characters: [],
        },
      ],
      dialogues: [],
    },
    prompts: [{ shot_id: "shot", prompt: "手部自然转动" }],
    evidence: [
      { preparation_frame_id: "frame", id: "proof", shot_id: "shot", asset_id: 12, at_ms: 1200 },
    ],
    observations: [
      {
        shot_id: "shot",
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
  get.mockResolvedValueOnce({ items: [row], next_cursor: "next" });
  const page = await listDramaReferenceDrafts("drama", "reference");
  expect(get.mock.calls.at(-1)?.[0]).toBe("/dramas/drama/references/reference/drafts");
  expect(page).toMatchObject({
    nextCursor: "next",
    items: [
      {
        id: "draft",
        source: "agent",
        prompts: [{ shotId: "shot" }],
        evidence: [{ preparationFrameId: "frame", assetId: 12 }],
        coverage: row.coverage,
      },
    ],
  });
  get.mockResolvedValueOnce(row);
  const current = await getDramaReferenceDraft("drama", "reference", "draft");
  expect(current.document.shots[0].sceneId).toBe("scene");
  patch.mockResolvedValueOnce({
    ...row,
    revision: 3,
    prompts: [{ shot_id: "shot", prompt: "已核对提示词" }],
  });
  const saved = await updateDramaReferenceDraft("drama", "reference", "draft", {
    revision: 2,
    prompts: [{ shotId: "shot", prompt: "已核对提示词" }],
  });
  expect(patch.mock.calls.at(-1)?.[1]).toMatchObject({
    revision: 2,
    prompts: [{ shot_id: "shot" }],
  });
  expect(saved.revision).toBe(3);
});

test("单镜路线报价保留修订、素材输入和可重验计划摘要", async () => {
  post.mockResolvedValueOnce({
    draft_id: "draft",
    project_id: "project",
    reference_id: "reference",
    draft_revision: 3,
    reference_revision: 2,
    source_asset_id: "source",
    model_id: "video",
    prompt_format: "text",
    configuration_digest: "config",
    plan_digest: "plan",
    executable: true,
    total_video_credits: 12,
    units: [
      {
        shot_id: "shot-2",
        mode: "video",
        source_start_ms: 4000,
        source_end_ms: 8000,
        duration: 5,
        trim_start_ms: 500,
        target_duration_ms: 3000,
        prompt: "人物转身",
        image_asset_ids: [],
        materials: [],
        input: { duration: 5, op: "omni" },
        credits: 12,
        issues: [],
      },
    ],
    issues: [],
  });
  const plan = await quoteDramaReferenceDraft("project", "reference", "draft", {
    revision: 3,
    modelId: "video",
    maxCredits: 20,
    units: [
      {
        shotId: "shot-2",
        mode: "video",
        duration: 5,
        trimStartMs: 500,
        targetDurationMs: 3000,
      },
    ],
  });
  expect(post.mock.calls.at(-1)?.[0]).toBe(
    "/dramas/project/references/reference/drafts/draft/quote",
  );
  expect(post.mock.calls.at(-1)?.[1]).toEqual({
    revision: 3,
    model_id: "video",
    units: [
      {
        shot_id: "shot-2",
        mode: "video",
        image_asset_ids: undefined,
        duration: 5,
        trim_start_ms: 500,
        target_duration_ms: 3000,
        params: undefined,
      },
    ],
    max_credits: 20,
  });
  expect(plan).toMatchObject({
    draftId: "draft",
    referenceRevision: 2,
    planDigest: "plan",
    executable: true,
    totalVideoCredits: 12,
    units: [{ shotId: "shot-2", sourceStartMs: 4000, targetDurationMs: 3000, credits: 12 }],
  });
});

test("草稿采用与批量生成都保留稳定键、报价修订和业务回执", async () => {
  post
    .mockResolvedValueOnce({
      application_id: "application",
      draft_id: "draft",
      draft_revision: 3,
      plan_digest: "digest",
      adoption_id: "adoption",
      episode_id: "episode",
      episode_no: 4,
      episode_revision: 2,
      segments: [
        {
          shot_id: "shot-2",
          segment_id: "segment",
          revision: 1,
          trim_start_ms: 300,
          target_duration_ms: 2000,
        },
      ],
      replayed: false,
    })
    .mockResolvedValueOnce({
      application_id: "application",
      plan_digest: "digest",
      status: "queued",
      items: [
        {
          shot_id: "shot-2",
          segment_id: "segment",
          task_id: "9001",
          status: "queued",
        },
      ],
      replayed: false,
    });
  const quote = {
    revision: 3,
    modelId: "video",
    units: [
      {
        shotId: "shot-2",
        mode: "video" as const,
        duration: 8,
        trimStartMs: 300,
        targetDurationMs: 2000,
      },
    ],
    maxCredits: 12,
  };
  const applied = await applyDramaReferenceDraft("project", "reference", "draft", {
    revision: 3,
    requestKey: "apply-key",
    planDigest: "digest",
    mode: "follow",
    quote,
  });
  expect(post.mock.calls[0]?.[0]).toBe("/dramas/project/references/reference/drafts/draft/apply");
  expect(post.mock.calls[0]?.[1]).toMatchObject({
    request_key: "apply-key",
    plan_digest: "digest",
    mode: "follow",
    quote: {
      revision: 3,
      model_id: "video",
      units: [{ shot_id: "shot-2", target_duration_ms: 2000 }],
    },
  });
  expect(applied).toMatchObject({
    applicationId: "application",
    episodeNo: 4,
    segments: [{ shotId: "shot-2", segmentId: "segment", revision: 1 }],
  });
  const generated = await generateDramaReferenceDraft("project", "reference", "draft", {
    revision: 3,
    requestKey: "generate-key",
    applicationId: applied.applicationId,
    planDigest: "digest",
    quote,
  });
  expect(post.mock.calls[1]?.[0]).toBe(
    "/dramas/project/references/reference/drafts/draft/generate",
  );
  expect(post.mock.calls[1]?.[1]).toMatchObject({
    request_key: "generate-key",
    application_id: "application",
    plan_digest: "digest",
  });
  expect(generated.items).toEqual([
    { shotId: "shot-2", segmentId: "segment", taskId: "9001", status: "queued" },
  ]);
});

test("媒体准备状态保留音轨区分与帧完成进度", async () => {
  get.mockResolvedValueOnce({
    items: [
      {
        id: "prep",
        reference_id: "reference",
        base_revision: 3,
        source_asset_id: 12,
        duration_ms: 10000,
        processor_version: "v1",
        status: "running",
        attempt: 2,
        shot_count: 4,
        total_frames: 12,
        completed_frames: 7,
        has_audio: true,
        audio_status: "not_analyzed",
        error: "",
        created_at: "a",
        updated_at: "b",
      },
    ],
    next_cursor: "",
  });
  const rows = await listDramaReferencePreparations("drama", "reference");
  expect(rows[0]).toMatchObject({
    id: "prep",
    status: "running",
    completedFrames: 7,
    totalFrames: 12,
    hasAudio: true,
    audioStatus: "not_analyzed",
  });
});

test("准备取消与恢复使用公开项目接口和稳定请求键", async () => {
  post.mockResolvedValue({});
  await cancelDramaReferencePreparation("drama", "reference", "prep");
  await resumeDramaReferencePreparation("drama", "reference", "prep", "resume-key");
  expect(post.mock.calls[0]?.[0]).toBe(
    "/dramas/drama/references/reference/preparations/prep/cancel",
  );
  expect(post.mock.calls[1]?.[0]).toBe(
    "/dramas/drama/references/reference/preparations/prep/resume",
  );
  expect(post.mock.calls[1]?.[1]).toEqual({ request_key: "resume-key" });
});
