import { afterAll, afterEach, expect, spyOn, test } from "bun:test";
import service from "@/utils/requests/service";
import { mapSegment, toSegmentUpdateBody } from "@/api/drama";
import { adoptDramaReference, listReferenceAdoptions } from "@/api/drama/adoption";

const post = spyOn(service, "post");
const get = spyOn(service, "get");
afterEach(() => {
  post.mockReset();
  get.mockReset();
});
afterAll(() => {
  post.mockRestore();
  get.mockRestore();
});

test("采用指定镜头固定参考和目标集修订，重传沿用请求键", async () => {
  post.mockResolvedValue({
    id: "adopt",
    reference_id: "ref",
    reference_revision: 4,
    mode: "follow",
    episode_id: "ep",
    episode_no: 2,
    episode_revision: 7,
    segment_ids: ["segment"],
    deleted: false,
  });
  const request = {
    revision: 4,
    requestKey: "same",
    mode: "follow" as const,
    targetEpisodeNo: 2,
    episodeRevision: 6,
    shotIds: ["three"],
  };
  const result = await adoptDramaReference("drama", "ref", request);
  await adoptDramaReference("drama", "ref", request);
  expect(post.mock.calls[0][0]).toBe("/dramas/drama/references/ref/adopt");
  expect(post.mock.calls[0][1]).toEqual({
    revision: 4,
    request_key: "same",
    mode: "follow",
    target_episode_no: 2,
    episode_revision: 6,
    shot_ids: ["three"],
  });
  expect(post.mock.calls[1][1]).toEqual(post.mock.calls[0][1]);
  expect(result).toMatchObject({ episodeNo: 2, referenceRevision: 4, segmentIds: ["segment"] });
  get.mockResolvedValue([{ id: "old", deleted: true, episode_no: 0, segment_ids: null }]);
  expect((await listReferenceAdoptions("drama"))[0]).toMatchObject({
    deleted: true,
    segmentIds: [],
  });
});

test("片段保留原镜、目标时长与裁剪起点，更新只发送目标窗口", () => {
  const segment = mapSegment({
    id: "segment",
    episode_no: 2,
    seq: 0,
    seconds: 4,
    revision: 3,
    prompt: "角色",
    takes: [],
    chosen_task_id: "",
    status: "idle",
    updated_at: "",
    remake: {
      adoption_id: "adopt",
      reference_id: "ref",
      reference_revision: 4,
      asset_id: 12,
      sources: [{ shot_id: "three", start_ms: 5000, end_ms: 7500 }],
      roles: [{ character_id: "a", target_node_id: "new-a", target_name: "新阿青" }],
      target_duration_ms: 2500,
      trim_start_ms: 500,
    },
  });
  expect(segment.remake).toMatchObject({
    referenceRevision: 4,
    assetId: "12",
    targetDurationMs: 2500,
    trimStartMs: 500,
    sources: [{ shotId: "three", startMs: 5000, endMs: 7500 }],
  });
  expect(toSegmentUpdateBody({ revision: 3, targetDurationMs: 2000, trimStartMs: 700 })).toEqual({
    revision: 3,
    target_duration_ms: 2000,
    trim_start_ms: 700,
  });
});
