import { useEffect, useState } from "react";

/**
 * 分拍挂载：返回当前放行到第几拍（0 起），每拍隔一帧推进一次，直到 last。
 * 进入一个很重的页面时，不要一帧把所有区域都挂完——先挂骨架（第 0 拍，立即），再挂用户马上要看的主体，
 * 最后挂最重的细节，用「phase >= n」决定第 n 拍的区域有没有挂。卸载后不再推进。
 * @param last 最后一拍的序号
 */
export function usePhasedMount(last: number): number {
  const [phase, setPhase] = useState(0);
  useEffect(() => {
    if (phase >= last) return;
    const frame = requestAnimationFrame(() => setPhase((value) => Math.min(value + 1, last)));
    return () => cancelAnimationFrame(frame);
  }, [phase, last]);
  return Math.min(phase, last);
}

/**
 * 长列表分批追加：返回此刻该画前几项。先给第一批（马上能看到），其余每帧追加一批，直到 total。
 * 已经画出来的不会收回（列表后来变长时接着往上追加，变短时不超过新的总数）。
 * 不做虚拟列表：资产库、片段条要拖动排序 / 拖进提示词，虚拟列表会和这些交互打架，分批只是别在同一帧画完。
 * @param total 一共多少项
 * @param options.first 第一批画几项
 * @param options.batch 之后每帧追加几项
 */
export function useStaggeredCount(
  total: number,
  { first, batch }: { first: number; batch: number },
): number {
  const [count, setCount] = useState(first);
  const shown = Math.min(Math.max(count, Math.min(first, total)), total);
  useEffect(() => {
    if (shown >= total) return;
    const frame = requestAnimationFrame(() => setCount(Math.min(shown + batch, total)));
    return () => cancelAnimationFrame(frame);
  }, [shown, total, batch]);
  return shown;
}
