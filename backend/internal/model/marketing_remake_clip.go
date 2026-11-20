package model

// MarketingRemakeClip 记录同一用户、原素材、裁切参数唯一采用的派生素材。
type MarketingRemakeClip struct {
	ID       uint64 `gorm:"primaryKey"`
	UserID   uint64 `gorm:"not null;uniqueIndex:uk_mkt_remake_clip,priority:1"`
	SourceID uint64 `gorm:"not null;uniqueIndex:uk_mkt_remake_clip,priority:2"`
	Recipe   string `gorm:"size:64;not null;uniqueIndex:uk_mkt_remake_clip,priority:3"`
	AssetID  uint64 `gorm:"not null"`
}

// TableName 返回裁片缓存表名。
func (MarketingRemakeClip) TableName() string { return "marketing_remake_clips" }
