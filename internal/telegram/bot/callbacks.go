package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// Callback data is "v1:<action>:<group id>:<requester id>" or "v1:pg:<action>:<page>:<requester id>".
// A requester of 0 means anyone may press the button (Join/Leave under mention posts).
const (
	callbackVersion = "v1"
	callbackPrefix  = callbackVersion + ":"
	pageToken       = "pg"
	pageSize        = 20 // groups per picker page (two columns)
	maxPostButtons  = 8  // groups that get Join/Leave buttons under one mention post
)

type callbackData struct {
	paging    bool
	action    string
	groupID   uint
	page      int
	requester int64
}

func (d callbackData) String() string {
	if d.paging {
		return fmt.Sprintf("%s:%s:%s:%d:%d", callbackVersion, pageToken, d.action, d.page, d.requester)
	}

	return fmt.Sprintf("%s:%s:%d:%d", callbackVersion, d.action, d.groupID, d.requester)
}

var errBadCallback = errors.New("invalid callback data")

// pickerActions lists the actions a callback may trigger; anything else is rejected.
var pickerActions = map[string]bool{"join": true, "leave": true, "m": true, "del": true, "show": true}

// confirmActions are the Delete (df) and Cancel (dx) buttons of a force-delete confirmation.
var confirmActions = map[string]bool{"df": true, "dx": true}

// parseCallback validates callback data (version, action whitelist, numeric fields).
func parseCallback(raw string) (callbackData, error) {
	parts := strings.Split(raw, ":")
	if len(parts) < 4 || parts[0] != callbackVersion {
		return callbackData{}, errBadCallback
	}

	if parts[1] == pageToken {
		if len(parts) != 5 || !pickerActions[parts[2]] {
			return callbackData{}, errBadCallback
		}

		page, err1 := strconv.Atoi(parts[3])
		requester, err2 := strconv.ParseInt(parts[4], 10, 64)

		if err1 != nil || err2 != nil || page < 0 || requester <= 0 {
			return callbackData{}, errBadCallback
		}

		return callbackData{paging: true, action: parts[2], page: page, requester: requester}, nil
	}

	if len(parts) != 4 || !(pickerActions[parts[1]] || confirmActions[parts[1]]) {
		return callbackData{}, errBadCallback
	}

	id, err1 := strconv.ParseUint(parts[2], 10, 32)
	requester, err2 := strconv.ParseInt(parts[3], 10, 64)

	if err1 != nil || err2 != nil || requester < 0 {
		return callbackData{}, errBadCallback
	}

	// Only Join/Leave buttons under mention posts are open to everyone
	if requester == 0 && parts[1] != "join" && parts[1] != "leave" {
		return callbackData{}, errBadCallback
	}

	return callbackData{action: parts[1], groupID: uint(id), requester: requester}, nil
}

// groupPickerKeyboard builds the inline picker (two columns, paged) for an action.
func groupPickerKeyboard(action string, groups []storage.MentionGroup, page int, requester int64) *t.InlineKeyboardMarkup {
	if len(groups) == 0 {
		return nil
	}

	pages := (len(groups) + pageSize - 1) / pageSize
	page = max(0, min(page, pages-1))

	visible := groups[page*pageSize : min(len(groups), (page+1)*pageSize)]

	buttons := make([]t.InlineKeyboardButton, 0, len(visible))
	for _, group := range visible {
		data := callbackData{action: action, groupID: group.ID, requester: requester}
		buttons = append(buttons, tu.InlineKeyboardButton(group.Name).WithCallbackData(data.String()))
	}

	rows := tu.InlineKeyboardCols(2, buttons...)

	if pages > 1 {
		var nav []t.InlineKeyboardButton

		if page > 0 {
			data := callbackData{paging: true, action: action, page: page - 1, requester: requester}
			nav = append(nav, tu.InlineKeyboardButton("‹ Prev").WithCallbackData(data.String()))
		}

		if page < pages-1 {
			data := callbackData{paging: true, action: action, page: page + 1, requester: requester}
			nav = append(nav, tu.InlineKeyboardButton("Next ›").WithCallbackData(data.String()))
		}

		rows = append(rows, nav)
	}

	return tu.InlineKeyboardGrid(rows)
}

// joinLeaveKeyboard builds the Join/Leave buttons shown under a mention post, or nil.
func joinLeaveKeyboard(groups []storage.MentionGroup) *t.InlineKeyboardMarkup {
	if len(groups) == 0 {
		return nil
	}

	groups = groups[:min(len(groups), maxPostButtons)]

	rows := make([][]t.InlineKeyboardButton, 0, len(groups))
	for _, group := range groups {
		join := callbackData{action: "join", groupID: group.ID}
		leave := callbackData{action: "leave", groupID: group.ID}
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("Join "+group.Name).WithCallbackData(join.String()),
			tu.InlineKeyboardButton("Leave "+group.Name).WithCallbackData(leave.String()),
		))
	}

	return tu.InlineKeyboardGrid(rows)
}

