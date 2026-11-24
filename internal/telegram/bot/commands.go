package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"telegram-group-mention-bot/internal/i18n"
	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

type commandHandler func(ctx context.Context, message t.Message) error

// command describes one bot command. The table returned by Bot.commands drives
// handler registration and the /help text.
type command struct {
	names   []string // first is canonical; the rest are aliases
	usage   string   // as shown in /help, e.g. "/join [<name>]"; the description is the i18n key "cmd.<first name>"
	handler commandHandler
	menu    []string // names shown in the Telegram command menu; defaults to the first name
	hidden  bool     // left out of /help and the command menu
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
	key    string // command name and callback action, e.g. "join"
	source groupSource
	// namedNotFound makes a missing group answer "Group '<name>' not found." instead of "Group not found."
	namedNotFound bool
	// run performs the action for actor. origin (may be nil) is the message a post should reply to;
	// rest is the text after the group name in the command (empty for buttons).
	run func(ctx context.Context, group *storage.MentionGroup, actor *t.User, origin *t.Message, rest string) outcome
}

// groupActions returns the actions that take a group, keyed by name (also the callback action).
func (b *Bot) groupActions() map[string]groupAction {
	actions := []groupAction{
		{key: "join", source: sourceJoinable,
			run: func(ctx context.Context, g *storage.MentionGroup, actor *t.User, _ *t.Message, _ string) outcome {
				return b.joinGroup(ctx, g, actor, g.ChatID)
			}},
		{key: "leave", source: sourceMine,
			run: func(ctx context.Context, g *storage.MentionGroup, actor *t.User, _ *t.Message, _ string) outcome {
				return b.leaveGroup(ctx, g, actor, g.ChatID)
			}},
		{key: "m", source: sourceAll, namedNotFound: true,
			run: func(ctx context.Context, g *storage.MentionGroup, actor *t.User, origin *t.Message, rest string) outcome {
				b.mentionGroups(ctx, []storage.MentionGroup{*g}, g.ChatID, replyTarget(origin), actor, rest)

				return outcome{silent: true}
			}},
		{key: "del", source: sourceAll,
			run: func(ctx context.Context, g *storage.MentionGroup, actor *t.User, _ *t.Message, _ string) outcome {
				return b.deleteGroup(ctx, langOf(actor), g, g.ChatID)
			}},
		{key: "show", source: sourceAll,
			run: func(_ context.Context, g *storage.MentionGroup, actor *t.User, _ *t.Message, _ string) outcome {
				return b.showGroupMembers(langOf(actor), g)
			}},
	}

	byKey := make(map[string]groupAction, len(actions))
	for _, action := range actions {
		byKey[action.key] = action
	}

	return byKey
}

func (b *Bot) commands() []command {
	actions := b.groupActions()
	join := b.groupCommand(actions["join"])
	leave := b.groupCommand(actions["leave"])
	mention := b.groupCommand(actions["m"])
	plainDel := b.groupCommand(actions["del"])
	del := func(ctx context.Context, message t.Message) error {
		if args := strings.Fields(message.Text); forceDeleteRequested(args) {
			return b.handleForceDelete(ctx, message, args[1])
		}

		return plainDel(ctx, message)
	}
	show := b.groupCommand(actions["show"])

	cmds := []command{
		{names: []string{"new"}, usage: "/new <name>", handler: b.handleNewGroup},
		{names: []string{"join"}, usage: "/join [<name>]", handler: join},
		{names: []string{"leave"}, usage: "/leave [<name>]", handler: leave},
		{names: []string{"mention", "m", "call"}, usage: "/mention <name> [text] or /m <name> [text] or /call <name> [text]", handler: mention, menu: []string{"mention", "call"}},
		{names: []string{"show"}, usage: "/show <name>", handler: show},
		{names: []string{"del"}, usage: "/del <name> [force]", handler: del},
		{names: []string{"list"}, usage: "/list", handler: b.handleList},
		{names: []string{"my"}, usage: "/my", handler: b.handleMy},
		{names: []string{"clear"}, usage: "/clear", handler: b.handleClearEmptyGroups},
		{names: []string{"add"}, usage: "/add <group>", handler: b.handleAdminMember(true)},
		{names: []string{"remove"}, usage: "/remove <group>", handler: b.handleAdminMember(false)},
		{names: []string{"rename"}, usage: "/rename <old> <new>", handler: b.handleRename},
		{names: []string{"alias"}, usage: "/alias <group> <alias>", handler: b.handleAlias},
		{names: []string{"unalias"}, usage: "/unalias <alias>", handler: b.handleUnalias},
		{names: []string{"help"}, usage: "/help", handler: b.handleHelp},
	}

	if len(b.owners) > 0 {
		// Hidden: not in /help and not in the command menu
		cmds = append(cmds, command{names: []string{"stats"}, usage: "/stats", handler: b.handleStats, hidden: true})
	}

	return cmds
}

