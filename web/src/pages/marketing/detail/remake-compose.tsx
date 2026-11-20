import { useEffect, useRef, useState } from "react";
import { ExternalLink } from "lucide-react";
import { composeRemakePlan, listRemakeCompositions } from "@/api/marketing/remake";
import type { RemakeComposition, RemakePlan } from "@/api/marketing/type";
import { Button } from "@/components/ui/button";
import { isMarketingConflict } from "@/api/marketing/errors";
import { isRetryableSubmitError } from "@/utils/tasks/submit";

/** 只记录尚未确认响应的请求，刷新或网络重传时仍使用原来的镜序和修订。 */
type ComposeIntent = Pick<RemakeComposition, "revision" | "idempotencyKey"> & {
  shotIds?: number[];
};

function readIntent(storageKey: string): ComposeIntent | null {
  try {
    return JSON.parse(sessionStorage.getItem(storageKey) ?? "null");
  } catch {
    return null;
  }
}

/** 合成固定当前选版和剪辑入点。用户可明确缩小范围，失败记录的重试沿用已提交的清单。 */
export function RemakeCompose({
  plan,
  onConflict,
}: {
  plan: RemakePlan;
  onConflict: () => Promise<void>;
}) {
  const storageKey = `remake:compose:${plan.id}`;
  const [history, setHistory] = useState<RemakeComposition[] | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [partial, setPartial] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [pending, setPending] = useState<ComposeIntent | null>(() => readIntent(storageKey));
  const lock = useRef(false);
  const [busy, setBusy] = useState(false);
  const waiting = !!history?.some((item) => item.status === "queued");
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function load() {
      try {
        const rows = await listRemakeCompositions(plan.id);
        if (!active) return;
        setLoadError(false);
        setHistory(rows);
        if (rows.some((row) => row.status === "queued")) timer = setTimeout(load, 2500);
      } catch {
        if (active) setLoadError(true);
      }
    }
    void load();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [plan.id, refresh, waiting]);
  const remember = (intent: ComposeIntent | null) => {
    setPending(intent);
    if (intent) sessionStorage.setItem(storageKey, JSON.stringify(intent));
    else sessionStorage.removeItem(storageKey);
  };
  const ids = partial
    ? plan.shots.filter((s) => selected.includes(s.id)).map((s) => s.id)
    : undefined;
  const missing = plan.shots.flatMap((s, i) =>
    (!ids || ids.includes(s.id)) && !s.chosenTaskId ? [i + 1] : [],
  );
  async function submit(intent: ComposeIntent) {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    try {
      remember(intent);
      const next = await composeRemakePlan(
        plan.id,
        intent.revision,
        intent.idempotencyKey,
        intent.shotIds,
      );
      remember(null);
      setHistory((list) => [next, ...(list ?? []).filter((item) => item.id !== next.id)]);
    } catch (err) {
      if (!isRetryableSubmitError(err)) remember(null);
      if (isMarketingConflict(err)) await onConflict();
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  return (
    <section className="grid gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant={!partial ? "secondary" : "ghost"}
          aria-pressed={!partial}
          onClick={() => setPartial(false)}
        >
          全部镜头
        </Button>
        <Button
          size="sm"
          variant={partial ? "secondary" : "ghost"}
          aria-pressed={partial}
          onClick={() => setPartial(true)}
        >
          部分镜头
        </Button>
      </div>
      {partial && (
        <fieldset className="flex flex-wrap gap-2">
          {plan.shots.map((s, i) => (
            <label
              key={s.id}
              className="bg-chrome-hover flex items-center gap-2 rounded-full px-3 py-2"
            >
              <input
                type="checkbox"
                aria-label={`合成第 ${i + 1} 镜`}
                checked={selected.includes(s.id)}
                onChange={(e) =>
                  setSelected(
                    e.target.checked ? [...selected, s.id] : selected.filter((id) => id !== s.id),
                  )
                }
              />
              第 {i + 1} 镜
            </label>
          ))}
        </fieldset>
      )}
      {missing.length > 0 && (
        <p className="text-muted-foreground text-sm">第 {missing.join("、")} 镜未选版</p>
      )}
      <Button
        className="justify-self-start"
        disabled={busy || !history || (!pending && (missing.length > 0 || ids?.length === 0))}
        onClick={() =>
          void submit(
            pending ?? {
              revision: plan.revision,
              idempotencyKey: crypto.randomUUID(),
              shotIds: ids,
            },
          )
        }
      >
        {busy ? "正在提交…" : pending ? "重试上次合成" : "提交合成"}
      </Button>
      {loadError && (
        <Button variant="outline" onClick={() => setRefresh((n) => n + 1)}>
          重新加载合成记录
        </Button>
      )}
      {history?.map((item) => (
        <article
          key={item.id}
          className="border-chrome-border flex flex-wrap items-center gap-2 border-t py-3 text-sm"
        >
          <span>合成 · {item.shotIds.length} 镜</span>
          <span className="text-muted-foreground">{new Date(item.createdAt).toLocaleString()}</span>
          {item.asset ? (
            <a
              className="text-beam ml-auto inline-flex items-center gap-1"
              href={item.asset.url}
              target="_blank"
              rel="noreferrer"
            >
              查看成片
              <ExternalLink size={14} />
            </a>
          ) : item.status === "failed" ? (
            <>
              <span className="text-destructive">{item.error || "合成失败"}</span>
              <Button
                size="sm"
                variant="outline"
                disabled={busy || !!pending}
                onClick={() =>
                  void submit({
                    revision: item.revision,
                    idempotencyKey: item.idempotencyKey,
                    shotIds: item.shotIds,
                  })
                }
              >
                重试此合成
              </Button>
            </>
          ) : (
            <span role="status">合成中…</span>
          )}
        </article>
      ))}
    </section>
  );
}
