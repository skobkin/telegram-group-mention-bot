package bot

import t "github.com/mymmrac/telego"

type botInfo struct {
	ID                      int64
	Username                string
	FirstName               string
	LastName                string
	IsBot                   bool
	CanJoinGroups           bool
	CanReadAllGroupMessages bool
	SupportsInlineQueries   bool
}

func botInfoFromMe(me *t.User) *botInfo {
	if me == nil {
		return &botInfo{}
	}

	return &botInfo{
		ID:                      me.ID,
		Username:                me.Username,
		FirstName:               me.FirstName,
		LastName:                me.LastName,
		IsBot:                   me.IsBot,
		CanJoinGroups:           me.CanJoinGroups,
		CanReadAllGroupMessages: me.CanReadAllGroupMessages,
		SupportsInlineQueries:   me.SupportsInlineQueries,
	}
}
