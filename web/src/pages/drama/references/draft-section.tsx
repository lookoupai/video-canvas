import { useCallback, useEffect, useRef, useState } from "react";
import {
  Calculator,
  ChevronLeft,
  ChevronRight,
  Play,
  RefreshCw,
  Save,
  Square,
  WandSparkles,
} from "lucide-react";
import { Link } from "react-router";

import { getModels } from "@/api/model";
import type { ModelInfo } from "@/api/model/type";
import {
  cancelDramaReferencePreparation,
  applyDramaReferenceDraft,
  listDramaReferenceDrafts,
  listDramaReferencePreparations,
  quoteDramaReferenceDraft,
  resumeDramaReferencePreparation,
  updateDramaReferenceDraft,
} from "@/api/drama/reference";
import type {
  DramaReference,
  ReferenceDraft,
  ReferenceDraftApplyReceipt,
  ReferenceDraftGenerateReceipt,
  ReferenceDraftGenerateTask,
  ReferencePlan,
  ReferencePlanRequest,
} from "@/api/drama/reference-type";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Textarea } from "@/components/ui/textarea";
import { useTask } from "@/store/tasks";
import { submitReferenceDraftTasks } from "@/utils/tasks/gateway";

const SHOTS_PER_PAGE = 20;
const stableRequestKeys = new Map<string, string>();

function GenerationRow({ item }: { item: ReferenceDraftGenerateTask }) {
  const task = useTask(item.taskId);
  return (
    <li>
      {item.shotId} · {task?.status ?? item.status}
      {item.taskId && ` · 任务 ${item.taskId}`}
      {(task?.error_message || item.error) && (
        <span className="text-destructive"> · {task?.error_message || item.error}</span>
      )}
    </li>
  );
}

function stableRequestKey(scope: string, referenceId: string, draftId: string, digest: string) {
  const storageKey = `vc.reference.${scope}.${referenceId}.${draftId}.${digest}`;
  const existing = stableRequestKeys.get(storageKey);
  if (existing) return existing;
  let requestKey: string = crypto.randomUUID();
  try {
    requestKey = sessionStorage.getItem(storageKey) || requestKey;
    sessionStorage.setItem(storageKey, requestKey);
  } catch {
    // The in-memory key keeps retries stable when the browser blocks session storage.
  }
  stableRequestKeys.set(storageKey, requestKey);
  return requestKey;
}

