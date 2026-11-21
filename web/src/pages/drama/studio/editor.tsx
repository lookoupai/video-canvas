import { motion } from "motion/react";
import { CircleX, Loader2, Trash2 } from "lucide-react";

import { StatusPill } from "@/components/drama/status-pill";
import { Button } from "@/components/ui/button";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { hasVideo, latestError } from "@/utils/drama/segments";
import { taskErrorMessage } from "@/utils/tasks/error-message";

import { PromptPane } from "./prompt-pane";
import { RefsBar } from "./refs-bar";
import { RemakePanel } from "./remake-panel";
import { useStudio } from "./use-studio";

/** 右侧检查器：撑满所在分栏（宽度由用户拖动），自己滚动 */
const INSPECTOR = "bg-card h-full w-full min-w-0 overflow-x-hidden overflow-y-auto";

/** 编辑区标题「第 N 集 · 第 M 段」：M 是当前选中的片段序号（和底部片段条格子上的序号一致），随选中片段变 */
export function EditorHeading({ no, seq }: { no: number; seq: number }) {
  return (
    <h2 className="font-display min-w-0 truncate text-[17px] font-bold tracking-wide">
      第 {no} 集 · 第 {seq + 1} 段
    </h2>
  );
}

/** 右侧编辑区：标题（第几集第几段）、参考素材条、提示词、失败提示（时长、视频模型和生成按钮在顶栏） */
export function Editor() {
  const { selected, actions, segmentsStatus, no } = useStudio();

  if (!selected) {
    return (
      <section className={INSPECTOR}>
        <p className="text-muted-foreground mt-20 px-6 text-center text-sm">
          {segmentsStatus === "loading"
            ? "加载中…"
            : segmentsStatus === "error"
              ? "片段读取失败，请刷新页面重试"
              : "这一集还没有片段，点下方时间线末尾的「+」新建一个"}
        </p>
      </section>
    );
  }

  const error = latestError(selected);
  const out = hasVideo(selected);

  return (
    <section aria-label={`第 ${no} 集 片段 ${selected.seq + 1}`} className={INSPECTOR}>
      <motion.div
        key={selected.id}
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        className="flex min-h-full flex-col gap-4 px-4 pt-4 pb-4"
      >
        <div className="flex items-baseline gap-2">
          <EditorHeading no={no} seq={selected.seq} />
          <span className="flex-1" />
          {selected.status === "error" && <StatusPill tone="destructive">生成失败</StatusPill>}
          {selected.status === "running" && (
            <StatusPill tone="running">
              <Loader2 className="animate-spin" />
              生成中
            </StatusPill>
          )}
          {selected.status !== "running" && selected.status !== "error" && out && (
            <StatusPill tone="success">已出视频</StatusPill>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="删除这个片段"
            title="删除这个片段"
            onClick={() => void actions.remove(selected)}
          >
            <Trash2 />
          </Button>
        </div>

        <RefsBar />

        <RemakePanel />

        {error && (
          <div
            role="status"
            aria-label="上次生成失败"
            className="bg-destructive/8 border-destructive/25 flex shrink-0 items-start gap-2 rounded-lg border px-3 py-2 text-sm"
          >
            <CircleX className="text-destructive mt-0.5 size-4 shrink-0" />
            <div className="min-w-0 [overflow-wrap:anywhere]">
              <p className="font-medium">上次生成失败</p>
              <p className="text-muted-foreground mt-1">{taskErrorMessage(error)}</p>
            </div>
          </div>
        )}

        <PromptPane key={selected.id} segment={selected} />
      </motion.div>
    </section>
  );
}
