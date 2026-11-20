import { useEffect, useMemo, useState } from "react";
import { ImagePlus, Loader2, RotateCw, Star, Trash2 } from "lucide-react";
import { toast } from "sonner";

import type { AssetDto } from "@/api/asset/type";
import {
  addVirtualHumanImage,
  listVirtualHumanRefs,
  removeVirtualHuman,
  removeVirtualHumanImage,
  renameVirtualHuman,
  retryVirtualHumanImage,
  setVirtualHumanCover,
  VIRTUAL_HUMAN_MAX_IMAGES,
} from "@/api/virtual-human";
import type {
  VirtualHumanDto,
  VirtualHumanImageDto,
  VirtualHumanRefDto,
} from "@/api/virtual-human/type";
import { AssetCover } from "@/components/assets/asset-card";
import { AssetPickerDialog } from "@/components/assets/asset-picker-dialog";
import { DialogSession } from "@/components/dialog-session";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { humanOfAsset, validateHumanName } from "@/utils/virtual-human/virtual-human";

import { FilingStatus } from "./filing-status";

/** 待确认的移除：整个真人，或其中一张图片 */
type RemoveTarget = { kind: "human" } | { kind: "image"; image: VirtualHumanImageDto };

/**
 * 真人详情：改名、看每张图片的备案状态和失败原因、重新提交、设封面、从资产库加图、移除图片 / 真人。
 * 打开时查一次引用：被角色卡用着的图片不能移除，真人有任一图片被用着也不能移除，确认框里列出是哪些角色卡。
 * 移除会删除方舟侧的素材（不可恢复），个人资产库里的原图不动。
 * @param human 当前真人（取自列表，轮询结果会实时反映进来）；null 是关着
 * @param humans 全部真人，加图时用来标出已在真人库的图片
 * @param filingReady 虚拟人像库是否已配置；没配置时不能加图、不能重新提交
 * @param onClose 关闭
 * @param onChange 真人有变化（改名、加图、设封面、重新提交）
 * @param onRemoved 真人已被移除
 */
export function HumanDetailDialog({
  human,
  humans,
  filingReady,
  onClose,
  onChange,
  onRemoved,
}: {
  human: VirtualHumanDto | null;
  humans: VirtualHumanDto[];
  filingReady: boolean;
  onClose: () => void;
  onChange: (human: VirtualHumanDto) => void;
  onRemoved: (id: string) => void;
}) {
  const open = human !== null;
  /** 关闭时 human 变成 null，正文仍用上一次的数据撑到退出动画播完 */
  const [shown, setShown] = useState(human);
  if (human && human !== shown) setShown(human);
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[min(88svh,46rem)] flex-col gap-0 p-0 sm:max-w-2xl">
        <DialogSession open={open}>
          {shown && (
            <HumanDetail
              human={shown}
              humans={humans}
              filingReady={filingReady}
              onClose={onClose}
              onChange={onChange}
              onRemoved={onRemoved}
            />
          )}
        </DialogSession>
      </DialogContent>
    </Dialog>
  );
}

