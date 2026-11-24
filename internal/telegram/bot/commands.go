package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

type commandHandler func(ctx context.Context, message t.Message) error

// command describes one bot command. The table returned by Bot.commands drives
// handler registration and the /help text.
type command struct {
	names   []string // first is canonical; the rest are aliases
	usage   string   // as shown in /help, e.g. "/join [<name>]"
	help    string   // one-line description
	handler commandHandler
}

// groupSource selects which groups the picker offers when a command is used without a name.
type groupSource int

const (
	sourceAll      groupSource = iota // every group in the chat
	sourceJoinable                    // groups the user has not joined yet
	sourceMine                        // groups the user has joined
)

// groupAction describes a command that works on one group chosen by name or from a picker.
type groupAction struct {
	keyboardCommand string // command text put on picker buttons, e.g. "join"
	verb            string // used in "Select a group to <verb>." and "No groups available to <verb>."
	source          groupSource
	notFound        string // reply when the named group does not exist; %s is the name
	run             func(ctx context.Context, group *storage.MentionGroup, message *t.Message) error
}

func (b *Bot) commands() []command {
	join := b.groupCommand(groupAction{
		keyboardCommand: "join", verb: "join", source: sourceJoinable,
		run: func(ctx context.Context, group *storage.MentionGroup, m *t.Message) error {
			return b.joinGroupOperation(ctx, group, m.From, m.Chat.ID, m)
		},
	})
	leave := b.groupCommand(groupAction{
		keyboardCommand: "leave", verb: "leave", source: sourceMine,
		run: func(ctx context.Context, group *storage.MentionGroup, m *t.Message) error {
			return b.leaveGroupOperation(ctx, group, m.From.ID, m.Chat.ID, m)
		},
	})
	mention := b.groupCommand(groupAction{
		keyboardCommand: "m", verb: "mention", source: sourceAll, notFound: "Group '%s' not found.",
		run: func(ctx context.Context, group *storage.MentionGroup, m *t.Message) error {
			return b.mentionGroups(ctx, []storage.MentionGroup{*group}, m.Chat.ID, m)
		},
	})
	del := b.groupCommand(groupAction{
		keyboardCommand: "del", verb: "delete", source: sourceAll,
		run: func(ctx context.Context, group *storage.MentionGroup, m *t.Message) error {
			return b.deleteGroupOperation(ctx, group, m.Chat.ID, m)
		},
	})
	show := b.groupCommand(groupAction{
		keyboardCommand: "show", verb: "show", source: sourceAll,
		run: func(ctx context.Context, group *storage.MentionGroup, m *t.Message) error {
			return b.showGroupMembersOperation(ctx, group, m.Chat.ID, m)
		},
	})

	return []command{
		{[]string{"new"}, "/new <name>", "Create a mention group (lowercase letters, numbers, dashes)", b.handleNewGroup},
		{[]string{"join"}, "/join [<name>]", "Join a group or pick one from the keyboard", join},
		{[]string{"leave"}, "/leave [<name>]", "Leave a group you are in", leave},
		{[]string{"mention", "m", "call"}, "/mention <name> or /m <name> or /call <name>", "Mention everyone in a group", mention},
		{[]string{"show"}, "/show <name>", "List members without pinging them", show},
		{[]string{"del"}, "/del <name>", "Delete a group (only when it is empty)", del},
		{[]string{"list"}, "/list", "Show all groups in this chat", b.handleList},
		{[]string{"my"}, "/my", "Show groups you have joined in this chat", b.handleMy},
		{[]string{"clear"}, "/clear", "Delete all empty groups in this chat", b.handleClearEmptyGroups},
		{[]string{"help"}, "/help", "Show this help message", b.handleHelp},
	}
}

// helpText renders /help from the command table.
func helpText(commands []command) string {
	var sb strings.Builder

	sb.WriteString("Available commands:\n")

	for _, cmd := range commands {
		sb.WriteString(cmd.usage + " - " + cmd.help + "\n")
	}

	sb.WriteString(`
Parameters in [brackets] are optional.

Tips:
- Use /join or /leave without a name to get a keyboard with available groups
- Mention groups inline by typing @group or several like @devs,@qa in a message
- A group named all will automatically include everyone who speaks in the chat`)

	return sb.String()
}

// groupCommand builds the handler shared by every command that takes a group name:
// with a name it runs the action on that group, without one it offers a picker.
func (b *Bot) groupCommand(action groupAction) commandHandler {
	return func(ctx context.Context, message t.Message) error {
		slog.Debug("bot: Handling group command", "command", action.keyboardCommand, "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

		b.sendTyping(ctx, tu.ID(message.Chat.ID))

		args := strings.Fields(message.Text)
		if len(args) < 2 {
			return b.showGroupPicker(ctx, message, action)
		}

		groupName := args[1]

		return b.executeOnGroup(ctx, message.Chat.ID, groupName, &message, action.notFound,
			func(group *storage.MentionGroup, original *t.Message) error {
				return action.run(ctx, group, original)
			})
	}
}

func (b *Bot) groupsFor(ctx context.Context, message t.Message, source groupSource) ([]storage.MentionGroup, error) {
	switch source {
	case sourceJoinable:
		return b.storage.GetGroupsToJoinByChatAndUser(ctx, message.Chat.ID, message.From.ID)
	case sourceMine:
		return b.storage.GetUserGroupsByChat(ctx, message.Chat.ID, message.From.ID)
	default:
		return b.storage.GetGroupsByChat(ctx, message.Chat.ID)
	}
}

func (b *Bot) showGroupPicker(ctx context.Context, message t.Message, action groupAction) error {
	chatID := message.Chat.ID

	groups, err := b.groupsFor(ctx, message, action.source)
	if err != nil {
		slog.Error("bot: Failed to get groups for picker", "error", err, "chat_id", chatID)
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)

		return nil
	}

	keyboard, err := b.createGroupSelectionReplyKeyboard(action.keyboardCommand, groups)
	if err != nil {
		b.sendMessage(ctx, chatID, escapeMarkdownV2(fmt.Sprintf("Failed to create keyboard: %v", err)), &message)

		return nil
	}

	if keyboard == nil {
		b.sendMessage(ctx, chatID, escapeMarkdownV2("No groups available to "+action.verb+"."), &message)

		return nil
	}

	msg := tu.Message(tu.ID(chatID), escapeMarkdownV2("Select a group to "+action.verb+"."))
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "MarkdownV2"
	_, err = b.api.SendMessage(ctx, b.reply(message, msg))

	return err
}

