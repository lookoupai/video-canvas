import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import { ArrowDown, ArrowLeft, ArrowUp, RefreshCw } from "lucide-react";
import { uploadAsset } from "@/api/asset";
import type { AssetDto } from "@/api/asset/type";
import {
  addDramaReference,
  analyzeDramaReference,
  getDramaReferences,
  getDramaReferenceVersion,
  importDramaReference,
  orderDramaReferences,
  saveDramaReference,
} from "@/api/drama/reference";
import type { DramaReference, DramaReferences } from "@/api/drama/reference-type";
import { AssetPickerDialog } from "@/components/assets/asset-picker-dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ReferenceEditor, type ReferenceDraft } from "./editor";
import { ReferenceRoles } from "./roles";
import { ReferenceAdopt } from "./adopt";

const STATUS = { idle: "待导入", queued: "处理中", ready: "可校订", failed: "处理失败" };

/** 一个原片保存为一个可独立重试的参考集，已有素材和新上传走同一入口。 */
function AddEpisode({ id, onAdded }: { id: string; onAdded: () => Promise<void> }) {
  const [picker, setPicker] = useState(false),
    [busy, setBusy] = useState(false),
    [failed, setFailed] = useState(false);
  const upload = useRef<HTMLInputElement>(null);
  const pending = useRef(new Map<string, string>());
  const lock = useRef(false);
  const add = async (assets: AssetDto[]) => {
    for (const a of assets) {
      const key = pending.current.get(a.id) ?? crypto.randomUUID();
      pending.current.set(a.id, key);
      await addDramaReference(id, a.id, a.fileName ?? "参考剧集", key);
      pending.current.delete(a.id);
    }
  };
  const perform = async (work: () => Promise<void>) => {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setFailed(false);
    try {
      await work();
      await onAdded();
    } catch {
      setFailed(true);
    } finally {
      lock.current = false;
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button size="sm" disabled={busy} onClick={() => setPicker(true)}>
        添加参考原片
      </Button>
      <Button size="sm" variant="outline" disabled={busy} onClick={() => upload.current?.click()}>
        上传原片
      </Button>
      <input
        ref={upload}
        type="file"
        accept="video/*"
        multiple
        hidden
        aria-label="上传参考原片"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? []);
          e.target.value = "";
          if (files.length)
            void perform(async () => {
              for (const file of files) {
                await add([await uploadAsset(file)]);
              }
            });
        }}
      />
      {busy && <span role="status">正在添加原片…</span>}
      {failed && (
        <span role="status" className="text-destructive text-sm">
          原片未全部添加，请重试
        </span>
      )}
      <AssetPickerDialog
        open={picker}
        onOpenChange={setPicker}
        kind="video"
        limit={20}
        title="选择参考剧集"
        onConfirm={(assets) => void perform(() => add(assets))}
      />
    </div>
  );
}

