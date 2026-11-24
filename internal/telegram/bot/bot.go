package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"telegram-group-mention-bot/internal/logging"
	"telegram-group-mention-bot/internal/storage"

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
	me      *botInfo
}

func New(token string, storage *storage.Storage) (*Bot, error) {
	slog.Debug("bot: Creating new bot instance", "token_length", len(token))

	// Create bot with debug logging
	bot, err := t.NewBot(token, t.WithLogger(logging.TelegoLogger{}))
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

// shutdownTimeout bounds how long Run waits for in-flight handlers after the context is cancelled.
const shutdownTimeout = 10 * time.Second

// Run starts long polling and blocks until ctx is cancelled (or polling fails).
// On cancellation it stops polling, lets in-flight handlers finish for up to
// shutdownTimeout and then stops the handler loop.
func (b *Bot) Run(ctx context.Context) error {
	me, err := b.bot.GetMe(ctx)
	if err != nil {
		slog.Error("bot: Failed to get bot information", "error", err)
		return fmt.Errorf("failed to get bot information: %w", err)
	}
	b.me = botInfoFromMe(me)
	slog.Info("bot: Bot started",
		"id", b.me.ID,
		"username", b.me.Username,
		"first_name", b.me.FirstName,
		"is_bot", b.me.IsBot,
		"can_join_groups", b.me.CanJoinGroups,
		"can_read_all_group_messages", b.me.CanReadAllGroupMessages,
		"supports_inline_queries", b.me.SupportsInlineQueries,
	)

	// The updates channel is closed when ctx is cancelled
	updates, err := b.bot.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		slog.Error("bot: Failed to get updates channel", "error", err)
		return fmt.Errorf("failed to get updates channel: %w", err)
	}

	h, err := th.NewBotHandler(b.bot, updates)
	if err != nil {
		slog.Error("bot: Failed to create handler", "error", err)
		return fmt.Errorf("failed to create handler: %w", err)
	}

	var inflight sync.WaitGroup

	h.Use(trackInflight(&inflight))
	h.Use(th.PanicRecoveryHandler(recoverFromPanic))
	h.Use(b.logUpdate)
	h.Use(b.migrateChat) // before the others, so groups already live under the new chat ID
	h.Use(b.syncUserData)
	h.Use(b.addToAllGroup)

	b.registerHandlers(h)

	slog.Info("bot: Starting bot handlers")
	startErr := h.Start() // returns once the updates channel is closed

	if ctx.Err() != nil {
		slog.Info("bot: Shutting down", "timeout", shutdownTimeout.String())
	}

	b.drain(h, &inflight)

	return startErr
}

// drain waits for in-flight handlers (bounded) and then stops the handler loop.
func (b *Bot) drain(h *th.BotHandler, inflight *sync.WaitGroup) {
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		inflight.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-stopCtx.Done():
		slog.Warn("bot: Timed out waiting for in-flight handlers")
	}

	// Cancels the contexts of any handlers still running after the timeout
	if err := h.StopWithContext(stopCtx); err != nil {
		slog.Warn("bot: Handler stop did not complete cleanly", "error", err)
	}
}

// trackInflight counts updates being processed so shutdown can wait for them.
func trackInflight(wg *sync.WaitGroup) th.Handler {
	return func(ctx *th.Context, update t.Update) error {
		wg.Add(1)
		defer wg.Done()

		return ctx.Next(update)
	}
}

func (b *Bot) registerHandlers(h *th.BotHandler) {
	commandForMe := b.commandForThisBot()
	h.HandleMessage(b.handleHelp, th.And(commandForMe, th.CommandEqual("help")))
	h.HandleMessage(b.handleList, th.And(commandForMe, th.CommandEqual("list")))
	h.HandleMessage(b.handleMy, th.And(commandForMe, th.CommandEqual("my")))
	h.HandleMessage(b.handleNewGroup, th.And(commandForMe, th.CommandEqual("new")))
	h.HandleMessage(b.handleJoin, th.And(commandForMe, th.CommandEqual("join")))
	h.HandleMessage(b.handleLeave, th.And(commandForMe, th.CommandEqual("leave")))
	h.HandleMessage(b.handleMention, th.Or(
		th.And(commandForMe, th.CommandEqual("mention")),
		th.And(commandForMe, th.CommandEqual("m")),
		th.And(commandForMe, th.CommandEqual("call")),
	))
	h.HandleMessage(b.handleDeleteGroup, th.And(commandForMe, th.CommandEqual("del")))
	h.HandleMessage(b.handleShowGroup, th.And(commandForMe, th.CommandEqual("show")))
	h.HandleMessage(b.handleClearEmptyGroups, th.And(commandForMe, th.CommandEqual("clear")))

	h.HandleMessage(b.handleFreeFormMessage, th.Not(th.AnyCommand()))
}

