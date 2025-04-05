package storage

import (
	"fmt"
	"log/slog"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Storage struct {
	db *gorm.DB
}

func New(dbPath string) (*Storage, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		slog.Error("storage: Failed to connect to database", "error", err, "path", dbPath)
		return nil, fmt.Errorf("failed to connect to database: %w", err)
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
				return fmt.Errorf("failed to drop column from MentionGroup: %w", err)
			}
		}

		if s.db.Migrator().HasColumn(&GroupMember{}, col) {
			err := s.db.Migrator().DropColumn(&GroupMember{}, col)
			if err != nil {
				slog.Error("storage: Failed to drop column from GroupMember", "error", err, "column", col)
				return fmt.Errorf("failed to drop column from GroupMember: %w", err)
			}
		}
	}

	// Auto migrate the schema
	err := s.db.AutoMigrate(&MentionGroup{}, &GroupMember{})
	if err != nil {
		slog.Error("storage: Failed to migrate database", "error", err)
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	return nil
}

// CreateGroup creates a new mention group in a chat
func (s *Storage) CreateGroup(name string, chatID int64) error {
	group := MentionGroup{
		Name:   name,
		ChatID: chatID,
	}

	result := s.db.Create(&group)
	if result.Error != nil {
		slog.Error("storage: Failed to create group", "error", result.Error, "name", name, "chat_id", chatID)
		return fmt.Errorf("failed to create group: %w", result.Error)
	}
	return nil
}

// GetGroup retrieves a group by name and chat ID
func (s *Storage) GetGroup(name string, chatID int64) (*MentionGroup, error) {
	var group MentionGroup
	result := s.db.Where("name = ? AND chat_id = ?", name, chatID).First(&group)
	if result.Error != nil {
		slog.Error("storage: Failed to get group", "error", result.Error, "name", name, "chat_id", chatID)
		return nil, fmt.Errorf("failed to get group: %w", result.Error)
	}
	return &group, nil
}

// AddMember adds a user to a mention group
func (s *Storage) AddMember(groupID uint, userID int64, username, firstName, lastName string) error {
	member := GroupMember{
		GroupID:   groupID,
		UserID:    userID,
		Username:  username,
		FirstName: firstName,
		LastName:  lastName,
	}

	result := s.db.Create(&member)
	if result.Error != nil {
		slog.Error("storage: Failed to add member", "error", result.Error,
			"group_id", groupID, "user_id", userID, "username", username)
		return fmt.Errorf("failed to add member: %w", result.Error)
	}
	return nil
}

// RemoveMember removes a user from a mention group
func (s *Storage) RemoveMember(groupID uint, userID int64) error {
	result := s.db.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&GroupMember{})
	if result.Error != nil {
		slog.Error("storage: Failed to remove member", "error", result.Error,
			"group_id", groupID, "user_id", userID)
		return fmt.Errorf("failed to remove member: %w", result.Error)
	}
	return nil
}

// GetGroupMembers retrieves all members of a group
func (s *Storage) GetGroupMembers(groupID uint) ([]GroupMember, error) {
	var members []GroupMember
	result := s.db.Where("group_id = ?", groupID).Find(&members)
	if result.Error != nil {
		slog.Error("storage: Failed to get group members", "error", result.Error, "group_id", groupID)
		return nil, fmt.Errorf("failed to get group members: %w", result.Error)
	}
	return members, nil
}

// DeleteGroup deletes a group by ID
func (s *Storage) DeleteGroup(groupID uint) error {
	result := s.db.Delete(&MentionGroup{}, groupID)
	if result.Error != nil {
		slog.Error("storage: Failed to delete group", "error", result.Error, "group_id", groupID)
		return fmt.Errorf("failed to delete group: %w", result.Error)
	}
	return nil
}

// GetGroups retrieves all groups for a specific chat
func (s *Storage) GetGroups(chatID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.Where("chat_id = ?", chatID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups", "error", result.Error, "chat_id", chatID)
		return nil, fmt.Errorf("failed to get groups: %w", result.Error)
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
		return false, fmt.Errorf("failed to check membership: %w", result.Error)
	}
	return count > 0, nil
}

func (s *Storage) GetGroupsToJoin(chatID int64, userID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.Where("chat_id = ? AND id NOT IN (SELECT group_id FROM group_members WHERE user_id = ?)", chatID, userID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups to join", "error", result.Error,
			"chat_id", chatID, "user_id", userID)
		return nil, fmt.Errorf("failed to get groups to join: %w", result.Error)
	}
	return groups, nil
}

func (s *Storage) GetGroupsToLeave(chatID int64, userID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.Where("chat_id = ? AND id IN (SELECT group_id FROM group_members WHERE user_id = ?)", chatID, userID).Find(&groups)
	if result.Error != nil {
		slog.Error("storage: Failed to get groups to leave", "error", result.Error,
			"chat_id", chatID, "user_id", userID)
		return nil, fmt.Errorf("failed to get groups to leave: %w", result.Error)
	}
	return groups, nil
}
