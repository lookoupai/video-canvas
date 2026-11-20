import { mapAsset } from "@/api/asset";
import service from "@/utils/requests/service";
import type {
  BackendVirtualHumanDto,
  BackendVirtualHumanListDto,
  BackendVirtualHumanRefDto,
  VirtualHumanDto,
  VirtualHumanListDto,
  VirtualHumanRefDto,
} from "./type";

/** 真人名称最多多少个字，和后端一致 */
export const VIRTUAL_HUMAN_NAME_MAX = 30;
/** 每个真人最多几张图，和角色卡一致（1 张主图 + 4 张补充图） */
export const VIRTUAL_HUMAN_MAX_IMAGES = 5;

const mapHuman = (raw: BackendVirtualHumanDto): VirtualHumanDto => ({
  id: raw.id,
  name: raw.name,
  status: raw.status,
  images: (raw.images ?? []).map((img) => ({
    id: img.id,
    asset: img.asset ? mapAsset(img.asset) : null,
    status: img.status,
    error: img.error ?? "",
  })),
  createdAt: raw.created_at,
});

/**
 * 获取当前用户的全部真人（不分页）
 * @param refresh 为 true 时后端先向方舟刷新审核中的图片（同一用户 3 秒一次），页面轮询时传
 * @returns 真人列表和虚拟人像库是否已配置
 */
export const listVirtualHumans = async (refresh = false): Promise<VirtualHumanListDto> => {
  const response = await service.get<BackendVirtualHumanListDto>(
    "/virtual-humans",
    refresh ? { refresh: 1 } : undefined,
  );
  return {
    items: (response.items ?? []).map(mapHuman),
    filingReady: response.filing_ready === true,
  };
};

/**
 * 用个人资产库里的一张图片新建真人，并立即提交方舟备案
 * @param name 名称，调用方先做 1–30 字校验
 * @param assetId 图片的素材 ID
 * @returns 新真人；提交方舟失败时图片状态为 failed，可以重新提交
 */
export const createVirtualHuman = async (name: string, assetId: string): Promise<VirtualHumanDto> =>
  mapHuman(
    await service.post<BackendVirtualHumanDto>("/virtual-humans", {
      name,
      asset_id: Number(assetId),
    }),
  );

/**
 * 修改真人名称（只改本地，不影响方舟侧已备案的素材）
 * @param id 真人 ID
 * @param name 新名称
 * @returns 改名后的真人
 */
export const renameVirtualHuman = async (id: string, name: string): Promise<VirtualHumanDto> =>
  mapHuman(await service.patch<BackendVirtualHumanDto>(`/virtual-humans/${id}`, { name }));

/**
 * 移除真人和它的全部图片，方舟侧素材一起删除（不可恢复）；图片被角色卡使用时后端拒绝
 * @param id 真人 ID
 */
export const removeVirtualHuman = async (id: string): Promise<void> => {
  await service.delete(`/virtual-humans/${id}`);
};

/**
 * 查询真人的图片被哪些角色卡使用（主图或补充图）
 * @param id 真人 ID
 * @returns 引用位置
 */
export const listVirtualHumanRefs = async (id: string): Promise<VirtualHumanRefDto[]> => {
  const response = await service.get<{ items: BackendVirtualHumanRefDto[] | null }>(
    `/virtual-humans/${id}/references`,
  );
  return (response.items ?? []).map((r) => ({
    imageId: r.image_id,
    canvasId: r.canvas_id,
    canvasTitle: r.canvas_title,
    nodeId: r.node_id,
    nodeName: r.node_name,
  }));
};

/**
 * 给真人加一张图片并提交备案
 * @param id 真人 ID
 * @param assetId 图片的素材 ID
 * @returns 更新后的真人
 */
export const addVirtualHumanImage = async (id: string, assetId: string): Promise<VirtualHumanDto> =>
  mapHuman(
    await service.post<BackendVirtualHumanDto>(`/virtual-humans/${id}/images`, {
      asset_id: Number(assetId),
    }),
  );

/**
 * 把一张图片设为封面（排到第一张）
 * @param id 真人 ID
 * @param imageId 图片 ID
 * @returns 更新后的真人
 */
export const setVirtualHumanCover = async (id: string, imageId: string): Promise<VirtualHumanDto> =>
  mapHuman(
    await service.put<BackendVirtualHumanDto>(`/virtual-humans/${id}/cover`, { image_id: imageId }),
  );

/**
 * 重新提交一张备案失败的图片
 * @param imageId 图片 ID
 * @returns 更新后的真人
 */
export const retryVirtualHumanImage = async (imageId: string): Promise<VirtualHumanDto> =>
  mapHuman(await service.post<BackendVirtualHumanDto>(`/virtual-human-images/${imageId}/retry`));

/**
 * 从真人里移除一张图片，方舟侧素材一起删除（不可恢复）；被角色卡使用、或是唯一一张时后端拒绝
 * @param imageId 图片 ID
 */
export const removeVirtualHumanImage = async (imageId: string): Promise<void> => {
  await service.delete(`/virtual-human-images/${imageId}`);
};