// helpText renders /help from the command table in the given language.
func helpText(lang i18n.Lang, commands []command) string {
	var sb strings.Builder

	sb.WriteString(tr(lang, "help.header") + "\n")

	for _, cmd := range commands {
		if cmd.hidden {
			continue
		}

		sb.WriteString(cmd.usage + " - " + tr(lang, "cmd."+cmd.names[0]) + "\n")
	}

	sb.WriteString("\n" + tr(lang, "help.footer"))

	return sb.String()
}

// groupCommand builds the handler shared by every command that takes a group name:
// with a name it runs the action on that group, without one it offers a picker.
func (b *Bot) groupCommand(action groupAction) commandHandler {
	return func(ctx context.Context, message t.Message) error {
		slog.Debug("bot: Handling group command", "command", action.key, "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

		b.sendTyping(ctx, tu.ID(message.Chat.ID))

		args := strings.Fields(message.Text)
		if len(args) < 2 {
			return b.showGroupPicker(ctx, message, action)
		}

		groupName := args[1]

		return b.executeOnGroup(ctx, message.Chat.ID, groupName, &message, action.namedNotFound,
			func(group *storage.MentionGroup, original *t.Message) error {
				out := action.run(ctx, group, message.From, original, restAfterFields(message.Text, 2))
				if !out.silent {
					b.respond(ctx, original, out)
				}

				return nil
			})
	}
}

// restAfterFields returns the text after the first n whitespace-separated fields, trimmed.
func restAfterFields(text string, n int) string {
	rest := text

	for range n {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)

		end := strings.IndexFunc(rest, unicode.IsSpace)
		if end < 0 {
			return ""
		}

		rest = rest[end:]
	}

	return strings.TrimSpace(rest)
}

func (b *Bot) groupsFor(ctx context.Context, chatID, userID int64, source groupSource) ([]storage.MentionGroup, error) {
	switch source {
	case sourceJoinable:
		return b.storage.GetGroupsToJoinByChatAndUser(ctx, chatID, userID)
	case sourceMine:
		return b.storage.GetUserGroupsByChat(ctx, chatID, userID)
	default:
		return b.storage.GetGroupsByChat(ctx, chatID)
	}
}

func (b *Bot) showGroupPicker(ctx context.Context, message t.Message, action groupAction) error {
	chatID := message.Chat.ID
	lang := langOf(message.From)

	groups, err := b.groupsFor(ctx, chatID, message.From.ID, action.source)
	if err != nil {
		logError("bot: Failed to get groups for picker", err, "chat_id", chatID)
		b.sendMessage(ctx, chatID, md(lang, "error.groups"), &message)

		return nil
	}

	keyboard := groupPickerKeyboard(lang, action.key, groups, 0, message.From.ID)
	if keyboard == nil {
		b.sendMessage(ctx, chatID, md(lang, "picker.none."+action.key), &message)

		return nil
	}

	b.sendMessage(ctx, chatID, md(lang, "picker.select."+action.key), &message, keyboard)

	return nil
}

func (b *Bot) handleNewGroup(ctx context.Context, message t.Message) error {
	slog.Debug("bot: Handling new group command", "chat_id", message.Chat.ID, "from_user_id", message.From.ID)

	lang := langOf(message.From)

	args := strings.Fields(message.Text)
	if len(args) != 2 {
		b.sendMessage(ctx, message.Chat.ID, md(lang, "new.usage"), &message)

		return nil
	}

	groupName := storage.NormalizeName(args[1])
	if !isValidGroupName(groupName) {
		b.sendMessage(ctx, message.Chat.ID, md(lang, "name.invalid"), &message)

		return nil
	}

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	if err := b.storage.CreateGroup(ctx, groupName, message.Chat.ID); err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			b.sendMessage(ctx, message.Chat.ID, md(lang, "name.taken", groupName), &message)

			return nil
		}

		logError("bot: Failed to create group", err, "group_name", groupName, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, md(lang, "new.failed"), &message)

		return nil
	}

	slog.Info("bot: Group created", "group_name", groupName, "chat_id", message.Chat.ID)
	b.respond(ctx, &message, outcome{text: tr(lang, "new.created", groupName, groupName), ok: true})

	return nil
}

