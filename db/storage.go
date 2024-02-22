package db

import (
	"errors"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log/slog"
)

var (
	ErrCreateDatabase       = errors.New("cannot create a database")
	ErrMigrationFailed      = errors.New("failed to migrate")
	ErrQuery                = errors.New("failed to select")
	ErrCreationFailed       = errors.New("failed to insert new row")
	ErrNotFound             = errors.New("record not found")
	ErrAlreadyExists        = errors.New("record already exists")
	ErrFindOrCreateRelation = errors.New("failed to find or create relation")
)

type Storage struct {
	db *gorm.DB
}

func NewStorage(dsn string) (*Storage, error) {
	db, err := gorm.Open(sqlite.Open(dsn))
	if err != nil {
		slog.Error("Cannot open GORM database", err)

		return nil, ErrCreateDatabase
	}

	return &Storage{db: db}, nil
}

func (s *Storage) Migrate() error {
	slog.Info("Going to start database migrations")

	err := s.db.AutoMigrate(&User{})
	if err != nil {
		slog.Error("User migration failed", err)

		return ErrMigrationFailed
	}

	err = s.db.AutoMigrate(&Chat{})
	if err != nil {
		slog.Error("Chat migration failed", err)

		return ErrMigrationFailed
	}

	err = s.db.AutoMigrate(&ChatMember{})
	if err != nil {
		slog.Error("ChatMember migration failed", err)

		return ErrMigrationFailed
	}

	err = s.db.AutoMigrate(&Group{})
	if err != nil {
		slog.Error("Group migration failed", err)

		return ErrMigrationFailed
	}

	return nil
}

func (s *Storage) FindOrCreateTelegramUser(telegramId int64) (*User, error) {
	user := &User{}

	result := s.db.First(user, telegramId)

	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		slog.Error("Cannot retrieve user from the DB", result.Error)

		return nil, ErrQuery
	}

	if result.RowsAffected != 0 && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		slog.Debug("User found. Returning.")

		return user, nil
	}

	slog.Debug("User not found. Creating new user.")

	user = &User{
		TelegramId: telegramId,
	}

	result = s.db.Create(user)
	if result.Error != nil || result.RowsAffected != 1 {
		slog.Error("Cannot create new user", result.Error, result.RowsAffected)

		return nil, ErrCreationFailed
	}

	return user, nil
}

func (s *Storage) FindOrCreateTelegramChat(telegramId int64) (*Chat, error) {
	chat := &Chat{}

	result := s.db.First(chat, telegramId)

	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		slog.Error("Cannot retrieve chat from the DB", result.Error)

		return nil, ErrQuery
	}

	if result.RowsAffected != 0 && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		slog.Debug("Chat found. Returning.")

		return chat, nil
	}

	slog.Debug("Chat not found. Creating new chat.")

	chat = &Chat{
		TelegramId: telegramId,
	}

	result = s.db.Create(chat)
	if result.Error != nil || result.RowsAffected != 1 {
		slog.Error("Cannot create new chat", result.Error, result.RowsAffected)

		return nil, ErrCreationFailed
	}

	return chat, nil
}

func (s *Storage) FindGroupByChatIdAndTag(chatId int64, tag string) (*Group, error) {
	slog.Debug("FindGroupByChatIdAndTag", chatId, tag)

	group := &Group{}

	result := s.db.Where("chat_telegram_id = ? AND tag = ?", chatId, tag).First(group)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) || result.RowsAffected == 0 {
		return nil, ErrNotFound
	}

	return group, nil
}

func (s *Storage) CreateGroup(chatId int64, tag string, title string) (*Group, error) {
	// TODO: validate tag

	group, err := s.FindGroupByChatIdAndTag(chatId, tag)
	if err == nil {
		slog.Info("Trying to create a group with already existing tag")

		return group, ErrAlreadyExists
	}

	chat, err := s.FindOrCreateTelegramChat(chatId)
	if err != nil {
		slog.Error("Cannot find or create chat while creating a group")

		return nil, ErrFindOrCreateRelation
	}

	group = &Group{
		Chat:  chat,
		Tag:   tag,
		Title: title,
	}

	result := s.db.Create(group)
	if result.Error != nil || result.RowsAffected != 1 {
		slog.Error("Cannot create new group", result.Error, result.RowsAffected)

		return nil, ErrCreationFailed
	}

	return group, nil
}
