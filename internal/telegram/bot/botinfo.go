package bot

import t "github.com/mymmrac/telego"

type botInfo struct {
	ID                      int64
	Username                string
	FirstName               string
	IsBot                   bool
	CanJoinGroups           bool
	CanReadAllGroupMessages bool
	SupportsInlineQueries   bool
}

func botInfoFromMe(me *t.BotFullInfo) *botInfo {
	if me == nil {
		return &botInfo{}
	}

	return &botInfo{
		ID:                      me.ID,
		Username:                me.Username,
		FirstName:               me.FirstName,
		IsBot:                   me.IsBot,
		CanJoinGroups:           me.CanJoinGroups,
		CanReadAllGroupMessages: me.CanReadAllGroupMessages,
		SupportsInlineQueries:   me.SupportsInlineQueries,
	}
}
