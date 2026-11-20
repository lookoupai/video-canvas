import { create } from "zustand";

import type { DramaImageModels } from "@/api/drama/type";

/** 会跟着换模型的只有三类资产，故事板的模型不在内 */
type Slot = Exclude<keyof DramaImageModels, "board">;

type ModelSwitchState = {
  /** 每部短剧待换的模型：资产类型 → 新模型 key（空串 = 跟随默认，即清单第一个） */
  byDrama: Record<string, Partial<Record<Slot, string>>>;
  /** 项目设置保存了新的图片模型后登记，资产区挂载着时把还没生成的卡换过去 */
  request: (dramaId: string, changes: Partial<Record<Slot, string>>) => void;
  /** 这一类的卡都换好了 */
  done: (dramaId: string, slot: Slot) => void;
};

/**
 * 项目设置 → 资产画布的「换图片模型」请求。项目设置在任何工序页都能改，资产画布只在资产页挂载，
 * 所以先记在这里，等资产区挂载、节点登记好后再换。
 */
export const useModelSwitchStore = create<ModelSwitchState>((set) => ({
  byDrama: {},
  request: (dramaId, changes) =>
    set((state) => ({
      byDrama: { ...state.byDrama, [dramaId]: { ...state.byDrama[dramaId], ...changes } },
    })),
  done: (dramaId, slot) =>
    set((state) => {
      const { [slot]: _, ...rest } = state.byDrama[dramaId] ?? {};
      const byDrama = { ...state.byDrama };
      if (Object.keys(rest).length > 0) byDrama[dramaId] = rest;
      else delete byDrama[dramaId];
      return { byDrama };
    }),
}));
