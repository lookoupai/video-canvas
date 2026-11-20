import type { FilingStatus as FilingStatusValue } from "@/api/virtual-human/type";
import { cn } from "@/lib/utils";
import { FILING_LABEL } from "@/utils/virtual-human/virtual-human";

/** 各状态的圆点颜色：备案中闪烁，提示还在等结果 */
const DOT: Record<FilingStatusValue, string> = {
  pending: "bg-amber-500 motion-safe:animate-pulse",
  active: "bg-emerald-500",
  failed: "bg-destructive",
};

/**
 * 备案状态标记：圆点 + 文案。cover 为 true 时用遮罩底，压在图片封面上也看得清。
 * @param status 备案状态
 * @param cover 是否压在封面上
 */
export function FilingStatus({
  status,
  cover,
  className,
}: {
  status: FilingStatusValue;
  cover?: boolean;
  className?: string;
}) {
  return (
    <span
      data-slot="filing-status"
      data-status={status}
      className={cn(
        "inline-flex h-5 shrink-0 items-center gap-1.5 rounded-full px-2 text-[11px] whitespace-nowrap",
        cover
          ? "bg-cover-scrim text-cover-foreground backdrop-blur-sm"
          : "bg-chrome-hover ring-chrome-border text-foreground ring-1",
        className,
      )}
    >
      <span aria-hidden className={cn("size-1.5 rounded-full", DOT[status])} />
      {FILING_LABEL[status]}
    </span>
  );
}