function HumanDetail({
  human,
  humans,
  filingReady,
  onClose,
  onChange,
  onRemoved,
}: {
  human: VirtualHumanDto;
  humans: VirtualHumanDto[];
  filingReady: boolean;
  onClose: () => void;
  onChange: (human: VirtualHumanDto) => void;
  onRemoved: (id: string) => void;
}) {
  const [refs, setRefs] = useState<VirtualHumanRefDto[] | null>(null);
  const [picking, setPicking] = useState(false);
  const [removing, setRemoving] = useState<RemoveTarget | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  /** 每次打开查一次引用；查不到（请求层已弹提示）按空处理，移除时后端还会再拦一次 */
  useEffect(() => {
    let active = true;
    listVirtualHumanRefs(human.id)
      .then((list) => active && setRefs(list))
      .catch(() => active && setRefs([]));
    return () => {
      active = false;
    };
  }, [human.id]);

  const refsOf = (imageId: string) => (refs ?? []).filter((r) => r.imageId === imageId);
  const owner = useMemo(() => humanOfAsset(humans), [humans]);

  /** 跑一个会返回最新真人的操作：busy 标记防重复点击，失败提示由请求层弹 */
  const run = async (key: string, action: () => Promise<VirtualHumanDto>) => {
    setBusy(key);
    try {
      onChange(await action());
    } catch {
      // 提示由请求层统一弹
    } finally {
      setBusy(null);
    }
  };

  const addImage = (assets: AssetDto[]) => {
    const asset = assets[0];
    if (asset) void run("add", () => addVirtualHumanImage(human.id, asset.id));
  };

  const confirmRemove = async () => {
    const target = removing;
    if (!target) return;
    setRemoving(null);
    setBusy("remove");
    try {
      if (target.kind === "human") {
        await removeVirtualHuman(human.id);
        onRemoved(human.id);
        onClose();
        toast.success(`已移除「${human.name}」`);
        return;
      }
      await removeVirtualHumanImage(target.image.id);
      onChange({ ...human, images: human.images.filter((img) => img.id !== target.image.id) });
    } catch {
      // 提示由请求层统一弹（含被角色卡使用的 409）
    } finally {
      setBusy(null);
    }
  };

  const canAdd = filingReady && human.images.length < VIRTUAL_HUMAN_MAX_IMAGES;

  return (
    <>
      <div className="flex items-center gap-3 px-6 pt-6 pr-14 pb-4">
        <DialogTitle className="sr-only">{human.name}</DialogTitle>
        <DialogDescription className="sr-only">真人的图片与备案状态</DialogDescription>
        <NameField human={human} onChange={onChange} />
        <FilingStatus status={human.status} />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-4">
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          {human.images.map((image, index) => (
            <ImageTile
              key={image.id}
              image={image}
              cover={index === 0}
              usedBy={refsOf(image.id).length}
              busy={busy}
              filingReady={filingReady}
              onRetry={() => void run(`retry-${image.id}`, () => retryVirtualHumanImage(image.id))}
              onCover={() =>
                void run(`cover-${image.id}`, () => setVirtualHumanCover(human.id, image.id))
              }
              onRemove={() => setRemoving({ kind: "image", image })}
            />
          ))}
          {canAdd && (
            <li>
              <button
                type="button"
                onClick={() => setPicking(true)}
                disabled={busy === "add"}
                className="text-muted-foreground hover:bg-chrome-hover ring-chrome-border focus-visible:ring-ring/60 grid aspect-square w-full place-items-center rounded-xl ring-1 ring-dashed outline-none focus-visible:ring-2"
              >
                <span className="flex flex-col items-center gap-1.5 text-xs">
                  {busy === "add" ? (
                    <Loader2 className="size-5 animate-spin" />
                  ) : (
                    <ImagePlus className="size-5" />
                  )}
                  添加图片
                </span>
              </button>
            </li>
          )}
        </ul>
      </div>

      <div className="flex items-center gap-2 border-t px-6 py-4">
        <Button
          variant="ghost"
          className="text-destructive hover:text-destructive"
          disabled={busy !== null || refs === null}
          onClick={() => setRemoving({ kind: "human" })}
        >
          <Trash2 />
          移除真人
        </Button>
        <Button className="ml-auto" onClick={onClose}>
          完成
        </Button>
      </div>

      <AssetPickerDialog
        open={picking}
        onOpenChange={setPicking}
        kind="image"
        limit={1}
        title="选择图片"
        confirmLabel="添加并提交备案"
        reasonOf={(asset) => (owner.has(asset.id) ? "已在真人库" : null)}
        onConfirm={addImage}
      />

      <RemoveConfirm
        human={human}
        target={removing}
        refs={refs ?? []}
        onCancel={() => setRemoving(null)}
        onConfirm={() => void confirmRemove()}
      />
    </>
  );
}

/** 名称：直接在标题位置改，回车或失焦保存；不合法时恢复原名 */
function NameField({
  human,
  onChange,
}: {
  human: VirtualHumanDto;
  onChange: (human: VirtualHumanDto) => void;
}) {
  const [value, setValue] = useState(human.name);
  const save = async () => {
    const result = validateHumanName(value);
    if (!result.ok) {
      toast.error(result.message);
      setValue(human.name);
      return;
    }
    if (result.name === human.name) return;
    try {
      onChange(await renameVirtualHuman(human.id, result.name));
    } catch {
      setValue(human.name);
    }
  };
  return (
    <Input
      value={value}
      aria-label="真人名称"
      onChange={(event) => setValue(event.target.value)}
      onBlur={() => void save()}
      onKeyDown={(event) => event.key === "Enter" && event.currentTarget.blur()}
      className="bg-chrome-hover border-chrome-border h-9 max-w-64 rounded-full text-base font-semibold"
    />
  );
}

