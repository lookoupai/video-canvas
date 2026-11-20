package model

import (
	"time"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 id 用十六进制串，与资产库一致
)

// VirtualHuman 真人库里的一个真人：名字 + 若干张图片（VirtualHumanImage）。
// 真人只能在个人资产库里创建和移除；其他地方（角色卡）只引用它的图片，不引用真人本身。
type VirtualHuman struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`                                  // 真人 ID
	UserID    uint64    `gorm:"not null;index:idx_virtual_humans_user" json:"user_id"` // 所属用户
	Name      string    `gorm:"size:30;not null" json:"name"`                          // 名称，1–30 字
	CreatedAt time.Time `json:"created_at"`                                            // 创建时间
	UpdatedAt time.Time `json:"updated_at"`                                            // 更新时间
}

// TableName 表名。
func (VirtualHuman) TableName() string { return "virtual_humans" }

// VirtualHumanImage 真人的一张图片，每张单独在方舟虚拟人像库备案。
// 图片就是个人资产库里的素材（assets 行），不复制文件；同一张素材在一个用户的真人库里最多出现一次。
// 生成时插件执行器按素材 ID 查这张表：备案可用且备案渠道就是任务所走的渠道，才把素材换成 asset://。
type VirtualHumanImage struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`                                                               // 图片 ID
	UserID     uint64    `gorm:"not null;uniqueIndex:uk_virtual_human_images_user_asset,priority:1" json:"user_id"`  // 所属用户
	HumanID    uint64    `gorm:"not null;index:idx_virtual_human_images_human" json:"human_id"`                      // 所属真人
	AssetID    uint64    `gorm:"not null;uniqueIndex:uk_virtual_human_images_user_asset,priority:2" json:"asset_id"` // 素材 ID（assets.id）
	Sort       int       `gorm:"not null;default:0" json:"sort"`                                                     // 顺序，越小越靠前；第一张是封面
	ChannelKey string    `gorm:"size:64;not null;default:''" json:"channel_key"`                                     // 备案所在渠道（提交时启用虚拟人像库的渠道）
	ArkAssetID string    `gorm:"size:128;not null;default:''" json:"-"`                                              // 方舟素材 ID，生成时拼成 asset://<id>
	Status     string    `gorm:"size:16;not null;default:'pending'" json:"status"`                                   // 备案状态：pending / active / failed
	Error      string    `gorm:"size:500;not null;default:''" json:"error"`                                          // 备案失败的原因（给用户看的那句）
	CreatedAt  time.Time `json:"created_at"`                                                                         // 创建时间
	UpdatedAt  time.Time `json:"updated_at"`                                                                         // 更新时间
}

// TableName 表名。
func (VirtualHumanImage) TableName() string { return "virtual_human_images" }

// 真人图片的备案状态。
const (
	FilingPending = "pending" // 已提交方舟，等待审核
	FilingActive  = "active"  // 方舟审核通过，可以用 asset:// 引用
	FilingFailed  = "failed"  // 提交失败或审核未通过
)

// 真人库的上限与长度规则。
const (
	MaxVirtualHumanImages  = 5   // 每个真人最多几张图，和角色卡一致（1 张主图 + 4 张补充图）
	MaxVirtualHumanNameLen = 30  // 名称最多多少个字
	MaxVirtualHumans       = 200 // 每个用户最多几个真人（防滥用）
)

// VirtualHumanImageView 返回给前端的真人图片。
type VirtualHumanImageView struct {
	ID     idcodec.ID `json:"id"`     // 图片 ID（十六进制串）
	Asset  *AssetView `json:"asset"`  // 素材；素材已被删除时为 null
	Status string     `json:"status"` // 备案状态：pending / active / failed
	Error  string     `json:"error"`  // 备案失败的原因
}

// VirtualHumanView 返回给前端的真人。
type VirtualHumanView struct {
	ID        idcodec.ID              `json:"id"`         // 真人 ID（十六进制串）
	Name      string                  `json:"name"`       // 名称
	Status    string                  `json:"status"`     // 整体状态：有一张可用就是 active，否则有备案中的是 pending，否则 failed
	Images    []VirtualHumanImageView `json:"images"`     // 图片，第一张是封面
	CreatedAt time.Time               `json:"created_at"` // 创建时间
}

// VirtualHumanListView 是真人列表（不分页，每用户最多 200 个）。
type VirtualHumanListView struct {
	Items       []VirtualHumanView `json:"items"`        // 全部真人，新建的在前
	FilingReady bool               `json:"filing_ready"` // 管理员是否已配置好虚拟人像库；没配好时不能加入新图片
}

// VirtualHumanRefView 是真人图片被角色卡使用的一处。
type VirtualHumanRefView struct {
	ImageID     idcodec.ID `json:"image_id"`     // 被使用的图片
	CanvasID    idcodec.ID `json:"canvas_id"`    // 所在画布
	CanvasTitle string     `json:"canvas_title"` // 画布标题
	NodeID      string     `json:"node_id"`      // 角色卡节点 id
	NodeName    string     `json:"node_name"`    // 角色名
}

// VirtualHumanRefListView 是真人图片被使用的全部位置。
type VirtualHumanRefListView struct {
	Items []VirtualHumanRefView `json:"items"` // 按画布、节点排列
}

// CreateVirtualHumanReq 新建真人：名称 + 第一张图片。名称的长度与空白校验在服务里做。
type CreateVirtualHumanReq struct {
	Name    string `json:"name" label:"名称"`                              // 名称
	AssetID uint64 `json:"asset_id" binding:"required,min=1" label:"素材"` // 第一张图片的素材 ID
}

// RenameVirtualHumanReq 修改真人名称。
type RenameVirtualHumanReq struct {
	Name string `json:"name" label:"名称"` // 新名称
}

// AddVirtualHumanImageReq 给真人加一张图片。
type AddVirtualHumanImageReq struct {
	AssetID uint64 `json:"asset_id" binding:"required,min=1" label:"素材"` // 素材 ID
}

// SetVirtualHumanCoverReq 把一张图片设为封面（排到第一张）。
type SetVirtualHumanCoverReq struct {
	ImageID idcodec.ID `json:"image_id" binding:"required" label:"图片"` // 图片 ID
}

// CanvasPayloadRow 是查引用时取出的画布内容。
type CanvasPayloadRow struct {
	ID      uint64 // 画布主键
	Title   string // 画布标题
	Payload []byte // 画布内容（payload_json）
}
