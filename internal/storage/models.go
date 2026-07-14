package storage

import (
	"time"
)

// Feed represents a feed to be updated.
type Feed struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)"`
	URL          string    `gorm:"uniqueIndex;not null;type:text"`
	Etag         string    `gorm:"type:text"`
	LastModified string    `gorm:"type:text"`
	Active       bool      `gorm:"not null;default:true"`
	CreatedAt    time.Time `gorm:"not null"`
	Channels     []Channel `gorm:"many2many:feed_channels;joinForeignKey:FeedID;joinReferences:ChannelID"`
}

// TableName overrides the default table name for Feed.
func (Feed) TableName() string {
	return "feeds"
}

// Update represents a parsed update from a feed.
type Update struct {
	ID                string     `gorm:"primaryKey;type:varchar(36)"`
	FeedID            string     `gorm:"index;index:idx_updates_feed_important_published,priority:1;not null"`
	Fingerprint       string     `gorm:"uniqueIndex;not null;type:text"`
	Title             string     `gorm:"type:text"`
	SourceURL         string     `gorm:"type:text"`
	PublishedAt       time.Time  `gorm:"not null;index:idx_updates_feed_important_published,priority:3"`
	CreatedAt         time.Time  `gorm:"not null"`
	VerdictImportant  *bool      `gorm:"index:idx_updates_feed_important_published,priority:2"` // Nullable
	VerdictCategory   string     `gorm:"type:text"`
	VerdictConfidence float64    `gorm:"type:real"`
	VerdictReason     string     `gorm:"type:text"`
	ClassifiedAt      *time.Time `gorm:"index"` // Nullable; indexed for the pending-reconcile scan

	RawContent *RawContent `gorm:"foreignKey:UpdateID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the default table name for Update.
func (Update) TableName() string {
	return "updates"
}

// RawContent represents the raw, unparsed content of an update.
type RawContent struct {
	UpdateID  string    `gorm:"primaryKey;type:varchar(36)"`
	Content   string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

// TableName overrides the default table name for RawContent.
func (RawContent) TableName() string {
	return "raw_contents"
}

// Channel represents a notification channel (e.g. Telegram, Slack, Webhook).
type Channel struct {
	ID         string `gorm:"primaryKey;type:varchar(36)"`
	Name       string `gorm:"uniqueIndex;not null;type:text"`
	Type       string `gorm:"type:text;not null"`
	ConfigJSON string `gorm:"type:text;not null"`
	Feeds      []Feed `gorm:"many2many:feed_channels;joinForeignKey:ChannelID;joinReferences:FeedID"`
}

// TableName overrides the default table name for Channel.
func (Channel) TableName() string {
	return "channels"
}

// Dispatch represents the delivery status of an update to a channel.
type Dispatch struct {
	UpdateID    string    `gorm:"primaryKey;type:varchar(36)"`
	ChannelID   string    `gorm:"primaryKey;type:varchar(36)"`
	DeliveredAt time.Time `gorm:"not null"`
}

// TableName overrides the default table name for Dispatch.
func (Dispatch) TableName() string {
	return "dispatches"
}
