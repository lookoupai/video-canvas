import { useState, type FormEvent } from "react";
import { Check, Loader2 } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import {
  addVirtualHumanImage,
  createVirtualHuman,
  VIRTUAL_HUMAN_MAX_IMAGES,
} from "@/api/virtual-human";
import type { VirtualHumanDto } from "@/api/virtual-human/type";
import { AssetCover } from "@/components/assets/asset-card";
import { DialogSession } from "@/components/dialog-session";
import { Segmented } from "@/components/home/segmented";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { validateHumanName } from "@/utils/virtual-human/virtual-human";

/** 加入方式：新建一个真人，或加到已有真人下面 */
type EnrollMode = "new" | "existing";

/**
 * 把个人资产库里的一张图片加入真人库：新建真人（名称默认取文件名），或加到已有真人（图片满 5 张的不能选）。
 * 确认后立即提交方舟备案；业务错误（已在真人库、未配置等）由请求层弹出，弹窗保持打开。
 * @param asset 要加入的图片；null 是关着
 * @param humans 已有的真人，选「加到已有真人」时列出
 * @param onClose 关闭
 * @param onDone 加入成功，拿到新建或更新后的真人
 */
export function EnrollDialog({
  asset,
  humans,
  onClose,
  onDone,
}: {
  asset: AssetDto | null;
  humans: VirtualHumanDto[];
  onClose: () => void;
  onDone: (human: VirtualHumanDto) => void;
}) {
  const open = asset !== null;
  /** 关闭时 asset 变成 null，正文仍用上一次的图片撑到退出动画播完 */
  const [shown, setShown] = useState(asset);
  if (asset && asset !== shown) setShown(asset);
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[min(88svh,40rem)] flex-col sm:max-w-md">
        <DialogSession open={open}>
          {shown && <EnrollForm asset={shown} humans={humans} onClose={onClose} onDone={onDone} />}
        </DialogSession>
      </DialogContent>
    </Dialog>
  );
}

/** 文件名去掉扩展名和「_正面」这类后缀，作为默认名称 */
const defaultName = (asset: AssetDto) =>
  (asset.fileName ?? "")
    .replace(/\.[^.]+$/, "")
    .split(/[_\-\s]/)[0]
    ?.slice(0, 30) ?? "";

function EnrollForm({
  asset,
  humans,
  onClose,
  onDone,
}: {
  asset: AssetDto;
  humans: VirtualHumanDto[];
  onClose: () => void;
  onDone: (human: VirtualHumanDto) => void;
}) {
  const [mode, setMode] = useState<EnrollMode>("new");
  const [name, setName] = useState(() => defaultName(asset));
  const [target, setTarget] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [touched, setTouched] = useState(false);
  const nameResult = validateHumanName(name);
  const canSubmit = mode === "new" ? nameResult.ok : target !== null;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (!canSubmit || saving) return;
    setSaving(true);
    try {
      const human =
        mode === "new" && nameResult.ok
          ? await createVirtualHuman(nameResult.name, asset.id)
          : await addVirtualHumanImage(target as string, asset.id);
      onDone(human);
      onClose();
    } catch {
      // 提示由请求层统一弹，弹窗保持打开
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col gap-4">
      <DialogHeader>
        <DialogTitle>加入真人库</DialogTitle>
        <DialogDescription className="sr-only">
          把这张图片作为真人形象提交方舟备案
        </DialogDescription>
      </DialogHeader>

      <div className="flex items-center gap-3">
        <div className="bg-muted relative size-14 shrink-0 overflow-hidden rounded-lg">
          <AssetCover asset={asset} />
        </div>
        <span className="text-muted-foreground min-w-0 truncate text-sm">
          {asset.fileName ?? "图片"}
        </span>
      </div>

      {humans.length > 0 && (
        <Segmented<EnrollMode>
          label="加入方式"
          value={mode}
          onChange={setMode}
          options={[
            { value: "new", label: "新建真人" },
            { value: "existing", label: "加到已有真人" },
          ]}
        />
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {mode === "new" ? (
          <div className="grid gap-1.5">
            <Input
              autoFocus
              value={name}
              aria-label="真人名称"
              aria-invalid={touched && !nameResult.ok}
              placeholder="名称"
              onChange={(event) => setName(event.target.value)}
              onFocus={(event) => event.currentTarget.select()}
            />
            {touched && !nameResult.ok && (
              <span role="alert" className="text-destructive text-xs">
                {nameResult.message}
              </span>
            )}
          </div>
        ) : (
          <ul aria-label="选择真人" className="grid grid-cols-3 gap-2 sm:grid-cols-4">
            {humans.map((human) => {
              const full = human.images.length >= VIRTUAL_HUMAN_MAX_IMAGES;
              const cover = human.images[0]?.asset;
              const selected = target === human.id;
              return (
                <li key={human.id}>
                  <button
                    type="button"
                    disabled={full}
                    aria-pressed={selected}
                    title={full ? `已有 ${VIRTUAL_HUMAN_MAX_IMAGES} 张图片` : undefined}
                    onClick={() => setTarget(human.id)}
                    className={cn(
                      "bg-muted relative block aspect-square w-full overflow-hidden rounded-xl ring-2 ring-transparent outline-none",
                      "focus-visible:ring-ring/60 disabled:cursor-not-allowed disabled:opacity-40",
                      selected && "ring-primary",
                    )}
                  >
                    {cover && <AssetCover asset={cover} />}
                    {selected && (
                      <span className="bg-primary text-primary-foreground absolute top-1.5 right-1.5 grid size-5 place-items-center rounded-full">
                        <Check className="size-3" />
                      </span>
                    )}
                    <span className="from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 flex justify-between gap-1 bg-linear-to-t to-transparent px-2 pt-5 pb-1.5 text-[11px]">
                      <span className="truncate">{human.name}</span>
                      <span className="tabular-nums">
                        {human.images.length}/{VIRTUAL_HUMAN_MAX_IMAGES}
                      </span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose} disabled={saving}>
          取消
        </Button>
        <Button
          type="submit"
          disabled={saving || (touched && !canSubmit) || (mode === "existing" && !target)}
        >
          {saving && <Loader2 className="animate-spin" />}
          加入并提交备案
        </Button>
      </DialogFooter>
    </form>
  );
}
