package storage

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"

	"telegram-group-mention-bot/internal/storage/migrations"
)

var (
	// Validation errors
	ErrEmptyGroupName = errors.New("group name cannot be empty")
	ErrZeroUserID     = errors.New("user ID cannot be zero")
	ErrNilUser        = errors.New("user cannot be nil")

	// ErrAlreadyExists means a group name or alias is already taken in the chat.
	ErrAlreadyExists = errors.New("already exists")

	// Operation errors
	ErrNotFound = errors.New("not found")
	ErrCreate   = errors.New("failed to create")
	ErrDelete   = errors.New("failed to delete")
	ErrGet      = errors.New("failed to get")
	ErrUpdate   = errors.New("failed to update")

	// Migration errors
	ErrMigrate   = errors.New("failed to migrate database")
	ErrConnectDB = errors.New("failed to connect to database")
)

type Storage struct {
	db   *gorm.DB
	path string
}

// Close releases the underlying database connection pool.
func (s *Storage) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}

	return sqlDB.Close()
}

// New opens (and migrates) the SQLite database at dbPath.
func New(dbPath string) (*Storage, error) {
	db, err := gorm.Open(sqlite.Open(dsn(dbPath)), &gorm.Config{
		// GORM's own logger prints "record not found" noise and inlines query values to stdout;
		// storage methods log their real errors through slog instead.
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		slog.Error("storage: Failed to connect to database", "error", err, "path", dbPath)

		return nil, errors.Join(ErrConnectDB, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, errors.Join(ErrConnectDB, err)
	}

	if err := migrations.Apply(context.Background(), sqlDB); err != nil {
		_ = sqlDB.Close()
		slog.Error("storage: Failed to migrate database", "error", err)

		return nil, errors.Join(ErrMigrate, err)
	}

	return &Storage{db: db, path: dbPath}, nil
}

// dsn adds the pragmas every connection needs: enforced foreign keys (so
// ON DELETE CASCADE works), WAL journaling and a busy timeout to avoid
// "database is locked" under concurrent handlers.
func dsn(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}

	return path + sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

// CreateGroup creates a new mention group in a chat
func (s *Storage) CreateGroup(ctx context.Context, name string, chatID int64) error {
	name = NormalizeName(name)
	if name == "" {
		return ErrEmptyGroupName
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		taken, err := nameTaken(tx, chatID, name)
		if err != nil {
			return err
		}

		if taken {
			return ErrAlreadyExists
		}

		return tx.Create(&MentionGroup{Name: name, ChatID: chatID}).Error
	})
	if errors.Is(err, ErrAlreadyExists) {
		return err
	}

	if err != nil {
		slog.Error("storage: Failed to create group", "error", err, "name", name, "chat_id", chatID)

		return errors.Join(ErrCreate, err)
	}

	return nil
}

// GetGroup retrieves a group by name and chat ID
func (s *Storage) GetGroup(ctx context.Context, name string, chatID int64) (*MentionGroup, error) {
	name = NormalizeName(name)

	var group MentionGroup
	result := s.db.WithContext(ctx).Where("name = ? AND chat_id = ?", name, chatID).First(&group)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.Join(ErrNotFound, result.Error)
		}
		slog.Error("storage: Failed to get group", "error", result.Error, "name", name, "chat_id", chatID)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return &group, nil
}

// GetGroupByID retrieves a group with its members by primary key.
func (s *Storage) GetGroupByID(ctx context.Context, id uint) (*MentionGroup, error) {
	var group MentionGroup

	result := s.db.WithContext(ctx).Preload("Members.User").First(&group, id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.Join(ErrNotFound, result.Error)
		}

		slog.Error("storage: Failed to get group by ID", "error", result.Error, "group_id", id)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return &group, nil
}

// GetUser retrieves a user by ID
func (s *Storage) GetUser(ctx context.Context, userID int64) (*User, error) {
	var user User
	result := s.db.WithContext(ctx).First(&user, userID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.Join(ErrNotFound, result.Error)
		}
		slog.Error("storage: Failed to get user", "error", result.Error, "user_id", userID)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return &user, nil
}

