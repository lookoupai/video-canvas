import { describe, expect, test } from "bun:test";

import { canvasEntryReducer, type CanvasEntry } from "@/utils/drama/canvas-entry";

/**
 * 片场「列表 | 画布」的状态。列表和画布都不卸载（像资产管理：画布一直挂着，列表只是盖在上面），
 * 切换只改看哪一面；画布在片场进入后的空闲时间后台挂好（mounted），点的时候已经在了，不用等。
 * 「进入画布查看」带一个目标节点，画布显示后飞过去（有过渡动画）。
 */

const start: CanvasEntry = { view: "list", mounted: false, target: null };

describe("canvasEntryReducer", () => {
  test("空闲时后台挂载画布：只挂载，仍然看着列表", () => {
    expect(canvasEntryReducer(start, { type: "mount" })).toEqual({
      view: "list",
      mounted: true,
      target: null,
    });
  });

  test("已经挂载过了：再挂载不变（同一个对象，不触发重渲染）", () => {
    const mounted = canvasEntryReducer(start, { type: "mount" });
    expect(canvasEntryReducer(mounted, { type: "mount" })).toBe(mounted);
  });

  test("点「画布」：切到画布面，没有目标节点；还没挂载的顺带挂载", () => {
    expect(canvasEntryReducer(start, { type: "show", target: null })).toEqual({
      view: "canvas",
      mounted: true,
      target: null,
    });
  });

  test("点「进入画布查看」：切到画布面并带上目标节点（画布显示后飞过去）", () => {
    const mounted = canvasEntryReducer(start, { type: "mount" });
    expect(canvasEntryReducer(mounted, { type: "show", target: "b1" })).toEqual({
      view: "canvas",
      mounted: true,
      target: "b1",
    });
  });

  test("已经在画布面又点「进入画布查看」：换一个目标，继续飞", () => {
    const inCanvas: CanvasEntry = { view: "canvas", mounted: true, target: null };
    expect(canvasEntryReducer(inCanvas, { type: "show", target: "b2" }).target).toBe("b2");
  });

  test("飞过去了：清掉目标，不再重复飞", () => {
    const inCanvas: CanvasEntry = { view: "canvas", mounted: true, target: "b1" };
    expect(canvasEntryReducer(inCanvas, { type: "focused" })).toEqual({
      view: "canvas",
      mounted: true,
      target: null,
    });
    const idle: CanvasEntry = { view: "canvas", mounted: true, target: null };
    expect(canvasEntryReducer(idle, { type: "focused" })).toBe(idle);
  });

  test("点「列表」：回到列表面，画布仍然挂着（下次切过去零成本），目标清掉", () => {
    const inCanvas: CanvasEntry = { view: "canvas", mounted: true, target: "b1" };
    expect(canvasEntryReducer(inCanvas, { type: "show-list" })).toEqual({
      view: "list",
      mounted: true,
      target: null,
    });
    expect(canvasEntryReducer(start, { type: "show-list" })).toBe(start);
  });

  test("画布加载失败（读取出错、被别的标签页占着编辑锁）：卸载画布，回列表", () => {
    const inCanvas: CanvasEntry = { view: "canvas", mounted: true, target: "b1" };
    expect(canvasEntryReducer(inCanvas, { type: "fail" })).toEqual(start);
    const bg: CanvasEntry = { view: "list", mounted: true, target: null };
    expect(canvasEntryReducer(bg, { type: "fail" })).toEqual(start);
  });
});
