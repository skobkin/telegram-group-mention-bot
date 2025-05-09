package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"telegram-group-mention-bot/storage"

	t "github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// executeOnGroup executes a function on a group if it exists
func (b *Bot) executeOnGroup(chatID int64, groupName string, originalMessage *t.Message, operation func(*storage.MentionGroup, *t.Message) error) error {
	slog.Debug("bot:helpers: Requested operation execution on group", "chat_id", chatID, "group_name", groupName)

	group, err := b.storage.GetGroup(groupName, chatID)
	if err != nil {
		slog.Error("bot:helpers: Failed to get group", "error", err,
			"group_name", groupName, "chat_id", chatID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Group not found: %v", err)), originalMessage)
		return err
	}

	slog.Debug("bot:helpers: Group found, executing operation", "group_id", group.ID, "group_name", group.Name)
	return operation(group, originalMessage)
}

// formatMemberList formats a list of members for display
func (b *Bot) formatMemberList(members []storage.GroupMember) []string {
	slog.Debug("bot:helpers: Formatting member list", "member_count", len(members))

	var memberList []string
	for _, member := range members {
		if member.User.Username != "" {
			memberList = append(memberList, escapeMarkdownV2(fmt.Sprintf("%s %s (%s)",
				member.User.FirstName,
				member.User.LastName,
				member.User.Username)))
		} else {
			memberList = append(memberList, escapeMarkdownV2(fmt.Sprintf("%s %s",
				member.User.FirstName,
				member.User.LastName)))
		}
	}

	slog.Debug("bot:helpers: Member list formatted", "formatted_count", len(memberList))
	return memberList
}

// formatMentions formats a list of members for mentioning
func (b *Bot) formatMentions(members []storage.GroupMember) []string {
	slog.Debug("bot:helpers: Formatting mentions", "member_count", len(members))

	var mentions []string
	for _, member := range members {
		if member.User.Username != "" {
			// Usernames are safe to use with @ as they can only contain [A-Za-z0-9_]
			mentions = append(mentions, fmt.Sprintf("@%s", escapeMarkdownV2(member.User.Username)))
		} else {
			// For users without username, we need to escape the name parts but not the tg:// URL
			mentions = append(mentions, fmt.Sprintf(
				"[%s %s](tg://user?id=%d)",
				escapeMarkdownV2(member.User.FirstName),
				escapeMarkdownV2(member.User.LastName),
				member.UserID,
			))
		}
	}

	slog.Debug("bot:helpers: Mentions formatted", "mention_count", len(mentions))
	return mentions
}

func (b *Bot) createGroupSelectionReplyKeyboard(commandPrefix string, groups []storage.MentionGroup) (*t.ReplyKeyboardMarkup, error) {
	slog.Debug("bot:helpers: Creating group selection keyboard", "command_prefix", commandPrefix, "group_count", len(groups))

	if len(groups) == 0 {
		slog.Debug("bot:helpers: No groups available for keyboard, returning nil")
		return nil, nil
	}

	// Create keyboard with 2 columns
	keyboard := make([][]t.KeyboardButton, 0, (len(groups)+1)/2)
	for i := 0; i < len(groups); i += 2 {
		row := make([]t.KeyboardButton, 0, 2)
		row = append(row, t.KeyboardButton{
			Text: fmt.Sprintf("/%s %s", commandPrefix, groups[i].Name),
		})
		if i+1 < len(groups) {
			row = append(row, t.KeyboardButton{
				Text: fmt.Sprintf("/%s %s", commandPrefix, groups[i+1].Name),
			})
		}
		keyboard = append(keyboard, row)
	}

	slog.Debug("bot:helpers: Group selection keyboard created", "row_count", len(keyboard))
	return &t.ReplyKeyboardMarkup{
		Keyboard:              keyboard,
		ResizeKeyboard:        true,
		OneTimeKeyboard:       true,
		Selective:             true,
		InputFieldPlaceholder: "Select group",
	}, nil
}

func escapeMarkdownV2(text string) string {
	slog.Debug("bot:helpers: Escaping markdown", "input_text", text)

	var result strings.Builder
	for _, r := range text {
		// Only escape non-alphanumeric ASCII characters (codes 1-126)
		if r > 0 && r < 127 && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			result.WriteRune('\\')
		}
		result.WriteRune(r)
	}

	slog.Debug("bot:helpers: Markdown escaped", "output_text", result.String())
	return result.String()
}

func isValidGroupName(name string) bool {
	slog.Debug("bot:helpers: Validating group name", "name", name)

	if len(name) == 0 {
		slog.Debug("bot:helpers: Group name is empty")
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			slog.Debug("bot:helpers: Group name contains invalid character", "char", string(c))
			return false
		}
	}

	slog.Debug("bot:helpers: Group name is valid")
	return true
}

func (b *Bot) sendMessage(chatID int64, text string, originalMessage *t.Message, replyMarkup ...t.ReplyMarkup) {
	slog.Debug("bot:helpers: Going to send message", "chat_id", chatID, "text", text, "has_reply_markup", len(replyMarkup) > 0, "has_original_message", originalMessage != nil)

	message := tu.Message(tu.ID(chatID), text)
	message.ParseMode = "MarkdownV2"
	if len(replyMarkup) > 0 {
		message.ReplyMarkup = replyMarkup[0]
	}

	if originalMessage != nil {
		message = b.reply(*originalMessage, message)
	}

	_, err := b.bot.SendMessage(context.Background(), message)
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
					_, retryErr := b.bot.SendMessage(context.Background(), message)
					if retryErr != nil {
						err = retryErr
					} else {
						slog.Info("bot:helpers: Message sent successfully after rate limit wait")
					}
				}
			}
		}
		if err != nil {
			slog.Error("bot:helpers: Failed to send message", "error", err, "chat_id", chatID, "text_length", len(text))
		}
	} else {
		slog.Debug("bot:helpers: Message sent successfully")
	}
}

// AddMember adds a user to a mention group
func (b *Bot) AddMember(groupID uint, userID int64, username, firstName, lastName string) error {
	slog.Debug("bot:helpers: Adding member to group", "group_id", groupID, "user_id", userID, "username", username)

	// First ensure the user exists in the database
	user, err := b.storage.CreateOrUpdateUser(userID, username, firstName, lastName)
	if err != nil {
		slog.Error("bot:helpers: Failed to create/update user", "error", err, "user_id", userID, "username", username)
		return fmt.Errorf("failed to create/update user: %w", err)
	}

	// Then add them to the group
	if err := b.storage.AddMember(groupID, user); err != nil {
		slog.Error("bot:helpers: Failed to add member", "error", err, "group_id", groupID, "user_id", userID, "username", username)
		return fmt.Errorf("failed to add member: %w", err)
	}

	slog.Info("bot:helpers: User added to group", "group_id", groupID, "user_id", userID, "username", username)
	return nil
}

func (b *Bot) reply(originalMessage t.Message, newMessage *t.SendMessageParams) *t.SendMessageParams {
	slog.Debug("bot:helpers: Creating reply message", "original_message_id", originalMessage.MessageID)
	return newMessage.WithReplyParameters(&t.ReplyParameters{
		MessageID: originalMessage.MessageID,
	})
}

func (b *Bot) sendTyping(chatID t.ChatID) {
	slog.Debug("bot:helpers: Setting 'typing' chat action", "chat_id", chatID)
	err := b.bot.SendChatAction(context.Background(), tu.ChatAction(chatID, "typing"))
	if err != nil {
		slog.Error("bot:helpers: Cannot set chat action", "error", err)
	}
}
