import { afterAll, afterEach, expect, spyOn, test } from "bun:test";
import service from "@/utils/requests/service";
import {
  getDramaReferences,
  importDramaReference,
  saveDramaReference,
} from "@/api/drama/reference";

const get = spyOn(service, "get");
const upload = spyOn(service, "upload");
const patch = spyOn(service, "patch");
afterAll(() => {
  get.mockRestore();
  upload.mockRestore();
  patch.mockRestore();
});
afterEach(() => {
  get.mockReset();
  upload.mockReset();
  patch.mockReset();
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