func (b *Bot) handleNewGroup(ctx context.Context, message t.Message) error {
	slog.Debug("bot: Handling new group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("Usage: /new <group_name>\nGroup name can only contain lowercase letters, numbers, and dashes."), &message)
		return nil
	}

	groupName := strings.ToLower(args[1])
	if !isValidGroupName(groupName) {
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("Invalid group name. Group name can only contain lowercase letters, numbers, and dashes."), &message)
		return nil
	}

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if err := b.storage.CreateGroup(ctx, groupName, message.Chat.ID); err != nil {
		slog.Error("bot: Failed to create group", "error", err, "group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to create group: %v", err)), &message)
		return nil
	}

	slog.Info("bot: Group created", "group_name", groupName, "chat_id", message.Chat.ID)
	b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Group '%s' created successfully!\nTo join this group, use: /join %s", groupName, groupName)), &message)

	return nil
}

func (b *Bot) handleHelp(ctx context.Context, message t.Message) error {
	b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(helpText(b.commands())), &message)

	return nil
}

func (b *Bot) handleList(ctx context.Context, message t.Message) error {
	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	groups, err := b.storage.GetGroupsByChat(ctx, message.Chat.ID)
	if err != nil {
		slog.Error("bot: Failed to get groups", "error", err, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get groups: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No groups found in this chat."), &message)
		return nil
	}

	b.sendMessage(ctx, message.Chat.ID, formatGroupList("Groups in this chat:\n", groups), &message)

	return nil
}

func (b *Bot) handleMy(ctx context.Context, message t.Message) error {
	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	groups, err := b.storage.GetUserGroupsByChat(ctx, message.Chat.ID, message.From.ID)
	if err != nil {
		slog.Error("bot: Failed to get user's groups", "error", err, "chat_id", message.Chat.ID, "user_id", message.From.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to get your groups: %v", err)), &message)
		return nil
	}

	if len(groups) == 0 {
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("You haven't joined any groups in this chat."), &message)
		return nil
	}

	b.sendMessage(ctx, message.Chat.ID, formatGroupList("Groups you've joined in this chat:\n", groups), &message)

	return nil
}

// formatGroupList renders a MarkdownV2 bullet list of group names under a header.
func formatGroupList(header string, groups []storage.MentionGroup) string {
	var sb strings.Builder

	sb.WriteString(header)

	for _, group := range groups {
		sb.WriteString(fmt.Sprintf("• %s\n", escapeMarkdownV2(group.Name)))
	}

	return sb.String()
}

func (b *Bot) handleClearEmptyGroups(ctx context.Context, message t.Message) error {
	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	removedGroups, err := b.storage.DeleteEmptyGroups(ctx, message.Chat.ID, groupNameAll)
	if err != nil {
		slog.Error("bot: Failed to clear empty groups", "error", err, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(fmt.Sprintf("Failed to delete empty groups: %v", err)), &message)
		return nil
	}

	if len(removedGroups) == 0 {
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("No empty groups to delete."), &message)
		return nil
	}

	msg := "Removed empty groups:\n"
	for _, name := range removedGroups {
		msg += fmt.Sprintf("• %s\n", name)
	}

	b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(msg), &message)

	return nil
}

// groupNamesFromText extracts valid @group names from free-form message text.
func groupNamesFromText(text string) []string {
	if !strings.Contains(text, "@") {
		return nil
	}

	const punct = ".,!?;:)]}'\"…—/([{"

	var names []string

	for _, word := range strings.Fields(text) {
		trimmed := strings.TrimRight(strings.TrimLeft(word, punct), punct)
		if name, ok := strings.CutPrefix(trimmed, "@"); ok && isValidGroupName(name) {
			names = append(names, name)
		}
	}

	return names
}

func (b *Bot) handleFreeFormMessage(ctx context.Context, message t.Message) error {
	groupNames := groupNamesFromText(message.Text)
	if len(groupNames) == 0 {
		return nil
	}

	groups, err := b.storage.FindGroupsByChatAndNamesWithMembers(ctx, message.Chat.ID, groupNames)
	if err != nil {
		slog.Error("bot: Failed to find groups", "error", err, "chat_id", message.Chat.ID)
		return nil
	}

	if len(groups) == 0 {
		return nil
	}

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if err := b.mentionGroups(ctx, groups, message.Chat.ID, &message); err != nil {
		slog.Error("bot: Failed to mention group members", "error", err, "chat_id", message.Chat.ID)
	}

	return nil
}
