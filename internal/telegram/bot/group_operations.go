package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
)

// outcome is the result of a group action. Commands reply with it; callback buttons
// show it as a toast and edit the picker message.
type outcome struct {
	text   string // plain text, or ready MarkdownV2 when md is set
	md     bool
	ok     bool // the action changed something (as opposed to "already a member" etc.)
	silent bool // the action already posted its own message (mentions)
}

// render returns the MarkdownV2 text of the outcome.
func (o outcome) render() string {
	if o.md {
		return o.text
	}

	return escapeMarkdownV2(o.text)
}

func (b *Bot) joinGroup(ctx context.Context, group *storage.MentionGroup, user *t.User, chatID int64) outcome {
	slog.Debug("bot: Joining group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)

	// Idempotent insert: the user row is created by middleware (messages) or the callback handler
	added, err := b.storage.AddMember(ctx, group.ID, &storage.User{ID: user.ID})
	if err != nil {
		slog.Error("bot: Failed to add user to group", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)

		return outcome{text: "Failed to join group."}
	}

	if !added {
		return outcome{text: fmt.Sprintf("You are already a member of group '%s'.", group.Name)}
	}

	slog.Info("bot: User joined group", "group_name", group.Name, "chat_id", chatID, "user_id", user.ID)

	return outcome{text: fmt.Sprintf("You have joined group '%s'!", group.Name), ok: true}
}

func (b *Bot) leaveGroup(ctx context.Context, group *storage.MentionGroup, userID, chatID int64) outcome {
	slog.Debug("bot: Leaving group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)

	removed, err := b.storage.RemoveMember(ctx, group.ID, userID)
	if err != nil {
		slog.Error("bot: Failed to remove user from group", "error", err, "group_name", group.Name, "chat_id", chatID, "user_id", userID)

		return outcome{text: "Failed to leave group."}
	}

	if !removed {
		return outcome{text: fmt.Sprintf("You are not a member of group '%s'.", group.Name)}
	}

	slog.Info("bot: User left group", "group_name", group.Name, "chat_id", chatID, "user_id", userID)

	return outcome{text: fmt.Sprintf("You have left group '%s'!", group.Name), ok: true}
}

func (b *Bot) deleteGroup(ctx context.Context, group *storage.MentionGroup, chatID int64) outcome {
	slog.Debug("bot: Deleting group", "group_name", group.Name, "chat_id", chatID)

	if len(group.Members) > 0 {
		return outcome{text: "Cannot delete group: it has members. Ask members to /leave first (chat admins can use /del <name> force)."}
	}

	if err := b.storage.DeleteGroup(ctx, group.ID); err != nil {
		slog.Error("bot: Failed to delete group", "error", err, "group_name", group.Name, "chat_id", chatID)

		return outcome{text: "Failed to delete group."}
	}

	slog.Info("bot: Group deleted", "group_name", group.Name, "chat_id", chatID)

	return outcome{text: fmt.Sprintf("Group '%s' has been deleted!", group.Name), ok: true}
}

func (b *Bot) showGroupMembers(group *storage.MentionGroup) outcome {
	if len(group.Members) == 0 {
		return outcome{text: fmt.Sprintf("Group '%s' has no members.", group.Name)}
	}

	memberList := b.formatMemberList(group.Members)
	header := fmt.Sprintf("Members of group '%s':\n", group.Name)

	return outcome{text: escapeMarkdownV2(header + strings.Join(memberList, "\n")), md: true}
}

// mentionGroups posts one mention message for the groups. originalMessage (may be nil) is
// the message the post replies to and whose forum topic it stays in.
func (b *Bot) mentionGroups(ctx context.Context, groups []storage.MentionGroup, chatID int64, originalMessage *t.Message) error {
	slog.Debug("bot: Mentioning groups", "chat_id", chatID, "group_count", len(groups))

	var groupNames []string
	var groupLines []string
	var buttons []storage.MentionGroup
	for _, group := range groups {
		if len(group.Members) == 0 {
			slog.Debug("bot: Group has no members", "group_name", group.Name, "chat_id", chatID)

			continue
		}
		groupNames = append(groupNames, group.Name)
		buttons = append(buttons, group)
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
	b.sendMessage(ctx, chatID, finalText, originalMessage, joinLeaveKeyboard(buttons))

	return nil
}
