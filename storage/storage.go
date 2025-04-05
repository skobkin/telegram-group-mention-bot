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
		slog.Error("Failed to connect to database", "error", err, "path", dbPath)
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Auto migrate the schema
	err = db.AutoMigrate(&MentionGroup{}, &GroupMember{})
	if err != nil {
		slog.Error("Failed to migrate database", "error", err)
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &Storage{db: db}, nil
}

// CreateGroup creates a new mention group in a chat
func (s *Storage) CreateGroup(name string, chatID int64) error {
	group := MentionGroup{
		Name:   name,
		ChatID: chatID,
	}

	result := s.db.Create(&group)
	if result.Error != nil {
		slog.Error("Failed to create group", "error", result.Error, "name", name, "chat_id", chatID)
		return fmt.Errorf("failed to create group: %w", result.Error)
	}
	return nil
}

// GetGroup retrieves a group by name and chat ID
func (s *Storage) GetGroup(name string, chatID int64) (*MentionGroup, error) {
	var group MentionGroup
	result := s.db.Where("name = ? AND chat_id = ?", name, chatID).First(&group)
	if result.Error != nil {
		slog.Error("Failed to get group", "error", result.Error, "name", name, "chat_id", chatID)
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
		slog.Error("Failed to add member", "error", result.Error,
			"group_id", groupID, "user_id", userID, "username", username)
		return fmt.Errorf("failed to add member: %w", result.Error)
	}
	return nil
}

// RemoveMember removes a user from a mention group
func (s *Storage) RemoveMember(groupID uint, userID int64) error {
	result := s.db.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&GroupMember{})
	if result.Error != nil {
		slog.Error("Failed to remove member", "error", result.Error,
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
		slog.Error("Failed to get group members", "error", result.Error, "group_id", groupID)
		return nil, fmt.Errorf("failed to get group members: %w", result.Error)
	}
	return members, nil
}

// DeleteGroup deletes a group by ID
func (s *Storage) DeleteGroup(groupID uint) error {
	result := s.db.Delete(&MentionGroup{}, groupID)
	if result.Error != nil {
		slog.Error("Failed to delete group", "error", result.Error, "group_id", groupID)
		return fmt.Errorf("failed to delete group: %w", result.Error)
	}
	return nil
}

// GetGroups retrieves all groups for a specific chat
func (s *Storage) GetGroups(chatID int64) ([]MentionGroup, error) {
	var groups []MentionGroup
	result := s.db.Where("chat_id = ?", chatID).Find(&groups)
	if result.Error != nil {
		slog.Error("Failed to get groups", "error", result.Error, "chat_id", chatID)
		return nil, fmt.Errorf("failed to get groups: %w", result.Error)
	}
	return groups, nil
}