/** 一张图片：封面 + 状态 + 失败原因 + 操作 */
function ImageTile({
  image,
  cover,
  usedBy,
  busy,
  filingReady,
  onRetry,
  onCover,
  onRemove,
}: {
  image: VirtualHumanImageDto;
  cover: boolean;
  usedBy: number;
  busy: string | null;
  filingReady: boolean;
  onRetry: () => void;
  onCover: () => void;
  onRemove: () => void;
}) {
  return (
    <li className="bg-chrome ring-chrome-border overflow-hidden rounded-xl ring-1">
      <div className="bg-muted relative aspect-square">
        {image.asset && <AssetCover asset={image.asset} />}
        {cover && (
          <span className="bg-cover-scrim text-cover-foreground absolute top-2 left-2 rounded-full px-2 py-0.5 text-[11px]">
            封面
          </span>
        )}
        <FilingStatus status={image.status} cover className="absolute top-2 right-2" />
      </div>
      <div className="grid gap-1.5 p-2">
        {image.status === "failed" && image.error && (
          <p className="text-destructive text-[11px] leading-snug">{image.error}</p>
        )}
        {usedBy > 0 && <p className="text-muted-foreground text-[11px]">{usedBy} 张角色卡在用</p>}
        <div className="flex flex-wrap gap-1">
          {image.status === "failed" && (
            <Button
              size="xs"
              variant="outline"
              className="rounded-full"
              disabled={!filingReady || busy !== null}
              title={filingReady ? undefined : "管理员尚未配置虚拟人像库"}
              onClick={onRetry}
            >
              {busy === `retry-${image.id}` ? <Loader2 className="animate-spin" /> : <RotateCw />}
              重新提交
            </Button>
          )}
          {!cover && (
            <Button
              size="xs"
              variant="outline"
              className="rounded-full"
              disabled={busy !== null}
              onClick={onCover}
            >
              <Star />
              设为封面
            </Button>
          )}
          <Button
            size="xs"
            variant="ghost"
            className="text-destructive hover:text-destructive rounded-full"
            disabled={busy !== null}
            onClick={onRemove}
          >
            移除
          </Button>
        </div>
      </div>
    </li>
  );
}

/**
 * 移除确认：被角色卡用着时列出位置并禁用确认；图片是真人唯一一张时提示改用移除真人。
 * 其余情况写明方舟侧素材会一起删除、不可恢复。
 */
function RemoveConfirm({
  human,
  target,
  refs,
  onCancel,
  onConfirm,
}: {
  human: VirtualHumanDto;
  target: RemoveTarget | null;
  refs: VirtualHumanRefDto[];
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const blocking =
    target?.kind === "image" ? refs.filter((r) => r.imageId === target.image.id) : refs;
  const lastImage = target?.kind === "image" && human.images.length <= 1;
  const title =
    target?.kind === "image"
      ? `移除图片「${target.image.asset?.fileName ?? "图片"}」？`
      : `移除真人「${human.name}」？`;
  let description = "方舟侧的素材会一起删除，不可恢复；个人资产库里的原图保留。";
  if (blocking.length > 0) description = "正被以下角色卡使用，先在角色卡里换掉或删掉：";
  else if (lastImage) description = `这是「${human.name}」唯一的图片，请改用「移除真人」。`;
  const blocked = blocking.length > 0 || lastImage;

  return (
    <AlertDialog open={target !== null} onOpenChange={(next) => !next && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        {blocking.length > 0 && (
          <ul className="grid max-h-48 gap-1.5 overflow-y-auto text-sm">
            {dedupeRefs(blocking).map((r) => (
              <li
                key={`${r.canvasId}-${r.nodeId}`}
                className="bg-chrome ring-chrome-border rounded-lg px-3 py-2 ring-1"
              >
                《{r.canvasTitle || "未命名画布"}》角色卡「{r.nodeName || "未命名"}」
              </li>
            ))}
          </ul>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={blocked} onClick={onConfirm}>
            确认移除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** 同一张角色卡用了同一真人的多张图时只列一次 */
const dedupeRefs = (refs: VirtualHumanRefDto[]) => {
  const seen = new Set<string>();
  return refs.filter((r) => {
    const key = `${r.canvasId}-${r.nodeId}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
};