// CreateOrUpdateUser creates a new user or updates an existing one
func (s *Storage) CreateOrUpdateUser(ctx context.Context, userID int64, username, firstName, lastName string) (*User, error) {
	if userID == 0 {
		return nil, ErrZeroUserID
	}

	user := User{
		ID:        userID,
		Username:  username,
		FirstName: firstName,
		LastName:  lastName,
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(&user).Error; err != nil {
		slog.Error("storage: Failed to create/update user", "error", err, "user_id", userID)

		return nil, errors.Join(ErrCreate, err)
	}

	return &user, nil
}

// AddMember adds a user to a group. It is idempotent: it reports whether a
// new membership row was created (false if the user was already a member).
func (s *Storage) AddMember(ctx context.Context, groupID uint, user *User) (bool, error) {
	if user == nil {
		return false, ErrNilUser
	}

	member := GroupMember{
		GroupID: groupID,
		UserID:  user.ID,
	}

	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&member)
	if result.Error != nil {
		slog.Error("storage: Failed to add member", "error", result.Error, "group_id", groupID, "user_id", user.ID)

		return false, errors.Join(ErrCreate, result.Error)
	}

	return result.RowsAffected > 0, nil
}

// RemoveMember removes a user from a mention group and reports whether the
// user was a member.
func (s *Storage) RemoveMember(ctx context.Context, groupID uint, userID int64) (bool, error) {
	result := s.db.WithContext(ctx).Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&GroupMember{})
	if result.Error != nil {
		slog.Error("storage: Failed to remove member", "error", result.Error,
			"group_id", groupID, "user_id", userID)

		return false, errors.Join(ErrDelete, result.Error)
	}

	return result.RowsAffected > 0, nil
}

// GetGroupMembers retrieves all members of a group
func (s *Storage) GetGroupMembers(ctx context.Context, groupID uint) ([]GroupMember, error) {
	var members []GroupMember
	result := s.db.WithContext(ctx).Preload("User").Where("group_id = ?", groupID).Find(&members)
	if result.Error != nil {
		slog.Error("storage: Failed to get group members", "error", result.Error, "group_id", groupID)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return members, nil
}

// DeleteGroup deletes a group by ID
func (s *Storage) DeleteGroup(ctx context.Context, groupID uint) error {
	result := s.db.WithContext(ctx).Delete(&MentionGroup{}, groupID)
	if result.Error != nil {
		slog.Error("storage: Failed to delete group", "error", result.Error, "group_id", groupID)

		return errors.Join(ErrDelete, result.Error)
	}

	return nil
}

// DeleteEmptyGroups removes, in one transaction, every group of the chat that has no
// members (except the one named keepName) and returns the removed names.
func (s *Storage) DeleteEmptyGroups(ctx context.Context, chatID int64, keepName string) ([]string, error) {
	var removed []string

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groups []MentionGroup
		if err := tx.Where("chat_id = ? AND name <> ? AND id NOT IN (SELECT group_id FROM group_members)", chatID, keepName).
			Order("name").Find(&groups).Error; err != nil {
			return err
		}

		for _, group := range groups {
			if err := tx.Delete(&MentionGroup{}, group.ID).Error; err != nil {
				return err
			}

			removed = append(removed, group.Name)
		}

		return nil
	})
	if err != nil {
		slog.Error("storage: Failed to delete empty groups", "error", err, "chat_id", chatID)

		return nil, errors.Join(ErrDelete, err)
	}

	return removed, nil
}

