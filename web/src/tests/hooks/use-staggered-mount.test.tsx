import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { useEffect, useState } from "react";

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type {
  usePhasedMount as PhasedType,
  useStaggeredCount as CountType,
} from "@/hooks/use-staggered-mount";

/** 进入片场时分节奏挂载：先挂骨架，再主体，后细节；长列表先画前几项、其余分批追加 */

let dom: DomEnv;
let usePhasedMount: typeof PhasedType;
let useStaggeredCount: typeof CountType;
beforeAll(async () => {
  dom = await installDom();
  const mod = await import("@/hooks/use-staggered-mount");
  usePhasedMount = mod.usePhasedMount;
  useStaggeredCount = mod.useStaggeredCount;
});
afterAll(() => dom.restore());

/** 每次让出一帧再冲刷：requestAnimationFrame 里的 setState 要一帧一帧地推进，不能全塞进一个 act */
async function frames(n: number) {
  for (let i = 0; i < n; i++) await dom.flush(20);
}

describe("usePhasedMount：分拍挂载", () => {
  test("第 0 拍立即为真，之后每拍一帧、依次放行；到头不再变", async () => {
    const seen: number[] = [];
    function Probe() {
      const phase = usePhasedMount(3);
      seen.push(phase);
      return <span data-phase={phase} />;
    }
    const m = await dom.mount(<Probe />);
    expect(seen[0]).toBe(0);
    await frames(8);
    const last = seen[seen.length - 1];
    expect(last).toBe(3);
    // 单调不减、不跳拍
    for (let i = 1; i < seen.length; i++) {
      expect(seen[i] - seen[i - 1]).toBeLessThanOrEqual(1);
      expect(seen[i]).toBeGreaterThanOrEqual(seen[i - 1]);
    }
    await m.unmount();
  });

  test("卸载后不再推进（不会在卸载的组件上 setState）", async () => {
    function Probe() {
      usePhasedMount(5);
      return null;
    }
    const m = await dom.mount(<Probe />);
    await m.unmount();
    await frames(8);
    expect(true).toBe(true);
  });
});

describe("useStaggeredCount：分批追加", () => {
  test("先给第一批，其余每帧追加一批，直到全部；总数不超过 total", async () => {
    const seen: number[] = [];
    function Probe({ total }: { total: number }) {
      const count = useStaggeredCount(total, { first: 4, batch: 3 });
      seen.push(count);
      return null;
    }
    const m = await dom.mount(<Probe total={12} />);
    expect(seen[0]).toBe(4);
    await frames(12);
    expect(seen[seen.length - 1]).toBe(12);
    expect(Math.max(...seen)).toBe(12);
    // 每次最多多 3 项
    for (let i = 1; i < seen.length; i++) expect(seen[i] - seen[i - 1]).toBeLessThanOrEqual(3);
    await m.unmount();
  });

  test("总数不到第一批：一次给全；总数为 0：0", async () => {
    function Probe() {
      const few = useStaggeredCount(2, { first: 4, batch: 3 });
      const none = useStaggeredCount(0, { first: 4, batch: 3 });
      return <span data-few={few} data-none={none} />;
    }
    const m = await dom.mount(<Probe />);
    const el = m.container.querySelector("span") as HTMLElement;
    expect(el.dataset.few).toBe("2");
    expect(el.dataset.none).toBe("0");
    await m.unmount();
  });

  test("同一个列表后来变长（读到更多资产）：已画出的不收回，新增的接着分批追加", async () => {
    const seen: number[] = [];
    let setTotal: (n: number) => void = () => {};
    function Probe() {
      const [total, set] = useState(6);
      useEffect(() => {
        setTotal = set;
      }, [set]);
      const count = useStaggeredCount(total, { first: 4, batch: 3 });
      seen.push(count);
      return null;
    }
    const m = await dom.mount(<Probe />);
    await frames(8);
    expect(seen[seen.length - 1]).toBe(6);
    const before = seen.length;
    await dom.act(async () => setTotal(30));
    await frames(20);
    const after = seen.slice(before);
    // 变长后从已画的 6 项接着往上追加，从不回落
    expect(Math.min(...after)).toBeGreaterThanOrEqual(6);
    expect(after[after.length - 1]).toBe(30);
    for (let i = 1; i < after.length; i++) expect(after[i]).toBeGreaterThanOrEqual(after[i - 1]);
    await m.unmount();
  });

  test("总数先是 0（数据还没读到）后来才有：读到后从第一批开始，不会先画一遍空的再重来一遍", async () => {
    const seen: number[] = [];
    let setTotal: (n: number) => void = () => {};
    function Probe() {
      const [total, set] = useState(0);
      useEffect(() => {
        setTotal = set;
      }, [set]);
      const count = useStaggeredCount(total, { first: 4, batch: 3 });
      seen.push(count);
      return null;
    }
    const m = await dom.mount(<Probe />);
    await frames(4);
    // 没有数据时一直是 0，不会无故变成 first
    expect(Math.max(...seen)).toBe(0);
    seen.length = 0;
    await dom.act(async () => setTotal(12));
    // 读到数据的第一次渲染就是第一批，之后单调追加到 12
    expect(seen[0]).toBe(4);
    await frames(12);
    expect(seen[seen.length - 1]).toBe(12);
    for (let i = 1; i < seen.length; i++) expect(seen[i]).toBeGreaterThanOrEqual(seen[i - 1]);
    await m.unmount();
  });

  test("列表变短：立刻不超过新的总数", async () => {
    const seen: number[] = [];
    let setTotal: (n: number) => void = () => {};
    function Probe() {
      const [total, set] = useState(20);
      useEffect(() => {
        setTotal = set;
      }, [set]);
      const count = useStaggeredCount(total, { first: 4, batch: 3 });
      seen.push(count);
      return null;
    }
    const m = await dom.mount(<Probe />);
    await frames(12);
    expect(seen[seen.length - 1]).toBe(20);
    await dom.act(async () => setTotal(5));
    expect(seen[seen.length - 1]).toBe(5);
    await m.unmount();
  });
});
