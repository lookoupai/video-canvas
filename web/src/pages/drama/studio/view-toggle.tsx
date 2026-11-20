import { Clapperboard } from "lucide-react";

import { ViewToggle } from "../workspace/assets-area";
import { useStudio } from "./use-studio";

/**
 * 片场顶部条左边的「片场 | 画布」切换（和资产管理同款）：画布视图里就是当前这张短剧画布，在片场里直接看、改故事板节点。
 * 两面都不卸载，点了只是换看哪一面（交叉淡入）。
 */
export function StudioViewToggle() {
  const { canvasEntry, showCanvas, showList } = useStudio();
  return (
    <ViewToggle
      view={canvasEntry.view}
      listLabel="片场"
      listIcon={Clapperboard}
      onChange={(next) => (next === "canvas" ? showCanvas() : showList())}
    />
  );
}
