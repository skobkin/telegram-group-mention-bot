# Upgrade notes

Operator-facing changes, newest first.

## Unreleased

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
