package bot

import (
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/storage"

	t "github.com/mymmrac/telego"
)

// joinGroup adds a user to a mention group
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

	err = b.storage.AddMember(group.ID, user.ID, user.Username, user.FirstName, user.LastName)
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

// leaveGroup removes a user from a mention group
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

// mentionGroupMembers mentions all members of a group
func (b *Bot) mentionGroupMembers(group *storage.MentionGroup, chatID int64) error {
	members, err := b.getGroupMembers(group, chatID)
	if err != nil {
		return err
	}

	if len(members) == 0 {
		slog.Info("bot: No members in group", "group_name", group.Name)
		b.sendMessage(chatID, escapeMarkdownV2("No members in this group!"))
		return nil
	}

	mentions := b.formatMentions(members)
	mentionText := fmt.Sprintf("Mentioning %s group members:\n%s", escapeMarkdownV2(group.Name), strings.Join(mentions, ", "))
	b.sendMessage(chatID, mentionText)
	return nil
}

// deleteGroup removes a group if it has no members
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

// showGroupMembers displays all members of a group without mentioning them
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
