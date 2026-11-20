import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { adoptDramaReference, listReferenceAdoptions } from "@/api/drama/adoption";
import type { DramaReference } from "@/api/drama/reference-type";
import type { ReferenceAdoption, ReferenceMode } from "@/api/drama/remake-type";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

/** 已采用分集的稳定入口，集号以服务端当前查询为准。 */
const destination = (id: string, row: ReferenceAdoption) =>
  `/drama/${id}?area=${row.mode === "follow" ? "studio" : "script"}&ep=${row.episodeNo}`;

/**
 * 将已保存的参考修订采用到已有片场；选择更新范围后才允许覆盖相应镜头稿。
 * 请求失败保留该输入的键，重传不会再次建立分集。
 */
export function ReferenceAdopt({
  id,
  episode,
  disabled,
}: {
  id: string;
  episode: DramaReference;
  disabled: boolean;
}) {
  const navigate = useNavigate();
  const [records, setRecords] = useState<ReferenceAdoption[]>([]);
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<ReferenceMode>("follow");
  const [target, setTarget] = useState("");
  const [instruction, setInstruction] = useState("");
  const [shots, setShots] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const [loaded, setLoaded] = useState(false);
  const lock = useRef(false);
  const pending = useRef(new Map<string, string>());
  useEffect(() => {
    let active = true;
    setLoaded(false);
    void listReferenceAdoptions(id).then(
      (rows) => {
        if (active) {
          setRecords(rows);
          setLoaded(true);
        }
      },
      () => {
        if (active) setError("采用记录读取失败，请重新读取后操作");
      },
    );
    return () => {
      active = false;
    };
  }, [id, reload]);

  const related = records.filter((row) => row.referenceId === episode.id);
  const targets = Array.from(
    new Map(
      related
        .filter((row) => !row.deleted && row.mode === "follow")
        .sort((a, b) => a.referenceRevision - b.referenceRevision)
        .map((row) => [row.episodeId, row]),
    ).values(),
  );
  const updating = targets.find((row) => row.episodeId === target);
  const selected =
    episode.document?.shots.filter((shot) => shots.includes(shot.id)).map((shot) => shot.id) ?? [];
  const ready = !disabled && loaded && !!episode.document?.shots.length && !busy;
  const adopt = async () => {
    if (lock.current || !ready || (updating && selected.length === 0)) return;
    const body = {
      revision: episode.revision,
      mode,
      ...(mode === "rewrite" ? { instruction: instruction.trim() } : {}),
      ...(updating && mode === "follow"
        ? {
            targetEpisodeNo: updating.episodeNo,
            episodeRevision: updating.episodeRevision,
            shotIds: selected,
          }
        : {}),
    };
    const fingerprint = JSON.stringify(body);
    const requestKey = pending.current.get(fingerprint) ?? crypto.randomUUID();
    pending.current.set(fingerprint, requestKey);
    lock.current = true;
    setBusy(true);
    setError("");
    try {
      const result = await adoptDramaReference(id, episode.id, { ...body, requestKey });
      pending.current.delete(fingerprint);
      navigate(destination(id, result));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "采用失败，请重试");
    } finally {
      lock.current = false;
      setBusy(false);
    }
  };
  return (
    <section className="border-chrome-border grid gap-3 border-y py-3" aria-label="采用参考修订">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          size="sm"
          variant={open ? "outline" : "default"}
          disabled={!ready}
          onClick={() => setOpen((value) => !value)}
        >
          采用到片场
        </Button>
        {disabled && (
          <span className="text-muted-foreground text-xs">请先保存当前校订，再采用到片场</span>
        )}
        {targets.map((row) => (
          <Link key={row.episodeId} className="text-beam text-sm" to={destination(id, row)}>
            第 {row.episodeNo} 集片场 · 原稿 v{row.referenceRevision}
          </Link>
        ))}
        {related
          .filter((row) => !row.deleted && row.mode === "rewrite")
          .map((row) => (
            <Link key={row.id} className="text-beam text-sm" to={destination(id, row)}>
              第 {row.episodeNo} 集改编稿
            </Link>
          ))}
      </div>
      {open && (
        <div className="grid gap-4 pt-1">
          <fieldset className="flex flex-wrap gap-5" disabled={busy}>
            <legend className="sr-only">制作方式</legend>
            {(
              [
                ["follow", "按原镜重拍"],
                ["rewrite", "原创改编"],
              ] as const
            ).map(([value, label]) => (
              <label key={value} className="flex items-center gap-2 text-sm">
                <input
                  type="radio"
                  name={`adopt-${episode.id}`}
                  aria-label={label}
                  value={value}
                  checked={mode === value}
                  onChange={() => {
                    setMode(value);
                    setTarget("");
                    setShots([]);
                  }}
                />
                {label}
              </label>
            ))}
          </fieldset>
          {mode === "rewrite" ? (
            <div className="grid gap-2">
              <Textarea
                aria-label="改编要求"
                placeholder="保留哪些冲突和转折，改成什么故事…"
                maxLength={4000}
                value={instruction}
                disabled={busy}
                onChange={(event) => setInstruction(event.target.value)}
              />
              <p className="text-muted-foreground text-xs">
                采用后进入分集剧本，使用「生成剧本」写改编稿，审核后再拆分镜。
              </p>
            </div>
          ) : targets.length > 0 ? (
            <label className="grid max-w-sm gap-2 text-sm">
              采用目标
              <select
                aria-label="采用目标"
                className="bg-chrome-hover rounded-lg px-3 py-2"
                value={target}
                disabled={busy}
                onChange={(event) => {
                  setTarget(event.target.value);
                  setShots([]);
                }}
              >
                <option value="">新建分集（同修订会复用已有分集）</option>
                {targets.map((row) => (
                  <option key={row.episodeId} value={row.episodeId}>
                    更新第 {row.episodeNo} 集的指定镜头
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <p className="text-muted-foreground text-xs">
              按原镜建立片段，使用已确认的新角色形象与音色。
            </p>
          )}
          {mode === "follow" && updating && (
            <fieldset className="grid gap-2" disabled={busy}>
              <legend className="mb-2 text-sm">选择要更新的原镜</legend>
              <div className="grid gap-2 sm:grid-cols-2">
                {episode.document?.shots.map((shot, index) => (
                  <label key={shot.id} className="flex items-start gap-2 text-sm">
                    <input
                      className="mt-1"
                      type="checkbox"
                      aria-label={`更新原镜 ${index + 1}`}
                      checked={shots.includes(shot.id)}
                      onChange={(event) =>
                        setShots((prior) =>
                          event.target.checked
                            ? [...prior, shot.id]
                            : prior.filter((value) => value !== shot.id),
                        )
                      }
                    />
                    <span>
                      原镜 {index + 1} · {shot.startMs / 1000}–{shot.endMs / 1000} 秒
                    </span>
                  </label>
                ))}
              </div>
              <p className="text-muted-foreground text-xs">
                只更新勾选镜头的稿件，已采用视频保留。
              </p>
            </fieldset>
          )}
          <Button
            className="justify-self-start"
            disabled={!ready || (!!updating && selected.length === 0)}
            onClick={() => void adopt()}
          >
            {busy ? "正在采用…" : mode === "follow" ? "采用并进入片场" : "采用并编辑剧本"}
          </Button>
        </div>
      )}
      {error && (
        <div role="alert" className="text-destructive flex flex-wrap items-center gap-2 text-sm">
          {error}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setError("");
              setReload((value) => value + 1);
            }}
          >
            重新读取记录
          </Button>
        </div>
      )}
      {related.some((row) => row.deleted) && (
        <p className="text-muted-foreground text-xs">
          部分历史采用的分集已删除，原参考修订仍保留。
        </p>
      )}
    </section>
  );
}
