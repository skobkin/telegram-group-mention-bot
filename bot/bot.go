package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/storage"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	"github.com/mymmrac/telego/telegoutil"
)

type Bot struct {
	bot     *telego.Bot
	storage *storage.Storage
}

func New(token string, storage *storage.Storage) (*Bot, error) {
	slog.Info("Initializing bot", "token_length", len(token))

	// Create bot with debug logging
	bot, err := telego.NewBot(token, telego.WithDefaultLogger(false, true))
	if err != nil {
		slog.Error("Failed to create bot", "error", err, "token_length", len(token))
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	return &Bot{
		bot:     bot,
		storage: storage,
	}, nil
}

func (b *Bot) Start() error {
	// Get and log bot information
	me, err := b.bot.GetMe(context.Background())
	if err != nil {
		slog.Error("Failed to get bot information", "error", err)
		return fmt.Errorf("failed to get bot information: %w", err)
	}
	slog.Info("Bot started",
		"id", me.ID,
		"username", me.Username,
		"first_name", me.FirstName,
		"last_name", me.LastName,
		"is_bot", me.IsBot,
		"can_join_groups", me.CanJoinGroups,
		"can_read_all_group_messages", me.CanReadAllGroupMessages,
		"supports_inline_queries", me.SupportsInlineQueries,
	)

	// Get updates channel
	updates, err := b.bot.UpdatesViaLongPolling(context.Background(), nil)
	if err != nil {
		slog.Error("Failed to get updates channel", "error", err)
		return fmt.Errorf("failed to get updates channel: %w", err)
	}

	// Create handler with updates channel
	h, err := th.NewBotHandler(b.bot, updates)
	if err != nil {
		slog.Error("Failed to create handler", "error", err)
		return fmt.Errorf("failed to create handler: %w", err)
	}

	// Register update middleware for logging
	h.Use(b.logUpdate)

	// Register command handlers
	h.HandleMessage(b.handleHelp, th.CommandEqual("help"))
	h.HandleMessage(b.handleList, th.CommandEqual("list"))
	h.HandleMessage(b.handleNewGroup, th.CommandEqual("new"))
	h.HandleMessage(b.handleJoin, th.CommandEqual("join"))
	h.HandleMessage(b.handleLeave, th.CommandEqual("leave"))
	h.HandleMessage(b.handleMention, th.Or(
		th.CommandEqual("mention"),
		th.CommandEqual("m"),
		th.CommandEqual("call"),
	))
	h.HandleMessage(b.handleDeleteGroup, th.CommandEqual("del"))
	h.HandleMessage(b.handleShowGroup, th.CommandEqual("show"))

	slog.Info("Starting bot handlers")
	return h.Start()
}

func (b *Bot) handleNewGroup(ctx *th.Context, message telego.Message) error {
	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /new <group_name>"))
		return nil
	}

	groupName := args[1]
	err := b.storage.CreateGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("Failed to create group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create group: %v", err)))
		return nil
	}

	slog.Info("Group created", "group_name", groupName, "chat_id", message.Chat.ID)
	b.sendMessage(message.Chat.ID, fmt.Sprintf("Group '%s' created successfully!\nTo join this group, use: /join %s",
		escapeMarkdownV2(groupName), escapeMarkdownV2(groupName)))
	return nil
}

func (b *Bot) handleJoin(ctx *th.Context, message telego.Message) error {
	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /join <group_name>"))
		return nil
	}

	groupName := args[1]
	group, err := b.storage.GetGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("Failed to get group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group not found: %v", err)))
		return nil
	}

	user := message.From
	err = b.storage.AddMember(group.ID, user.ID, user.Username, user.FirstName, user.LastName)
	if err != nil {
		slog.Error("Failed to join group", "error", err,
			"group_id", group.ID, "user_id", user.ID, "username", user.Username)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to join group: %v", err)))
		return nil
	}

	slog.Info("User joined group", "group_name", groupName,
		"user_id", user.ID, "username", user.Username)
	b.sendMessage(message.Chat.ID, fmt.Sprintf("Successfully joined group '%s'!", escapeMarkdownV2(groupName)))
	return nil
}

func (b *Bot) handleLeave(ctx *th.Context, message telego.Message) error {
	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /leave <group_name>"))
		return nil
	}

	groupName := args[1]
	group, err := b.storage.GetGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("Failed to get group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group not found: %v", err)))
		return nil
	}

	err = b.storage.RemoveMember(group.ID, message.From.ID)
	if err != nil {
		slog.Error("Failed to leave group", "error", err,
			"group_id", group.ID, "user_id", message.From.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to leave group: %v", err)))
		return nil
	}

	slog.Info("User left group", "group_name", groupName,
		"user_id", message.From.ID)
	b.sendMessage(message.Chat.ID, fmt.Sprintf("Successfully left group '%s'!", escapeMarkdownV2(groupName)))
	return nil
}

func (b *Bot) handleMention(ctx *th.Context, message telego.Message) error {
	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /mention <group_name>"))
		return nil
	}

	groupName := args[1]
	group, err := b.storage.GetGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("Failed to get group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group not found: %v", err)))
		return nil
	}

	members, err := b.storage.GetGroupMembers(group.ID)
	if err != nil {
		slog.Error("Failed to get group members", "error", err,
			"group_id", group.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get group members: %v", err)))
		return nil
	}

	if len(members) == 0 {
		slog.Info("No members in group", "group_name", groupName)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("No members in this group!"))
		return nil
	}

	var mentions []string
	for _, member := range members {
		if member.Username != "" {
			mentions = append(mentions, fmt.Sprintf("@%s", member.Username))
		} else {
			mentions = append(mentions, fmt.Sprintf("[%s %s](tg://user?id=%d)",
				escapeMarkdownV2(member.FirstName), escapeMarkdownV2(member.LastName), member.UserID))
		}
	}

	mentionText := fmt.Sprintf("Mentioning %s group members:\n%s", escapeMarkdownV2(groupName), strings.Join(mentions, ", "))
	b.sendMessage(message.Chat.ID, mentionText)
	return nil
}

