package storage

import (
	"errors"
	"log/slog"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// Validation errors
	ErrEmptyGroupName = errors.New("group name cannot be empty")
	ErrZeroUserID     = errors.New("user ID cannot be zero")
	ErrNilUser        = errors.New("user cannot be nil")

	// Operation errors
	ErrNotFound = errors.New("not found")
	ErrCreate   = errors.New("failed to create")
	ErrDelete   = errors.New("failed to delete")
	ErrGet      = errors.New("failed to get")
	ErrUpdate   = errors.New("failed to update")

	// Migration errors
	ErrDropColumn      = errors.New("failed to drop column")
	ErrAutoMigrate     = errors.New("failed to auto migrate schema")
	ErrMigrateUserData = errors.New("failed to migrate user data")
	ErrConnectDB       = errors.New("failed to connect to database")
)

type Storage struct {
	db *gorm.DB
}

func New(dbPath string) (*Storage, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		slog.Error("storage: Failed to connect to database", "error", err, "path", dbPath)
		return nil, errors.Join(ErrConnectDB, err)
	}

	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Storage) migrate() error {
	for _, col := range []string{"created_at", "updated_at", "deleted_at"} {
		if s.db.Migrator().HasColumn(&MentionGroup{}, col) {
			err := s.db.Migrator().DropColumn(&MentionGroup{}, col)
			if err != nil {
				slog.Error("storage: Failed to drop column from MentionGroup", "error", err, "column", col)
				return errors.Join(ErrDropColumn, err)
			}
		}

		if s.db.Migrator().HasColumn(&GroupMember{}, col) {
			err := s.db.Migrator().DropColumn(&GroupMember{}, col)
			if err != nil {
				slog.Error("storage: Failed to drop column from GroupMember", "error", err, "column", col)
				return errors.Join(ErrDropColumn, err)
			}
		}
	}

	// Auto migrate the schema
	err := s.db.AutoMigrate(&User{}, &MentionGroup{}, &GroupMember{})
	if err != nil {
		slog.Error("storage: Failed to migrate database", "error", err)
		return errors.Join(ErrAutoMigrate, err)
	}

	if err := s.migrateUserData(); err != nil {
		return errors.Join(ErrMigrateUserData, err)
	}

	return nil
}

// TODO: This function can be removed in a future release after all users have migrated to the new schema.
func (s *Storage) migrateUserData() error {
	// Check if old columns still exist
	if !s.db.Migrator().HasColumn(&GroupMember{}, "username") {
		return nil // Migration already completed
	}

	// Get all existing group members with their user info
	var oldMembers []struct {
		ID        uint
		GroupID   uint
		UserID    int64
		Username  string
		FirstName string
		LastName  string
	}
	if err := s.db.Raw("SELECT id, group_id, user_id, username, first_name, last_name FROM group_members").Scan(&oldMembers).Error; err != nil {
		return errors.Join(ErrMigrateUserData, err)
	}

	for _, member := range oldMembers {
		user := User{
			ID:        member.UserID,
			Username:  member.Username,
			FirstName: member.FirstName,
			LastName:  member.LastName,
		}
		if err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).Create(&user).Error; err != nil {
			return errors.Join(ErrMigrateUserData, err)
		}
	}

	// Remove unused columns from GroupMember after data migration
	for _, col := range []string{"username", "first_name", "last_name"} {
		if s.db.Migrator().HasColumn(&GroupMember{}, col) {
			err := s.db.Migrator().DropColumn(&GroupMember{}, col)
			if err != nil {
				slog.Error("storage: Failed to drop column from GroupMember", "error", err, "column", col)
				return errors.Join(ErrDropColumn, err)
			}
		}
	}

	return nil
}

// CreateGroup creates a new mention group in a chat
func (s *Storage) CreateGroup(name string, chatID int64) error {
	if name == "" {
		return ErrEmptyGroupName
	}

	group := MentionGroup{
		Name:   name,
		ChatID: chatID,
	}

	result := s.db.Create(&group)
	if result.Error != nil {
		slog.Error("storage: Failed to create group", "error", result.Error, "name", name, "chat_id", chatID)
		return errors.Join(ErrCreate, result.Error)
	}
	return nil
}

// GetGroup retrieves a group by name and chat ID
func (s *Storage) GetGroup(name string, chatID int64) (*MentionGroup, error) {
	var group MentionGroup
	result := s.db.Where("name = ? AND chat_id = ?", name, chatID).First(&group)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, errors.Join(ErrNotFound, result.Error)
		}
		slog.Error("storage: Failed to get group", "error", result.Error, "name", name, "chat_id", chatID)
		return nil, errors.Join(ErrGet, result.Error)
	}
	return &group, nil
}