func (b *Bot) handleHelp(ctx context.Context, message t.Message) error {
	b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(helpText(langOf(message.From), b.commands())), &message)

	return nil
}

func (b *Bot) handleList(ctx context.Context, message t.Message) error {
	lang := langOf(message.From)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	groups, err := b.storage.GetGroupsByChat(ctx, message.Chat.ID)
	if err != nil {
		logError("bot: Failed to get groups", err, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, md(lang, "error.groups"), &message)

		return nil
	}

	if len(groups) == 0 {
		b.sendMessage(ctx, message.Chat.ID, md(lang, "list.none"), &message)

		return nil
	}

	aliases, err := b.storage.GetAliasesByChat(ctx, message.Chat.ID)
	if err != nil {
		logError("bot: Failed to get aliases", err, "chat_id", message.Chat.ID)
	}

	counts := b.memberCounts(ctx, message.Chat.ID)

	b.sendMessage(ctx, message.Chat.ID, formatGroupList(lang, tr(lang, "list.header"), groups, aliases, counts), &message)

	return nil
}

func (b *Bot) handleMy(ctx context.Context, message t.Message) error {
	lang := langOf(message.From)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	groups, err := b.storage.GetUserGroupsByChat(ctx, message.Chat.ID, message.From.ID)
	if err != nil {
		logError("bot: Failed to get user's groups", err, "chat_id", message.Chat.ID, "user_id", message.From.ID)
		b.sendMessage(ctx, message.Chat.ID, md(lang, "error.groups"), &message)

		return nil
	}

	if len(groups) == 0 {
		b.sendMessage(ctx, message.Chat.ID, md(lang, "my.none"), &message)

		return nil
	}

	b.sendMessage(ctx, message.Chat.ID, formatGroupList(lang, tr(lang, "my.header"), groups, nil, b.memberCounts(ctx, message.Chat.ID)), &message)

	return nil
}

// memberCounts returns member counts per group; on failure it logs and returns nil (lists then omit counts).
func (b *Bot) memberCounts(ctx context.Context, chatID int64) map[uint]int {
	counts, err := b.storage.GetGroupMemberCounts(ctx, chatID)
	if err != nil {
		logError("bot: Failed to count group members", err, "chat_id", chatID)

		return nil
	}

	return counts
}

// formatGroupList renders a MarkdownV2 bullet list of group names under a header, each with its
// member count (when known) and aliases (if any).
func formatGroupList(lang i18n.Lang, header string, groups []storage.MentionGroup, aliases map[uint][]string, counts map[uint]int) string {
	var sb strings.Builder

	sb.WriteString(escapeMarkdownV2(header))

	for _, group := range groups {
		var details []string

		if counts != nil {
			details = append(details, i18n.Plural(lang, "members", counts[group.ID]))
		}

		if names := aliases[group.ID]; len(names) > 0 {
			details = append(details, tr(lang, "list.aliases", strings.Join(names, ", ")))
		}

		line := group.Name
		if len(details) > 0 {
			line += " (" + strings.Join(details, "; ") + ")"
		}

		fmt.Fprintf(&sb, "• %s\n", escapeMarkdownV2(line))
	}

	return sb.String()
}

func (b *Bot) handleClearEmptyGroups(ctx context.Context, message t.Message) error {
	lang := langOf(message.From)

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	removedGroups, err := b.storage.DeleteEmptyGroups(ctx, message.Chat.ID, groupNameAll)
	if err != nil {
		logError("bot: Failed to clear empty groups", err, "chat_id", message.Chat.ID)
		b.sendMessage(ctx, message.Chat.ID, md(lang, "clear.failed"), &message)

		return nil
	}

	if len(removedGroups) == 0 {
		b.sendMessage(ctx, message.Chat.ID, md(lang, "clear.none"), &message)

		return nil
	}

	msg := tr(lang, "clear.header")
	for _, name := range removedGroups {
		msg += fmt.Sprintf("• %s\n", name)
	}

	b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(msg), &message)

	return nil
}

