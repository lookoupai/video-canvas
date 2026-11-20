/**
 * 片场「列表 | 画布」的状态。像资产管理那样：列表和画布都不卸载，切换只改看哪一面（交叉淡入，没有卸载 / 挂载的一帧卡顿和闪烁）。
 * 画布在片场进入后的空闲时间后台挂好（mounted），用户点的时候已经在了，不用等。
 * 「进入画布查看」带一个目标节点：画布显示后视口过渡着飞过去（能看到从当前位置到节点的过程）。
 */
export type CanvasEntry = {
  /** 此刻看哪一面 */
  view: "list" | "canvas";
  /** 画布有没有挂载：挂上之后一直留着，直到加载失败或离开片场 */
  mounted: boolean;
  /** 要飞到的节点；没有为 null */
  target: string | null;
};

export type CanvasEntryAction =
  /** 空闲时后台挂载画布（仍然看着列表） */
  | { type: "mount" }
  /** 切到画布面，可以带要飞到的节点；还没挂载的顺带挂载 */
  | { type: "show"; target: string | null }
  /** 画布里已经飞到目标节点（或放弃了） */
  | { type: "focused" }
  /** 切回列表面（画布仍然挂着） */
  | { type: "show-list" }
  /** 画布没能加载出来：卸载画布、回列表 */
  | { type: "fail" };

export function canvasEntryReducer(state: CanvasEntry, action: CanvasEntryAction): CanvasEntry {
  switch (action.type) {
    case "mount":
      return state.mounted ? state : { ...state, mounted: true };
    case "show":
      return { view: "canvas", mounted: true, target: action.target };
    case "focused":
      return state.target === null ? state : { ...state, target: null };
    case "show-list":
      return state.view === "list" ? state : { view: "list", mounted: state.mounted, target: null };
    case "fail":
      return { view: "list", mounted: false, target: null };
  }
}