/** 将分析草稿、持久准备和逐镜提示词直接放在原片校订工作区。 */
export function ReferenceDraftSection({
  id,
  reference,
  selectedShotIDs,
  refreshVersion,
}: {
  id: string;
  reference: DramaReference | null;
  selectedShotIDs: string[];
  refreshVersion: number;
}) {
  const [drafts, setDrafts] = useState<ReferenceDraft[]>([]);
  const [preparations, setPreparations] = useState<
    Awaited<ReturnType<typeof listDramaReferencePreparations>>
  >([]);
  const [selectedId, setSelectedId] = useState("");
  const [promptEdits, setPromptEdits] = useState<Record<string, Record<string, string>>>({});
  const [shotPage, setShotPage] = useState(0);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveFailed, setSaveFailed] = useState(false);
  const [controlFailed, setControlFailed] = useState(false);
  const [controlling, setControlling] = useState(false);
  const [videoModels, setVideoModels] = useState<ModelInfo[]>([]);
  const [modelId, setModelId] = useState("");
  const [routeMode, setRouteMode] = useState<ReferencePlanRequest["units"][number]["mode"]>("auto");
  const [duration, setDuration] = useState(8);
  const [trimStartMs, setTrimStartMs] = useState(0);
  const [targetDurationMs, setTargetDurationMs] = useState<number | undefined>();
  const [maxCredits, setMaxCredits] = useState("");
  const [plan, setPlan] = useState<ReferencePlan | null>(null);
  const [planRequest, setPlanRequest] = useState<ReferencePlanRequest | null>(null);
  const [application, setApplication] = useState<ReferenceDraftApplyReceipt | null>(null);
  const [generated, setGenerated] = useState<ReferenceDraftGenerateReceipt | null>(null);
  const [applyConfirm, setApplyConfirm] = useState(false);
  const [generateConfirm, setGenerateConfirm] = useState(false);
  const [applying, setApplying] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [generateError, setGenerateError] = useState("");
  const [quoting, setQuoting] = useState(false);
  const resumeKeys = useRef(new Map<string, string>());

  useEffect(() => {
    let active = true;
    void getModels("video")
      .then((items) => {
        if (!active) return;
        setVideoModels(items);
        setModelId((current) => current || items[0]?.key || "");
      })
      .catch(() => {
        if (active) setVideoModels([]);
      });
    return () => {
      active = false;
    };
  }, []);

  const reload = useCallback(async () => {
    if (!reference) return;
    try {
      const [draftPage, prepRows] = await Promise.all([
        listDramaReferenceDrafts(id, reference.id),
        listDramaReferencePreparations(id, reference.id),
      ]);
      setPlan(null);
      setPlanRequest(null);
      setApplication(null);
      setGenerated(null);
      setDrafts(draftPage.items);
      setPreparations(prepRows);
      setSelectedId((current) =>
        draftPage.items.some((draft) => draft.id === current)
          ? current
          : (draftPage.items[0]?.id ?? ""),
      );
    } catch {
      setSaveFailed(true);
    } finally {
      setLoading(false);
    }
  }, [id, reference]);

  useEffect(() => {
    void reload();
  }, [reload, refreshVersion]);

  const draft = drafts.find((item) => item.id === selectedId) ?? null;

  const runningPreparation = preparations.find(
    (item) => item.status === "queued" || item.status === "running",
  );
  useEffect(() => {
    if (!runningPreparation) return;
    const timer = setInterval(() => void reload(), 2000);
    return () => clearInterval(timer);
  }, [reload, runningPreparation]);

  const savedPrompts = Object.fromEntries(
    draft?.prompts.map((item) => [item.shotId, item.prompt]) ?? [],
  );
  const promptValues = { ...savedPrompts, ...(draft ? promptEdits[draft.id] : {}) };

  const shots = draft?.document.shots ?? [];
  const pageCount = Math.max(1, Math.ceil(shots.length / SHOTS_PER_PAGE));
  const pageShots = shots.slice(shotPage * SHOTS_PER_PAGE, (shotPage + 1) * SHOTS_PER_PAGE);
  const changed = (() => {
    if (!draft) return false;
    const saved = new Map(draft.prompts.map((item) => [item.shotId, item.prompt]));
    return draft.document.shots.some(
      (shot) => (promptValues[shot.id] ?? "") !== (saved.get(shot.id) ?? ""),
    );
  })();
  const reviewCount =
    draft?.observations.filter(
      (item) => item.reviewState === "inferred" || item.reviewState === "unknown",
    ).length ?? 0;
  const baseShots = new Map(reference?.document?.shots.map((shot) => [shot.id, shot]) ?? []);
  const changedShotCount =
    draft?.document.shots.filter((shot) => {
      const base = baseShots.get(shot.id);
      return base && (base.action !== shot.action || base.camera !== shot.camera);
    }).length ?? 0;
  const quotedShots = shots.filter((shot) => selectedShotIDs.includes(shot.id));
  const validDuration = Number.isSafeInteger(duration) && duration >= 1 && duration <= 86400;
  const validTrim = Number.isSafeInteger(trimStartMs) && trimStartMs >= 0;
  const validTargetDuration =
    targetDurationMs === undefined ||
    (Number.isSafeInteger(targetDurationMs) && targetDurationMs >= 100);
  const validMaxCredits =
    maxCredits === "" || (Number.isSafeInteger(Number(maxCredits)) && Number(maxCredits) >= 0);
  const quoteIsCurrent =
    plan !== null &&
    draft !== null &&
    reference !== null &&
    plan.draftId === draft.id &&
    plan.referenceId === reference.id &&
    plan.draftRevision === draft.revision &&
    plan.modelId === modelId &&
    plan.units.map((unit) => unit.shotId).join("\0") ===
      quotedShots.map((shot) => shot.id).join("\0");

  const controlPreparation = async (preparationId: string, action: "cancel" | "resume") => {
    if (!reference || controlling) return;
    setControlling(true);
    setControlFailed(false);
    try {
      if (action === "cancel") {
        await cancelDramaReferencePreparation(id, reference.id, preparationId);
      } else {
        const requestKey = resumeKeys.current.get(preparationId) ?? crypto.randomUUID();
        resumeKeys.current.set(preparationId, requestKey);
        await resumeDramaReferencePreparation(id, reference.id, preparationId, requestKey);
      }
      await reload();
    } catch {
      setControlFailed(true);
    } finally {
      setControlling(false);
    }
  };

  const save = async () => {
    if (!draft || saving || !changed) return;
    setSaving(true);
    setSaveFailed(false);
    try {
      const prompts = shots
        .map((shot) => ({ shotId: shot.id, prompt: (promptValues[shot.id] ?? "").trim() }))
        .filter((item) => item.prompt !== "");
      const updated = await updateDramaReferenceDraft(id, draft.referenceId, draft.id, {
        revision: draft.revision,
        prompts,
      });
      setDrafts((rows) => rows.map((row) => (row.id === updated.id ? updated : row)));
      setPlan(null);
      setPlanRequest(null);
      setApplication(null);
      setGenerated(null);
      setPromptEdits((values) => {
        const next = { ...values };
        delete next[draft.id];
        return next;
      });
    } catch {
      setSaveFailed(true);
      void reload();
    } finally {
      setSaving(false);
    }
  };

  const quote = async () => {
    if (!draft || !reference || changed || quoting || !modelId || quotedShots.length === 0) return;
    setQuoting(true);
    try {
      const imageLimit =
        videoModels.find((item) => item.key === modelId)?.capabilities.refs.image.max ?? 8;
      const imageCount =
        routeMode === "keyframe" ? 1 : routeMode === "image" ? Math.min(imageLimit, 8) : 0;
      const request: ReferencePlanRequest = {
        revision: draft.revision,
        modelId,
        units: quotedShots.map((shot) => ({
          shotId: shot.id,
          mode: routeMode,
          ...(imageCount > 0 && {
            imageAssetIDs: draft.evidence
              .filter((item) => item.shotId === shot.id)
              .slice(0, imageCount)
              .map((item) => String(item.assetId)),
          }),
          duration: duration,
          trimStartMs,
          targetDurationMs: targetDurationMs ?? shot.endMs - shot.startMs,
        })),
        ...(maxCredits !== "" && { maxCredits: Number(maxCredits) }),
      };
      setPlanRequest(request);
      const result = await quoteDramaReferenceDraft(id, reference.id, draft.id, request);
      setPlan(result);
    } finally {
      setQuoting(false);
    }
  };

  const applyPlan = async () => {
    if (
      !draft ||
      !reference ||
      !plan ||
      !planRequest ||
      !quoteIsCurrent ||
      !plan.executable ||
      applying
    )
      return;
    setApplying(true);
    setApplyError("");
    const requestKey = stableRequestKey("apply", reference.id, draft.id, plan.planDigest);
    try {
      const result = await applyDramaReferenceDraft(id, reference.id, draft.id, {
        revision: draft.revision,
        requestKey,
        planDigest: plan.planDigest,
        mode: "follow",
        quote: planRequest,
      });
      setApplication(result);
      setGenerated(null);
      setApplyConfirm(false);
    } catch (error) {
      setApplyError(error instanceof Error ? error.message : "采用失败，请重试");
    } finally {
      setApplying(false);
    }
  };

  const generatePlan = async () => {
    if (
      !draft ||
      !reference ||
      !plan ||
      !planRequest ||
      !application ||
      !quoteIsCurrent ||
      generating
    )
      return;
    setGenerating(true);
    setGenerateError("");
    const requestKey = stableRequestKey("generate", reference.id, draft.id, plan.planDigest);
    try {
      const result = await submitReferenceDraftTasks(id, reference.id, draft.id, {
        revision: application.draftRevision,
        requestKey,
        applicationId: application.applicationId,
        planDigest: plan.planDigest,
        quote: planRequest,
      });
      setGenerated(result);
      setGenerateConfirm(false);
    } catch (error) {
      setGenerateError(error instanceof Error ? error.message : "提交生成失败，请重试");
    } finally {
      setGenerating(false);
    }
  };

  if (!reference) return null;

  return (
    <section className="border-border min-w-0 space-y-3 border-t py-4" aria-label="分析草稿">
      <header className="flex flex-wrap items-center gap-2">
        <h2 className="mr-auto text-sm font-semibold">分析草稿</h2>
        {drafts.length > 0 && (
          <select
            aria-label="选择分析草稿"
            className="bg-chrome-hover max-w-48 rounded-lg px-2 py-1.5 text-sm"
            value={draft?.id ?? ""}
            onChange={(event) => {
              setSelectedId(event.target.value);
              setPlan(null);
              setPlanRequest(null);
              setApplication(null);
              setGenerated(null);
              setShotPage(0);
            }}
          >
            {drafts.map((item) => (
              <option key={item.id} value={item.id}>
                {item.source === "agent" ? "助手" : item.source === "manual" ? "人工" : "外部"} · v
                {item.revision}
              </option>
            ))}
          </select>
        )}
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label="刷新分析结果"
          title="刷新分析结果"
          onClick={() => {
            setLoading(true);
            void reload();
          }}
        >
          <RefreshCw />
        </Button>
      </header>

      {draft && (
        <section
          className="border-border grid min-w-0 gap-3 border-t py-3"
          aria-label="单镜路线报价"
        >
          <div className="flex flex-wrap items-end gap-2">
            <label className="grid min-w-36 flex-1 gap-1 text-xs">
              <span>视频模型</span>
              <select
                aria-label="报价视频模型"
                className="bg-chrome-hover border-chrome-border h-9 min-w-0 rounded-lg border px-2 text-sm"
                value={modelId}
                onChange={(event) => {
                  setModelId(event.target.value);
                  setPlan(null);
                }}
              >
                <option value="" disabled>
                  选择模型
                </option>
                {videoModels.map((item) => (
                  <option key={item.key} value={item.key}>
                    {item.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="grid gap-1 text-xs">
              <span>生成路线</span>
              <select
                aria-label="报价生成路线"
                className="bg-chrome-hover border-chrome-border h-9 rounded-lg border px-2 text-sm"
                value={routeMode}
                onChange={(event) => {
                  setRouteMode(event.target.value as ReferencePlanRequest["units"][number]["mode"]);
                  setPlan(null);
                }}
              >
                <option value="auto">自动</option>
                <option value="video">原片视频</option>
                <option value="keyframe">首帧</option>
                <option value="image">图片参考</option>
                <option value="text">文字</option>
              </select>
            </label>
            <label className="grid w-24 gap-1 text-xs">
              <span>申请秒数</span>
              <input
                aria-label="报价申请生成秒数"
                className="bg-chrome-hover border-chrome-border h-9 rounded-lg border px-2 text-sm"
                type="number"
                min={1}
                max={86400}
                step={1}
                value={duration}
                onChange={(event) => {
                  setDuration(Number(event.target.value));
                  setPlan(null);
                }}
              />
            </label>
            <label className="grid w-28 gap-1 text-xs">
              <span>裁剪起点 ms</span>
              <input
                aria-label="报价裁剪起点毫秒"
                className="bg-chrome-hover border-chrome-border h-9 rounded-lg border px-2 text-sm"
                type="number"
                min={0}
                max={86400000}
                step={100}
                value={trimStartMs}
                onChange={(event) => {
                  setTrimStartMs(Number(event.target.value));
                  setPlan(null);
                }}
              />
            </label>
            <label className="grid w-28 gap-1 text-xs">
              <span>成片时长 ms</span>
              <input
                aria-label="报价成片时长毫秒"
                className="bg-chrome-hover border-chrome-border h-9 rounded-lg border px-2 text-sm"
                type="number"
                min={100}
                max={86400000}
                step={100}
                placeholder="原镜时长"
                value={targetDurationMs ?? ""}
                onChange={(event) => {
                  setTargetDurationMs(
                    event.target.value === "" ? undefined : Number(event.target.value),
                  );
                  setPlan(null);
                }}
              />
            </label>
            <label className="grid w-28 gap-1 text-xs">
              <span>积分上限</span>
              <input
                aria-label="本次报价积分上限"
                className="bg-chrome-hover border-chrome-border h-9 rounded-lg border px-2 text-sm"
                type="number"
                min={0}
                step={1}
                value={maxCredits}
                onChange={(event) => {
                  setMaxCredits(event.target.value);
                  setPlan(null);
                }}
              />
            </label>
            <Button
              size="sm"
              disabled={
                quoting ||
                saving ||
                changed ||
                !modelId ||
                quotedShots.length === 0 ||
                !validDuration ||
                !validTrim ||
                !validTargetDuration ||
                !validMaxCredits
              }
              title={changed ? "请先保存草稿修改" : undefined}
              onClick={() => void quote()}
            >
              <Calculator />
              {quoting ? "报价中" : "获取报价"}
            </Button>
          </div>
          {quoteIsCurrent && plan && (
            <div className="grid gap-2" aria-live="polite">
              <div className="flex flex-wrap items-center gap-3 text-sm">
                <span>{plan.units.length} 镜</span>
                <span>站内积分 {plan.totalVideoCredits}</span>
                <span className={plan.executable ? "text-primary" : "text-destructive"}>
                  {plan.executable ? "可提交" : "需要处理报价问题"}
                </span>
              </div>
              <ul className="grid gap-1 text-xs" aria-label="逐镜积分报价">
                {plan.units.map((unit) => (
                  <li key={unit.shotId}>
                    {unit.shotId} · {unit.mode} ·{" "}
                    {unit.credits === null ? "未报价" : `${unit.credits} 积分`}
                  </li>
                ))}
              </ul>
              <ul className="grid gap-1 text-xs">
                {plan.issues.map((issue, index) => (
                  <li
                    key={`${issue.code}:${issue.field}:${index}`}
                    className={issue.blocking ? "text-destructive" : "text-muted-foreground"}
                  >
                    {issue.field} · {issue.message}
                  </li>
                ))}
                {plan.units.flatMap((unit) =>
                  unit.issues.map((issue, index) => (
                    <li
                      key={`${unit.shotId}:${issue.code}:${issue.field}:${index}`}
                      className={issue.blocking ? "text-destructive" : "text-muted-foreground"}
                    >
                      {unit.shotId} · {issue.field} · {issue.message}
                    </li>
                  )),
                )}
              </ul>
              <p className="text-muted-foreground truncate text-xs" title={plan.planDigest}>
                计划摘要 {plan.planDigest}
              </p>
              {applyError && (
                <p role="status" className="text-destructive text-xs">
                  {applyError}
                </p>
              )}
              {generateError && (
                <p role="status" className="text-destructive text-xs">
                  {generateError}
                </p>
              )}
              {application && application.planDigest === plan.planDigest ? (
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm">
                    已采用到第 {application.episodeNo} 集 · {application.segments.length} 镜
                  </span>
                  <Button
                    size="sm"
                    variant="outline"
                    nativeButton={false}
                    render={<Link to={`/drama/${id}?area=studio&ep=${application.episodeNo}`} />}
                  >
                    打开片场
                  </Button>
                  <Button
                    size="sm"
                    disabled={!plan.executable || generating}
                    onClick={() => setGenerateConfirm(true)}
                  >
                    <WandSparkles />
                    提交生成 · {plan.totalVideoCredits} 积分
                  </Button>
                </div>
              ) : (
                <Button
                  className="justify-self-start"
                  size="sm"
                  disabled={!plan.executable || applying || !quoteIsCurrent}
                  onClick={() => setApplyConfirm(true)}
                >
                  {applying ? "采用中" : "采用到新分集"}
                </Button>
              )}
              {generated?.planDigest === plan.planDigest && (
                <ul className="grid gap-1 text-xs" aria-label="生成任务">
                  {generated.items.map((item) => (
                    <GenerationRow key={item.shotId} item={item} />
                  ))}
                </ul>
              )}
            </div>
          )}
        </section>
      )}

      {loading && drafts.length === 0 && (
        <p role="status" className="text-muted-foreground text-sm">
          正在读取分析结果
        </p>
      )}
      {!loading && drafts.length === 0 && (
        <p className="text-muted-foreground text-sm">暂无分析草稿</p>
      )}
      {runningPreparation && (
        <div className="flex flex-wrap items-center gap-2" role="status">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1 text-sm">
            <span>原片准备中</span>
            <progress
              className="h-1.5 min-w-24 flex-1 accent-primary"
              value={runningPreparation.completedFrames}
              max={Math.max(1, runningPreparation.totalFrames)}
              aria-label="已完成帧图"
            />
            <span className="text-muted-foreground text-xs">
              {runningPreparation.completedFrames}/{runningPreparation.totalFrames} 帧
            </span>
          </div>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="取消原片准备"
            title="取消原片准备"
            disabled={controlling}
            onClick={() => void controlPreparation(runningPreparation.id, "cancel")}
          >
            <Square />
          </Button>
        </div>
      )}
      {!runningPreparation &&
        (preparations[0]?.status === "failed" || preparations[0]?.status === "canceled") && (
          <div className="flex items-center gap-2 text-sm" role="status">
            <span className="text-destructive min-w-0 flex-1 truncate">
              {preparations[0].error || "原片准备失败"}
            </span>
            <Button
              size="sm"
              variant="outline"
              disabled={controlling}
              onClick={() => void controlPreparation(preparations[0].id, "resume")}
            >
              <Play />
              继续准备
            </Button>
          </div>
        )}
      {controlFailed && (
        <p role="status" className="text-destructive text-xs">
          准备状态未更新
        </p>
      )}
      {preparations[0] && (
        <p className="text-muted-foreground text-xs">
          音轨{" "}
          {preparations[0].hasAudio === null
            ? "未探测"
            : preparations[0].hasAudio
              ? "存在，未分析"
              : "未检测到"}
        </p>
      )}

      {draft && (
        <>
          <div className="flex flex-wrap items-center gap-3 text-xs">
            <span>基于修订 {draft.baseRevision}</span>
            {draft.baseRevision === reference.revision ? (
              <span>分镜改动 {changedShotCount} 镜</span>
            ) : (
              <span className="text-destructive">当前参考已到修订 {reference.revision}</span>
            )}
            <span>镜头 {draft.coverage.shots}</span>
            <span>画面 {draft.coverage.visual}</span>
            <span>对白 {draft.coverage.dialogue}</span>
            {reviewCount > 0 && (
              <span className="text-muted-foreground">待核对 {reviewCount} 项</span>
            )}
          </div>
          <div className="divide-chrome-border divide-y">
            {pageShots.map((shot, index) => {
              const shotNumber = shotPage * SHOTS_PER_PAGE + index + 1;
              const base = baseShots.get(shot.id);
              const observations = draft.observations.filter((item) => item.shotId === shot.id);
              const evidence = draft.evidence.filter((item) => item.shotId === shot.id);
              return (
                <label key={shot.id} className="grid min-w-0 gap-2 py-3">
                  <span className="text-sm font-medium">
                    原镜 {shotNumber} · {shot.startMs / 1000}–{shot.endMs / 1000} 秒
                  </span>
                  {base && (base.action !== shot.action || base.camera !== shot.camera) && (
                    <p className="text-muted-foreground text-xs">
                      {base.action !== shot.action &&
                        `动作：${base.action || "未填写"} → ${shot.action || "未填写"}`}
                      {base.action !== shot.action && base.camera !== shot.camera && " · "}
                      {base.camera !== shot.camera &&
                        `运镜：${base.camera || "未填写"} → ${shot.camera || "未填写"}`}
                    </p>
                  )}
                  {(observations.length > 0 || evidence.length > 0) && (
                    <p className="text-muted-foreground text-xs">
                      {observations.map((item) => `${item.field} ${item.reviewState}`).join(" · ")}
                      {observations.length > 0 && evidence.length > 0 && " · "}
                      {evidence.length > 0 &&
                        `证据 ${evidence.map((item) => `${(item.atMs / 1000).toFixed(1)} 秒`).join("、")}`}
                    </p>
                  )}
                  <Textarea
                    aria-label={`分析草稿原镜 ${shotNumber} 提示词`}
                    className="max-h-36 min-h-20 overflow-y-auto"
                    maxLength={40000}
                    value={promptValues[shot.id] ?? ""}
                    onChange={(event) => {
                      if (!draft) return;
                      setPromptEdits((values) => ({
                        ...values,
                        [draft.id]: { ...values[draft.id], [shot.id]: event.target.value },
                      }));
                      setPlan(null);
                    }}
                  />
                </label>
              );
            })}
          </div>
          <footer className="flex flex-wrap items-center gap-2">
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="上一页镜头"
              disabled={shotPage === 0}
              onClick={() => setShotPage((page) => page - 1)}
            >
              <ChevronLeft />
            </Button>
            <span className="text-muted-foreground text-xs">
              {shotPage + 1}/{pageCount}
            </span>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label="下一页镜头"
              disabled={shotPage + 1 >= pageCount}
              onClick={() => setShotPage((page) => page + 1)}
            >
              <ChevronRight />
            </Button>
            <span className="flex-1" />
            {saveFailed && (
              <span role="status" className="text-destructive text-xs">
                保存失败
              </span>
            )}
            <Button size="sm" disabled={!changed || saving} onClick={() => void save()}>
              <Save />
              保存草稿
            </Button>
          </footer>
        </>
      )}
      <AlertDialog open={applyConfirm} onOpenChange={setApplyConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>采用这份镜头计划？</AlertDialogTitle>
            <AlertDialogDescription>
              将 {plan?.units.length ?? 0}{" "}
              个原片镜头创建为跟拍分集片段。此步骤不会提交视频生成任务。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={applying}>返回</AlertDialogCancel>
            <AlertDialogAction disabled={applying} onClick={() => void applyPlan()}>
              {applying ? "采用中" : "采用"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={generateConfirm} onOpenChange={setGenerateConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>提交 {plan?.units.length ?? 0} 个视频生成任务？</AlertDialogTitle>
            <AlertDialogDescription>
              预计冻结站内积分 {plan?.totalVideoCredits ?? 0}。每镜会重新核验模型、素材与计划修订。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={generating}>返回</AlertDialogCancel>
            <AlertDialogAction disabled={generating} onClick={() => void generatePlan()}>
              {generating ? "提交中" : "提交生成"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
