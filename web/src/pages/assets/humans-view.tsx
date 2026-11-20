import { useState } from "react";
import { motion } from "motion/react";

import type { VirtualHumanDto } from "@/api/virtual-human/type";
import { AssetCover } from "@/components/assets/asset-card";
import { AssetGridSkeleton } from "@/components/assets/asset-grid";
import { AssetsToolbar } from "@/components/assets/assets-toolbar";
import { EmptyMark } from "@/components/home/empty-mark";
import { Segmented } from "@/components/home/segmented";
import { Button } from "@/components/ui/button";
import { FilingStatus } from "@/components/virtual-human/filing-status";
import { HumanDetailDialog } from "@/components/virtual-human/human-detail-dialog";
import { useVirtualHumans } from "@/hooks/use-virtual-humans";
import { TAP_CARD } from "@/lib/motion";
import {
  filterHumans,
  humanCounts,
  readyImages,
  type HumanFilter,
} from "@/utils/virtual-human/virtual-human";

/** 筛选标签 */
const FILTERS: { value: HumanFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "active", label: "可用" },
  { value: "pending", label: "备案中" },
  { value: "failed", label: "失败" },
];

/**
 * 资产页「真人库」：在个人资产库里加入的真人，每张图片单独在方舟备案。
 * 按整体状态筛选；有图片审核中时自动轮询结果。点卡片打开详情改名、加图、重新提交、移除。
 * 加入真人库只能从「个人资产库」的图片菜单进入，这里不放新建按钮。
 */
export function HumansView() {
  const { humans, filingReady, loading, error, reload, upsert, drop } = useVirtualHumans(true);
  const [filter, setFilter] = useState<HumanFilter>("all");
  const [openId, setOpenId] = useState<string | null>(null);

  const list = humans ?? [];
  const counts = humanCounts(list);
  const shown = filterHumans(list, filter);
  const opened = list.find((h) => h.id === openId) ?? null;

  return (
    <div className="relative min-h-[60vh]">
      <AssetsToolbar className="mt-6 mb-7">
        <h2 className="flex h-9 items-center pr-2 pl-3 text-[15px] font-semibold">真人库</h2>
        <Segmented<HumanFilter>
          className="ml-auto"
          label="按状态筛选"
          value={filter}
          onChange={setFilter}
          options={FILTERS.map((f) => ({ value: f.value, label: `${f.label} ${counts[f.value]}` }))}
        />
      </AssetsToolbar>

      {loading ? (
        <AssetGridSkeleton />
      ) : error ? (
        <EmptyMark
          title="加载失败"
          hint={
            <Button variant="outline" size="sm" onClick={reload}>
              重试
            </Button>
          }
        />
      ) : list.length === 0 ? (
        <EmptyMark title={filingReady ? "还没有真人" : "管理员尚未配置虚拟人像库"} />
      ) : shown.length === 0 ? (
        <EmptyMark title="没有符合条件的真人" />
      ) : (
        <ul className="grid grid-cols-2 gap-3 md:grid-cols-[repeat(auto-fill,minmax(200px,1fr))] md:gap-4">
          {shown.map((human) => (
            <HumanCard key={human.id} human={human} onOpen={() => setOpenId(human.id)} />
          ))}
        </ul>
      )}

      <HumanDetailDialog
        human={opened}
        humans={list}
        filingReady={filingReady}
        onClose={() => setOpenId(null)}
        onChange={upsert}
        onRemoved={drop}
      />
    </div>
  );
}

/** 一张真人卡：封面 + 状态 + 名称 + 可用张数 */
function HumanCard({ human, onOpen }: { human: VirtualHumanDto; onOpen: () => void }) {
  const cover = human.images[0]?.asset;
  const ready = readyImages(human).length;
  return (
    <li>
      <motion.button
        type="button"
        whileTap={TAP_CARD}
        onClick={onOpen}
        aria-label={human.name}
        className="group/card bg-muted focus-visible:ring-ring/60 relative block aspect-[3/4] w-full overflow-hidden rounded-2xl text-left outline-none [corner-shape:squircle] focus-visible:ring-3"
      >
        {cover && <AssetCover asset={cover} />}
        <FilingStatus status={human.status} cover className="absolute top-2 right-2" />
        <span className="from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 flex items-end justify-between gap-2 bg-linear-to-t to-transparent px-3 pt-10 pb-2.5">
          <span className="truncate text-sm font-medium">{human.name}</span>
          <span className="shrink-0 text-[11px] tabular-nums opacity-80">
            可用 {ready}/{human.images.length}
          </span>
        </span>
      </motion.button>
    </li>
  );
}
