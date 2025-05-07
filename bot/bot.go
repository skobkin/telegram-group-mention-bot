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

const (
	groupNameAll = "all"
)

type Bot struct {
	bot     *t.Bot
	storage *storage.Storage
}

func New(token string, storage *storage.Storage) (*Bot, error) {
	slog.Debug("bot: Creating new bot instance", "token_length", len(token))

	// Create bot with debug logging
	bot, err := t.NewBot(token, t.WithDefaultLogger(false, true))
	if err != nil {
		slog.Error("bot: Failed to create bot", "error", err, "token_length", len(token))
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	slog.Debug("bot: Bot instance created successfully")
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
	slog.Debug("bot: Getting updates channel")
	updates, err := b.bot.UpdatesViaLongPolling(context.Background(), nil)
	if err != nil {
		slog.Error("bot: Failed to get updates channel", "error", err)
		return fmt.Errorf("failed to get updates channel: %w", err)
	}
	slog.Debug("bot: Updates channel obtained successfully")

	// Create handler with updates channel
	slog.Debug("bot: Creating bot handler")
	h, err := th.NewBotHandler(b.bot, updates)
	if err != nil {
		slog.Error("bot: Failed to create handler", "error", err)
		return fmt.Errorf("failed to create handler: %w", err)
	}
	slog.Debug("bot: Bot handler created successfully")

	// Register update middleware for logging
	slog.Debug("bot: Registering middleware")
	h.Use(b.logUpdate)
	h.Use(b.syncUserData)
	h.Use(b.addToAllGroup)
	h.Use(b.migrateChat)

	// Register command handlers
	slog.Debug("bot: Registering command handlers")
	h.HandleMessage(b.handleHelp, th.CommandEqual("help"))
	h.HandleMessage(b.handleList, th.CommandEqual("list"))
	h.HandleMessage(b.handleMy, th.CommandEqual("my"))
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

	slog.Info("bot: Starting bot handlers")
	return h.Start()
}

func (b *Bot) handleNewGroup(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling new group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)
	if len(args) != 2 {
		slog.Debug("bot: Invalid new group command format", "args_count", len(args))
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Usage: /new <group_name>\nGroup name can only contain lowercase letters, numbers, and dashes."), &message)
		return nil
	}

	groupName := strings.ToLower(args[1])
	if !isValidGroupName(groupName) {
		slog.Debug("bot: Invalid group name", "group_name", groupName)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("Invalid group name. Group name can only contain lowercase letters, numbers, and dashes."), &message)
		return nil
	}

	b.sendTyping(tu.ID(message.Chat.ID))
	slog.Debug("bot: Creating new group", "group_name", groupName, "chat_id", message.Chat.ID)
	err := b.storage.CreateGroup(groupName, message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to create group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create group: %v", err)), &message)
		return nil
	}

	slog.Info("bot: Group created", "group_name", groupName, "chat_id", message.Chat.ID)
	b.sendMessage(message.Chat.ID, fmt.Sprintf("Group '%s' created successfully\\!\nTo join this group, use: /join %s",
		escapeMarkdownV2(groupName), escapeMarkdownV2(groupName)), &message)
	return nil
}

