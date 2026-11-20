import { useState, useRef } from "react";
import { ArrowDown, ArrowUp, Plus, X } from "lucide-react";
import type {
  RemakePlan,
  RemakePlanShot,
  RemakeSource,
  RemakeShotPatch,
  TaskLite,
} from "@/api/marketing/type";
import type { TaskStatus } from "@/api/generation-task/type";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useTask } from "@/store/tasks";
import { useCreditsStore } from "@/store/credits";
import { submitRemakeShotTask } from "@/utils/tasks/gateway";
import { isActiveStatus } from "@/utils/tasks/status";
import { TaskGenerationStatus } from "@/components/generation-status";
import { isRetryableSubmitError } from "@/utils/tasks/submit";
import { isMarketingConflict } from "@/api/marketing/errors";
import { IntervalPlayer } from "./remake-player";

/** 同一份任务历史在 WS 或 HTTP 对账更新后立即可回放、采用。 */
function Version({
  take,
  number,
  chosen,
  disabled,
  onChoose,
}: {
  take: TaskLite;
  number: number;
  chosen: boolean;
  disabled: boolean;
  onChoose: () => void;
}) {
  const task = useTask(take.id);
  const url = take.outputs[0]?.url || task?.outputs?.[0]?.url;
  const success = take.status === "succeeded" || task?.status === "succeeded";
  return (
    <li className="border-chrome-border flex flex-wrap items-center gap-2 border-t py-2">
      <span>版本 {number}</span>
      {success && url ? (
        <>
          <a className="text-beam" href={url} target="_blank" rel="noreferrer">
            回看
          </a>
          <Button
            size="sm"
            variant={chosen ? "secondary" : "outline"}
            disabled={disabled || chosen}
            aria-label={`采用版本 ${number}`}
            onClick={onChoose}
          >
            {chosen ? "已采用" : "采用"}
          </Button>
        </>
      ) : (
        <TaskGenerationStatus
          className="min-w-0 flex-1"
          taskId={take.id}
          fallback={{ status: take.status as TaskStatus, error_message: take.errorMessage }}
        />
      )}
    </li>
  );
}

const sameSources = (a: RemakeSource[], b: RemakeSource[]) =>
  a.length === b.length &&
  a.every((s, i) => s.refShot === b[i].refShot && s.start === b[i].start && s.end === b[i].end);

/** 原镜边界来自计划快照，多个来源可独立缩短和重排。 */
function Sources({
  sources,
  catalog,
  onChange,
}: {
  sources: RemakeSource[];
  catalog: RemakeSource[];
  onChange: (s: RemakeSource[]) => void;
}) {
  const [add, setAdd] = useState("");
  const change = (i: number, ref: RemakeSource) =>
    onChange(sources.map((s, n) => (n === i ? ref : s)));
  return (
    <details className="border-chrome-border rounded-xl border p-3">
      <summary className="cursor-pointer">参考区间 · {sources.length} 段</summary>
      <div className="mt-3 grid gap-2">
        {sources.map((ref, i) => {
          const bounds = catalog.find((s) => s.refShot === ref.refShot);
          return (
            <div key={i} className="flex flex-wrap items-center gap-1.5">
              <span>原镜 {ref.refShot}</span>
              <Input
                className="w-21"
                type="number"
                step=".001"
                min={bounds?.start}
                max={ref.end}
                aria-label={`来源 ${i + 1} 起点（秒）`}
                value={Number.isFinite(ref.start) ? ref.start : ""}
                onChange={(e) => change(i, { ...ref, start: e.target.valueAsNumber })}
              />
              –
              <Input
                className="w-21"
                type="number"
                step=".001"
                min={ref.start}
                max={bounds?.end}
                aria-label={`来源 ${i + 1} 终点（秒）`}
                value={Number.isFinite(ref.end) ? ref.end : ""}
                onChange={(e) => change(i, { ...ref, end: e.target.valueAsNumber })}
              />{" "}
              秒
              <Button
                size="icon-sm"
                variant="ghost"
                disabled={!i}
                aria-label={`上移来源 ${i + 1}`}
                onClick={() => {
                  const next = sources.slice();
                  [next[i - 1], next[i]] = [next[i], next[i - 1]];
                  onChange(next);
                }}
              >
                <ArrowUp />
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={`移除来源 ${i + 1}`}
                onClick={() => onChange(sources.filter((_, n) => n !== i))}
              >
                <X />
              </Button>
            </div>
          );
        })}
        <div className="flex items-center gap-1">
          <select
            aria-label="添加原镜"
            className="bg-chrome-hover rounded-lg px-2 py-1"
            value={add}
            onChange={(e) => setAdd(e.target.value)}
          >
            <option value="">选择原镜</option>
            {catalog.map((s) => (
              <option key={s.refShot} value={s.refShot}>
                原镜 {s.refShot} · {s.start}–{s.end} 秒
              </option>
            ))}
          </select>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="添加来源区间"
            disabled={!add}
            onClick={() => {
              const ref = catalog.find((s) => s.refShot === Number(add));
              if (ref) onChange([...sources, ref]);
              setAdd("");
            }}
          >
            <Plus />
          </Button>
        </div>
      </div>
    </details>
  );
}

