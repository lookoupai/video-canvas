import { useState } from "react";
import type { DramaReference, ReferenceDocument, ReferenceRole } from "@/api/drama/reference-type";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { IntervalPlayer } from "@/pages/marketing/detail/remake-player";

/** 一集校订稿在切换到其他集时由父层保留。 */
export interface ReferenceDraft {
  /** 用户校订后的完整文档 */
  document: ReferenceDocument;
  /** 本集身份与全剧角色的对应 */
  bindings: Record<string, string>;
}
/** 原片分镜、跨镜对白以及本集角色映射共用同一修订保存。 */
export function ReferenceEditor({
  episode,
  draft,
  roles,
  disabled,
  onChange,
  onSave,
}: {
  episode: DramaReference;
  draft: ReferenceDraft;
  roles: ReferenceRole[];
  disabled: boolean;
  onChange: (draft: ReferenceDraft) => void;
  onSave: () => void;
}) {
  const [playing, setPlaying] = useState<{ refShot: number; start: number; end: number } | null>(
    null,
  );
  const [playCount, setPlayCount] = useState(0);
  const [mediaFailed, setMediaFailed] = useState(false);
  const [name, setName] = useState("");
  const doc = draft.document;
  const setDoc = (document: ReferenceDocument) => onChange({ ...draft, document });
  const invalid = [...doc.shots, ...doc.dialogues].some(
    (s) =>
      !Number.isFinite(s.startMs) ||
      !Number.isFinite(s.endMs) ||
      s.startMs < 0 ||
      s.endMs <= s.startMs ||
      s.endMs > episode.durationMs,
  );
  const play = (startMs: number, endMs: number, index: number) => {
    setPlaying({ refShot: index + 1, start: startMs / 1000, end: endMs / 1000 });
    setPlayCount((n) => n + 1);
    setMediaFailed(false);
  };
  const updateShot = (index: number, field: "action" | "camera", value: string) =>
    setDoc({
      ...doc,
      shots: doc.shots.map((s, i) => (i === index ? { ...s, [field]: value } : s)),
    });
  const updateDialogue = (index: number, patch: Partial<ReferenceDocument["dialogues"][number]>) =>
    setDoc({
      ...doc,
      dialogues: doc.dialogues.map((d, i) => (i === index ? { ...d, ...patch } : d)),
    });
  return (
    <div className="grid min-w-0 items-start gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <section className="min-w-0 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="font-semibold">原片镜头</h2>
          <span className="text-muted-foreground text-xs">
            原片 {episode.durationMs / 1000} 秒 · 修订 {episode.revision}
          </span>
        </div>
        {playing && episode.asset && (
          <IntervalPlayer
            key={playCount}
            url={episode.asset.url}
            source={playing}
            autoPlay
            onError={() => setMediaFailed(true)}
          />
        )}
        {mediaFailed && <p role="status">原片播放失败</p>}
        {!episode.asset && <p role="status">原片素材不可用</p>}
        <div className="divide-chrome-border divide-y">
          {doc.shots.map((shot, index) => (
            <article key={shot.id} className="grid gap-2 py-3">
              <Button
                variant="outline"
                size="sm"
                className="justify-self-start"
                disabled={!episode.asset}
                onClick={() => play(shot.startMs, shot.endMs, index)}
              >
                原镜 {index + 1} · {shot.startMs / 1000}–{shot.endMs / 1000} 秒
              </Button>
              <span className="text-muted-foreground text-xs">
                {doc.scenes.find((s) => s.id === shot.sceneId)?.title}
              </span>
              <Textarea
                aria-label={`原镜 ${index + 1} 动作`}
                value={shot.action}
                maxLength={4000}
                disabled={disabled}
                onChange={(e) => updateShot(index, "action", e.target.value)}
                className="max-h-36"
              />
              <Input
                aria-label={`原镜 ${index + 1} 运镜`}
                value={shot.camera}
                maxLength={1000}
                disabled={disabled}
                onChange={(e) => updateShot(index, "camera", e.target.value)}
              />
            </article>
          ))}
        </div>
      </section>
      <section className="min-w-0 space-y-5">
        <div className="flex items-center justify-between gap-2">
          <h2 className="font-semibold">角色与对白</h2>
          <Button disabled={disabled || invalid} onClick={onSave}>
            保存校订
          </Button>
        </div>
        <div className="grid gap-3">
          {doc.characters.map((c, index) => (
            <div key={c.id} className="grid gap-1 sm:grid-cols-2">
              <Input
                aria-label={`本集角色 ${index + 1} 名字`}
                value={c.name}
                maxLength={255}
                disabled={disabled}
                onChange={(e) =>
                  setDoc({
                    ...doc,
                    characters: doc.characters.map((item) =>
                      item.id === c.id ? { ...item, name: e.target.value } : item,
                    ),
                  })
                }
              />
              <select
                aria-label={`${c.name}对应的全剧角色`}
                className="bg-chrome-hover min-w-0 rounded-lg px-3 py-2"
                value={draft.bindings[c.id] ?? ""}
                disabled={disabled}
                onChange={(e) =>
                  onChange({ ...draft, bindings: { ...draft.bindings, [c.id]: e.target.value } })
                }
              >
                <option value="">待关联全剧角色</option>
                {roles.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name}
                  </option>
                ))}
              </select>
            </div>
          ))}
          <div className="flex items-center gap-2">
            <Input
              aria-label="新增本集角色"
              placeholder="角色名"
              maxLength={255}
              value={name}
              disabled={disabled}
              onChange={(e) => setName(e.target.value)}
            />
            <Button
              variant="outline"
              size="sm"
              disabled={disabled || !name.trim()}
              onClick={() => {
                setDoc({
                  ...doc,
                  characters: [...doc.characters, { id: crypto.randomUUID(), name: name.trim() }],
                });
                setName("");
              }}
            >
              添加人物
            </Button>
          </div>
        </div>
        <div className="flex justify-between">
          <h3 className="font-medium">对白校订</h3>
          <Button
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={() =>
              setDoc({
                ...doc,
                dialogues: [
                  ...doc.dialogues,
                  {
                    id: crypto.randomUUID(),
                    startMs: 0,
                    endMs: Math.min(2000, episode.durationMs),
                    text: "",
                    speaker: "",
                  },
                ],
              })
            }
          >
            添加对白
          </Button>
        </div>
        {doc.dialogues.length === 0 && (
          <p className="text-muted-foreground text-sm">暂无对白记录</p>
        )}
        {doc.dialogues.map((cue, index) => (
          <div key={cue.id} className="border-chrome-border grid gap-2 border-t pt-3">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                disabled={!episode.asset || cue.startMs < 0 || cue.endMs <= cue.startMs}
                onClick={() => play(cue.startMs, cue.endMs, index)}
              >
                回放对白 {index + 1}
              </Button>
              <select
                className="bg-chrome-hover rounded-lg px-3 py-2"
                aria-label={`对白 ${index + 1} 说话人`}
                value={cue.speaker}
                disabled={disabled}
                onChange={(e) => updateDialogue(index, { speaker: e.target.value })}
              >
                <option value="">说话人待确认</option>
                {doc.characters.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
              <Button
                variant="ghost"
                size="sm"
                disabled={disabled}
                aria-label={`删除对白 ${index + 1}`}
                onClick={() =>
                  setDoc({ ...doc, dialogues: doc.dialogues.filter((d) => d.id !== cue.id) })
                }
              >
                删除
              </Button>
            </div>
            <div className="flex items-center gap-2">
              <Input
                type="number"
                className="w-24"
                aria-label={`对白 ${index + 1} 起点秒`}
                step=".001"
                min={0}
                max={episode.durationMs / 1000}
                value={Number.isFinite(cue.startMs) ? cue.startMs / 1000 : ""}
                disabled={disabled}
                onChange={(e) =>
                  updateDialogue(index, { startMs: Math.round(e.target.valueAsNumber * 1000) })
                }
              />
              –
              <Input
                type="number"
                className="w-24"
                aria-label={`对白 ${index + 1} 终点秒`}
                step=".001"
                min={0}
                max={episode.durationMs / 1000}
                value={Number.isFinite(cue.endMs) ? cue.endMs / 1000 : ""}
                disabled={disabled}
                onChange={(e) =>
                  updateDialogue(index, { endMs: Math.round(e.target.valueAsNumber * 1000) })
                }
              />
              <span className="text-muted-foreground text-xs">秒</span>
            </div>
            <Textarea
              aria-label={`对白 ${index + 1} 文本`}
              value={cue.text}
              maxLength={4000}
              disabled={disabled}
              className="max-h-32"
              onChange={(e) => updateDialogue(index, { text: e.target.value })}
            />
          </div>
        ))}
        {invalid && (
          <p role="status" className="text-destructive text-sm">
            请检查对白时间，区间须在本集原片内。
          </p>
        )}
      </section>
    </div>
  );
}
