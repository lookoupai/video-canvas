import { IntervalPlayer } from "./remake-player";
import { RemakeProduction } from "./remake-production";
import { useEffect, useState } from "react";
import { Play } from "lucide-react";

import { getRemakePreview } from "@/api/marketing";
import type { RefVideo, RemakePreview, RemakeSource, Round, Script } from "@/api/marketing/type";
import { DialogSession } from "@/components/dialog-session";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

/** 原片回放只使用跟拍采用的区间，成片镜头的时长可以与它不同。 */
function SourceList({
  preview,
  video,
  initialShot,
}: {
  preview: RemakePreview;
  video: RefVideo | null;
  initialShot: number | null;
}) {
  const [source, setSource] = useState<RemakeSource | undefined>(
    initialShot === null
      ? preview.shots.find((shot) => shot.sources.length)?.sources[0]
      : preview.shots[initialShot]?.sources[0],
  );
  const [playCount, setPlayCount] = useState(0);
  const [failed, setFailed] = useState(false);
  const url = video?.id === preview.refVideoAssetId ? video?.url : null;
  return (
    <div className="flex min-h-0 flex-col gap-3">
      <p className="text-muted-foreground text-xs">
        跟拍脚本 v{preview.revision}
        {preview.basisChanged && " · 当前拆解已更新，正在使用原拆解"}
        {preview.partialMapping && " · 历史脚本的映射可能不完整"}
      </p>
      {url && source && (
        <IntervalPlayer
          key={playCount}
          url={url}
          source={source}
          autoPlay={playCount > 0 || initialShot !== null}
          onError={() => setFailed(true)}
        />
      )}
      {(!url || failed) && (
        <p role="status" className="text-muted-foreground text-sm">
          {url ? "原片加载失败" : "原片素材不可用"}
        </p>
      )}
      <ol className="divide-chrome-border min-h-0 overflow-y-auto divide-y">
        {preview.shots.map((shot, index) => (
          <li key={index} className="space-y-2 py-3">
            <div className="flex items-baseline gap-2">
              <b>成片镜 {index + 1}</b>
              <span className="text-muted-foreground text-xs tabular-nums">
                成片 {shot.duration} 秒
              </span>
            </div>
            {shot.scene && <p className="text-sm">{shot.scene}</p>}
            <div className="flex flex-wrap items-center gap-1.5">
              {shot.sources.map((ref) => (
                <Button
                  key={ref.refShot}
                  size="sm"
                  variant={source === ref ? "secondary" : "outline"}
                  aria-pressed={source === ref}
                  disabled={!url}
                  onClick={() => {
                    setSource(ref);
                    setPlayCount((count) => count + 1);
                    setFailed(false);
                  }}
                >
                  <Play />
                  原镜 {ref.refShot} · {ref.start}–{ref.end} 秒
                </Button>
              ))}
              {(!shot.sources.length || shot.unresolved) && (
                <span className="text-muted-foreground text-xs">待关联</span>
              )}
            </div>
          </li>
        ))}
      </ol>
    </div>
  );
}

/** 每次打开、切换脚本或拆解修订时读库，过期请求随本次挂载丢弃。 */
function PreviewBody({
  scriptId,
  video,
  initialShot,
}: {
  scriptId: string;
  video: RefVideo | null;
  initialShot: number | null;
}) {
  const [preview, setPreview] = useState<RemakePreview | null>(null);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    getRemakePreview(scriptId)
      .then((value) => {
        if (active) setPreview(value);
      })
      .catch(() => {
        if (active) setFailed(true);
      });
    return () => {
      active = false;
    };
  }, [scriptId, attempt]);
  if (failed)
    return (
      <Button
        variant="outline"
        onClick={() => {
          setFailed(false);
          setAttempt((n) => n + 1);
        }}
      >
        重新加载来源
      </Button>
    );
  if (!preview) return <p role="status">正在读取逐镜来源…</p>;
  return <SourceList preview={preview} video={video} initialShot={initialShot} />;
}

/** 跟拍脚本的只读来源预览，可从单镜标签直接回放其第一段来源。 */
export function RemakePreviewDialog({
  script,
  round,
  open,
  initialShot,
  onOpenChange,
}: {
  script: Script;
  round: Round;
  open: boolean;
  initialShot: number | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [production, setProduction] = useState(false);
  const [wasOpen, setWasOpen] = useState(open);
  if (wasOpen !== open) {
    setWasOpen(open);
    if (open) setProduction(false);
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className={
          production
            ? "flex max-h-[88svh] flex-col sm:max-w-5xl"
            : "flex max-h-[88svh] flex-col sm:max-w-xl"
        }
      >
        <DialogHeader>
          <DialogTitle>逐镜仿拍</DialogTitle>
          <DialogDescription>{script.title}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-1">
          <Button
            size="sm"
            variant={!production ? "secondary" : "ghost"}
            aria-pressed={!production}
            onClick={() => setProduction(false)}
          >
            来源预览
          </Button>
          <Button
            size="sm"
            variant={production ? "secondary" : "ghost"}
            aria-pressed={production}
            onClick={() => setProduction(true)}
          >
            单镜制作
          </Button>
        </div>
        <DialogSession open={open}>
          {production ? (
            <RemakeProduction
              key={script.id}
              script={script}
              refVideo={round.refVideo}
              initialShot={initialShot}
            />
          ) : (
            <PreviewBody
              key={`${script.id}-${script.revision}-${round.reverseRevision}`}
              scriptId={script.id}
              video={round.refVideo}
              initialShot={initialShot}
            />
          )}
        </DialogSession>
      </DialogContent>
    </Dialog>
  );
}
