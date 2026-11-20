package model

// MarketingRemakePreview 是跟拍脚本某次修订的只读来源预览。
type MarketingRemakePreview struct {
	Revision        int64                 `json:"revision"`
	RefVideoAssetID *uint64               `json:"ref_video_asset_id"`
	BasisChanged    bool                  `json:"basis_changed"`   // 当前拆解与跟拍采用的快照不同
	PartialMapping  bool                  `json:"partial_mapping"` // 历史稿缺少完整映射或采用的拆解
	Shots           []MarketingRemakeShot `json:"shots"`
}

// MarketingRemakeShot 按成片镜序返回，目标时长独立于来源区间。
type MarketingRemakeShot struct {
	DurationMs int                     `json:"duration_ms"`
	Scene      string                  `json:"scene"`
	Sources    []MarketingRemakeSource `json:"sources"`
	Unresolved bool                    `json:"unresolved"` // 至少一个映射缺少可靠区间
}

// MarketingRemakeSource 的起止毫秒均为原片本地时间，区间左闭右开。
type MarketingRemakeSource struct {
	RefShot int `json:"ref_shot"`
	StartMs int `json:"start_ms"`
	EndMs   int `json:"end_ms"`
}
