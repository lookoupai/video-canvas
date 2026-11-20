import { describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";

import type { Segment } from "@/api/drama/type";
import type { Capabilities } from "@/api/model/type";
import {
  adaptState,
  batchChoice,
  formatLabel,
  type BatchSplit,
  sha256Hex,
  sha256HexSync,
  splitBatch,
  submitTextFor,
  effectiveView,
  targetFormat,
} from "@/utils/drama/adaptation";

const caps = (prompt_format?: Capabilities["prompt_format"]): Capabilities => ({
  refs: {
    image: { on: true, max: 9, max_mb: 10 },
    audio: { on: false, max: 0, max_mb: 0 },
    video: { on: false, max: 0, max_mb: 0 },
  },
  prompt: { max_length: 2000 },
  ...(prompt_format === undefined ? {} : { prompt_format }),
});

const seg = (patch: Partial<Segment> = {}): Segment => ({
  id: "s1",
  episodeNo: 1,
  seq: 0,
  seconds: 10,
  prompt: "原稿",
  takes: [],
  chosenTaskId: "",
  status: "idle",
  revision: 1,
  updatedAt: "",
  adaptations: {},
  ...patch,
});

const ad = (prompt: string, sourceHash: string, edited = false) => ({
  prompt,
  sourceHash,
  edited,
  updatedAt: "2026-11-09T10:00:00Z",
});

describe("目标格式", () => {
  test("Seedance 模型不需要转写", () => {
    expect(targetFormat(caps("seedance"))).toBe("seedance");
  });
  test("H3 模型转写为 h3", () => {
    expect(targetFormat(caps("h3"))).toBe("h3");
  });
  test("没配格式（空 / 缺省）按通用", () => {
    expect(targetFormat(caps(""))).toBe("generic");
    expect(targetFormat(caps())).toBe("generic");
  });
  test("还没有模型时不显示转写（当作 Seedance）", () => {
    expect(targetFormat(undefined)).toBe("seedance");
  });
  test("标签用短名", () => {
    expect(formatLabel("seedance")).toBe("Seedance");
    expect(formatLabel("h3")).toBe("H3");
    expect(formatLabel("generic")).toBe("通用");
  });
});

describe("sha256Hex", () => {
  test("与后端一致：UTF-8 全文的小写 hex", async () => {
    expect(await sha256Hex("abc")).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
    const text = "【场景】@[垂钓台](n_1)";
    expect(await sha256Hex(text)).toBe(createHash("sha256").update(text, "utf8").digest("hex"));
  });

  test("没有 crypto.subtle（http + 局域网 IP 访问）时用纯 JS 实现，结果一致", () => {
    for (const text of ["", "abc", "【场景】@[垂钓台](n_1) 😀", "x".repeat(1000)]) {
      expect(sha256HexSync(text)).toBe(createHash("sha256").update(text, "utf8").digest("hex"));
    }
  });
});

describe("转写稿状态", () => {
  test("没有这个格式的转写稿 = 未转写", () => {
    expect(adaptState(seg(), "h3", "h")).toBe("none");
  });
  test("指纹与当前原稿一致 = 最新；不一致 = 已过期", () => {
    const s = seg({ adaptations: { h3: ad("x", "h1") } });
    expect(adaptState(s, "h3", "h1")).toBe("fresh");
    expect(adaptState(s, "h3", "h2")).toBe("stale");
  });
  test("别的格式的转写稿不算", () => {
    const s = seg({ adaptations: { generic: ad("x", "h1") } });
    expect(adaptState(s, "h3", "h1")).toBe("none");
  });
});

describe("批量拆分", () => {
  test("分出已就绪、未转写、已过期，并数出手改过的过期稿", () => {
    const list = [
      seg({ id: "a", prompt: "A", adaptations: { h3: ad("ta", "hA") } }),
      seg({ id: "b", prompt: "B" }),
      seg({ id: "c", prompt: "C", adaptations: { h3: ad("tc", "old", true) } }),
      seg({ id: "d", prompt: "D", adaptations: { h3: ad("td", "old") } }),
    ];
    const hashes: Record<string, string> = { a: "hA", b: "hB", c: "hC", d: "hD" };
    const split = splitBatch(list, "h3", (s) => hashes[s.id]);
    expect(split.ready.map((s) => s.id)).toEqual(["a"]);
    expect(split.none.map((s) => s.id)).toEqual(["b"]);
    expect(split.stale.map((s) => s.id)).toEqual(["c", "d"]);
    expect(split.edited).toBe(1);
  });
});

describe("批量前的转写选择弹窗", () => {
  const split = (ready: number, none: number, stale: number, edited = 0): BatchSplit => ({
    ready: Array.from({ length: ready }, (_, i) => seg({ id: `r${i}` })),
    none: Array.from({ length: none }, (_, i) => seg({ id: `n${i}` })),
    stale: Array.from({ length: stale }, (_, i) => seg({ id: `s${i}` })),
    edited,
  });

  test("推荐的「转写后生成」排第一，各项标出会生成几个", () => {
    const view = batchChoice(split(2, 3, 1), "h3");
    expect(view.title).toBe("4 个片段还没转写为 H3");
    expect(view.options.map((o) => [o.value, o.label, o.count, o.primary ?? false])).toEqual([
      ["adapt", "转写后生成", 6, true],
      ["source", "原稿生成", 6, false],
      ["ready", "只生成已转写的", 2, false],
    ]);
  });

  test("统计只列不为零的项", () => {
    expect(batchChoice(split(0, 4, 0), "h3").stats).toEqual([{ label: "未转写", value: 4 }]);
    expect(batchChoice(split(2, 0, 1), "h3").stats).toEqual([
      { label: "已就绪", value: 2 },
      { label: "已过期", value: 1 },
    ]);
  });

  test("一个都没转写好时，不给「只生成已转写的」", () => {
    const values = batchChoice(split(0, 4, 0), "h3").options.map((o) => o.value);
    expect(values).toEqual(["adapt", "source"]);
  });

  test("有手改过的过期稿：提示重新转写会覆盖，没有就不提", () => {
    expect(batchChoice(split(0, 0, 2, 1), "h3").note).toBe("1 个转写稿被手改过，重新转写会覆盖");
    expect(batchChoice(split(0, 0, 2), "h3").note).toBeUndefined();
  });
});

describe("effectiveView：此刻用哪份文字生成", () => {
  const h3 = { h3: ad("转写", "h") };

  test("视频模型本身吃 Seedance 原稿：只有原稿，不管选了什么", () => {
    expect(effectiveView("seedance", "adapted", seg())).toBe("source");
    expect(effectiveView("seedance", null, seg())).toBe("source");
  });

  test("选了哪个标签就用哪份：选原稿用原稿，选转写用转写稿", () => {
    expect(effectiveView("h3", "source", seg({ adaptations: h3 }))).toBe("source");
    expect(effectiveView("h3", "adapted", seg({ adaptations: h3 }))).toBe("adapted");
  });

  test("还没手动选过：有转写稿默认看转写（同面板默认），没有就看原稿", () => {
    expect(effectiveView("h3", null, seg({ adaptations: h3 }))).toBe("adapted");
    expect(effectiveView("h3", null, seg())).toBe("source");
  });

  test("选了转写但这个片段没有转写稿：仍按「转写」（调用方据此提示先转写），不偷偷退回原稿", () => {
    expect(effectiveView("h3", "adapted", seg())).toBe("adapted");
  });
});

describe("submitTextFor：按此刻用的那份文字取内容", () => {
  const h3 = { h3: ad("转写", "h") };

  test("用原稿：交原稿", () => {
    expect(submitTextFor(seg({ adaptations: h3 }), "h3", "source")).toBe("原稿");
  });

  test("用转写：交转写稿，没有为空串", () => {
    expect(submitTextFor(seg({ adaptations: h3 }), "h3", "adapted")).toBe("转写");
    expect(submitTextFor(seg(), "h3", "adapted")).toBe("");
  });

  test("视频模型是 Seedance：永远交原稿", () => {
    expect(submitTextFor(seg({ adaptations: h3 }), "seedance", "adapted")).toBe("原稿");
  });
});
