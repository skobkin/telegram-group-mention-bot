# Upgrade notes

Operator-facing changes, newest first.

## Unreleased

- Replies are now localized (English and Russian) by the sender's Telegram `language_code`; everyone else gets English. No configuration needed.
  The command menu is registered for `ru` as well. Error replies no longer include raw error text (details are logged). `/stats` stays English.
- New optional `SENTRY_DSN` / `SENTRY_ENVIRONMENT`: unexpected errors and recovered panics are reported to Sentry (disabled when the DSN
  is empty). Events are scrubbed of the bot token; expected outcomes (not found, already exists, Telegram 403/429, context cancellation)
  are not reported. Adds the `sentry-go` dependency.
- Bot messages no longer generate link previews, `/list` and `/my` show member counts, and long `/show` lists use an expandable quote
  (needs a Telegram client that supports expandable blockquotes; older clients show a plain quote).
- Successful `/new`, `/join`, `/leave`, `/del` are now acknowledged with a 👍 reaction instead of a text reply (text fallback when the chat
  does not allow reactions). Errors and informational replies stay text.
- The bot registers its commands with `setMyCommands` at startup (private chats and groups, only when the list changed).
  This overwrites commands previously set via @BotFather for those scopes.
- New optional `AUTO_DELETE_SECONDS` (default 0 = off): delete service replies and the commands they answer after N seconds.
  Requires the bot to have delete-messages rights; failures are ignored. Pending deletions are lost on restart.
- **Behavior change:** mentions are now rate-limited by default: 30 s per group and 10 s per user in a chat (chat admins are exempt),
  with one short notice per window. Set `MENTION_GROUP_COOLDOWN_SECONDS=0` and `MENTION_USER_COOLDOWN_SECONDS=0` to keep the old behavior.
  `MENTION_MAX_MEMBERS` (default 0 = unlimited) optionally caps pings per post. Cooldown state is in memory and resets on restart.
- New admin commands `/add`, `/remove` and `/del <name> force`. Admin status is checked with `getChatMember` (cached for 60 s),
  so the bot needs to be a member of the chat; for the check of *other* users Telegram only guarantees results when the bot is an admin there.
- Reply keyboards are gone: `/join`, `/leave`, `/m`, `/show`, `/del` without a name now show an inline keyboard that is edited in place,
  and mention posts carry Join/Leave buttons. The bot now handles `callback_query` updates (no configuration needed).
- New optional `BOT_OWNER_IDS` (comma-separated Telegram user IDs). Owners get a hidden `/stats` command in a private chat with the bot
  (version, uptime, chats, groups, users, memberships, database size). Empty (default) disables it.
- `.env` and the compiled binary are no longer tracked in git. Copy `.env.example` to `.env` locally.
  If you ever used a real token from the previously committed `.env`, rotate it via @BotFather.
- Project layout changed: build with `go build ./cmd/bot` (was `go build .`).
- Dependencies updated (Go 1.27, telego 1.14, gorm 1.31); building from source needs Go 1.27+.
- SQLite is now CGO-free (`modernc.org/sqlite`); existing database files keep working. No C toolchain is needed to build.
- The Docker image now runs as a **non-root user (uid/gid 10001)**. Make an existing `/data` volume writable for it, e.g.
  `chown -R 10001:10001 ./data` on the host, otherwise the bot cannot open its database.
- The database is now migrated by versioned migrations (`PRAGMA user_version`) instead of GORM AutoMigrate; existing
  databases are upgraded automatically on first start (legacy columns removed, orphan member rows cleaned up).
  The bot refuses to start on a database written by a newer version. Back up `data.sqlite` before upgrading.
  SQLite now runs in WAL mode (extra `-wal`/`-shm` files next to the database) with foreign keys enforced.
- Group names are now case-insensitive and stored lowercased. A migration lowercases existing names and merges
  groups whose names collide within a chat (members are combined). New `/rename`, `/alias`, `/unalias` commands.
  A new `group_aliases` table is created automatically.
- Logs no longer contain message text or user names, and the bot token is scrubbed from all log output.
