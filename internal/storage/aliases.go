package storage

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

// NormalizeName is the canonical form of a group name or alias: group names are
// case-insensitive, so everything is stored and looked up lowercased.
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// nameTaken reports whether name is already used as a group name or alias in the chat.
func nameTaken(tx *gorm.DB, chatID int64, name string) (bool, error) {
	var groups, aliases int64

	if err := tx.Model(&MentionGroup{}).Where("chat_id = ? AND name = ?", chatID, name).Count(&groups).Error; err != nil {
		return false, err
	}

	if err := tx.Model(&GroupAlias{}).Where("chat_id = ? AND alias = ?", chatID, name).Count(&aliases).Error; err != nil {
		return false, err
	}

	return groups+aliases > 0, nil
}

// RenameGroup changes a group's name, keeping its members and ID. It returns ErrNotFound
// when oldName is not a group of the chat and ErrAlreadyExists when newName is taken by
// another group or alias.
func (s *Storage) RenameGroup(ctx context.Context, chatID int64, oldName, newName string) error {
	oldName, newName = NormalizeName(oldName), NormalizeName(newName)
	if newName == "" {
		return ErrEmptyGroupName
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group MentionGroup
		if err := tx.Where("chat_id = ? AND name = ?", chatID, oldName).First(&group).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}

			return err
		}

		if oldName == newName {
			return nil
		}

		taken, err := nameTaken(tx, chatID, newName)
		if err != nil {
			return err
		}

		if taken {
			return ErrAlreadyExists
		}

		return tx.Model(&MentionGroup{}).Where("id = ?", group.ID).Update("name", newName).Error
	})

	return translate("rename group", err, "chat_id", chatID, "old_name", oldName, "new_name", newName)
}

// AddAlias makes alias point to the group (found by name or alias). It returns
// ErrNotFound for an unknown group and ErrAlreadyExists when alias is taken.
func (s *Storage) AddAlias(ctx context.Context, chatID int64, groupName, alias string) error {
	groupName, alias = NormalizeName(groupName), NormalizeName(alias)
	if alias == "" {
		return ErrEmptyGroupName
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group MentionGroup

		err := tx.Where("chat_id = ? AND (name = ? OR id IN (SELECT group_id FROM group_aliases WHERE chat_id = ? AND alias = ?))",
			chatID, groupName, chatID, groupName).First(&group).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}

		if err != nil {
			return err
		}

		taken, err := nameTaken(tx, chatID, alias)
		if err != nil {
			return err
		}

		if taken {
			return ErrAlreadyExists
		}

		return tx.Create(&GroupAlias{ChatID: chatID, Alias: alias, GroupID: group.ID}).Error
	})

	return translate("add alias", err, "chat_id", chatID, "group", groupName, "alias", alias)
}

// RemoveAlias deletes an alias and reports whether it existed.
func (s *Storage) RemoveAlias(ctx context.Context, chatID int64, alias string) (bool, error) {
	result := s.db.WithContext(ctx).Where("chat_id = ? AND alias = ?", chatID, NormalizeName(alias)).Delete(&GroupAlias{})
	if result.Error != nil {
		slog.Error("storage: Failed to remove alias", "error", result.Error, "chat_id", chatID)

		return false, errors.Join(ErrDelete, result.Error)
	}

	return result.RowsAffected > 0, nil
}

// GetAliasesByChat returns the aliases of every group in the chat, keyed by group ID.
func (s *Storage) GetAliasesByChat(ctx context.Context, chatID int64) (map[uint][]string, error) {
	var aliases []GroupAlias
	if err := s.db.WithContext(ctx).Where("chat_id = ?", chatID).Order("alias").Find(&aliases).Error; err != nil {
		slog.Error("storage: Failed to get aliases", "error", err, "chat_id", chatID)

		return nil, errors.Join(ErrGet, err)
	}

	byGroup := make(map[uint][]string)
	for _, a := range aliases {
		byGroup[a.GroupID] = append(byGroup[a.GroupID], a.Alias)
	}

	return byGroup, nil
}

// translate passes sentinel errors through and logs and wraps anything else.
func translate(op string, err error, attrs ...any) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrEmptyGroupName):
		return err
	default:
		slog.Error("storage: Failed to "+op, append([]any{"error", err}, attrs...)...)

		return errors.Join(ErrUpdate, err)
	}
}
