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
func (b *Bot) executeOnGroup(chatID int64, groupName string, operation func(*storage.MentionGroup) error) error {
	group, err := b.storage.GetGroup(groupName, chatID)
	if err != nil {
		slog.Error("bot: Failed to get group", "error", err,
			"group_name", groupName, "chat_id", chatID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Group not found: %v", err)))
		return err
	}

	return operation(group)
}

// getGroupMembers retrieves all members of a group and handles any errors
func (b *Bot) getGroupMembers(group *storage.MentionGroup, chatID int64) ([]storage.GroupMember, error) {
	members, err := b.storage.GetGroupMembers(group.ID)
	if err != nil {
		slog.Error("bot: Failed to get group members", "error", err,
			"group_id", group.ID)
		b.sendMessage(chatID, escapeMarkdownV2(fmt.Sprintf("Failed to get group members: %v", err)))
		return nil, err
	}
	return members, nil
}

// formatMemberList formats a list of members for display
func (b *Bot) formatMemberList(members []storage.GroupMember) []string {
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
	return memberList
}

// formatMentions formats a list of members for mentioning
func (b *Bot) formatMentions(members []storage.GroupMember) []string {
	var mentions []string
	for _, member := range members {
		if member.User.Username != "" {
			mentions = append(mentions, fmt.Sprintf("@%s", member.User.Username))
		} else {
			mentions = append(mentions, fmt.Sprintf(
				"[%s %s](tg://user?id=%d)",
				escapeMarkdownV2(member.User.FirstName),
				escapeMarkdownV2(member.User.LastName),
				member.UserID,
			))
		}
	}
	return mentions
}

func (b *Bot) createGroupSelectionReplyKeyboard(commandPrefix string, groups []storage.MentionGroup) (*t.ReplyKeyboardMarkup, error) {
	if len(groups) == 0 {
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

	return &t.ReplyKeyboardMarkup{
		Keyboard:              keyboard,
		ResizeKeyboard:        true,
		OneTimeKeyboard:       true,
		Selective:             true,
		InputFieldPlaceholder: "Select group",
	}, nil
}

func escapeMarkdownV2(text string) string {
	specialChars := []string{
		"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!", "&", "<",
	}

	for _, char := range specialChars {
		text = strings.ReplaceAll(text, char, "\\"+char)
	}
	return text
}

func isValidGroupName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

func (b *Bot) sendMessage(chatID int64, text string, replyMarkup ...t.ReplyMarkup) {
	message := tu.Message(tu.ID(chatID), text)
	message.ParseMode = "MarkdownV2"
	if len(replyMarkup) > 0 {
		message.ReplyMarkup = replyMarkup[0]
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
					slog.Debug("bot: API error", "error", err.Error())
					slog.Info("bot: Rate limit hit, waiting", "seconds", retryAfter)
					time.Sleep(time.Duration(retryAfter) * time.Second)
					_, retryErr := b.bot.SendMessage(context.Background(), message)
					if retryErr != nil {
						err = retryErr
					} else {
						slog.Info("bot: Message sent successfully after rate limit wait")
					}
				}
			}
		}
		if err != nil {
			slog.Error("bot: Failed to send message", "error", err, "chat_id", chatID, "text_length", len(text))
		}
	} else {
		slog.Info("bot: Message sent successfully")
	}
}

// AddMember adds a user to a mention group
func (b *Bot) AddMember(groupID uint, userID int64, username, firstName, lastName string) error {
	// First ensure the user exists in the database
	user, err := b.storage.CreateOrUpdateUser(userID, username, firstName, lastName)
	if err != nil {
		slog.Error("bot: Failed to create/update user", "error", err, "user_id", userID, "username", username)
		return fmt.Errorf("failed to create/update user: %w", err)
	}

	// Then add them to the group
	if err := b.storage.AddMember(groupID, user); err != nil {
		slog.Error("bot: Failed to add member", "error", err, "group_id", groupID, "user_id", userID, "username", username)
		return fmt.Errorf("failed to add member: %w", err)
	}

	slog.Info("bot: User added to group", "group_id", groupID, "user_id", userID, "username", username)
	return nil
}

func (b *Bot) reply(originalMessage t.Message, newMessage *t.SendMessageParams) *t.SendMessageParams {
	return newMessage.WithReplyParameters(&t.ReplyParameters{
		MessageID: originalMessage.MessageID,
	})
}