// toastText shortens plain outcome text to Telegram's 200-character toast limit.
func toastText(o outcome) string {
	if o.md || o.silent {
		return ""
	}

	runes := []rune(o.text)
	if len(runes) > 200 {
		return string(runes[:199]) + "…"
	}

	return o.text
}

func (b *Bot) answerCallback(ctx context.Context, id, text string) {
	err := b.api.AnswerCallbackQuery(ctx, &t.AnswerCallbackQueryParams{CallbackQueryID: id, Text: text})
	if err != nil {
		slog.Debug("bot: Failed to answer callback query", "error", err)
	}
}

// editMessage replaces a bot message's text and removes its inline keyboard.
func (b *Bot) editMessage(ctx context.Context, msg *t.Message, markdown string) {
	_, err := b.api.EditMessageText(ctx, &t.EditMessageTextParams{
		ChatID:             tu.ID(msg.Chat.ID),
		MessageID:          msg.MessageID,
		Text:               markdown,
		ParseMode:          t.ModeMarkdownV2,
		LinkPreviewOptions: &t.LinkPreviewOptions{IsDisabled: true},
	})
	if err != nil {
		slog.Warn("bot: Failed to edit message", "error", err, "chat_id", msg.Chat.ID)
	}
}

// handleCallback processes inline keyboard presses: group pickers and Join/Leave buttons.
func (b *Bot) handleCallback(ctx context.Context, query t.CallbackQuery) error {
	data, err := parseCallback(query.Data)
	if err != nil {
		slog.Debug("bot: Rejected callback data", "from_id", query.From.ID)
		b.answerCallback(ctx, query.ID, "Invalid button.")

		return nil
	}

	if query.Message == nil || !query.Message.IsAccessible() {
		b.answerCallback(ctx, query.ID, "This menu has expired.")

		return nil
	}

	msg := query.Message.Message()
	isPicker := data.requester != 0

	if isPicker && data.requester != query.From.ID {
		b.answerCallback(ctx, query.ID, "This menu is not for you.")

		return nil
	}

	if confirmActions[data.action] {
		b.handleForceDeleteCallback(ctx, query, msg, data)

		return nil
	}

	action := b.groupActions()[data.action]

	if data.paging {
		return b.handlePickerPage(ctx, query, msg, action, data)
	}

	group, err := b.storage.GetGroupByID(ctx, data.groupID)

	switch {
	case errors.Is(err, storage.ErrNotFound) || (err == nil && group.ChatID != msg.Chat.ID):
		b.answerCallback(ctx, query.ID, "This group no longer exists.")

		if isPicker {
			b.editMessage(ctx, msg, escapeMarkdownV2("This group no longer exists."))
		}

		return nil
	case err != nil:
		b.answerCallback(ctx, query.ID, "Something went wrong. Please try again.")

		return nil
	}

	// Joining needs the user row (foreign key); messages create it in middleware, callbacks here
	if _, err := b.storage.CreateOrUpdateUser(ctx, query.From.ID, query.From.Username, query.From.FirstName, query.From.LastName); err != nil {
		slog.Error("bot: Failed to sync user for callback", "error", err, "user_id", query.From.ID)
		b.answerCallback(ctx, query.ID, "Something went wrong. Please try again.")

		return nil
	}

	out := action.run(ctx, group, &query.From, msg.ReplyToMessage)
	b.answerCallback(ctx, query.ID, toastText(out))

	switch {
	case !isPicker:
		// buttons under mention posts stay; the toast is the feedback
	case out.silent:
		// the action posted its own message (mention); the picker is no longer needed
		if err := b.api.DeleteMessage(ctx, &t.DeleteMessageParams{ChatID: tu.ID(msg.Chat.ID), MessageID: msg.MessageID}); err != nil {
			slog.Debug("bot: Failed to delete picker message", "error", err)
		}
	default:
		b.editMessage(ctx, msg, out.render())
	}

	return nil
}

// handlePickerPage switches a picker to another page.
func (b *Bot) handlePickerPage(ctx context.Context, query t.CallbackQuery, msg *t.Message, action groupAction, data callbackData) error {
	groups, err := b.groupsFor(ctx, msg.Chat.ID, query.From.ID, action.source)
	if err != nil {
		b.answerCallback(ctx, query.ID, "Something went wrong. Please try again.")

		return nil
	}

	keyboard := groupPickerKeyboard(action.key, groups, data.page, query.From.ID)
	if keyboard == nil {
		b.answerCallback(ctx, query.ID, "No groups available to "+action.verb+".")
		b.editMessage(ctx, msg, escapeMarkdownV2("No groups available to "+action.verb+"."))

		return nil
	}

	_, err = b.api.EditMessageReplyMarkup(ctx, &t.EditMessageReplyMarkupParams{
		ChatID:      tu.ID(msg.Chat.ID),
		MessageID:   msg.MessageID,
		ReplyMarkup: keyboard,
	})
	if err != nil {
		slog.Debug("bot: Failed to switch picker page", "error", err)
	}

	b.answerCallback(ctx, query.ID, "")

	return nil
}
