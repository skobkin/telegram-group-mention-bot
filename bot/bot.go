package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/storage"

	t "github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

type Bot struct {
	bot     *t.Bot
	storage *storage.Storage
}

func New(token string, storage *storage.Storage) (*Bot, error) {
	slog.Info("bot: Initializing bot", "token_length", len(token))

	// Create bot with debug logging
	bot, err := t.NewBot(token, t.WithDefaultLogger(false, true))
	if err != nil {
		slog.Error("bot: Failed to create bot", "error", err, "token_length", len(token))
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
		slog.Error("bot: Failed to get bot information", "error", err)
		return fmt.Errorf("failed to get bot information: %w", err)
	}
	slog.Info("bot: Bot started",
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
		slog.Error("bot: Failed to get updates channel", "error", err)
		return fmt.Errorf("failed to get updates channel: %w", err)
	}

	// Create handler with updates channel
	h, err := th.NewBotHandler(b.bot, updates)
	if err != nil {
		slog.Error("bot: Failed to create handler", "error", err)
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

	h.HandleMessage(b.handleFreeFormMessage, th.Not(th.AnyCommand()))

	h.HandleCallbackQuery(b.handleGroupCallback, th.Or(
		th.CallbackDataPrefix("join:"),
		th.CallbackDataPrefix("leave:"),
		th.CallbackDataPrefix("del:"),
		th.CallbackDataPrefix("show:"),
	))

	slog.Info("bot: Starting bot handlers")
	return h.Start()
}

func (b *Bot) handleNewGroup(ctx *th.Context, message t.Message) error {
	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /new <group_name>\nGroup name can only contain lowercase letters, numbers, and dashes."))
		return nil
	}

	groupName := strings.ToLower(args[1])
	if !isValidGroupName(groupName) {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Invalid group name. Group name can only contain lowercase letters, numbers, and dashes."))
		return nil
	}

	b.sendTyping(tu.ID(message.Chat.ID))
	err := b.storage.CreateGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to create group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create group: %v", err)))
		return nil
	}

	slog.Info("bot: Group created", "group_name", groupName, "chat_id", message.Chat.ID)
	b.sendMessage(message.Chat.ID, fmt.Sprintf("Group '%s' created successfully\\!\nTo join this group, use: /join %s",
		escapeMarkdownV2(groupName), escapeMarkdownV2(groupName)))
	return nil
}

func (b *Bot) handleJoin(ctx *th.Context, message t.Message) error {
	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		groups, err := b.storage.GetGroupsToJoinByChatAndUser(message.Chat.ID, message.From.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)))
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("join", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)))
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to join."))
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to join."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	err := b.executeOnGroup(message.Chat.ID, groupName, func(group *storage.MentionGroup) error {
		return b.joinGroup(group, message.From, message.Chat.ID)
	})
	return err
}

func (b *Bot) handleLeave(ctx *th.Context, message t.Message) error {
	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		groups, err := b.storage.GetGroupsToLeaveByChatAndUser(message.Chat.ID, message.From.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)))
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("leave", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)))
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to leave."))
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to leave."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	err := b.executeOnGroup(message.Chat.ID, groupName, func(group *storage.MentionGroup) error {
		return b.leaveGroup(group, message.From.ID, message.Chat.ID)
	})
	return err
}

func (b *Bot) handleMention(ctx *th.Context, message t.Message) error {
	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)))
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("m", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)))
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to mention."))
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to mention."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(message.Chat.ID, []string{groupName})
	if err != nil {
		slog.Error("bot: Failed to find group", "error", err, "chat_id", message.Chat.ID, "group_name", groupName)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to find group: %v", err)))
		return nil
	}

	if len(groups) == 0 {
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group '%s' not found.", groupName)))
		return nil
	}

	return b.mentionGroups(groups, message.Chat.ID)
}

func (b *Bot) handleDeleteGroup(ctx *th.Context, message t.Message) error {
	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)))
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("del", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)))
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to delete."))
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to delete."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	err := b.executeOnGroup(message.Chat.ID, groupName, func(group *storage.MentionGroup) error {
		return b.deleteGroup(group, message.Chat.ID)
	})
	return err
}

func (b *Bot) handleShowGroup(ctx *th.Context, message t.Message) error {
	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)))
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("show", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)))
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to show."))
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to show."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	err := b.executeOnGroup(message.Chat.ID, groupName, func(group *storage.MentionGroup) error {
		return b.showGroupMembers(group, message.Chat.ID)
	})
	return err
}

func (b *Bot) handleHelp(ctx *th.Context, message t.Message) error {
	helpText := escapeMarkdownV2(`Available commands:
/new <name> - Create a new mention group
/join <name> - Join an existing mention group
/leave <name> - Leave a mention group
/mention <name> or /m <name> or /call <name> - Mention all members of a group
/show <name> - Show all members of a group without mentioning them
/del <name> - Delete a group (only if it has no members)
/list - Show all groups in this chat
/help - Show this help message`)

	b.sendMessage(message.Chat.ID, helpText)
	return nil
}

func (b *Bot) handleList(ctx *th.Context, message t.Message) error {
	b.sendTyping(tu.ID(message.Chat.ID))

	groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to get groups", "error", err, "chat_id", message.Chat.ID)
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

// handleGroupCallback is a generic handler for group-related callbacks
func (b *Bot) handleGroupCallback(ctx *th.Context, query t.CallbackQuery) error {
	if query.Message == nil {
		return nil
	}

	chatID := query.Message.GetChat().ID
	parts := strings.Split(query.Data, ":")
	if len(parts) != 2 {
		return nil
	}
	action := parts[0]
	groupName := parts[1]

	switch action {
	case "join":
		err := b.executeOnGroup(chatID, groupName, func(group *storage.MentionGroup) error {
			return b.joinGroup(group, &query.From, chatID)
		})
		return err

	case "leave":
		err := b.executeOnGroup(chatID, groupName, func(group *storage.MentionGroup) error {
			return b.leaveGroup(group, query.From.ID, chatID)
		})
		return err

	case "del":
		err := b.executeOnGroup(chatID, groupName, func(group *storage.MentionGroup) error {
			return b.deleteGroup(group, chatID)
		})
		return err

	case "show":
		err := b.executeOnGroup(chatID, groupName, func(group *storage.MentionGroup) error {
			return b.showGroupMembers(group, chatID)
		})
		return err
	}

	return nil
}

func (b *Bot) handleFreeFormMessage(ctx *th.Context, message t.Message) error {
	if !strings.Contains(message.Text, "@") {
		return nil
	}

	var groupNames []string
	// Split the message into words separated by spaces or commas
	words := strings.FieldsFunc(message.Text, func(r rune) bool {
		return r == ' ' || r == ','
	})
	for _, word := range words {
		if strings.HasPrefix(word, "@") {
			// Remove @ and any trailing punctuation (period or comma)
			name := strings.TrimRight(strings.TrimPrefix(word, "@"), ".,")
			if isValidGroupName(name) {
				groupNames = append(groupNames, name)
			}
		}
	}

	if len(groupNames) == 0 {
		return nil
	}

	b.sendTyping(tu.ID(message.Chat.ID))

	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(message.Chat.ID, groupNames)
	if err != nil {
		slog.Error("bot: Failed to find groups", "error", err, "chat_id", message.Chat.ID)
		return nil
	}

	if len(groups) == 0 {
		return nil
	}

	err = b.mentionGroups(groups, message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to mention group members", "error", err, "chat_id", message.Chat.ID)
	}

	return nil
}