func (s *Storage) GetGroupsByChat(ctx context.Context, chatID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.WithContext(ctx).Where("chat_id = ?", chatID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups", "error", result.Error, "chat_id", chatID)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return groups, nil
}

// IsMember checks if a user is a member of a group
func (s *Storage) IsMember(ctx context.Context, groupID uint, userID int64) (bool, error) {
	var count int64
	result := s.db.WithContext(ctx).Model(&GroupMember{}).Where("group_id = ? AND user_id = ?", groupID, userID).Count(&count)
	if result.Error != nil {
		slog.Error("storage: Failed to check membership", "error", result.Error,
			"group_id", groupID, "user_id", userID)

		return false, errors.Join(ErrGet, result.Error)
	}

	return count > 0, nil
}

func (s *Storage) GetGroupsToJoinByChatAndUser(ctx context.Context, chatID int64, userID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.WithContext(ctx).Where("chat_id = ? AND id NOT IN (SELECT group_id FROM group_members WHERE user_id = ?)", chatID, userID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups to join", "error", result.Error,
			"chat_id", chatID, "user_id", userID)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return groups, nil
}

func (s *Storage) GetUserGroupsByChat(ctx context.Context, chatID int64, userID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.WithContext(ctx).Where("chat_id = ? AND id IN (SELECT group_id FROM group_members WHERE user_id = ?)", chatID, userID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get user's groups", "error", result.Error,
			"chat_id", chatID, "user_id", userID)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return groups, nil
}

func (s *Storage) FindGroupsByChatAndNamesWithMembers(ctx context.Context, chatID int64, names []string) ([]MentionGroup, error) {
	if len(names) == 0 {
		return nil, nil
	}

	normalized := make([]string, len(names))
	for i, name := range names {
		normalized[i] = NormalizeName(name)
	}

	// Names match group names or aliases of the chat (case-insensitively, as everything is stored lowercased)
	var groups []MentionGroup
	result := s.db.WithContext(ctx).
		Where("chat_id = ? AND (name IN ? OR id IN (SELECT group_id FROM group_aliases WHERE chat_id = ? AND alias IN ?))",
			chatID, normalized, chatID, normalized).
		Preload("Members.User").Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to find groups", "error", result.Error, "chat_id", chatID, "names", names)

		return nil, errors.Join(ErrGet, result.Error)
	}

	return groups, nil
}

// MigrateChatGroups moves all groups from one chat ID to another (e.g. when a group is
// upgraded to a supergroup). It runs in a transaction and is idempotent: with no
// groups under fromChatID it does nothing. If the target chat already has a group with
// the same name, the members are merged into it and the source group is removed.
func (s *Storage) MigrateChatGroups(ctx context.Context, fromChatID, toChatID int64) error {
	if fromChatID == toChatID {
		return nil
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groups []MentionGroup
		if err := tx.Where("chat_id = ?", fromChatID).Find(&groups).Error; err != nil {
			return err
		}

		for _, group := range groups {
			var target MentionGroup

			err := tx.Where("chat_id = ? AND name = ?", toChatID, group.Name).First(&target).Error

			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				if err := tx.Model(&MentionGroup{}).Where("id = ?", group.ID).Update("chat_id", toChatID).Error; err != nil {
					return err
				}

				if err := tx.Model(&GroupAlias{}).Where("group_id = ?", group.ID).Update("chat_id", toChatID).Error; err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				// Name conflict: merge members into the existing group, then drop the source
				if err := tx.Exec(
					"INSERT OR IGNORE INTO group_members (group_id, user_id) SELECT ?, user_id FROM group_members WHERE group_id = ?",
					target.ID, group.ID,
				).Error; err != nil {
					return err
				}

				// Aliases follow the merged group; those that now clash are dropped with the source group
				if err := tx.Exec(
					"UPDATE OR IGNORE group_aliases SET group_id = ?, chat_id = ? WHERE group_id = ?",
					target.ID, toChatID, group.ID,
				).Error; err != nil {
					return err
				}

				if err := tx.Delete(&MentionGroup{}, group.ID).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		slog.Error("storage: Failed to migrate chat groups", "error", err,
			"from_chat_id", fromChatID, "to_chat_id", toChatID)

		return errors.Join(ErrUpdate, err)
	}

	slog.Info("storage: Migrated chat groups", "from_chat_id", fromChatID, "to_chat_id", toChatID)

	return nil
}
