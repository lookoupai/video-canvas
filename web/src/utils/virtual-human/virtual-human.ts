import { VIRTUAL_HUMAN_NAME_MAX } from "@/api/virtual-human";
import type { AssetDto } from "@/api/asset/type";
import type { AssetItem } from "@/utils/drama/assets";
import type { FilingStatus, VirtualHumanDto, VirtualHumanImageDto } from "@/api/virtual-human/type";

/** 名称校验结果：通过时带去掉首尾空白后的名称，不通过时带可直接展示的原因 */
export type HumanNameResult = { ok: true; name: string } | { ok: false; message: string };

/**
 * 校验真人名称：去首尾空白后 1–30 字（按字符计）。
 * @param raw 输入框里的原文
 * @returns 校验结果
 */
export function validateHumanName(raw: string): HumanNameResult {
  const name = raw.trim();
  if (!name) return { ok: false, message: "请输入名称" };
  if (Array.from(name).length > VIRTUAL_HUMAN_NAME_MAX) {
    return { ok: false, message: `名称最多 ${VIRTUAL_HUMAN_NAME_MAX} 个字` };
  }
  return { ok: true, name };
}

/** 备案状态的展示文案 */
export const FILING_LABEL: Record<FilingStatus, string> = {
  pending: "备案中",
  active: "可用",
  failed: "失败",
};

/** 真人库的筛选：全部或某个整体状态 */
export type HumanFilter = "all" | FilingStatus;

/**
 * 按整体状态筛选真人，保持原顺序
 * @param humans 全部真人
 * @param filter 筛选条件
 * @returns 符合条件的真人
 */
export function filterHumans(humans: VirtualHumanDto[], filter: HumanFilter): VirtualHumanDto[] {
  return filter === "all" ? humans : humans.filter((h) => h.status === filter);
}

/**
 * 各筛选条件下的真人数，显示在筛选标签上
 * @param humans 全部真人
 * @returns 每个筛选条件的数量
 */
export function humanCounts(humans: VirtualHumanDto[]): Record<HumanFilter, number> {
  const counts: Record<HumanFilter, number> = {
    all: humans.length,
    active: 0,
    pending: 0,
    failed: 0,
  };
  for (const h of humans) counts[h.status] += 1;
  return counts;
}

/**
 * 是否需要轮询审核结果：任意一张图片还在审核中（方舟没有回调，只能轮询）
 * @param humans 全部真人
 * @returns 有审核中的图片时为 true
 */
export function shouldPoll(humans: VirtualHumanDto[]): boolean {
  return humans.some((h) => h.images.some((img) => img.status === "pending"));
}

/**
 * 素材 ID → 所在的真人，资产库卡片据此显示「真人 · 名字」、不再提供「加入真人库」
 * @param humans 全部真人
 * @returns 素材 ID 到真人的映射
 */
export function humanOfAsset(humans: VirtualHumanDto[]): Map<string, VirtualHumanDto> {
  const map = new Map<string, VirtualHumanDto>();
  for (const h of humans) {
    for (const img of h.images) {
      if (img.asset) map.set(img.asset.id, h);
    }
  }
  return map;
}

/**
 * 真人里备案可用的图片（按顺序，第一张是封面）：引入到角色卡时只带这些
 * @param human 真人
 * @returns 可用且素材还在的图片
 */
export function readyImages(human: VirtualHumanDto): VirtualHumanImageDto[] {
  return human.images.filter((img) => img.status === "active" && img.asset !== null);
}

/** 资产库弹框「真人库」来源里的一张图 */
export type HumanPickItem = {
  /** 图片 ID */
  imageId: string;
  /** 素材 */
  asset: AssetDto;
  /** 不能选的原因：备案中、备案失败；null 表示可以选 */
  reason: string | null;
};

/** 资产库弹框「真人库」来源里的一个真人和它的图片 */
export type HumanPickGroup = {
  /** 真人 */
  human: VirtualHumanDto;
  /** 图片，按顺序 */
  items: HumanPickItem[];
};

/** 不可用图片在卡片上标的原因 */
const PICK_REASON: Record<FilingStatus, string | null> = {
  active: null,
  pending: "备案中",
  failed: "备案失败",
};

/**
 * 资产库弹框的「真人库」来源：按真人分组列出图片，只有备案可用的能选，其余标出原因（不隐藏，让用户知道还在审核）。
 * 素材已删除的图片不列；没有可列图片的真人也不列。
 * @param humans 全部真人
 * @param q 搜索词，按真人名称匹配
 * @returns 分组
 */
export function humanPickGroups(humans: VirtualHumanDto[], q: string): HumanPickGroup[] {
  const keyword = q.trim().toLowerCase();
  const groups: HumanPickGroup[] = [];
  for (const human of humans) {
    if (keyword && !human.name.toLowerCase().includes(keyword)) continue;
    const items = human.images.flatMap((img) =>
      img.asset ? [{ imageId: img.id, asset: img.asset, reason: PICK_REASON[img.status] }] : [],
    );
    if (items.length > 0) groups.push({ human, items });
  }
  return groups;
}

/**
 * 短剧资产卡能加入真人库的形象图：只有已生成（或已放进）形象图的角色卡才有；
 * 还没生成形象、上传中、生成中、失败的卡，以及不是角色的资产都没有。
 * @param item 短剧资产
 * @returns 形象图素材（只有 id / url / 文件名有意义，文件名取角色名，作为入库时的默认名称）；没有返回 null
 */
export function enrollTarget(item: AssetItem): AssetDto | null {
  const { data } = item;
  if (item.type !== "character" || item.status !== "done") return null;
  if (!data.assetId || !data.src || data.mediaType === "video") return null;
  return {
    id: data.assetId,
    url: data.src,
    kind: "image",
    mimeType: "",
    byteSize: 0,
    width: null,
    height: null,
    durationMs: null,
    fileName: item.name,
  };
}