func (b *Bot) handleDeleteGroup(ctx *th.Context, message telego.Message) error {
	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /del <group_name>"))
		return nil
	}

	groupName := args[1]
	group, err := b.storage.GetGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("Failed to get group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group not found: %v", err)))
		return nil
	}

	members, err := b.storage.GetGroupMembers(group.ID)
	if err != nil {
		slog.Error("Failed to get group members", "error", err,
			"group_id", group.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get group members: %v", err)))
		return nil
	}

	if len(members) > 0 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Cannot delete group: it has members. Ask members to /leave first."))
		return nil
	}

	err = b.storage.DeleteGroup(group.ID)
	if err != nil {
		slog.Error("Failed to delete group", "error", err,
			"group_id", group.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to delete group: %v", err)))
		return nil
	}

	slog.Info("Group deleted", "group_name", groupName, "chat_id", message.Chat.ID)
	b.sendMessage(message.Chat.ID, fmt.Sprintf("Group '%s' deleted successfully!", escapeMarkdownV2(groupName)))
	return nil
}

func (b *Bot) handleShowGroup(ctx *th.Context, message telego.Message) error {
	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /show <group_name>"))
		return nil
	}

	groupName := args[1]
	group, err := b.storage.GetGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("Failed to get group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group not found: %v", err)))
		return nil
	}

	members, err := b.storage.GetGroupMembers(group.ID)
	if err != nil {
		slog.Error("Failed to get group members", "error", err,
			"group_id", group.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get group members: %v", err)))
		return nil
	}

	if len(members) == 0 {
		b.sendMessage(message.Chat.ID, fmt.Sprintf("Group '%s' has no members.", escapeMarkdownV2(groupName)))
		return nil
	}

	var memberList []string
	for _, member := range members {
		if member.Username != "" {
			memberList = append(memberList, fmt.Sprintf("%s %s \\(%s\\)",
				escapeMarkdownV2(member.FirstName),
				escapeMarkdownV2(member.LastName),
				escapeMarkdownV2(member.Username),
			))
		} else {
			memberList = append(memberList, fmt.Sprintf("%s %s",
				escapeMarkdownV2(member.FirstName), escapeMarkdownV2(member.LastName)))
		}
	}

	showText := fmt.Sprintf("Members of '%s':\n%s", escapeMarkdownV2(groupName), strings.Join(memberList, "\n"))
	b.sendMessage(message.Chat.ID, showText)
	return nil
}

func (b *Bot) handleHelp(ctx *th.Context, message telego.Message) error {
	helpText := escapeMarkdownV2(`Available commands:
/new <name> - Create a new mention group
/join <name> - Join an existing mention group
/leave <name> - Leave a mention group
/mention <name> - Mention all members of a group
/show <name> - Show all members of a group without mentioning them
/del <name> - Delete a group (only if it has no members)
/list - Show all groups in this chat
/help - Show this help message`)

	b.sendMessage(message.Chat.ID, helpText)
	return nil
}

func (b *Bot) handleList(ctx *th.Context, message telego.Message) error {
	groups, err := b.storage.GetGroups(message.Chat.ID)
	if err != nil {
		slog.Error("Failed to get groups", "error", err, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)))
		return nil
	}

	if len(groups) == 0 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups found in this chat."))
		return nil
	}

	header := "Groups in this chat:\n"
	var groupNames []string
	for _, group := range groups {
		groupText := fmt.Sprintf("• %s\n", escapeMarkdownV2(group.Name))
		groupNames = append(groupNames, groupText)
	}

	listText := header + strings.Join(groupNames, "")
	b.sendMessage(message.Chat.ID, listText)
	return nil
}

func (b *Bot) sendMessage(chatID int64, text string) {
	message := telegoutil.Message(telegoutil.ID(chatID), text)
	message.ParseMode = "MarkdownV2"
	_, err := b.bot.SendMessage(context.Background(), message)
	if err != nil {
		slog.Error("Failed to send message", "error", err,
			"chat_id", chatID, "text_length", len(text))
	}
}

// logUpdate is a middleware that logs all incoming updates
func (b *Bot) logUpdate(ctx *th.Context, update telego.Update) error {
	updateType := "unknown"
	var details string

	switch {
	case update.Message != nil:
		updateType = "message"
		msg := update.Message
		details = fmt.Sprintf("from: %d, chat: %d, text: %q",
			msg.From.ID, msg.Chat.ID, msg.Text)
	case update.EditedMessage != nil:
		updateType = "edited_message"
		msg := update.EditedMessage
		details = fmt.Sprintf("from: %d, chat: %d, text: %q",
			msg.From.ID, msg.Chat.ID, msg.Text)
	case update.CallbackQuery != nil:
		updateType = "callback_query"
		cb := update.CallbackQuery
		details = fmt.Sprintf("from: %d, data: %q", cb.From.ID, cb.Data)
	}

	slog.Info("Received update",
		"type", updateType,
		"update_id", update.UpdateID,
		"details", details)

	return ctx.Next(update)
}

func escapeMarkdownV2(text string) string {
	specialChars := []string{
		"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!",
	}

	for _, char := range specialChars {
		text = strings.ReplaceAll(text, char, "\\"+char)
	}
	return text
}
