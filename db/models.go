package db

type User struct {
	TelegramId int64 `gorm:"primaryKey"`
}

type Chat struct {
	TelegramId int64 `gorm:"primaryKey"`
}

type ChatMember struct {
	ID             int `gorm:"primaryKey"`
	ChatTelegramId int64
	Chat           *Chat `gorm:"foreignKey:ChatTelegramId;references:TelegramId"`
	UserTelegramId int64
	User           *User    `gorm:"foreignKey:UserTelegramId;references:TelegramId"`
	Groups         []*Group `gorm:"many2many:group_members;"`
}

type Group struct {
	Id             int `gorm:"primaryKey"`
	ChatTelegramId int64
	Chat           *Chat `gorm:"foreignKey:ChatTelegramId;references:TelegramId"`
	Tag            string
	Title          string
	Members        []*ChatMember `gorm:"many2many:group_members;"`
}
