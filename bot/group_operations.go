package bot

import (
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/storage"

	t "github.com/mymmrac/telego"
)

func (b *Bot) joinGroup(group *storage.MentionGroup, user *t.User, chatID int64) error {
	// Check if user is already a member
	isMember, err := b.storage.IsMember(group.ID, user.ID)
	if err != nil {
		slog.Error("bot: Failed to check membership", "error", err, "group_id", group.ID, "user_id", user.ID)
		return err
	}

	if isMember {
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("You are already a member of group '%s'!", group.Name)))
		return nil
	}

	storageUser := &storage.User{
		ID:        user.ID,
		Username:  user.Username,
		FirstName: user.FirstName,
		LastName:  user.LastName,
	}
	err = b.storage.AddMember(group.ID, storageUser)
	if err != nil {
		slog.Error("bot: Failed to join group", "error", err,
			"group_id", group.ID, "user_id", user.ID, "username", user.Username)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to join group: %v", err)))
		return err
	}

	slog.Info("bot: User joined group", "group_name", group.Name,
		"user_id", user.ID, "username", user.Username)
	b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Successfully joined group '%s'!", group.Name)))
	return nil
}

func (b *Bot) leaveGroup(group *storage.MentionGroup, userID int64, chatID int64) error {
	// Check if user is a member
	isMember, err := b.storage.IsMember(group.ID, userID)
	if err != nil {
		slog.Error("bot: Failed to check membership", "error", err, "group_id", group.ID, "user_id", userID)
		return err
	}

	if !isMember {
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("You are not a member of group '%s'!", group.Name)))
		return nil
	}

	err = b.storage.RemoveMember(group.ID, userID)
	if err != nil {
		slog.Error("bot: Failed to leave group", "error", err,
			"group_id", group.ID, "user_id", userID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to leave group: %v", err)))
		return err
	}

	slog.Info("bot: User left group", "group_name", group.Name,
		"user_id", userID)
	b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Successfully left group '%s'!", group.Name)))
	return nil
}

func (b *Bot) mentionGroups(groups []storage.MentionGroup, chatID int64) error {
	if len(groups) == 0 {
		return nil
	}

	var groupMentions []string
	for _, group := range groups {
		if len(group.Members) == 0 {
			continue
		}

		mentions := b.formatMentions(group.Members)
		groupMentions = append(groupMentions, fmt.Sprintf("*%s*:\n%s",
			escapeMarkdownV2(group.Name),
			strings.Join(mentions, "\n"),
		))
	}

	if len(groupMentions) == 0 {
		b.sendMessage(chatID, escapeMarkdownV2("No members in these groups!"))
		return nil
	}

	b.sendMessage(chatID, strings.Join(groupMentions, "\n\n"))
	return nil
}

func (b *Bot) deleteGroup(group *storage.MentionGroup, chatID int64) error {
	members, err := b.getGroupMembers(group, chatID)
	if err != nil {
		return err
	}

	if len(members) > 0 {
		b.sendMessage(chatID, escapeMarkdownV2("Cannot delete group: it has members. Ask members to /leave first."))
		return nil
	}

	err = b.storage.DeleteGroup(group.ID)
	if err != nil {
		slog.Error("bot: Failed to delete group", "error", err,
			"group_id", group.ID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to delete group: %v", err)))
		return err
	}

	slog.Info("bot: Group deleted", "group_name", group.Name, "chat_id", chatID)
	b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Group '%s' deleted successfully!", group.Name)))
	return nil
}

func (b *Bot) showGroupMembers(group *storage.MentionGroup, chatID int64) error {
	members, err := b.getGroupMembers(group, chatID)
	if err != nil {
		return err
	}

	if len(members) == 0 {
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Group '%s' has no members.", group.Name)))
		return nil
	}

	memberList := b.formatMemberList(members)
	showText := fmt.Sprintf("Members of '%s':\n%s", escapeMarkdownV2(group.Name), strings.Join(memberList, "\n"))
	b.sendMessage(chatID, showText)
	return nil
}
