package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
)

func (b *Bot) joinGroupOperation(ctx context.Context, group *storage.MentionGroup, user *t.User, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Joining group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)

	// Idempotent insert: user data is already synced by middleware
	added, err := b.storage.AddMember(ctx, group.ID, &storage.User{ID: user.ID})
	if err != nil {
		slog.Error("bot: Failed to add user to group", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Failed to join group: %v", err)), originalMessage)

		return nil
	}

	if !added {
		slog.Debug("bot: User is already a member of the group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("You are already a member of group '%s'.", group.Name)), originalMessage)

		return nil
	}

	slog.Info("bot: User joined group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)
	b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("You have joined group '%s'!", group.Name)), originalMessage)

	return nil
}

func (b *Bot) leaveGroupOperation(ctx context.Context, group *storage.MentionGroup, userID int64, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Leaving group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)

	removed, err := b.storage.RemoveMember(ctx, group.ID, userID)
	if err != nil {
		slog.Error("bot: Failed to remove user from group", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", userID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Failed to leave group: %v", err)), originalMessage)

		return nil
	}

	if !removed {
		slog.Debug("bot: User is not a member of the group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("You are not a member of group '%s'.", group.Name)), originalMessage)

		return nil
	}

	slog.Info("bot: User left group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)
	b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("You have left group '%s'!", group.Name)), originalMessage)

	return nil
}

func (b *Bot) mentionGroups(ctx context.Context, groups []storage.MentionGroup, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Mentioning groups", "chat_id", chatID, "group_count", len(groups))

	var groupNames []string
	var groupLines []string
	for _, group := range groups {
		if len(group.Members) == 0 {
			slog.Debug("bot: Group has no members", "group_name", group.Name, "chat_id", chatID)

			continue
		}
		groupNames = append(groupNames, group.Name)
		mentions := b.formatMentions(group.Members)
		if len(mentions) == 0 {
			continue
		}
		groupLine := fmt.Sprintf("%s: %s", escapeMarkdownV2(group.Name), strings.Join(mentions, ", "))
		groupLines = append(groupLines, groupLine)
	}

	if len(groupLines) == 0 {
		slog.Debug("bot: No members to mention", "chat_id", chatID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2("No members to mention."), originalMessage)

		return nil
	}

	header := fmt.Sprintf("⚠️ %s ⚠️", escapeMarkdownV2(strings.Join(groupNames, ", ")))
	finalText := header + "\n\n" + strings.Join(groupLines, "\n")
	slog.Debug("bot: Sending mentions", "chat_id", chatID, "message_length", len(finalText))
	b.sendMessage(ctx, chatID, finalText, originalMessage)

	return nil
}

func (b *Bot) deleteGroupOperation(ctx context.Context, group *storage.MentionGroup, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Deleting group", "group_name", group.Name, "chat_id", chatID)

	if len(group.Members) > 0 {
		slog.Debug("bot: Cannot delete group with members", "group_name", group.Name, "chat_id", chatID, "member_count", len(group.Members))
		b.sendMessage(ctx, chatID, escapeMarkdownV2("Cannot delete group: it has members. Ask members to /leave first."), originalMessage)

		return nil
	}

	err := b.storage.DeleteGroup(ctx, group.ID)
	if err != nil {
		slog.Error("bot: Failed to delete group", "error", err, "group_name", group.Name, "chat_id", chatID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Failed to delete group: %v", err)), originalMessage)

		return nil
	}

	slog.Info("bot: Group deleted", "group_name", group.Name, "chat_id", chatID)
	b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Group '%s' has been deleted!", group.Name)), originalMessage)

	return nil
}

func (b *Bot) showGroupMembersOperation(ctx context.Context, group *storage.MentionGroup, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Showing group members", "group_name", group.Name, "chat_id", chatID)

	if len(group.Members) == 0 {
		slog.Debug("bot: Group has no members", "group_name", group.Name, "chat_id", chatID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Group '%s' has no members.", group.Name)), originalMessage)

		return nil
	}

	memberList := b.formatMemberList(group.Members)
	header := fmt.Sprintf("Members of group '%s':\n", group.Name)
	messageText := header + strings.Join(memberList, "\n")
	slog.Debug("bot: Sending member list", "chat_id", chatID, "member_count", len(memberList))
	b.sendMessage(ctx, chatID, escapeMarkdownV2(messageText), originalMessage, &t.ReplyKeyboardRemove{RemoveKeyboard: true})

	return nil
}
