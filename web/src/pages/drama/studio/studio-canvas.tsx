import { useCallback, useRef } from "react";

import type { CanvasEditorCommands } from "@/components/canvas-editor/editor-api";
import { useApplyCanvasFocus } from "@/hooks/use-apply-canvas-focus";
import type { CanvasNode } from "@/types";

import { CanvasTools } from "../workspace/assets-overlay";
import { DramaCanvas, withNewDrama } from "../workspace/assets-area";
import { useStudio } from "./use-studio";

/** 编辑器插槽里的元素放在模块顶层，引用不变，页面重渲染时不会牵动编辑器：片场的画布只要底部工具条和视图控件 */
const overlay = <CanvasTools />;

/** 片场里新建的节点和资产管理里画布视图新建的一样：默认「全集」 */
const onNodeCreate = (node: CanvasNode) => withNewDrama(node, null);

/**
 * 片场的画布面：当前这张短剧画布（和资产管理里是同一张），在片场里直接看、改故事板节点。
 * 像资产管理那样一直挂着（片场进入后的空闲时间后台挂好），切换只改可见性，没有卸载 / 挂载的卡顿和闪烁；
 * 编辑器要拿编辑锁，所以只在需要时才挂，离开片场才卸载。
 *
 * 从故事板点「进入画布查看」时，画布显示出来后视口**过渡着飞到**那个节点（和资产管理里点卡片一样，
 * 能看到从当前位置到节点的过程）。目标只在画布看得见的时候才飞，飞完（或放弃）清掉。
 */
export function StudioCanvas() {
  const { canvasEntry, canvasFocused, canvasFailed } = useStudio();
  const commands = useRef<CanvasEditorCommands | null>(null);
  const shown = canvasEntry.view === "canvas";
  const done = useCallback(() => canvasFocused(), [canvasFocused]);
  useApplyCanvasFocus(shown ? canvasEntry.target : null, commands, done);
  return (
    <div className="relative size-full">
      {/* 绝对定位铺满：编辑器的 h-full 才有确定的高度可依 */}
      <div className="absolute inset-0">
        <DramaCanvas
          overlay={overlay}
          commandsRef={commands}
          onNodeCreate={onNodeCreate}
          onFail={canvasFailed}
        />
      </div>
    </div>
  );
}
