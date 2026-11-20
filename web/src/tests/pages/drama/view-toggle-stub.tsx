import { Frame, LayoutGrid, type LucideIcon } from "lucide-react";

/** 测试用的「列表 | 画布」切换：只保留行为（高亮、点击回调）和图标，不拉资产管理那一整块 */
export function ViewToggleStub({
  view,
  onChange,
  listLabel = "列表",
  listIcon = LayoutGrid,
}: {
  view: "list" | "canvas";
  onChange: (view: "list" | "canvas") => void;
  listLabel?: string;
  listIcon?: LucideIcon;
}) {
  return (
    <div role="group">
      {(
        [
          ["list", listLabel, listIcon],
          ["canvas", "画布", Frame],
        ] as const
      ).map(([key, label, Icon]) => (
        <button key={key} type="button" aria-pressed={view === key} onClick={() => onChange(key)}>
          <Icon />
          {label}
        </button>
      ))}
    </div>
  );
}
