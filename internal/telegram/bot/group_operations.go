package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

// anonymousAdminBotID is the user ID Telegram shows for messages sent by anonymous group admins.
const anonymousAdminBotID int64 = 1087968824

// mentionGroups posts one mention message for the groups on behalf of actor. originalMessage
// (may be nil) is the message the post replies to and whose forum topic it stays in.
// Cooldowns and the member cap apply here, so commands, buttons and free-form mentions share them.
func (b *Bot) mentionGroups(ctx context.Context, groups []storage.MentionGroup, chatID int64, originalMessage *t.Message, actor *t.User) error {
	slog.Debug("bot: Mentioning groups", "chat_id", chatID, "group_count", len(groups))

	var withMembers []storage.MentionGroup
	for _, group := range groups {
		if len(group.Members) == 0 {
			slog.Debug("bot: Group has no members", "group_name", group.Name, "chat_id", chatID)

			continue
		}

		withMembers = append(withMembers, group)
	}

	if len(withMembers) == 0 {
		slog.Debug("bot: No members to mention", "chat_id", chatID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2("No members to mention."), originalMessage)

		return nil
	}

	if !b.allowMention(ctx, chatID, withMembers, originalMessage, actor) {
		return nil
	}

	budget := b.maxMentions // 0 = unlimited
	skipped := 0

	var groupNames []string
	var groupLines []string
	var buttons []storage.MentionGroup
	for _, group := range withMembers {
		members := group.Members

		if b.maxMentions > 0 {
			if budget < len(members) {
				skipped += len(members) - budget
				members = members[:budget]
			}

			budget -= len(members)
		}

		groupNames = append(groupNames, group.Name)
		buttons = append(buttons, group)

		if len(members) == 0 {
			continue
		}

		mentions := b.formatMentions(members)
		groupLines = append(groupLines, fmt.Sprintf("%s: %s", escapeMarkdownV2(group.Name), strings.Join(mentions, ", ")))
	}

	header := fmt.Sprintf("⚠️ %s ⚠️", escapeMarkdownV2(strings.Join(groupNames, ", ")))
	finalText := header + "\n\n" + strings.Join(groupLines, "\n")

	if skipped > 0 {
		finalText += "\n\n" + escapeMarkdownV2(fmt.Sprintf("…and %d more not pinged (limit: %d per post).", skipped, b.maxMentions))
	}

	slog.Debug("bot: Sending mentions", "chat_id", chatID, "message_length", len(finalText), "skipped", skipped)
	b.sendPost(ctx, chatID, finalText, originalMessage, joinLeaveKeyboard(buttons))

	return nil
}

// allowMention applies the per-group and per-user cooldowns. When a mention is throttled it
// posts one short notice per cooldown window and returns false. Chat admins are exempt.
func (b *Bot) allowMention(ctx context.Context, chatID int64, groups []storage.MentionGroup, originalMessage *t.Message, actor *t.User) bool {
	if b.groupCooldown <= 0 && b.userCooldown <= 0 {
		return true
	}

	var rules []cooldownRule

	for _, group := range groups {
		rules = append(rules, cooldownRule{key: fmt.Sprintf("g:%d:%d", chatID, group.ID), window: b.groupCooldown})
	}

	if actor != nil {
		rules = append(rules, cooldownRule{key: fmt.Sprintf("u:%d:%d", chatID, actor.ID), window: b.userCooldown})
	}

	exempt := func() bool {
		if actor == nil {
			return false
		}

		return actor.ID == anonymousAdminBotID || b.isChatAdmin(ctx, chatID, actor.ID)
	}

	res := b.cooldowns.reserve(b.clock(), rules, exempt)
	if !res.blocked {
		return true
	}

	slog.Debug("bot: Mention throttled", "chat_id", chatID, "wait", res.wait.String(), "notify", res.notify)

	if res.notify {
		seconds := int((res.wait + time.Second - 1) / time.Second)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Please wait %d s before mentioning again.", seconds)), originalMessage)
	}

	return false
}
