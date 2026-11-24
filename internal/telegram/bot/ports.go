package bot

import (
	"context"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
)

// API is the subset of the Telegram Bot API used by handlers. *telego.Bot satisfies it;
// tests inject a fake.
type API interface {
	SendMessage(ctx context.Context, params *t.SendMessageParams) (*t.Message, error)
	SendChatAction(ctx context.Context, params *t.SendChatActionParams) error
	AnswerCallbackQuery(ctx context.Context, params *t.AnswerCallbackQueryParams) error
	EditMessageText(ctx context.Context, params *t.EditMessageTextParams) (*t.Message, error)
	EditMessageReplyMarkup(ctx context.Context, params *t.EditMessageReplyMarkupParams) (*t.Message, error)
	GetChatMember(ctx context.Context, params *t.GetChatMemberParams) (t.ChatMember, error)
	DeleteMessage(ctx context.Context, params *t.DeleteMessageParams) error
}

// Store is the persistence the handlers need. *storage.Storage satisfies it.
type Store interface {
	CreateGroup(ctx context.Context, name string, chatID int64) error
	GetGroupByID(ctx context.Context, id uint) (*storage.MentionGroup, error)
	GetGroup(ctx context.Context, name string, chatID int64) (*storage.MentionGroup, error)
	GetUserByUsername(ctx context.Context, username string) (*storage.User, error)
	CreateOrUpdateUser(ctx context.Context, userID int64, username, firstName, lastName string) (*storage.User, error)
	AddMember(ctx context.Context, groupID uint, user *storage.User) (bool, error)
	RemoveMember(ctx context.Context, groupID uint, userID int64) (bool, error)
	DeleteGroup(ctx context.Context, groupID uint) error
	DeleteEmptyGroups(ctx context.Context, chatID int64, keepName string) ([]string, error)
	GetGroupsByChat(ctx context.Context, chatID int64) ([]storage.MentionGroup, error)
	GetGroupsToJoinByChatAndUser(ctx context.Context, chatID int64, userID int64) ([]storage.MentionGroup, error)
	GetUserGroupsByChat(ctx context.Context, chatID int64, userID int64) ([]storage.MentionGroup, error)
	FindGroupsByChatAndNamesWithMembers(ctx context.Context, chatID int64, names []string) ([]storage.MentionGroup, error)
	RenameGroup(ctx context.Context, chatID int64, oldName, newName string) error
	AddAlias(ctx context.Context, chatID int64, groupName, alias string) error
	RemoveAlias(ctx context.Context, chatID int64, alias string) (bool, error)
	GetAliasesByChat(ctx context.Context, chatID int64) (map[uint][]string, error)
	Stats(ctx context.Context) (storage.Stats, error)
	MigrateChatGroups(ctx context.Context, fromChatID, toChatID int64) error
}