/** 单镜编辑稿独立于任务状态，同一修订的推送不会打断输入。 */
export function RemakeShotEditor({
  plan,
  shot,
  index,
  url,
  busy,
  save,
  reorder,
  reload,
}: {
  plan: RemakePlan;
  shot: RemakePlanShot;
  index: number;
  url?: string;
  busy: boolean;
  save: (patch: RemakeShotPatch) => Promise<void>;
  reorder: (offset: number) => Promise<void>;
  reload: () => Promise<void>;
}) {
  const [scene, setScene] = useState(shot.scene),
    [narration, setNarration] = useState(shot.narration);
  const [duration, setDuration] = useState(String(shot.durationMs / 1000)),
    [trim, setTrim] = useState(String(shot.trimStartMs / 1000));
  const [sources, setSources] = useState(shot.sources),
    [refIndex, setRefIndex] = useState(0);
  const [mediaFailed, setMediaFailed] = useState(false);
  const storageKey = `remake:generate:${plan.id}:${shot.id}:${shot.revision}`;
  const [pending, setPending] = useState(() => sessionStorage.getItem(storageKey));
  const [submitting, setSubmitting] = useState(false);
  const lock = useRef(false);
  const durationMs = Math.round(Number(duration) * 1000),
    trimStartMs = Math.round(Number(trim) * 1000);
  const changed =
    scene !== shot.scene ||
    narration !== shot.narration ||
    durationMs !== shot.durationMs ||
    trimStartMs !== shot.trimStartMs ||
    !sameSources(sources, shot.sources);
  const invalid =
    !scene.trim() ||
    durationMs < 1 ||
    durationMs > 600000 ||
    trim === "" ||
    trimStartMs < 0 ||
    trimStartMs > 600000 ||
    sources.some(
      (s) =>
        !plan.sourceCatalog.some(
          (c) => c.refShot === s.refShot && s.start >= c.start && s.end <= c.end && s.start < s.end,
        ),
    );
  const last = shot.takes.at(-1);
  const live = useTask(last?.id);
  const working =
    !!last && isActiveStatus(last.status as TaskStatus) && (!live || isActiveStatus(live.status));
  const available = useCreditsStore((state) => state.credits?.available);
  const blocked = busy || submitting;
  const reason = changed
    ? "请先保存镜头"
    : shot.generateError ||
      (available !== undefined && available < shot.renderCredits ? "积分不足" : "");
  const selectedSource = sources[refIndex] ?? sources[0];
  const source =
    selectedSource &&
    Number.isFinite(selectedSource.start) &&
    selectedSource.end > selectedSource.start
      ? selectedSource
      : undefined;
  const chosen = shot.takes.find((t) => t.id === shot.chosenTaskId);
  const [view, setView] = useState<"ref" | "take">("ref");
  const generation = async () => {
    if (lock.current) return;
    lock.current = true;
    setSubmitting(true);
    const key = pending ?? crypto.randomUUID();
    sessionStorage.setItem(storageKey, key);
    setPending(key);
    try {
      await submitRemakeShotTask(plan.id, shot.id, shot.revision, key);
      sessionStorage.removeItem(storageKey);
      setPending(null);
      await reload();
    } catch (err) {
      if (!isRetryableSubmitError(err)) {
        sessionStorage.removeItem(storageKey);
        setPending(null);
      }
      if (isMarketingConflict(err)) await reload();
    } finally {
      lock.current = false;
      setSubmitting(false);
    }
  };
  const edit = {
    revision: plan.revision,
    scene,
    narration,
    durationMs,
    trimStartMs,
    ...(!sameSources(sources, shot.sources) ? { sources } : {}),
  };
  return (
    <section className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,.9fr)_minmax(0,1fr)]">
      <div className="min-w-0 space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <b>第 {index + 1} 镜</b>
          <Button
            aria-label="上移镜头"
            size="icon-sm"
            variant="ghost"
            disabled={blocked || changed || index === 0}
            onClick={() => void reorder(-1)}
          >
            <ArrowUp />
          </Button>
          <Button
            aria-label="下移镜头"
            size="icon-sm"
            variant="ghost"
            disabled={blocked || changed || index === plan.shots.length - 1}
            onClick={() => void reorder(1)}
          >
            <ArrowDown />
          </Button>
        </div>
        <div className="flex gap-1">
          <Button
            size="sm"
            variant={view === "ref" ? "secondary" : "ghost"}
            onClick={() => setView("ref")}
          >
            原片
          </Button>
          <Button
            size="sm"
            variant={view === "take" ? "secondary" : "ghost"}
            disabled={!chosen?.outputs[0]?.url}
            onClick={() => setView("take")}
          >
            采用视频
          </Button>
        </div>
        {view === "take" && chosen?.outputs[0] ? (
          <video
            className="max-h-60 w-full rounded-xl bg-black"
            controls
            playsInline
            src={chosen.outputs[0].url}
          />
        ) : source && url ? (
          <>
            <IntervalPlayer
              key={source.refShot + "-" + refIndex + "-" + source.start + "-" + source.end}
              source={source}
              url={url}
              autoPlay={false}
              onError={() => setMediaFailed(true)}
            />
            {mediaFailed && <p>原片加载失败</p>}
            <div className="flex flex-wrap gap-1">
              {sources.map((s, i) => (
                <Button
                  size="sm"
                  variant={i === refIndex ? "secondary" : "ghost"}
                  key={i}
                  onClick={() => {
                    setRefIndex(i);
                    setMediaFailed(false);
                  }}
                >
                  原镜 {s.refShot} · {s.start}–{s.end} 秒
                </Button>
              ))}
            </div>
          </>
        ) : (
          <p className="text-muted-foreground">
            {sources.length ? "原片素材不可用" : "未关联原片区间"}
          </p>
        )}
        <h3 className="font-medium">生成版本</h3>
        {shot.takes.length ? (
          <ul>
            {shot.takes.map((t, i) => (
              <Version
                key={t.id}
                take={t}
                number={i + 1}
                chosen={t.id === shot.chosenTaskId}
                disabled={blocked || changed}
                onChoose={() => void save({ revision: plan.revision, chosenTaskId: t.id })}
              />
            ))}
          </ul>
        ) : (
          <p className="text-muted-foreground">暂无版本</p>
        )}
      </div>
      <div className="grid content-start gap-3">
        <label>
          画面与动作
          <Textarea
            aria-label="镜头画面"
            maxLength={4000}
            value={scene}
            onChange={(e) => setScene(e.target.value)}
            className="mt-1 max-h-36"
          />
        </label>
        <label>
          旁白
          <Textarea
            maxLength={4000}
            value={narration}
            onChange={(e) => setNarration(e.target.value)}
            className="mt-1 max-h-32"
          />
        </label>
        <div className="grid grid-cols-2 gap-2">
          <label>
            成片时长（秒）
            <Input
              type="number"
              min=".001"
              max="600"
              step=".001"
              value={duration}
              onChange={(e) => setDuration(e.target.value)}
            />
          </label>
          <label>
            采用视频入点（秒）
            <Input
              type="number"
              min="0"
              max="600"
              step=".001"
              value={trim}
              onChange={(e) => setTrim(e.target.value)}
            />
          </label>
        </div>
        <Sources sources={sources} catalog={plan.sourceCatalog} onChange={setSources} />
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={!changed || invalid || blocked}
            onClick={() => void save(edit)}
          >
            保存镜头
          </Button>
        </div>
        <div className="border-chrome-border space-y-2 border-t pt-3">
          <p>
            {plan.videoModel} · 申请 {shot.generateSeconds} 秒 · {shot.renderCredits} 积分
          </p>
          <Button
            disabled={blocked || (!pending && (working || !!reason))}
            aria-label={pending ? "重试提交" : `生成第 ${index + 1} 镜`}
            onClick={() => void generation()}
          >
            {submitting
              ? "正在提交…"
              : pending
                ? "重试提交"
                : working
                  ? "生成中…"
                  : `生成第 ${index + 1} 镜`}
          </Button>
          {reason && <p className="text-muted-foreground text-xs">{reason}</p>}
        </div>
      </div>
    </section>
  );
}
