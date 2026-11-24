package storage

type User struct {
	ID        int64 `gorm:"primarykey"`
	Username  string
	FirstName string
	LastName  string
}

type MentionGroup struct {
	ID      uint          `gorm:"primarykey"`
	Name    string        `gorm:"uniqueIndex:idx_chat_group"`
	ChatID  int64         `gorm:"uniqueIndex:idx_chat_group"`
	Members []GroupMember `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE"`
}

type GroupMember struct {
	ID           uint         `gorm:"primarykey"`
	GroupID      uint         `gorm:"uniqueIndex:idx_group_user"`
	UserID       int64        `gorm:"uniqueIndex:idx_group_user"`
	User         User         `gorm:"foreignKey:UserID;references:ID"`
	MentionGroup MentionGroup `gorm:"foreignKey:GroupID;references:ID"`
}

// GroupAlias is an additional short name for a group. Aliases are unique per chat and
// never collide with group names.
type GroupAlias struct {
	ID      uint   `gorm:"primarykey"`
	ChatID  int64  `gorm:"uniqueIndex:idx_chat_alias"`
	Alias   string `gorm:"uniqueIndex:idx_chat_alias"`
	GroupID uint
}
