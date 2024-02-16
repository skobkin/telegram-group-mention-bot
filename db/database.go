package db

import (
	"errors"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log/slog"
)

var (
	ErrCreateDatabase  = errors.New("cannot create a database")
	ErrMigrationFailed = errors.New("failed to migrate")
)

type Database struct {
	db *gorm.DB
}

func NewDatabase(dsn string) (*Database, error) {
	db, err := gorm.Open(sqlite.Open(dsn))
	if err != nil {
		slog.Error("Cannot open GORM database", err)

		return nil, ErrCreateDatabase
	}

	return &Database{db: db}, nil
}

func (d *Database) Migrate() error {
	err := d.db.AutoMigrate(&User{})
	if err != nil {
		slog.Error("User migration failed", err)

		return ErrMigrationFailed
	}

	err = d.db.AutoMigrate(&Chat{})
	if err != nil {
		slog.Error("Chat migration failed", err)

		return ErrMigrationFailed
	}

	err = d.db.AutoMigrate(&ChatMember{})
	if err != nil {
		slog.Error("ChatMember migration failed", err)

		return ErrMigrationFailed
	}

	err = d.db.AutoMigrate(&Group{})
	if err != nil {
		slog.Error("Group migration failed", err)

		return ErrMigrationFailed
	}

	return nil
}

func (d *Database) FindOrCreateTelegramUser(id int) *User {
	return &User{}
}