// reservedGroupName is the group the bot manages itself; it can be neither renamed nor aliased.
const reservedGroupName = groupNameAll

func (b *Bot) handleRename(ctx context.Context, message t.Message) error {
	chatID := message.Chat.ID
	lang := langOf(message.From)
	args := strings.Fields(message.Text)

	if len(args) != 3 {
		b.sendMessage(ctx, chatID, md(lang, "rename.usage"), &message)

		return nil
	}

	oldName, newName := storage.NormalizeName(args[1]), storage.NormalizeName(args[2])
	if !isValidGroupName(newName) {
		b.sendMessage(ctx, chatID, md(lang, "name.invalid"), &message)

		return nil
	}

	if oldName == reservedGroupName || newName == reservedGroupName {
		b.sendMessage(ctx, chatID, md(lang, "rename.reserved", reservedGroupName), &message)

		return nil
	}

	switch err := b.storage.RenameGroup(ctx, chatID, oldName, newName); {
	case err == nil:
		slog.Info("bot: Group renamed", "chat_id", chatID, "from_user_id", message.From.ID)
		b.sendMessage(ctx, chatID, md(lang, "rename.done", oldName, newName), &message)
	case errors.Is(err, storage.ErrNotFound):
		b.sendMessage(ctx, chatID, md(lang, "group.not_found", oldName), &message)
	case errors.Is(err, storage.ErrAlreadyExists):
		b.sendMessage(ctx, chatID, md(lang, "name.taken", newName), &message)
	default:
		b.sendMessage(ctx, chatID, md(lang, "rename.failed"), &message)
	}

	return nil
}

func (b *Bot) handleAlias(ctx context.Context, message t.Message) error {
	chatID := message.Chat.ID
	lang := langOf(message.From)
	args := strings.Fields(message.Text)

	if len(args) != 3 {
		b.sendMessage(ctx, chatID, md(lang, "alias.usage"), &message)

		return nil
	}

	groupName, alias := storage.NormalizeName(args[1]), storage.NormalizeName(args[2])
	if !isValidGroupName(alias) {
		b.sendMessage(ctx, chatID, md(lang, "alias.invalid"), &message)

		return nil
	}

	if alias == reservedGroupName {
		b.sendMessage(ctx, chatID, md(lang, "alias.reserved", reservedGroupName), &message)

		return nil
	}

	switch err := b.storage.AddAlias(ctx, chatID, groupName, alias); {
	case err == nil:
		slog.Info("bot: Alias added", "chat_id", chatID, "from_user_id", message.From.ID)
		b.sendMessage(ctx, chatID, md(lang, "alias.added", alias, groupName), &message)
	case errors.Is(err, storage.ErrNotFound):
		b.sendMessage(ctx, chatID, md(lang, "group.not_found", groupName), &message)
	case errors.Is(err, storage.ErrAlreadyExists):
		b.sendMessage(ctx, chatID, md(lang, "name.taken", alias), &message)
	default:
		b.sendMessage(ctx, chatID, md(lang, "alias.failed"), &message)
	}

	return nil
}

func (b *Bot) handleUnalias(ctx context.Context, message t.Message) error {
	chatID := message.Chat.ID
	lang := langOf(message.From)
	args := strings.Fields(message.Text)

	if len(args) != 2 {
		b.sendMessage(ctx, chatID, md(lang, "unalias.usage"), &message)

		return nil
	}

	alias := storage.NormalizeName(args[1])

	removed, err := b.storage.RemoveAlias(ctx, chatID, alias)

	switch {
	case err != nil:
		b.sendMessage(ctx, chatID, md(lang, "unalias.failed"), &message)
	case !removed:
		b.sendMessage(ctx, chatID, md(lang, "unalias.none", alias), &message)
	default:
		b.sendMessage(ctx, chatID, md(lang, "unalias.done", alias), &message)
	}

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
		if name, ok := strings.CutPrefix(trimmed, "@"); ok {
			name = storage.NormalizeName(name)
			if isValidGroupName(name) {
				names = append(names, name)
			}
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
		logError("bot: Failed to find groups", err, "chat_id", message.Chat.ID)

		return nil
	}

	if len(groups) == 0 {
		return nil
	}

	b.sendTyping(ctx, tu.ID(message.Chat.ID))

	b.mentionGroups(ctx, groups, message.Chat.ID, &message, message.From, "")

	return nil
}
