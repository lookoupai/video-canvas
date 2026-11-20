import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowLeft, Film, RefreshCw } from "lucide-react";
import {
  createRemakePlan,
  getRemakePlan,
  listRemakePlans,
  orderRemakeShots,
  updateRemakeShot,
} from "@/api/marketing/remake";
import { isMarketingConflict } from "@/api/marketing/errors";
import type {
  RefVideo,
  RemakePlan,
  RemakePlanSummary,
  RemakeShotPatch,
  Script,
} from "@/api/marketing/type";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { RemakeCompose } from "./remake-compose";
import { RemakeShotEditor } from "./remake-shot";

/** 正在制作的计划只用其稳定镜号关联选版。加载、编辑和重排请求均绑定该计划。 */
function PlanWorkspace({
  id,
  initial,
  initialShot,
  video,
}: {
  id: string;
  initial: RemakePlan | null;
  initialShot: number | null;
  video: RefVideo | null;
}) {
  const [plan, setPlan] = useState<RemakePlan | null>(() => (initial?.id === id ? initial : null));
  const [selected, setSelected] = useState((initialShot ?? 0) + 1);
  const [compose, setCompose] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState(false);
  const lock = useRef(false);
  const serial = useRef(0);
  const reload = useCallback(
    async (signal?: AbortSignal) => {
      const request = ++serial.current;
      try {
        const latest = await getRemakePlan(id);
        if (!signal?.aborted && request === serial.current) {
          setPlan(latest);
          setLoadError(false);
        }
      } catch {
        if (!signal?.aborted && request === serial.current) setLoadError(true);
      }
    },
    [id],
  );
  useEffect(() => {
    const controller = new AbortController();
    const refresh = () => void reload(controller.signal);
    if (!initial || initial.id !== id) refresh();
    const visible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    window.addEventListener("online", refresh);
    document.addEventListener("visibilitychange", visible);
    return () => {
      controller.abort();
      window.removeEventListener("online", refresh);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [id, initial, reload]);
  const change = async (action: () => Promise<RemakePlan>) => {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setActionError(false);
    try {
      const next = await action();
      ++serial.current;
      setPlan(next);
    } catch (err) {
      if (isMarketingConflict(err)) await reload();
      else setActionError(true);
    } finally {
      lock.current = false;
      setBusy(false);
    }
  };
  if (!plan)
    return loadError ? (
      <Button onClick={() => void reload()}>重新加载计划</Button>
    ) : (
      <p role="status">正在读取制作计划…</p>
    );
  const shot = plan.shots.find((s) => s.id === selected) ?? plan.shots[0];
  const index = plan.shots.indexOf(shot);
  const save = (patch: RemakeShotPatch) => change(() => updateRemakeShot(id, shot.id, patch));
  const move = (offset: number) =>
    change(() => {
      const ids = plan.shots.map((s) => s.id);
      [ids[index], ids[index + offset]] = [ids[index + offset], ids[index]];
      return orderRemakeShots(id, plan.revision, ids);
    });
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground mr-auto text-xs">
          脚本 v{plan.scriptRevision} · 计划 v{plan.revision}
        </span>
        {loadError || actionError ? (
          <span role="status" className="text-muted-foreground text-xs">
            请重新核对镜头状态
          </span>
        ) : null}
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label="刷新制作计划"
          disabled={busy}
          onClick={() => void reload()}
        >
          <RefreshCw />
        </Button>
        <Button size="sm" variant="outline" onClick={() => setCompose((v) => !v)}>
          {compose ? <ArrowLeft /> : <Film />}
          {compose ? "返回分镜" : "合成成片"}
        </Button>
      </div>
      {compose ? (
        <RemakeCompose key={id} plan={plan} onConflict={reload} />
      ) : (
        <div className="grid items-start gap-4 sm:grid-cols-[7rem_minmax(0,1fr)]">
          <nav aria-label="成片镜序" className="grid grid-cols-2 gap-1 sm:grid-cols-1">
            {plan.shots.map((item, i) => (
              <button
                key={item.id}
                type="button"
                aria-label={`选择第 ${i + 1} 镜`}
                aria-pressed={item.id === shot?.id}
                onClick={() => setSelected(item.id)}
                className={cn(
                  "hover:bg-chrome-hover flex items-center justify-between gap-2 rounded-lg px-2 py-2.5 text-sm",
                  item.id === shot?.id && "bg-chrome-hover ring-chrome-border ring-1",
                )}
              >
                第 {i + 1} 镜{" "}
                <span className="text-muted-foreground text-xs">
                  {item.chosenTaskId ? "已选版" : `${item.durationMs / 1000}s`}
                </span>
              </button>
            ))}
          </nav>
          {shot && (
            <RemakeShotEditor
              key={`${id}/${shot.id}/${shot.revision}`}
              plan={plan}
              shot={shot}
              index={index}
              busy={busy}
              save={save}
              reorder={move}
              reload={reload}
              url={video?.id === plan.refVideoAssetId ? video?.url : undefined}
            />
          )}
        </div>
      )}
    </div>
  );
}

/** 一次脚本修订只建立一份计划，重写脚本后仍可按修订打开旧计划。 */
export function RemakeProduction({
  script,
  refVideo,
  initialShot,
}: {
  script: Script;
  refVideo: RefVideo | null;
  initialShot: number | null;
}) {
  const [plans, setPlans] = useState<RemakePlanSummary[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [created, setCreated] = useState<RemakePlan | null>(null);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  useEffect(() => {
    let active = true;
    listRemakePlans(script.id)
      .then((list) => {
        if (!active) return;
        setPlans(list);
        setFailed(false);
        setSelected((id) =>
          list.some((p) => p.id === id)
            ? id
            : ((list.find((p) => p.scriptRevision === script.revision) ?? list[0])?.id ?? null),
        );
      })
      .catch(() => {
        if (active) setFailed(true);
      });
    return () => {
      active = false;
    };
  }, [script.id, script.revision, attempt]);
  const create = async () => {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    try {
      const plan = await createRemakePlan(script.id, script.revision);
      setCreated(plan);
      setPlans((list) => [plan, ...(list ?? []).filter((p) => p.id !== plan.id)]);
      setSelected(plan.id);
    } catch {
      setFailed(true);
    } finally {
      lock.current = false;
      setBusy(false);
    }
  };
  return (
    <div className="min-h-0 overflow-y-auto pr-1">
      <div className="grid gap-3">
        {failed && (
          <Button
            variant="outline"
            onClick={() => {
              setFailed(false);
              setAttempt((a) => a + 1);
            }}
          >
            重新加载计划列表
          </Button>
        )}
        {plans === null && !failed && <p role="status">正在读取计划…</p>}
        {plans !== null && (
          <div className="flex flex-wrap items-center gap-2">
            {plans.length > 0 && (
              <label className="flex items-center gap-2">
                制作计划
                <select
                  className="bg-chrome-hover rounded-full px-3 py-1.5"
                  aria-label="选择制作计划"
                  value={selected ?? ""}
                  disabled={busy}
                  onChange={(e) => {
                    setSelected(e.target.value);
                    setCreated(null);
                  }}
                >
                  {plans.map((p) => (
                    <option key={p.id} value={p.id}>
                      脚本 v{p.scriptRevision}
                      {p.scriptRevision === script.revision ? " · 当前稿" : " · 历史稿"}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {!plans.some((p) => p.scriptRevision === script.revision) && (
              <>
                <span className="text-muted-foreground text-sm">
                  当前跟拍脚本 v{script.revision}
                </span>
                <Button disabled={busy} size="sm" onClick={() => void create()}>
                  建立制作计划
                </Button>
              </>
            )}
          </div>
        )}
        {selected && (
          <PlanWorkspace
            key={selected}
            id={selected}
            initial={created}
            video={refVideo}
            initialShot={initialShot}
          />
        )}
      </div>
    </div>
  );
}
