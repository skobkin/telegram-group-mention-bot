package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	TelegramId int64
}

type Chat struct {
	gorm.Model
	TelegramId int64
}

type ChatMember struct {
	gorm.Model
	Chat Chat
	User User
}

type Group struct {
	gorm.Model
	Id      int
	Chat    *Chat
	Tag     string
	Title   string
	Members []ChatMember
}
