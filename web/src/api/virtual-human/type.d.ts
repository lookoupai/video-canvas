import type { AssetDto, BackendAssetDto } from "@/api/asset/type";

/**
 * 真人图片的备案状态
 * - pending：已提交方舟，审核中
 * - active：审核通过，生成时以素材引用（asset://）传给模型
 * - failed：提交失败或审核未通过，可以重新提交
 */
export type FilingStatus = "pending" | "active" | "failed";

/** 后端真人图片（snake_case） */
export interface BackendVirtualHumanImageDto {
  /** 图片 ID（十六进制串） */
  id: string;
  /** 素材；素材已被删除时为 null */
  asset: BackendAssetDto | null;
  /** 备案状态 */
  status: FilingStatus;
  /** 备案失败的原因，其余状态为空串 */
  error: string;
}

/** 后端真人（snake_case） */
export interface BackendVirtualHumanDto {
  /** 真人 ID（十六进制串） */
  id: string;
  /** 名称，1–30 字 */
  name: string;
  /** 整体状态：有一张可用就是 active，否则有审核中的是 pending，否则 failed */
  status: FilingStatus;
  /** 图片，第一张是封面 */
  images: BackendVirtualHumanImageDto[] | null;
  /** 创建时间（ISO） */
  created_at: string;
}

/** 后端真人列表 */
export interface BackendVirtualHumanListDto {
  /** 全部真人，新建的在前 */
  items: BackendVirtualHumanDto[] | null;
  /** 管理员是否已配置好虚拟人像库 */
  filing_ready: boolean;
}

/** 后端引用位置 */
export interface BackendVirtualHumanRefDto {
  /** 被使用的图片 ID */
  image_id: string;
  /** 所在画布 ID */
  canvas_id: string;
  /** 画布标题 */
  canvas_title: string;
  /** 角色卡节点 id */
  node_id: string;
  /** 角色名 */
  node_name: string;
}

/** 前端使用的真人图片 */
export interface VirtualHumanImageDto {
  /** 图片 ID（十六进制串），重新提交、移除、设封面都用它 */
  id: string;
  /** 素材；素材已被删除时为 null */
  asset: AssetDto | null;
  /** 备案状态 */
  status: FilingStatus;
  /** 备案失败的原因，其余状态为空串 */
  error: string;
}

/** 前端使用的真人 */
export interface VirtualHumanDto {
  /** 真人 ID（十六进制串） */
  id: string;
  /** 名称，1–30 字 */
  name: string;
  /** 整体状态 */
  status: FilingStatus;
  /** 图片，第一张是封面 */
  images: VirtualHumanImageDto[];
  /** 创建时间（ISO） */
  createdAt: string;
}

/** 前端使用的真人列表 */
export interface VirtualHumanListDto {
  /** 全部真人，新建的在前 */
  items: VirtualHumanDto[];
  /** 管理员是否已配置好虚拟人像库；false 时不能加入新图片 */
  filingReady: boolean;
}

/** 真人图片被角色卡使用的一处 */
export interface VirtualHumanRefDto {
  /** 被使用的图片 ID */
  imageId: string;
  /** 所在画布 ID */
  canvasId: string;
  /** 画布标题 */
  canvasTitle: string;
  /** 角色卡节点 id */
  nodeId: string;
  /** 角色名 */
  nodeName: string;
}