func (b *Bot) handleJoin(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling join command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for join command, showing available groups")
		groups, err := b.storage.GetGroupsToJoinByChatAndUser(message.Chat.ID, message.From.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("join", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to join."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to join."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Joining group", "group_name", groupName, "chat_id", message.Chat.ID, "user_id", message.From.ID)
	err := b.executeOnGroup(message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.joinGroup(group, message.From, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleLeave(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling leave command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for leave command, showing available groups")
		groups, err := b.storage.GetUserGroupsByChat(message.Chat.ID, message.From.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("leave", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to leave."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to leave."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Leaving group", "group_name", groupName, "chat_id", message.Chat.ID, "user_id", message.From.ID)
	err := b.executeOnGroup(message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.leaveGroup(group, message.From.ID, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleMention(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling mention command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for mention command, showing available groups")
		groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("m", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to mention."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to mention."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Mentioning group", "group_name", groupName, "chat_id", message.Chat.ID)
	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(message.Chat.ID, []string{groupName})
	if err != nil {
		slog.Error("bot: Failed to find group", "error", err, "chat_id", message.Chat.ID, "group_name", groupName)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to find group: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: Group not found", "group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group '%s' not found.", groupName)), &message)
		return nil
	}

	slog.Debug("bot: Found group for mention", "group_name", groupName, "group_id", groups[0].ID, "member_count", len(groups[0].Members))
	return b.mentionGroups(groups, message.Chat.ID, &message)
}

func (b *Bot) handleDeleteGroup(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling delete group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for delete command, showing available groups")
		groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("del", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to delete."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to delete."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Deleting group", "group_name", groupName, "chat_id", message.Chat.ID)
	err := b.executeOnGroup(message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.deleteGroup(group, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleShowGroup(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling show group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for show command, showing available groups")
		groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("show", groups)
		if err != nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups available to show."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to show."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(context.Background(), b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Showing group", "group_name", groupName, "chat_id", message.Chat.ID)
	err := b.executeOnGroup(message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.showGroupMembers(group, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleHelp(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling help command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	helpText := escapeMarkdownV2(`Available commands:
/new <name> - Create a new mention group
/join <name> - Join an existing mention group
/leave <name> - Leave a mention group
/mention <name> or /m <name> or /call <name> - Mention all members of a group
/show <name> - Show all members of a group without mentioning them
/del <name> - Delete a group (only if it has no members)
/list - Show all groups in this chat
/my - Show groups you've joined in this chat
/help - Show this help message`)

	b.sendMessage(message.Chat.ID, helpText, &message)
	return nil
}

func (b *Bot) handleList(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling list command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	b.sendTyping(tu.ID(message.Chat.ID))

	groups, err := b.storage.GetGroupsByChat(message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to get groups", "error", err, "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: No groups found for chat", "chat_id", message.Chat.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("No groups found in this chat."), &message)
		return nil
	}

	slog.Debug("bot: Listing groups", "chat_id", message.Chat.ID, "group_count", len(groups))
	header := "Groups in this chat:\n"
	var groupNames []string
	for _, group := range groups {
		groupText := fmt.Sprintf("• %s\n", escapeMarkdownV2(group.Name))
		groupNames = append(groupNames, groupText)
	}

	listText := header + strings.Join(groupNames, "")
	b.sendMessage(message.Chat.ID, listText, &message)
	return nil
}

func (b *Bot) handleMy(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling my command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	b.sendTyping(tu.ID(message.Chat.ID))

	groups, err := b.storage.GetUserGroupsByChat(message.Chat.ID, message.From.ID)
	if err != nil {
		slog.Error("bot: Failed to get user's groups", "error", err, "chat_id", message.Chat.ID, "user_id", message.From.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get your groups: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: No groups found for user", "chat_id", message.Chat.ID, "user_id", message.From.ID)
		b.sendMessage(message.Chat.ID, escapeMarkdownV2("You haven't joined any groups in this chat."), &message)
		return nil
	}

	slog.Debug("bot: Listing user's groups", "chat_id", message.Chat.ID, "user_id", message.From.ID, "group_count", len(groups))
	header := "Groups you've joined in this chat:\n"
	var groupNames []string
	for _, group := range groups {
		groupText := fmt.Sprintf("• %s\n", escapeMarkdownV2(group.Name))
		groupNames = append(groupNames, groupText)
	}

	listText := header + strings.Join(groupNames, "")
	b.sendMessage(message.Chat.ID, listText, &message)
	return nil
}

func (b *Bot) handleFreeFormMessage(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling free form message", "chat_id", message.Chat.ID, "from_user_id", message.From.ID, "text_length", len(message.Text))

	if !strings.Contains(message.Text, "@") {
		slog.Debug("bot: Message does not contain @ mentions, skipping", "chat_id", message.Chat.ID)
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
		slog.Debug("bot: No valid group names found in message", "chat_id", message.Chat.ID)
		return nil
	}

	slog.Debug("bot: Found group mentions in message", "chat_id", message.Chat.ID, "group_names", groupNames)

	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(message.Chat.ID, groupNames)
	if err != nil {
		slog.Error("bot: Failed to find groups", "error", err, "chat_id", message.Chat.ID)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: No groups found for mentions", "chat_id", message.Chat.ID, "group_names", groupNames)
		return nil
	}

	b.sendTyping(tu.ID(message.Chat.ID))

	slog.Debug("bot: Found groups for mentions", "chat_id", message.Chat.ID, "group_count", len(groups))
	err = b.mentionGroups(groups, message.Chat.ID, &message)
	if err != nil {
		slog.Error("bot: Failed to mention group members", "error", err, "chat_id", message.Chat.ID)
	}

	return nil
}
