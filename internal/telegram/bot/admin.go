package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"telegram-group-mention-bot/internal/i18n"
	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const adminCacheTTL = 60 * time.Second

// adminCache remembers getChatMember answers for a short time so that busy chats do not
// cost an API call per command.
type adminCache struct {
	mu      sync.Mutex
	entries map[adminKey]adminEntry
}

type adminKey struct{ chatID, userID int64 }

type adminEntry struct {
	admin   bool
	expires time.Time
}

func (c *adminCache) get(key adminKey, now time.Time) (admin, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.entries[key]
	if !found || !now.Before(entry.expires) {
		return false, false
	}

	return entry.admin, true
}

func (c *adminCache) set(key adminKey, admin bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[adminKey]adminEntry)
	}

	// Drop expired entries once the cache grows, so it cannot grow without bound
	if len(c.entries) > 1024 {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
	}

	c.entries[key] = adminEntry{admin: admin, expires: now.Add(adminCacheTTL)}
}

// isChatAdmin reports whether the user is the creator or an administrator of the chat.
// Failures are logged and treated as "not an admin" (and are not cached).
func (b *Bot) isChatAdmin(ctx context.Context, chatID, userID int64) bool {
	key := adminKey{chatID, userID}
	now := b.clock()

	if admin, ok := b.admins.get(key, now); ok {
		return admin
	}

	member, err := b.api.GetChatMember(ctx, &t.GetChatMemberParams{ChatID: tu.ID(chatID), UserID: userID})
	if err != nil {
		slog.Warn("bot: Failed to check chat admin status", "error", err, "chat_id", chatID, "user_id", userID)

		return false
	}

	status := member.MemberStatus()
	admin := status == t.MemberStatusCreator || status == t.MemberStatusAdministrator
	b.admins.set(key, admin, now)

	return admin
}

// isAdminMessage reports whether the message was sent by a chat admin. Anonymous admins
// (messages sent on behalf of the chat itself) count as admins.
func (b *Bot) isAdminMessage(ctx context.Context, message *t.Message) bool {
	if message.Chat.Type == t.ChatTypePrivate || message.From == nil {
		return false
	}

	if message.SenderChat != nil && message.SenderChat.ID == message.Chat.ID {
		return true
	}

	return b.isChatAdmin(ctx, message.Chat.ID, message.From.ID)
}

// entityText returns the text covered by a message entity (offsets are UTF-16 code units).
func entityText(text string, entity t.MessageEntity) string {
	units := utf16.Encode([]rune(text))
	if entity.Offset < 0 || entity.Length <= 0 || entity.Offset+entity.Length > len(units) {
		return ""
	}

	return string(utf16.Decode(units[entity.Offset : entity.Offset+entity.Length]))
}

var errNoTarget = errors.New("no target user")

// targetUser finds the user an admin command is about: a text mention, an @username of a
// user the bot has seen, or the author of the replied-to message.
func (b *Bot) targetUser(ctx context.Context, message *t.Message) (*storage.User, error) {
	for _, entity := range message.Entities {
		switch entity.Type {
		case t.EntityTypeTextMention:
			if entity.User != nil && !entity.User.IsBot {
				return b.storage.CreateOrUpdateUser(ctx, entity.User.ID, entity.User.Username, entity.User.FirstName, entity.User.LastName)
			}
		case t.EntityTypeMention:
			username := strings.TrimPrefix(entityText(message.Text, entity), "@")
			if username == "" {
				continue
			}

			user, err := b.storage.GetUserByUsername(ctx, username)
			if err != nil {
				return nil, err
			}

			return user, nil
		}
	}

	if reply := message.ReplyToMessage; reply != nil && reply.From != nil && !reply.From.IsBot {
		return b.storage.CreateOrUpdateUser(ctx, reply.From.ID, reply.From.Username, reply.From.FirstName, reply.From.LastName)
	}

	return nil, errNoTarget
}

// handleAdminMember adds (add=true) or removes the targeted user to/from a group. Admins only.
func (b *Bot) handleAdminMember(add bool) commandHandler {
	name := "remove"
	if add {
		name = "add"
	}

	return func(ctx context.Context, message t.Message) error {
		chatID := message.Chat.ID
		lang := langOf(message.From)

		if message.Chat.Type == t.ChatTypePrivate {
			b.sendMessage(ctx, chatID, md(lang, "admin.group_only"), &message)

			return nil
		}

		if !b.isAdminMessage(ctx, &message) {
			b.sendMessage(ctx, chatID, md(lang, "admin.only."+name), &message)

			return nil
		}

		args := strings.Fields(message.Text)
		if len(args) < 2 {
			b.sendMessage(ctx, chatID, md(lang, "admin.usage."+name), &message)

			return nil
		}

		target, err := b.targetUser(ctx, &message)

		switch {
		case errors.Is(err, errNoTarget):
			b.sendMessage(ctx, chatID, md(lang, "admin.target."+name), &message)

			return nil
		case errors.Is(err, storage.ErrNotFound):
			b.sendMessage(ctx, chatID, md(lang, "admin.unknown_user"), &message)

			return nil
		case err != nil:
			logError("bot: Failed to resolve target user", err, "chat_id", chatID)
			b.sendMessage(ctx, chatID, md(lang, "admin.user_failed"), &message)

			return nil
		}

		return b.executeOnGroup(ctx, chatID, args[1], &message, true,
			func(group *storage.MentionGroup, original *t.Message) error {
				var out outcome
				if add {
					out = b.adminAddMember(ctx, lang, group, target)
				} else {
					out = b.adminRemoveMember(ctx, lang, group, target)
				}

				if out.ok {
					slog.Info("bot: Admin action", "action", name, "chat_id", chatID, "admin_id", message.From.ID, "target_id", target.ID, "group_id", group.ID)
				}

				b.sendMessage(ctx, chatID, out.render(), original)

				return nil
			})
	}
}

