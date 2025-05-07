package bot

import (
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/storage"

	t "github.com/mymmrac/telego"
)

func (b *Bot) joinGroupOperation(group *storage.MentionGroup, user *t.User, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Joining group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)

	// Check if user is already a member using storage method
	isMember, err := b.storage.IsMember(group.ID, user.ID)
	if err != nil {
		slog.Error("bot: Failed to check membership", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to join group: %v", err)), originalMessage)
		return nil
	}

	if isMember {
		slog.Debug("bot: User is already a member of the group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("You are already a member of group '%s'.", group.Name)), originalMessage)
		return nil
	}

	// Add user to group - user data is already synced by middleware
	err = b.storage.AddMember(group.ID, &storage.User{ID: user.ID})
	if err != nil {
		slog.Error("bot: Failed to add user to group", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to join group: %v", err)), originalMessage)
		return nil
	}

	slog.Info("bot: User joined group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)
	b.sendMessage(chatID, fmt.Sprintf("You have joined group '%s'!", escapeMarkdownV2(group.Name)), originalMessage)
	return nil
}

func (b *Bot) leaveGroupOperation(group *storage.MentionGroup, userID int64, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Leaving group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)

	isMember, err := b.storage.IsMember(group.ID, userID)
	if err != nil {
		slog.Error("bot: Failed to check membership", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", userID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to leave group: %v", err)), originalMessage)
		return nil
	}

	if !isMember {
		slog.Debug("bot: User is not a member of the group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("You are not a member of group '%s'.", group.Name)), originalMessage)
		return nil
	}

	err = b.storage.RemoveMember(group.ID, userID)
	if err != nil {
		slog.Error("bot: Failed to remove user from group", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", userID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to leave group: %v", err)), originalMessage)
		return nil
	}

	slog.Info("bot: User left group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)
	b.sendMessage(chatID, fmt.Sprintf("You have left group '%s'!", escapeMarkdownV2(group.Name)), originalMessage)
	return nil
}

func (b *Bot) mentionGroups(groups []storage.MentionGroup, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Mentioning groups", "chat_id", chatID, "group_count", len(groups))

	var allMentions []string
	for _, group := range groups {
		if len(group.Members) == 0 {
			slog.Debug("bot: Group has no members", "group_name", group.Name, "chat_id", chatID)
			continue
		}

		mentions := b.formatMentions(group.Members)
		allMentions = append(allMentions, mentions...)
	}

	if len(allMentions) == 0 {
		slog.Debug("bot: No members to mention", "chat_id", chatID)
		b.sendMessage(chatID, escapeMarkdownV2("No members to mention."), originalMessage)
		return nil
	}

	mentionText := strings.Join(allMentions, " ")
	slog.Debug("bot: Sending mentions", "chat_id", chatID, "mention_count", len(allMentions))
	b.sendMessage(chatID, mentionText, originalMessage)
	return nil
}

func (b *Bot) deleteGroupOperation(group *storage.MentionGroup, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Deleting group", "group_name", group.Name, "chat_id", chatID)

	if len(group.Members) > 0 {
		slog.Debug("bot: Cannot delete group with members", "group_name", group.Name, "chat_id", chatID, "member_count", len(group.Members))
		b.sendMessage(chatID, escapeMarkdownV2("Cannot delete group: it has members. Ask members to /leave first."), originalMessage)
		return nil
	}

	err := b.storage.DeleteGroup(group.ID)
	if err != nil {
		slog.Error("bot: Failed to delete group", "error", err, "group_name", group.Name, "chat_id", chatID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to delete group: %v", err)), originalMessage)
		return nil
	}

	slog.Info("bot: Group deleted", "group_name", group.Name, "chat_id", chatID)
	b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Group '%s' has been deleted!", group.Name)), originalMessage)
	return nil
}

func (b *Bot) showGroupMembersOperation(group *storage.MentionGroup, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Showing group members", "group_name", group.Name, "chat_id", chatID)

	if len(group.Members) == 0 {
		slog.Debug("bot: Group has no members", "group_name", group.Name, "chat_id", chatID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Group '%s' has no members.", group.Name)), originalMessage)
		return nil
	}

	memberList := b.formatMemberList(group.Members)
	header := fmt.Sprintf("Members of group '%s':\n", escapeMarkdownV2(group.Name))
	messageText := header + strings.Join(memberList, "\n")
	slog.Debug("bot: Sending member list", "chat_id", chatID, "member_count", len(memberList))
	b.sendMessage(chatID, messageText, originalMessage, &t.ReplyKeyboardRemove{RemoveKeyboard: true})
	return nil
}