/** 参考集工作台通过修订和单集状态处理并发，另一集完成分析不会覆盖当前未保存的校订稿。 */
export function ReferenceWorkspace({ id }: { id: string }) {
  const [data, setData] = useState<DramaReferences | null>(null),
    [failed, setFailed] = useState(false);
  const [selected, setSelected] = useState<string | null>(null),
    [busy, setBusy] = useState(false),
    [actionError, setActionError] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, ReferenceDraft>>({});
  const [historical, setHistorical] = useState<DramaReference | null>(null);
  const [historyLoading, setHistoryLoading] = useState(false);
  const serial = useRef(0),
    historySerial = useRef(0),
    lock = useRef(false),
    file = useRef<HTMLInputElement>(null);
  const reload = useCallback(async () => {
    const mine = ++serial.current;
    try {
      const next = await getDramaReferences(id);
      if (mine === serial.current) {
        setData(next);
        setFailed(false);
        setSelected((old) =>
          next.episodes.some((e) => e.id === old) ? old : (next.episodes[0]?.id ?? null),
        );
      }
    } catch {
      if (mine === serial.current) setFailed(true);
    }
  }, [id]);
  const invalidatePending = useCallback(() => {
    serial.current++;
    historySerial.current++;
  }, []);
  useEffect(() => {
    void reload();
    return invalidatePending;
  }, [reload, invalidatePending]);
  const queued = data?.episodes.some((e) => e.status === "queued") ?? false;
  useEffect(() => {
    if (!queued) return;
    const timer = setInterval(() => void reload(), 2000);
    return () => clearInterval(timer);
  }, [queued, reload]);
  const current = data?.episodes.find((e) => e.id === selected) ?? null;
  const episode = historical ?? current;
  const draftKey = episode ? `${episode.id}:${episode.revision}` : "";
  const draft = episode?.document
    ? (drafts[draftKey] ?? { document: episode.document, bindings: episode.bindings })
    : null;
  const apply = (ep: DramaReference) => {
    serial.current++;
    setData((old) =>
      old ? { ...old, episodes: old.episodes.map((item) => (item.id === ep.id ? ep : item)) } : old,
    );
  };
  const action = async (work: () => Promise<void>) => {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setActionError(false);
    try {
      await work();
    } catch {
      setActionError(true);
    } finally {
      lock.current = false;
      setBusy(false);
    }
  };
  const save = () =>
    current &&
    draft &&
    action(async () => {
      const saved = await saveDramaReference(id, current.id, {
        revision: current.revision,
        ...draft,
      });
      apply(saved);
      setDrafts((old) => {
        const next = { ...old };
        delete next[draftKey];
        return next;
      });
    });
  const analyze = () =>
    current &&
    action(async () => {
      apply(await analyzeDramaReference(id, current.id, current.revision));
    });
  const move = (offset: number) =>
    current &&
    data &&
    action(async () => {
      const ids = data.episodes.map((e) => e.id),
        at = ids.indexOf(current.id);
      [ids[at], ids[at + offset]] = [ids[at + offset], ids[at]];
      await orderDramaReferences(id, data.revision, ids);
      await reload();
    });
  const history = async (revision: number) => {
    const mine = ++historySerial.current;
    if (!revision) {
      setHistorical(null);
      setHistoryLoading(false);
      return;
    }
    if (!current) return;
    setHistoryLoading(true);
    try {
      const saved = await getDramaReferenceVersion(id, current.id, revision);
      if (mine === historySerial.current) setHistorical(saved);
    } catch {
      if (mine === historySerial.current) setActionError(true);
    } finally {
      if (mine === historySerial.current) setHistoryLoading(false);
    }
  };
  return (
    <div className="drama-theme bg-background text-foreground min-h-svh px-4 pb-10 md:px-8">
      <header className="mx-auto flex max-w-360 flex-wrap items-center gap-3 py-5">
        <Button
          variant="ghost"
          nativeButton={false}
          size="icon-sm"
          render={<Link to="/drama" />}
          aria-label="返回短剧列表"
        >
          <ArrowLeft />
        </Button>
        <h1 className="mr-auto text-lg font-semibold">{data?.title ?? "参考剧集"} · 拆解与校订</h1>
        <Button
          variant="outline"
          size="sm"
          nativeButton={false}
          render={<Link to={`/drama/${id}`} />}
        >
          短剧工作台
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="刷新参考剧集"
          onClick={() => void reload()}
        >
          <RefreshCw />
        </Button>
      </header>
      <main className="mx-auto grid max-w-360 gap-5">
        {failed && (
          <div role="status" className="flex items-center gap-2">
            参考剧集读取失败
            <Button variant="outline" onClick={() => void reload()}>
              重新加载
            </Button>
          </div>
        )}
        {!data && !failed && <p role="status">正在读取参考剧集…</p>}
        {data && (
          <>
            <ReferenceRoles id={id} data={data} onSaved={reload} />
            <AddEpisode id={id} onAdded={reload} />
            <div className="grid items-start gap-6 md:grid-cols-[12rem_minmax(0,1fr)]">
              <nav aria-label="参考集序" className="grid gap-1">
                {data.episodes.map((ep) => (
                  <div
                    key={ep.id}
                    className={cn("rounded-xl p-2", ep.id === selected && "bg-chrome-hover")}
                  >
                    <button
                      type="button"
                      className="w-full min-w-0 truncate py-2 text-left text-sm"
                      aria-pressed={ep.id === selected}
                      onClick={() => {
                        historySerial.current++;
                        setHistorical(null);
                        setHistoryLoading(false);
                        setSelected(ep.id);
                        setActionError(false);
                      }}
                    >
                      第 {ep.seq} 集 · {ep.title}
                    </button>
                    <span className="text-muted-foreground text-xs">{STATUS[ep.status]}</span>
                  </div>
                ))}
                {data.episodes.length === 0 && (
                  <p className="text-muted-foreground py-4 text-sm">暂无参考剧集</p>
                )}
              </nav>
              <section className="min-w-0 space-y-4">
                {current && (
                  <>
                    <div className="flex flex-wrap items-center gap-2">
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label="上移参考集"
                        disabled={busy || current.seq === 1}
                        onClick={() => void move(-1)}
                      >
                        <ArrowUp />
                      </Button>
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label="下移参考集"
                        disabled={busy || current.seq === data.episodes.length}
                        onClick={() => void move(1)}
                      >
                        <ArrowDown />
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={busy || current.status === "queued"}
                        onClick={() => file.current?.click()}
                      >
                        导入拆片包
                      </Button>
                      <input
                        type="file"
                        ref={file}
                        hidden
                        accept=".json,.zip,application/json,application/zip"
                        aria-label="本集拆片包"
                        onChange={(e) => {
                          const selectedFile = e.target.files?.[0];
                          e.target.value = "";
                          if (selectedFile)
                            void action(async () => {
                              apply(
                                await importDramaReference(
                                  id,
                                  current.id,
                                  current.revision,
                                  selectedFile,
                                ),
                              );
                              setHistorical(null);
                            });
                        }}
                      />
                      <Button
                        size="sm"
                        disabled={busy || current.status === "queued" || !data.analyzerAvailable}
                        title={
                          !data.analyzerAvailable
                            ? "当前服务器未安装 rea-video，可导入拆片包"
                            : undefined
                        }
                        onClick={() => void analyze()}
                      >
                        {current.status === "idle" ? "分析本集" : "重新分析本集"}
                      </Button>
                      {current.versions.length > 0 && (
                        <select
                          aria-label="查看拆解修订"
                          className="bg-chrome-hover ml-auto rounded-lg px-3 py-2 text-sm"
                          value={historical?.revision ?? 0}
                          onChange={(e) => void history(Number(e.target.value))}
                        >
                          <option value={0}>当前修订 v{current.revision}</option>
                          {current.versions
                            .filter((v) => v.revision !== current.revision)
                            .map((v) => (
                              <option value={v.revision} key={v.revision}>
                                v{v.revision} · {v.method}
                              </option>
                            ))}
                        </select>
                      )}
                    </div>
                    {current.document && (
                      <ReferenceAdopt
                        key={`${current.id}:${current.revision}`}
                        id={id}
                        episode={current}
                        disabled={
                          busy ||
                          !!historical ||
                          historyLoading ||
                          current.status === "queued" ||
                          !!drafts[`${current.id}:${current.revision}`]
                        }
                      />
                    )}
                    {current.status === "queued" && (
                      <p role="status">本集正在处理中，其他集可继续校订</p>
                    )}
                    {current.status === "failed" && (
                      <p role="status" className="text-destructive">
                        {current.error || "本集处理失败"}
                      </p>
                    )}
                    {actionError && (
                      <p role="status" className="text-destructive">
                        操作未保存，请核对修订并重试
                      </p>
                    )}
                    {historical && (
                      <p role="status" className="text-muted-foreground text-sm">
                        正在回看历史修订 v{historical.revision}
                      </p>
                    )}
                    {episode?.method === "rea-video-local" && (
                      <p className="text-muted-foreground text-sm">
                        本地切镜分析，角色和对白待校订
                      </p>
                    )}
                    {historyLoading ? (
                      <p role="status">正在读取历史修订…</p>
                    ) : episode && draft ? (
                      <ReferenceEditor
                        key={episode.id}
                        episode={episode}
                        draft={draft}
                        roles={data.roles}
                        disabled={busy || !!historical || current.status === "queued"}
                        onChange={(next) => setDrafts((old) => ({ ...old, [draftKey]: next }))}
                        onSave={() => void save()}
                      />
                    ) : (
                      current.status !== "queued" && (
                        <p className="text-muted-foreground py-8">导入拆片包或分析原片后开始校订</p>
                      )
                    )}
                  </>
                )}
              </section>
            </div>
          </>
        )}
      </main>
    </div>
  );
}

/** 参考工作台刷新时按路径中的真实短剧 ID 重新读取。 */
export default function DramaReferencePage() {
  const { id } = useParams();
  return id ? <ReferenceWorkspace key={id} id={id} /> : null;
}
