import { afterEach, describe, expect, spyOn, test } from "bun:test";

import {
  composeRemakePlan,
  createRemakePlan,
  getRemakePlan,
  listRemakeCompositions,
  listRemakePlans,
  orderRemakeShots,
  updateRemakeShot,
} from "@/api/marketing/remake";
import service from "@/utils/requests/service";
import { submitRemakeShotTask } from "@/utils/tasks/gateway";
import { useTasksStore } from "@/store/tasks";
import { makeTask } from "../utils/tasks/fixtures";

const rawPlan = {
  id: "p9",
  script_id: "s3",
  script_revision: 2,
  revision: 6,
  video_model: "video-a",
  ref_video_asset_id: 31,
  source_catalog: [{ ref_shot: 3, start_ms: 5000, end_ms: 8000 }],
  shots: [
    {
      id: 3,
      revision: 4,
      scene: "涂抹",
      narration: "轻轻推开",
      duration_ms: 2500,
      trim_start_ms: 400,
      unresolved: false,
      sources: [
        { ref_shot: 1, start_ms: 0, end_ms: 2500 },
        { ref_shot: 3, start_ms: 5123, end_ms: 7500 },
      ],
      chosen_task_id: 37,
      takes: [{ id: 37, status: "succeeded", outputs: [{ url: "/take.mp4" }] }],
      generate_seconds: 5,
      render_credits: 9,
      generate_error: "",
    },
  ],
};
const rawCompose = {
  id: "c9",
  revision: 6,
  idempotency_key: "compose-1",
  shot_ids: [3],
  status: "succeeded",
  error: "",
  created_at: "now",
  asset: { id: 54, kind: "video", url: "/final.mp4", duration_ms: 2500 },
};

describe("营销逐镜制作 API", () => {
  afterEach(() => {
    useTasksStore.setState({ tasks: {} });
  });

  test("创建与重开历史计划保留稳定镜号、成片毫秒和独立原片区间", async () => {
    const post = spyOn(service, "post").mockResolvedValue(rawPlan);
    const get = spyOn(service, "get")
      .mockResolvedValueOnce([{ id: "p9", script_revision: 2 }])
      .mockResolvedValueOnce(rawPlan);
    try {
      expect(await listRemakePlans("s3")).toEqual([{ id: "p9", scriptRevision: 2 }]);
      expect(get.mock.calls[0][0]).toBe("/marketing-scripts/s3/remake-plans");
      const plan = await createRemakePlan("s3", 2);
      expect(post).toHaveBeenCalledWith("/marketing-scripts/s3/remake-plans", { revision: 2 });
      expect(plan).toMatchObject({
        id: "p9",
        scriptId: "s3",
        scriptRevision: 2,
        revision: 6,
        videoModel: "video-a",
        refVideoAssetId: "31",
        sourceCatalog: [{ refShot: 3, start: 5, end: 8 }],
      });
      expect(plan.shots[0]).toMatchObject({
        id: 3,
        revision: 4,
        durationMs: 2500,
        trimStartMs: 400,
        generateSeconds: 5,
        renderCredits: 9,
        generateError: "",
        chosenTaskId: "37",
        takes: [{ id: "37", outputs: [{ url: "/take.mp4" }] }],
        sources: [
          { refShot: 1, start: 0, end: 2.5 },
          { refShot: 3, start: 5.123, end: 7.5 },
        ],
      });
      expect(await getRemakePlan("p9")).toEqual(plan);
      expect(get.mock.calls[1][0]).toBe("/remake-plans/p9");
    } finally {
      post.mockRestore();
      get.mockRestore();
    }
  });

  test("编辑与重排使用计划修订号，毫秒换算和老版本选择只发送用户指定的字段", async () => {
    const patch = spyOn(service, "patch").mockResolvedValue(rawPlan);
    const put = spyOn(service, "put").mockResolvedValue(rawPlan);
    try {
      await updateRemakeShot("p9", 3, {
        revision: 6,
        scene: "抹开",
        narration: "",
        durationMs: 1900,
        trimStartMs: 400,
        sources: [{ refShot: 3, start: 5.123, end: 6.001 }],
      });
      expect(patch.mock.calls[0]).toEqual([
        "/remake-plans/p9/shots/3",
        {
          revision: 6,
          scene: "抹开",
          narration: "",
          duration_ms: 1900,
          trim_start_ms: 400,
          sources: [{ ref_shot: 3, start_ms: 5123, end_ms: 6001 }],
        },
      ]);
      await updateRemakeShot("p9", 3, { revision: 7, chosenTaskId: "37" });
      expect(patch.mock.calls[1]).toEqual([
        "/remake-plans/p9/shots/3",
        { revision: 7, chosen_task_id: 37 },
      ]);
      await orderRemakeShots("p9", 8, [3, 1, 2, 4]);
      expect(put.mock.calls[0]).toEqual([
        "/remake-plans/p9/order",
        { revision: 8, shot_ids: [3, 1, 2, 4] },
      ]);
    } finally {
      patch.mockRestore();
      put.mockRestore();
    }
  });

  test("重做第三镜时网络重传携带同一键和镜头修订，返回的快照进入任务库", async () => {
    const task = makeTask({ id: 72, node_id: "remake:9:3", status: "pending" });
    const post = spyOn(service, "post")
      .mockRejectedValueOnce({ code: "NETWORK_ERROR" })
      .mockResolvedValueOnce(task);
    const get = spyOn(service, "get").mockResolvedValue({ available: 20 });
    try {
      expect(await submitRemakeShotTask("p9", 3, 4, "retake-click-1")).toEqual(task);
      expect(post.mock.calls).toEqual(
        Array(2).fill([
          "/remake-plans/p9/shots/3/generate",
          { revision: 4 },
          { headers: { "Idempotency-Key": "retake-click-1" } },
        ]),
      );
      expect(useTasksStore.getState().tasks["72"]?.version).toBe(task.version);
    } finally {
      post.mockRestore();
      get.mockRestore();
    }
  });

  test("整条合成使用计划镜序，部分合成发送明确范围，再次打开可读回成片", async () => {
    const post = spyOn(service, "post").mockResolvedValue(rawCompose);
    const get = spyOn(service, "get").mockResolvedValue([rawCompose]);
    try {
      const result = await composeRemakePlan("p9", 6, "compose-1", [3]);
      expect(post.mock.calls[0]).toEqual([
        "/remake-plans/p9/compose",
        { revision: 6, shot_ids: [3] },
        { headers: { "Idempotency-Key": "compose-1" } },
      ]);
      await composeRemakePlan("p9", 7, "compose-2");
      expect(post.mock.calls[1][1]).toEqual({ revision: 7 });
      expect(result).toMatchObject({
        id: "c9",
        revision: 6,
        idempotencyKey: "compose-1",
        shotIds: [3],
        status: "succeeded",
        asset: { id: "54", url: "/final.mp4", durationMs: 2500 },
      });
      expect(await listRemakeCompositions("p9")).toEqual([result]);
      expect(get.mock.calls[0][0]).toBe("/remake-plans/p9/compositions");
    } finally {
      post.mockRestore();
      get.mockRestore();
    }
  });
});
