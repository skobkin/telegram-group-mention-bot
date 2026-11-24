package bot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"telegram-group-mention-bot/internal/logging"

	t "github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

const (
	groupNameAll = "all"
)

// Bot wires the Telegram API, the group store and the command handlers together.
type Bot struct {
	bot     *t.Bot // used for GetMe and long polling
	api     API    // used by handlers to send; replaceable in tests
	storage Store
	me      *botInfo

	owners    map[int64]struct{} // users allowed to use owner tools; empty disables them
	startedAt time.Time

	admins adminCache

	autoDelete time.Duration               // 0 disables
	afterFunc  func(time.Duration, func()) // test hook; nil means time.AfterFunc

	groupCooldown time.Duration // 0 disables
	userCooldown  time.Duration // 0 disables
	maxMentions   int           // 0 means unlimited
	cooldowns     cooldownTracker
	now           func() time.Time // test hook; nil means time.Now
}

func (b *Bot) clock() time.Time {
	if b.now != nil {
		return b.now()
	}

	return time.Now()
}

// Options are the optional settings of a Bot.
type Options struct {
	// OwnerIDs enables owner-only tools (/stats) for these Telegram user IDs.
	OwnerIDs []int64
	// GroupCooldown and UserCooldown throttle mentions (0 disables); MaxMentionMembers caps pings per post (0 = unlimited).
	// AutoDelete deletes service replies and the commands they answer after this time (0 disables).
	AutoDelete        time.Duration
	GroupCooldown     time.Duration
	UserCooldown      time.Duration
	MaxMentionMembers int
}

func New(token string, store Store, opts Options) (*Bot, error) {
	slog.Debug("bot: Creating new bot instance", "token_length", len(token))

	// Create bot with debug logging
	bot, err := t.NewBot(token, t.WithLogger(logging.TelegoLogger{}))
	if err != nil {
		logError("bot: Failed to create bot", err, "token_length", len(token))

		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	slog.Debug("bot: Bot instance created successfully")

	owners := make(map[int64]struct{}, len(opts.OwnerIDs))
	for _, id := range opts.OwnerIDs {
		owners[id] = struct{}{}
	}

	return &Bot{
		bot:       bot,
		api:       bot,
		storage:   store,
		owners:    owners,
		startedAt: time.Now(),

		autoDelete:    opts.AutoDelete,
		groupCooldown: opts.GroupCooldown,
		userCooldown:  opts.UserCooldown,
		maxMentions:   opts.MaxMentionMembers,
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
		logError("bot: Failed to get bot information", err)

		return fmt.Errorf("failed to get bot information: %w", err)
	}
	b.me = botInfoFromMe(me)
	b.syncCommandMenu(ctx)
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
		logError("bot: Failed to get updates channel", err)

		return fmt.Errorf("failed to get updates channel: %w", err)
	}

	h, err := th.NewBotHandler(b.bot, updates)
	if err != nil {
		logError("bot: Failed to create handler", err)

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

// registerHandlers registers every command from the command table plus the free-form @group handler.
func (b *Bot) registerHandlers(h *th.BotHandler) {
	commandForMe := b.commandForThisBot()

	for _, cmd := range b.commands() {
		handler := cmd.handler
		predicates := make([]th.Predicate, 0, len(cmd.names))
		for _, name := range cmd.names {
			predicates = append(predicates, th.And(commandForMe, th.CommandEqual(name)))
		}

		h.HandleMessage(func(ctx *th.Context, message t.Message) error {
			return handler(ctx, message)
		}, th.Or(predicates...))
	}

	h.HandleCallbackQuery(func(ctx *th.Context, query t.CallbackQuery) error {
		return b.handleCallback(ctx, query)
	}, th.CallbackDataPrefix(callbackPrefix))

	h.HandleMessage(func(ctx *th.Context, message t.Message) error {
		return b.handleFreeFormMessage(ctx, message)
	}, th.Not(th.AnyCommand()))
}
