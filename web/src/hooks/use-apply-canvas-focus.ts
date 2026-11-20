import { useEffect, useRef, type RefObject } from "react";

import type { CanvasEditorCommands } from "@/components/canvas-editor/editor-api";

/** 编辑器要等画布加载完才有命令表，每帧试一次，最多等这么多帧（约 5 秒） */
const MAX_FRAMES = 300;

/**
 * 片场切到画布视图后兑现「进入画布查看」：等编辑器的命令表里能读到目标节点，再飞过去，然后通知调用方用完了。
 * 目标为 null 时什么都不做；目标节点一直没出现（被删了）就放弃，也会通知，免得下次切回来又飞。
 * @param target 要看的节点 id
 * @param commands 编辑器的命令表（挂载后才有）
 * @param onDone 飞过去了（或放弃了），调用方据此清掉目标
 */
export function useApplyCanvasFocus(
  target: string | null,
  commands: RefObject<CanvasEditorCommands | null>,
  onDone: () => void,
) {
  const done = useRef(onDone);
  useEffect(() => {
    done.current = onDone;
  });
  useEffect(() => {
    if (!target) return;
    let frame = 0;
    let tries = 0;
    const apply = () => {
      if (!commands.current?.getNodes().some((node) => node.id === target)) {
        if (++tries < MAX_FRAMES) frame = requestAnimationFrame(apply);
        else done.current();
        return;
      }
      // 视图切过去、节点渲染出来后再飞，让用户看得到
      requestAnimationFrame(() => {
        commands.current?.focusNode(target);
        done.current();
      });
    };
    apply();
    return () => cancelAnimationFrame(frame);
  }, [target, commands]);
}
