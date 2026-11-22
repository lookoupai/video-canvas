import { useEffect, useRef } from "react";
import { Bot, ScanSearch } from "lucide-react";

import type { DramaReference } from "@/api/drama/reference-type";
import { Checkbox } from "@/components/ui/checkbox";
import type { AgentEmptyCopy } from "@/pages/canvas/agent/agent-empty";
import { AgentPanel } from "@/pages/canvas/agent/agent-panel";
import { useAgentController, useAgentModels } from "@/hooks/use-agent-controller";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { subscribeLiveAgentEvents } from "@/store/agent";

const NO_NODES: never[] = [];

const EMPTY_COPY: AgentEmptyCopy = {
  title: "参考剧集助手",
  subtitle: null,
  starters: [{ icon: ScanSearch, text: "分析当前选择的参考集" }],
  hint: null,
};

/** 参考页助手在原片工作台内发起分析，并复用短剧 Agent 的会话与审批状态。 */
export function ReferenceAssistantPane({
  canvasId,
  projectId,
  reference,
  selectedShotIDs,
  onShotSelectionChange,
  onAnalysisChanged,
}: {
  canvasId: string;
  projectId: string;
  reference: DramaReference | null;
  selectedShotIDs: string[];
  onShotSelectionChange: (shotID: string, checked: boolean) => void;
  onAnalysisChanged: () => Promise<void>;
}) {
  const models = useAgentModels();
  const pageContext = reference
    ? {
        kind: "drama_reference" as const,
        project_id: projectId,
        reference_id: reference.id,
        reference_revision: reference.revision,
        reference_title: reference.title,
        reference_seq: reference.seq,
        selected_shot_ids: selectedShotIDs,
      }
    : undefined;
  const ctl = useAgentController({ canvasId, models: models ?? [], mode: "drama", pageContext });
  const refreshRef = useRef(onAnalysisChanged);

  useEffect(() => {
    refreshRef.current = onAnalysisChanged;
  }, [onAnalysisChanged]);

  useEffect(
    () =>
      subscribeLiveAgentEvents((event) => {
        if (event.canvas_id !== canvasId || event.type !== "tool.end" || event.data.is_error)
          return;
        if (
          event.data.name === "drama_reference_draft" ||
          event.data.name === "drama_reference_prepare"
        ) {
          void refreshRef.current();
        }
      }),
    [canvasId],
  );

  if (models === null) {
    return (
      <div className="grid gap-3 p-4" aria-busy>
        <Skeleton className="h-6 w-32 motion-reduce:animate-none" />
        <Skeleton className="h-24 w-full motion-reduce:animate-none" />
      </div>
    );
  }
  if (models.length === 0) {
    return (
      <div className="flex min-h-72 flex-col items-center justify-center gap-2 p-6 text-center">
        <Bot className="text-muted-foreground size-5" aria-hidden="true" />
        <p className="text-sm font-medium">暂时没有可用的 Agent 模型</p>
      </div>
    );
  }

  const canAnalyze =
    reference !== null &&
    reference.status !== "queued" &&
    !ctl.busy &&
    !ctl.sending &&
    (!reference.document?.shots.length || selectedShotIDs.length > 0);
  const analyze = () => {
    if (!reference || !canAnalyze) return;
    const message = [
      `分析参考集「第 ${reference.seq} 集 · ${reference.title}」。`,
      `reference_id=${reference.id}，reference_revision=${reference.revision}。`,
      "按页面选中的镜头范围分析。",
      "先读取已有参考内容，再准备并检查逐镜帧图，保留现有镜头边界，把有证据的观察和可编辑提示词保存为独立草稿。不要改写正式参考稿。",
    ].join("\n");
    void ctl.send(message);
  };

  return (
    <section className="flex min-h-[32rem] min-w-0 flex-col" aria-label="参考剧集助手">
      <div className="border-border flex flex-wrap items-center gap-2 border-b p-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">
            {reference ? `第 ${reference.seq} 集 · ${reference.title}` : "未选择参考集"}
          </p>
          {reference && (
            <p className="text-muted-foreground text-xs">参考修订 {reference.revision}</p>
          )}
        </div>
        <Button
          size="sm"
          disabled={!canAnalyze}
          title={
            reference?.document?.shots.length && selectedShotIDs.length === 0
              ? "至少选择一个镜头"
              : undefined
          }
          onClick={analyze}
        >
          <ScanSearch />
          分析当前集
        </Button>
      </div>
      {reference?.document && (
        <div className="border-border grid gap-2 border-b p-3">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground mr-auto text-xs">
              镜头范围 · {selectedShotIDs.length}/{reference.document.shots.length}
            </span>
            <Button
              size="xs"
              variant="ghost"
              onClick={() =>
                reference.document?.shots.forEach((shot) => onShotSelectionChange(shot.id, true))
              }
            >
              全选
            </Button>
            <Button
              size="xs"
              variant="ghost"
              onClick={() =>
                selectedShotIDs.forEach((shotID) => onShotSelectionChange(shotID, false))
              }
            >
              清空
            </Button>
          </div>
          <div
            className="grid max-h-40 gap-1 overflow-y-auto"
            role="group"
            aria-label="选择分析镜头"
          >
            {reference.document.shots.map((shot, index) => (
              <div key={shot.id} className="flex min-h-8 min-w-0 items-center gap-2 text-xs">
                <Checkbox
                  checked={selectedShotIDs.includes(shot.id)}
                  onCheckedChange={(checked) => onShotSelectionChange(shot.id, checked === true)}
                  aria-label={`分析镜头 ${index + 1} · ${shot.action || "待分析"}`}
                />
                <span className="min-w-0 truncate">
                  {index + 1}. {shot.action || "待分析"}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
      <div className="min-h-[28rem] flex-1">
        <AgentPanel
          variant="embedded"
          label="短剧助手"
          ctl={ctl}
          models={models}
          selection={NO_NODES}
          nodes={NO_NODES}
          refNodes={NO_NODES}
          emptyCopy={EMPTY_COPY}
        />
      </div>
    </section>
  );
}
