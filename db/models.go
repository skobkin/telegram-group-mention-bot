package db

import "gorm.io/gorm"

type User struct {
	gorm.Model
	TelegramId int
}

type Chat struct {
	gorm.Model
	TelegramId int
}

type ChatMember struct {
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