func (b *Bot) handleNewGroup(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling new group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)
	if len(args) != 2 {
		slog.Debug("bot: Invalid new group command format", "args_count", len(args))
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("Usage: /new <group_name>\nGroup name can only contain lowercase letters, numbers, and dashes."), &message)
		return nil
	}

	groupName := strings.ToLower(args[1])
	if !isValidGroupName(groupName) {
		slog.Debug("bot: Invalid group name", "group_name", groupName)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("Invalid group name. Group name can only contain lowercase letters, numbers, and dashes."), &message)
		return nil
	}

	b.sendTyping(ctx, tu.ID(message.Chat.ID))
	slog.Debug("bot: Creating new group", "group_name", groupName, "chat_id", message.Chat.ID)
	err := b.storage.CreateGroup(ctx, groupName, message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to create group", "error", err,
			"group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create group: %v", err)), &message)
		return nil
	}

	slog.Info("bot: Group created", "group_name", groupName, "chat_id", message.Chat.ID)
	b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group '%s' created successfully!\nTo join this group, use: /join %s", groupName, groupName)), &message)
	return nil
}

func (b *Bot) handleJoin(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling join command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for join command, showing available groups")
		groups, err := b.storage.GetGroupsToJoinByChatAndUser(ctx, message.Chat.ID, message.From.ID)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("join", groups)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No groups available to join."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to join."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(ctx, b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Joining group", "group_name", groupName, "chat_id", message.Chat.ID, "user_id", message.From.ID)
	err := b.executeOnGroup(ctx, message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.joinGroupOperation(ctx, group, message.From, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleLeave(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling leave command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for leave command, showing available groups")
		groups, err := b.storage.GetUserGroupsByChat(ctx, message.Chat.ID, message.From.ID)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("leave", groups)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No groups available to leave."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to leave."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(ctx, b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Leaving group", "group_name", groupName, "chat_id", message.Chat.ID, "user_id", message.From.ID)
	err := b.executeOnGroup(ctx, message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.leaveGroupOperation(ctx, group, message.From.ID, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleMention(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling mention command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for mention command, showing available groups")
		groups, err := b.storage.GetGroupsByChat(ctx, message.Chat.ID)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("m", groups)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No groups available to mention."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to mention."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(ctx, b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Mentioning group", "group_name", groupName, "chat_id", message.Chat.ID)
	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(ctx, message.Chat.ID, []string{groupName})
	if err != nil {
		slog.Error("bot: Failed to find group", "error", err, "chat_id", message.Chat.ID, "group_name", groupName)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to find group: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: Group not found", "group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group '%s' not found.", groupName)), &message)
		return nil
	}

	slog.Debug("bot: Found group for mention", "group_name", groupName, "group_id", groups[0].ID, "member_count", len(groups[0].Members))
	return b.mentionGroups(ctx, groups, message.Chat.ID, &message)
}

func (b *Bot) handleDeleteGroup(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling delete group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for delete command, showing available groups")
		groups, err := b.storage.GetGroupsByChat(ctx, message.Chat.ID)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("del", groups)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No groups available to delete."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to delete."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(ctx, b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Deleting group", "group_name", groupName, "chat_id", message.Chat.ID)
	err := b.executeOnGroup(ctx, message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.deleteGroupOperation(ctx, group, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleShowGroup(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling show group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if len(args) < 2 {
		slog.Debug("bot: No group name provided for show command, showing available groups")
		groups, err := b.storage.GetGroupsByChat(ctx, message.Chat.ID)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
			return nil
		}

		keyboard, err := b.createGroupSelectionReplyKeyboard("show", groups)
		if err != nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)
			return nil
		}
		if keyboard == nil {
			b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No groups available to show."), &message)
			return nil
		}

		msg := tu.Message(tu.ID(message.Chat.ID), escapeMarkdownV2("Select a group to show."))
		msg.ReplyMarkup = keyboard
		msg.ParseMode = "MarkdownV2"
		_, err = b.bot.SendMessage(ctx, b.reply(message, msg))
		return err
	}

	groupName := args[1]
	slog.Debug("bot: Showing group", "group_name", groupName, "chat_id", message.Chat.ID)
	err := b.executeOnGroup(ctx, message.Chat.ID, groupName, &message, func(group *storage.MentionGroup, originalMessage *t.Message) error {
		return b.showGroupMembersOperation(ctx, group, message.Chat.ID, originalMessage)
	})
	return err
}

func (b *Bot) handleHelp(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling help command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	helpText := escapeMarkdownV2(`Available commands:
/new <name> - Create a mention group (lowercase letters, numbers, dashes)
/join [<name>] - Join a group or pick one from the keyboard
/leave [<name>] - Leave a group you are in
/mention <name> or /m <name> or /call <name> - Mention everyone in a group
/show <name> - List members without pinging them
/del <name> - Delete a group (only when it is empty)
/list - Show all groups in this chat
/my - Show groups you have joined in this chat
/clear - Delete all empty groups in this chat
/help - Show this help message

Parameters in [brackets] are optional.

Tips:
- Use /join or /leave without a name to get a keyboard with available groups
- Mention groups inline by typing @group or several like @devs,@qa in a message
- A group named all will automatically include everyone who speaks in the chat`)

	b.sendMessage(ctx, message.Chat.ID, helpText, &message)
	return nil
}

func (b *Bot) handleList(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling list command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	groups, err := b.storage.GetGroupsByChat(ctx, message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to get groups", "error", err, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: No groups found for chat", "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No groups found in this chat."), &message)
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
	b.sendMessage(ctx, message.Chat.ID, listText, &message)
	return nil
}

func (b *Bot) handleMy(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling my command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	groups, err := b.storage.GetUserGroupsByChat(ctx, message.Chat.ID, message.From.ID)
	if err != nil {
		slog.Error("bot: Failed to get user's groups", "error", err, "chat_id", message.Chat.ID, "user_id", message.From.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get your groups: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: No groups found for user", "chat_id", message.Chat.ID, "user_id", message.From.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("You haven't joined any groups in this chat."), &message)
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
	b.sendMessage(ctx, message.Chat.ID, listText, &message)
	return nil
}

func (b *Bot) handleClearEmptyGroups(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling clear command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	removedGroups, err := b.storage.DeleteEmptyGroups(ctx, message.Chat.ID, groupNameAll)
	if err != nil {
		slog.Error("bot: Failed to clear empty groups", "error", err, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to delete empty groups: %v", err)), &message)
		return nil
	}

	if len(removedGroups) == 0 {
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No empty groups to delete."), &message)
	} else {
		msg := "Removed empty groups:\n"
		for _, name := range removedGroups {
			msg += fmt.Sprintf("• %s\n", name)
		}
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(msg), &message)
	}
	return nil
}

func (b *Bot) handleFreeFormMessage(ctx *th.Context, message t.Message) error {
	slog.Debug("bot: Handling free form message", "chat_id", message.Chat.ID, "from_user_id", message.From.ID, "text_length", len(message.Text))

	if !strings.Contains(message.Text, "@") {
		slog.Debug("bot: Message does not contain @ mentions, skipping", "chat_id", message.Chat.ID)
		return nil
	}

	var groupNames []string

	const punct = ".,!?;:)]}'\"…—/([{"

	words := strings.Fields(message.Text)
	for _, word := range words {
		trimmed := strings.TrimLeft(word, punct)
		trimmed = strings.TrimRight(trimmed, punct)
		if strings.HasPrefix(trimmed, "@") {
			name := strings.TrimPrefix(trimmed, "@")
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

	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(ctx, message.Chat.ID, groupNames)
	if err != nil {
		slog.Error("bot: Failed to find groups", "error", err, "chat_id", message.Chat.ID)
		return nil
	}

	if len(groups) == 0 {
		slog.Debug("bot: No groups found for mentions", "chat_id", message.Chat.ID, "group_names", groupNames)
		return nil
	}

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	slog.Debug("bot: Found groups for mentions", "chat_id", message.Chat.ID, "group_count", len(groups))
	err = b.mentionGroups(ctx, groups, message.Chat.ID, &message)
	if err != nil {
		slog.Error("bot: Failed to mention group members", "error", err, "chat_id", message.Chat.ID)
	}

	return nil
}