// GetUser retrieves a user by ID
func (s *Storage) GetUser(userID int64) (*User, error) {
	var user User
	result := s.db.First(&user, userID)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, errors.Join(ErrNotFound, result.Error)
		}
		slog.Error("storage: Failed to get user", "error", result.Error, "user_id", userID)
		return nil, errors.Join(ErrGet, result.Error)
	}
	return &user, nil
}

// CreateOrUpdateUser creates a new user or updates an existing one
func (s *Storage) CreateOrUpdateUser(userID int64, username, firstName, lastName string) (*User, error) {
	if userID == 0 {
		return nil, ErrZeroUserID
	}

	user := User{
		ID:        userID,
		Username:  username,
		FirstName: firstName,
		LastName:  lastName,
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(&user).Error; err != nil {
		slog.Error("storage: Failed to create/update user", "error", err, "user_id", userID, "username", username)
		return nil, errors.Join(ErrCreate, err)
	}
	return &user, nil
}

func (s *Storage) AddMember(groupID uint, user *User) error {
	if user == nil {
		return ErrNilUser
	}

	member := GroupMember{
		GroupID: groupID,
		UserID:  user.ID,
	}

	result := s.db.Create(&member)
	if result.Error != nil {
		slog.Error("storage: Failed to add member", "error", result.Error, "group_id", groupID, "user_id", user.ID, "username", user.Username)
		return errors.Join(ErrCreate, result.Error)
	}
	return nil
}

// RemoveMember removes a user from a mention group
func (s *Storage) RemoveMember(groupID uint, userID int64) error {
	result := s.db.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&GroupMember{})
	if result.Error != nil {
		slog.Error("storage: Failed to remove member", "error", result.Error,
			"group_id", groupID, "user_id", userID)
		return errors.Join(ErrDelete, result.Error)
	}
	return nil
}

// GetGroupMembers retrieves all members of a group
func (s *Storage) GetGroupMembers(groupID uint) ([]GroupMember, error) {
	var members []GroupMember
	result := s.db.Preload("User").Where("group_id = ?", groupID).Find(&members)
	if result.Error != nil {
		slog.Error("storage: Failed to get group members", "error", result.Error, "group_id", groupID)
		return nil, errors.Join(ErrGet, result.Error)
	}
	return members, nil
}

// DeleteGroup deletes a group by ID
func (s *Storage) DeleteGroup(groupID uint) error {
	result := s.db.Delete(&MentionGroup{}, groupID)
	if result.Error != nil {
		slog.Error("storage: Failed to delete group", "error", result.Error, "group_id", groupID)
		return errors.Join(ErrDelete, result.Error)
	}
	return nil
}

func (s *Storage) GetGroupsByChat(chatID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.Where("chat_id = ?", chatID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups", "error", result.Error, "chat_id", chatID)
		return nil, errors.Join(ErrGet, result.Error)
	}
	return groups, nil
}

// IsMember checks if a user is a member of a group
func (s *Storage) IsMember(groupID uint, userID int64) (bool, error) {
	var count int64
	result := s.db.Model(&GroupMember{}).Where("group_id = ? AND user_id = ?", groupID, userID).Count(&count)
	if result.Error != nil {
		slog.Error("storage: Failed to check membership", "error", result.Error,
			"group_id", groupID, "user_id", userID)
		return false, errors.Join(ErrGet, result.Error)
	}
	return count > 0, nil
}

func (s *Storage) GetGroupsToJoinByChatAndUser(chatID int64, userID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.Where("chat_id = ? AND id NOT IN (SELECT group_id FROM group_members WHERE user_id = ?)", chatID, userID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups to join", "error", result.Error,
			"chat_id", chatID, "user_id", userID)
		return nil, errors.Join(ErrGet, result.Error)
	}
	return groups, nil
}

func (s *Storage) GetGroupsToLeaveByChatAndUser(chatID int64, userID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.Where("chat_id = ? AND id IN (SELECT group_id FROM group_members WHERE user_id = ?)", chatID, userID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups to leave", "error", result.Error,
			"chat_id", chatID, "user_id", userID)
		return nil, errors.Join(ErrGet, result.Error)
	}
	return groups, nil
}

func (s *Storage) FindGroupsByChatAndNamesWithMembers(chatID int64, names []string) ([]MentionGroup, error) {
	if len(names) == 0 {
		return nil, nil
	}

	var groups []MentionGroup
	result := s.db.Where("chat_id = ? AND name IN ?", chatID, names).Preload("Members.User").Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to find groups", "error", result.Error, "chat_id", chatID, "names", names)
		return nil, errors.Join(ErrGet, result.Error)
	}
	return groups, nil
}
