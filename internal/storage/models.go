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
