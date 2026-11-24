package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// executeOnGroup executes a function on a group if it exists. notFound is the reply
// format for a missing group (%s = name); empty means "Group not found.".
func (b *Bot) executeOnGroup(ctx context.Context, chatID int64, groupName string, originalMessage *t.Message, notFound string, operation func(*storage.MentionGroup, *t.Message) error) error {
	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(ctx, chatID, []string{groupName})
	if err != nil {
		slog.Error("bot:helpers: Failed to get group", "error", err, "group_name", groupName, "chat_id", chatID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2("Failed to look up the group."), originalMessage)

		return err
	}

	if len(groups) == 0 {
		text := "Group not found."
		if notFound != "" {
			text = fmt.Sprintf(notFound, groupName)
		}

		b.sendMessage(ctx, chatID, escapeMarkdownV2(text), originalMessage)

		return storage.ErrNotFound
	}

	return operation(&groups[0], originalMessage)
}

// displayName joins first and last name without stray spaces; empty names become "user".
func displayName(u storage.User) string {
	name := strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
	if name == "" {
		return "user"
	}

	return name
}

// formatMemberList formats a list of members for display
func (b *Bot) formatMemberList(members []storage.GroupMember) []string {
	memberList := make([]string, 0, len(members))
	for _, member := range members {
		line := displayName(member.User)
		if member.User.Username != "" {
			line += " (" + member.User.Username + ")"
		}

		memberList = append(memberList, line)
	}

	return memberList
}

// formatMentions formats a list of members for mentioning
func (b *Bot) formatMentions(members []storage.GroupMember) []string {
	mentions := make([]string, 0, len(members))
	for _, member := range members {
		if member.User.Username != "" {
			// Usernames are safe to use with @ as they can only contain [A-Za-z0-9_]
			mentions = append(mentions, "@"+escapeMarkdownV2(member.User.Username))
		} else {
			// For users without username, escape the name but not the tg:// URL
			mentions = append(mentions, fmt.Sprintf("[%s](tg://user?id=%d)", escapeMarkdownV2(displayName(member.User)), member.UserID))
		}
	}

	return mentions
}

func escapeMarkdownV2(text string) string {
	var result strings.Builder
	for _, r := range text {
		// Only escape non-alphanumeric ASCII characters (codes 1-126)
		if r > 0 && r < 127 && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			result.WriteRune('\\')
		}
		result.WriteRune(r)
	}

	return result.String()
}

func isValidGroupName(name string) bool {
	slog.Debug("bot:helpers: Validating group name", "name", name)

	if len(name) == 0 {
		slog.Debug("bot:helpers: Group name is empty")

		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			slog.Debug("bot:helpers: Group name contains invalid character", "char", string(c))

			return false
		}
	}

	slog.Debug("bot:helpers: Group name is valid")

	return true
}

func (b *Bot) send(ctx context.Context, chatID int64, text string, originalMessage *t.Message, autoDelete bool, replyMarkup ...t.ReplyMarkup) *t.Message {
	slog.Debug("bot:helpers: Going to send message", "chat_id", chatID, "text_length", len(text), "has_reply_markup", len(replyMarkup) > 0, "has_original_message", originalMessage != nil)

	message := tu.Message(tu.ID(chatID), text)
	message.ParseMode = "MarkdownV2"
	message.LinkPreviewOptions = &t.LinkPreviewOptions{IsDisabled: true}
	if len(replyMarkup) > 0 {
		message.ReplyMarkup = replyMarkup[0]
	}

	if originalMessage != nil {
		message = b.reply(*originalMessage, message)

		// Keep the answer in the originating forum topic. Only for real topics: outside forums
		// message_thread_id is the reply-chain root and the API rejects it.
		if originalMessage.IsTopicMessage {
			message.MessageThreadID = originalMessage.MessageThreadID
		}
	}

	sent, err := b.api.SendMessage(ctx, message)
	if err != nil {
		// Check if it's a rate limit error
		if strings.Contains(err.Error(), "Too Many Requests") {
			// Extract retry_after value from error message
			// Format: "telego: sendMessage: api: 429 \"Too Many Requests: retry after 5\", migrate to chat ID: 0, retry after: 5"
			parts := strings.Split(err.Error(), "retry after: ")
			if len(parts) == 2 {
				// Parse the retry_after value
				var retryAfter int
				if _, _ = fmt.Sscanf(parts[1], "%d", &retryAfter); retryAfter > 0 {
					slog.Info("bot:helpers: Rate limit hit, waiting", "seconds", retryAfter)
					time.Sleep(time.Duration(retryAfter) * time.Second)
					retried, retryErr := b.api.SendMessage(ctx, message)
					if retryErr != nil {
						err = retryErr
					} else {
						sent, err = retried, nil
						slog.Info("bot:helpers: Message sent successfully after rate limit wait")
					}
				}
			}
		}
		if err != nil {
			slog.Error("bot:helpers: Failed to send message", "error", err, "chat_id", chatID, "text_length", len(text))

			return nil
		}
	} else {
		slog.Debug("bot:helpers: Message sent successfully")
	}

	if autoDelete && sent != nil {
		ids := []int{sent.MessageID}
		if originalMessage != nil {
			ids = append(ids, originalMessage.MessageID)
		}

		b.scheduleDelete(chatID, ids...)
	}

	return sent
}

// sendMessage sends a service reply (confirmation, error, list, picker). With AUTO_DELETE_SECONDS set,
// the reply and the command it answers are deleted after that time.
func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string, originalMessage *t.Message, replyMarkup ...t.ReplyMarkup) {
	b.send(ctx, chatID, text, originalMessage, true, replyMarkup...)
}

// sendPost sends content people are meant to keep, such as mention posts; it is never auto-deleted.
func (b *Bot) sendPost(ctx context.Context, chatID int64, text string, originalMessage *t.Message, replyMarkup ...t.ReplyMarkup) {
	b.send(ctx, chatID, text, originalMessage, false, replyMarkup...)
}

func (b *Bot) reply(originalMessage t.Message, newMessage *t.SendMessageParams) *t.SendMessageParams {
	slog.Debug("bot:helpers: Creating reply message", "original_message_id", originalMessage.MessageID)

	return newMessage.WithReplyParameters(&t.ReplyParameters{
		MessageID: originalMessage.MessageID,
	})
}

func (b *Bot) sendTyping(ctx context.Context, chatID t.ChatID) {
	slog.Debug("bot:helpers: Setting 'typing' chat action", "chat_id", chatID)
	err := b.api.SendChatAction(ctx, tu.ChatAction(chatID, "typing"))
	if err != nil {
		slog.Error("bot:helpers: Cannot set chat action", "error", err)
	}
}

// replyTarget returns the message a mention post should reply to: the message a command replies to
// (so pinged people see the context), otherwise the command itself. The result keeps the command's
// forum topic. In forum topics Telegram sets reply_to_message to the topic's creation message for
// plain messages; that does not count as a reply.
func replyTarget(command *t.Message) *t.Message {
	if command == nil {
		return nil
	}

	reply := command.ReplyToMessage
	if reply == nil || reply.ForumTopicCreated != nil || reply.MessageID == 0 {
		return command
	}

	target := *command
	target.MessageID = reply.MessageID

	return &target
}