func (b *Bot) adminAddMember(ctx context.Context, lang i18n.Lang, group *storage.MentionGroup, target *storage.User) outcome {
	added, err := b.storage.AddMember(ctx, group.ID, &storage.User{ID: target.ID})
	if err != nil {
		logError("bot: Failed to add member", err, "group_id", group.ID, "user_id", target.ID)

		return outcome{text: tr(lang, "admin.add_failed")}
	}

	if !added {
		return outcome{text: tr(lang, "admin.already", target.FirstName, group.Name)}
	}

	return outcome{text: tr(lang, "admin.added", target.FirstName, group.Name), ok: true}
}

func (b *Bot) adminRemoveMember(ctx context.Context, lang i18n.Lang, group *storage.MentionGroup, target *storage.User) outcome {
	removed, err := b.storage.RemoveMember(ctx, group.ID, target.ID)
	if err != nil {
		logError("bot: Failed to remove member", err, "group_id", group.ID, "user_id", target.ID)

		return outcome{text: tr(lang, "admin.remove_failed")}
	}

	if !removed {
		return outcome{text: tr(lang, "admin.not_member", target.FirstName, group.Name)}
	}

	return outcome{text: tr(lang, "admin.removed", target.FirstName, group.Name), ok: true}
}

// forceDeleteRequested reports whether a /del command ends with the "force" keyword.
func forceDeleteRequested(args []string) bool {
	return len(args) >= 3 && strings.EqualFold(args[2], "force")
}

// handleForceDelete asks an admin to confirm deleting a group together with its members.
func (b *Bot) handleForceDelete(ctx context.Context, message t.Message, groupName string) error {
	chatID := message.Chat.ID
	lang := langOf(message.From)

	if !b.isAdminMessage(ctx, &message) {
		b.sendMessage(ctx, chatID, md(lang, "force.only_admins"), &message)

		return nil
	}

	return b.executeOnGroup(ctx, chatID, groupName, &message, false, func(group *storage.MentionGroup, original *t.Message) error {
		confirm := callbackData{action: "df", groupID: group.ID, requester: message.From.ID}
		cancel := callbackData{action: "dx", groupID: group.ID, requester: message.From.ID}

		keyboard := tu.InlineKeyboard(tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(tr(lang, "btn.delete")).WithCallbackData(confirm.String()),
			tu.InlineKeyboardButton(tr(lang, "btn.cancel")).WithCallbackData(cancel.String()),
		))

		b.sendMessage(ctx, chatID, md(lang, "force.confirm", group.Name, i18n.Plural(lang, "members", len(group.Members))), original, keyboard)

		return nil
	})
}

// handleForceDeleteCallback processes the Delete/Cancel buttons of a force-delete confirmation.
func (b *Bot) handleForceDeleteCallback(ctx context.Context, query t.CallbackQuery, msg *t.Message, data callbackData) {
	lang := langOf(&query.From)

	if data.action == "dx" {
		b.answerCallback(ctx, query.ID, tr(lang, "force.toast_cancel"))
		b.editMessage(ctx, msg, md(lang, "force.cancelled"))

		return
	}

	// Re-check: the button may be pressed long after the command was issued
	if !b.isChatAdmin(ctx, msg.Chat.ID, query.From.ID) {
		b.answerCallback(ctx, query.ID, tr(lang, "force.not_admin"))

		return
	}

	group, err := b.storage.GetGroupByID(ctx, data.groupID)

	switch {
	case errors.Is(err, storage.ErrNotFound) || (err == nil && group.ChatID != msg.Chat.ID):
		b.answerCallback(ctx, query.ID, tr(lang, "cb.group_gone"))
		b.editMessage(ctx, msg, md(lang, "cb.group_gone"))

		return
	case err != nil:
		b.answerCallback(ctx, query.ID, tr(lang, "error.generic"))

		return
	}

	if err := b.storage.DeleteGroup(ctx, group.ID); err != nil {
		logError("bot: Failed to force-delete group", err, "group_id", group.ID)
		b.answerCallback(ctx, query.ID, tr(lang, "force.failed"))

		return
	}

	slog.Info("bot: Admin action", "action", "force-delete", "chat_id", msg.Chat.ID, "admin_id", query.From.ID, "group_id", group.ID, "member_count", len(group.Members))
	b.answerCallback(ctx, query.ID, tr(lang, "force.toast_done"))
	b.editMessage(ctx, msg, md(lang, "force.done", group.Name, i18n.Plural(lang, "members", len(group.Members))))
}
