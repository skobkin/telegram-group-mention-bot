package storage

import (
	"gorm.io/gorm"
)

// MentionGroup represents a group that can be mentioned
type MentionGroup struct {
	gorm.Model
	Name    string        `gorm:"uniqueIndex:idx_chat_group"`
	ChatID  int64         `gorm:"uniqueIndex:idx_chat_group"`
	Members []GroupMember `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE"`
}

// GroupMember represents a user who is a member of a mention group
type GroupMember struct {
	gorm.Model
	GroupID      uint  `gorm:"uniqueIndex:idx_group_user"`
	UserID       int64 `gorm:"uniqueIndex:idx_group_user"`
	Username     string
	FirstName    string
	LastName     string
	MentionGroup MentionGroup `gorm:"foreignKey:GroupID;references:ID"`
}
