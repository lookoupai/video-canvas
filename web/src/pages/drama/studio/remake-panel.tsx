import { useCallback, useEffect, useRef, useState } from "react";
import { getAsset } from "@/api/asset";
import type { AssetDto } from "@/api/asset/type";
import type { Segment } from "@/api/drama/type";
import type { SegmentRemake } from "@/api/drama/remake-type";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { IntervalPlayer } from "@/pages/marketing/detail/remake-player";
import { chosenTake } from "@/utils/drama/segments";
import { useStudio } from "./use-studio";

/** 精确窗口独立于模型生成秒数，生成前也会通过编辑器 flush 保存当前输入。 */
function WindowFields({ segment, remake }: { segment: Segment; remake: SegmentRemake }) {
  const { actions, registerEditor, reportSave, plan } = useStudio();
  const { setWindow } = actions;
  const [duration, setDuration] = useState(String(remake.targetDurationMs / 1000));
  const [start, setStart] = useState(String(remake.trimStartMs / 1000));
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [initial, setInitial] = useState({
    duration: remake.targetDurationMs,
    start: remake.trimStartMs,
  });
  const pending = useRef<Promise<void> | null>(null);
  const durationMs = duration.trim() ? Math.round(Number(duration) * 1000) : NaN;
  const startMs = start.trim() ? Math.round(Number(start) * 1000) : NaN;
  const valid =
    Number.isFinite(durationMs) &&
    Number.isFinite(startMs) &&
    durationMs >= 33 &&
    startMs >= 0 &&
    startMs + durationMs <= 86400000;
  const dirty = durationMs !== initial.duration || startMs !== initial.start;
  const changed =
    remake.targetDurationMs !== initial.duration || remake.trimStartMs !== initial.start;
  const conflict = changed && dirty && !busy;
  const saveKey = "remake:" + segment.id;
  const reset = useCallback(() => {
    setInitial({ duration: remake.targetDurationMs, start: remake.trimStartMs });
    setDuration(String(remake.targetDurationMs / 1000));
    setStart(String(remake.trimStartMs / 1000));
    setFailed(false);
  }, [remake.targetDurationMs, remake.trimStartMs]);
  useEffect(() => {
    if (changed && !dirty && !busy) reset();
  }, [changed, dirty, busy, reset]);
  const save = useCallback(async () => {
    if (pending.current) return pending.current;
    if (!dirty) return;
    if (!valid || conflict) {
      setFailed(true);
      throw new Error(conflict ? "裁剪窗口已在别处更改，请先重新载入" : "请填写有效的成片裁剪窗口");
    }
    const work = async () => {
      setBusy(true);
      setFailed(false);
      try {
        await setWindow(segment, durationMs, startMs);
        setInitial({ duration: durationMs, start: startMs });
      } catch (cause) {
        setFailed(true);
        throw cause;
      } finally {
        setBusy(false);
      }
    };
    pending.current = work();
    try {
      await pending.current;
    } finally {
      pending.current = null;
    }
  }, [dirty, valid, conflict, setWindow, segment, durationMs, startMs]);
  useEffect(() => registerEditor({ flush: save }), [registerEditor, save]);
  useEffect(() => {
    if (!dirty || !valid || conflict || busy || failed) return;
    const timer = setTimeout(() => void save().catch(() => undefined), 800);
    return () => clearTimeout(timer);
  }, [dirty, valid, conflict, busy, failed, save]);
  useEffect(() => {
    reportSave(
      saveKey,
      conflict ? "conflict" : busy ? "saving" : failed ? "error" : dirty ? "dirty" : "saved",
    );
  }, [reportSave, saveKey, conflict, busy, failed, dirty]);
  useEffect(() => () => reportSave(saveKey, "saved"), [reportSave, saveKey]);
  const chosen = chosenTake(segment)?.asset;
  const tooShort = valid && !!chosen?.durationMs && startMs + durationMs > chosen.durationMs;
  return (
    <div className="grid gap-2">
      <div className="grid grid-cols-2 gap-3">
        <label className="text-muted-foreground grid gap-1 text-xs">
          成片保留（秒）
          <Input
            aria-label="成片保留秒数"
            type="number"
            min={0.033}
            max={86400}
            step={0.001}
            value={duration}
            disabled={busy}
            onChange={(event) => {
              setDuration(event.target.value);
              setFailed(false);
            }}
          />
        </label>
        <label className="text-muted-foreground grid gap-1 text-xs">
          采用视频起点（秒）
          <Input
            aria-label="采用素材裁剪起点秒"
            type="number"
            min={0}
            max={86400}
            step={0.001}
            value={start}
            disabled={busy}
            onChange={(event) => {
              setStart(event.target.value);
              setFailed(false);
            }}
          />
        </label>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-muted-foreground text-xs">
          本次申请生成 {String(plan?.input.duration ?? segment.seconds)} 秒
        </span>
        <Button
          size="sm"
          variant="outline"
          disabled={busy || !dirty || !valid || conflict}
          onClick={() => void save().catch(() => undefined)}
        >
          {busy ? "保存中…" : "保存裁剪"}
        </Button>
      </div>
      {tooShort && (
        <p role="status" className="text-status-warning text-xs">
          当前采用视频不足以覆盖这个窗口，请调整裁剪或重新生成。
        </p>
      )}
      {failed && !conflict && (
        <p role="alert" className="text-destructive text-xs">
          裁剪未保存，请检查输入后重试
        </p>
      )}
      {conflict && (
        <div role="alert" className="text-destructive flex items-center gap-2 text-xs">
          裁剪窗口已在别处更改
          <Button size="sm" variant="ghost" onClick={reset}>
            重新载入
          </Button>
        </div>
      )}
    </div>
  );
}

