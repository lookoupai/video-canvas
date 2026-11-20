import { describe, expect, test } from "bun:test";

import type { VirtualHumanDto } from "@/api/virtual-human/type";
import type { CanvasNodeData } from "@/types/canvas";
import type { AssetItem } from "@/utils/drama/assets";
import {
  enrollTarget,
  filterHumans,
  humanCounts,
  humanOfAsset,
  humanPickGroups,
  readyImages,
  shouldPoll,
  validateHumanName,
} from "@/utils/virtual-human/virtual-human";

const asset = (id: string) => ({
  id,
  url: `/files/${id}`,
  kind: "image" as const,
  mimeType: "image/png",
  byteSize: 1,
  width: 10,
  height: 10,
  durationMs: null,
  fileName: `${id}.png`,
});

const human = (id: string, statuses: ("pending" | "active" | "failed")[]): VirtualHumanDto => ({
  id,
  name: `人${id}`,
  status: statuses.includes("active")
    ? "active"
    : statuses.includes("pending")
      ? "pending"
      : "failed",
  images: statuses.map((status, i) => ({
    id: `${id}-${i}`,
    asset: asset(`${id}${i}`),
    status,
    error: "",
  })),
  createdAt: "2026-11-18T00:00:00Z",
});

describe("validateHumanName：真人名称", () => {
  test("去首尾空白后 1–30 字", () => {
    expect(validateHumanName("  迅哥儿 ")).toEqual({ ok: true, name: "迅哥儿" });
    expect(validateHumanName("   ").ok).toBe(false);
    expect(validateHumanName("长".repeat(31)).ok).toBe(false);
    expect(validateHumanName("长".repeat(30)).ok).toBe(true);
  });
});

describe("filterHumans / humanCounts：按状态筛选", () => {
  const list = [human("a", ["active", "pending"]), human("b", ["pending"]), human("c", ["failed"])];

  test("全部 / 可用 / 备案中 / 失败", () => {
    expect(filterHumans(list, "all").map((h) => h.id)).toEqual(["a", "b", "c"]);
    expect(filterHumans(list, "active").map((h) => h.id)).toEqual(["a"]);
    expect(filterHumans(list, "pending").map((h) => h.id)).toEqual(["b"]);
    expect(filterHumans(list, "failed").map((h) => h.id)).toEqual(["c"]);
  });

  test("各状态的数量", () => {
    expect(humanCounts(list)).toEqual({ all: 3, active: 1, pending: 1, failed: 1 });
  });
});

describe("shouldPoll：有图片审核中才轮询", () => {
  test("可用的真人里还有审核中的图片也要轮询", () => {
    expect(shouldPoll([human("a", ["active", "pending"])])).toBe(true);
    expect(shouldPoll([human("a", ["active", "failed"])])).toBe(false);
    expect(shouldPoll([])).toBe(false);
  });
});

describe("humanOfAsset / readyImages", () => {
  const list = [human("a", ["active", "pending", "failed"])];

  test("按素材 ID 找到所在的真人", () => {
    expect(humanOfAsset(list).get("a1")?.id).toBe("a");
    expect(humanOfAsset(list).get("zz")).toBeUndefined();
  });

  test("只取可用的图片，按顺序", () => {
    expect(readyImages(list[0]!).map((img) => img.asset?.id)).toEqual(["a0"]);
  });
});

describe("humanPickGroups：资产库弹框的真人库来源", () => {
  const list = [human("a", ["active", "pending", "failed"]), human("b", ["pending"])];

  test("按真人分组列出全部图片，备案中、失败的带不能选的原因", () => {
    const groups = humanPickGroups(list, "");
    expect(groups.map((g) => g.human.id)).toEqual(["a", "b"]);
    expect(groups[0]!.items.map((it) => [it.asset.id, it.reason])).toEqual([
      ["a0", null],
      ["a1", "备案中"],
      ["a2", "备案失败"],
    ]);
  });

  test("按名称搜索（忽略大小写和首尾空白），素材已删除的图片不列", () => {
    const named = [{ ...list[0]!, name: "Lu Xun" }, list[1]!];
    expect(humanPickGroups(named, "  lu ").map((g) => g.human.id)).toEqual(["a"]);
    const gone = [{ ...list[1]!, images: [{ ...list[1]!.images[0]!, asset: null }] }];
    expect(humanPickGroups(gone, "")).toEqual([]);
  });
});

describe("enrollTarget：短剧资产卡能不能加入真人库", () => {
  const item = (overrides: Partial<AssetItem> = {}): AssetItem => ({
    id: "n1",
    type: "character",
    name: "迅哥儿",
    look: "少年",
    label: "迅哥儿",
    episodes: [1],
    status: "done",
    data: { kind: "character", assetId: "11", src: "/files/a.png" } as CanvasNodeData,
    ...overrides,
  });

  test("已生成形象的角色卡：交出形象图，名称默认取角色名", () => {
    expect(enrollTarget(item())).toMatchObject({
      id: "11",
      url: "/files/a.png",
      kind: "image",
      fileName: "迅哥儿",
    });
  });

  test("没生成形象、生成中、失败、不是角色的都不行", () => {
    expect(
      enrollTarget(item({ status: "idle", data: { kind: "character" } as CanvasNodeData })),
    ).toBeNull();
    expect(enrollTarget(item({ status: "running" }))).toBeNull();
    expect(enrollTarget(item({ status: "error" }))).toBeNull();
    expect(enrollTarget(item({ type: "scene" }))).toBeNull();
    expect(
      enrollTarget(item({ data: { kind: "character", src: "/files/a.png" } as CanvasNodeData })),
    ).toBeNull();
  });
});
