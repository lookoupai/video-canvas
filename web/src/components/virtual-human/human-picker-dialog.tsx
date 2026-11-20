import { useState } from "react";
import { Check, Loader2 } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import type { VirtualHumanDto } from "@/api/virtual-human/type";
import { AssetCover } from "@/components/assets/asset-card";
import { DialogSession } from "@/components/dialog-session";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useVirtualHumans } from "@/hooks/use-virtual-humans";
import { cn } from "@/lib/utils";
import { readyImages } from "@/utils/virtual-human/virtual-human";

import { FilingStatus } from "./filing-status";

/**
 * 从真人库引入：只有备案可用的真人能选（备案中、失败的置灰并标出状态），确认后交出它可用的图片（按顺序，第一张是封面）。
 * 真人只能在个人资产库里加入和移除，这里只引用。
 * @param open 是否打开
 * @param onOpenChange 打开状态变化
 * @param onConfirm 确认：拿到选中真人的可用图片
 */
export function HumanPickerDialog({
  open,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (images: AssetDto[], human: VirtualHumanDto) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(88svh,40rem)] flex-col sm:max-w-lg">
        <DialogSession open={open}>
          <PickerBody
            onClose={() => onOpenChange(false)}
            onConfirm={(images, human) => {
              onConfirm(images, human);
              onOpenChange(false);
            }}
          />
        </DialogSession>
      </DialogContent>
    </Dialog>
  );
}

function PickerBody({
  onClose,
  onConfirm,
}: {
  onClose: () => void;
  onConfirm: (images: AssetDto[], human: VirtualHumanDto) => void;
}) {
  const { humans, loading, error, reload } = useVirtualHumans(true);
  const [picked, setPicked] = useState<string | null>(null);
  const list = humans ?? [];
  const chosen = list.find((h) => h.id === picked) ?? null;

  const confirm = () => {
    if (!chosen) return;
    const images = readyImages(chosen).flatMap((img) => (img.asset ? [img.asset] : []));
    if (images.length > 0) onConfirm(images, chosen);
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>从真人库引入</DialogTitle>
        <DialogDescription className="sr-only">
          选择一个备案可用的真人，用它的图片作为形象
        </DialogDescription>
      </DialogHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {loading ? (
          <div className="text-muted-foreground grid h-40 place-items-center">
            <Loader2 className="size-5 animate-spin" />
          </div>
        ) : error ? (
          <div className="grid h-40 place-items-center">
            <Button variant="outline" size="sm" onClick={reload}>
              重试
            </Button>
          </div>
        ) : list.length === 0 ? (
          <p className="text-muted-foreground grid h-40 place-items-center text-sm">真人库是空的</p>
        ) : (
          <ul aria-label="真人" className="grid grid-cols-3 gap-2.5 sm:grid-cols-4">
            {list.map((human) => {
              const usable = readyImages(human).length > 0;
              const selected = picked === human.id;
              const cover = human.images[0]?.asset;
              return (
                <li key={human.id}>
                  <button
                    type="button"
                    disabled={!usable}
                    aria-pressed={selected}
                    onClick={() => setPicked(human.id)}
                    className={cn(
                      "bg-muted relative block aspect-3/4 w-full overflow-hidden rounded-xl ring-2 ring-transparent outline-none",
                      "focus-visible:ring-ring/60 disabled:cursor-not-allowed disabled:opacity-45",
                      selected && "ring-primary",
                    )}
                  >
                    {cover && <AssetCover asset={cover} />}
                    {!usable && (
                      <FilingStatus
                        status={human.status}
                        cover
                        className="absolute top-1.5 right-1.5"
                      />
                    )}
                    {selected && (
                      <span className="bg-primary text-primary-foreground absolute top-1.5 right-1.5 grid size-5 place-items-center rounded-full">
                        <Check className="size-3" />
                      </span>
                    )}
                    <span className="from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 flex justify-between gap-1 bg-linear-to-t to-transparent px-2 pt-6 pb-1.5 text-[11px]">
                      <span className="truncate">{human.name}</span>
                      {usable && (
                        <span className="tabular-nums">{readyImages(human).length} 张</span>
                      )}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          取消
        </Button>
        <Button disabled={!chosen} onClick={confirm}>
          引入
        </Button>
      </DialogFooter>
    </>
  );
}