/** 原镜回放和视频参考开关使用固定采用依据，不跟随当前参考稿的后续修改。 */
function PanelContent({ segment, remake }: { segment: Segment; remake: SegmentRemake }) {
  const { canReferenceVideo, useReferenceVideo, setReferenceVideo } = useStudio();
  const [asset, setAsset] = useState<AssetDto | null>(null);
  const [failed, setFailed] = useState(false);
  const [play, setPlay] = useState<{ index: number; count: number } | null>(null);
  useEffect(() => {
    let active = true;
    setAsset(null);
    setFailed(false);
    void getAsset(remake.assetId).then(
      (value) => {
        if (active) setAsset(value);
      },
      () => {
        if (active) setFailed(true);
      },
    );
    return () => {
      active = false;
    };
  }, [remake.assetId]);
  const source = play ? remake.sources[play.index] : null;
  return (
    <section aria-label="原镜与成片裁剪" className="border-border grid gap-3 border-y py-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">原镜依据</h3>
        <span className="text-muted-foreground text-xs">已采用 v{remake.referenceRevision}</span>
      </div>
      <div className="flex flex-wrap gap-2">
        {remake.sources.map((item, index) => (
          <Button
            key={item.shotId + ":" + index}
            variant="outline"
            size="sm"
            disabled={!asset}
            onClick={() => setPlay((prior) => ({ index, count: (prior?.count ?? 0) + 1 }))}
          >
            原镜 {index + 1} · {item.startMs / 1000}–{item.endMs / 1000} 秒
          </Button>
        ))}
      </div>
      {asset && source && (
        <IntervalPlayer
          key={play?.count}
          url={asset.url}
          source={{
            refShot: (play?.index ?? 0) + 1,
            start: source.startMs / 1000,
            end: source.endMs / 1000,
          }}
          autoPlay
          onError={() => setFailed(true)}
        />
      )}
      {failed && (
        <p role="status" className="text-destructive text-xs">
          原片不可用，可关闭视频参考后继续制作。
        </p>
      )}
      <label className="flex items-center gap-2 text-xs">
        <input
          type="checkbox"
          aria-label="使用无声原镜参考"
          checked={useReferenceVideo}
          disabled={!canReferenceVideo || remake.sources.length === 0}
          onChange={(event) => setReferenceVideo(event.target.checked)}
        />
        使用原镜视频参考（无声）
      </label>
      {!canReferenceVideo && (
        <p className="text-muted-foreground text-xs">
          当前模型不支持原镜视频参考，使用角色素材与提示词生成。
        </p>
      )}
      <WindowFields segment={segment} remake={remake} />
    </section>
  );
}

/** 只有从参考镜采用的片段展示原镜与精确成片窗口。 */
export function RemakePanel() {
  const { selected } = useStudio();
  return selected?.remake ? (
    <PanelContent
      key={selected.id + ":" + selected.remake.adoptionId}
      segment={selected}
      remake={selected.remake}
    />
  ) : null;
}
