# Repository guidelines

Telegram bot (Go) that lets chat members create mention groups and ping them at once.

## Layout

- `cmd/bot` — entry point (flags, logging setup, wiring).
- `internal/config` — environment parsing (the only place that reads `TELEGRAM_BOT_TOKEN`, `DATABASE_PATH`).
- `internal/telegram/bot` — Telegram handlers, middleware, sending helpers (telego).
  Commands live in one table (`commands.go`) that drives registration and `/help`; commands taking a group
  name share `groupCommand` (name or picker). Handlers depend on the `API` and `Store` interfaces (`ports.go`),
  so tests inject a fake API and a temp-file SQLite store (see `handlers_test.go`).
- `internal/storage` — GORM/SQLite persistence.
- `internal/logging`, `internal/support/secretscrub` — slog scrubbing; the bot token is registered as a secret and must never reach logs.
- `internal/buildinfo` — `Version`, set with `-ldflags "-X telegram-group-mention-bot/internal/buildinfo.Version=..."`.

## Commands

```bash
go build ./cmd/bot
go vet ./...
go test ./...
golangci-lint run      # once .golangci.yml exists
```

## Conventions

- Never log message text, user names or the token; log IDs and lengths only.
- No unrelated refactoring in a change; keep each change focused on its issue.
- Operator-facing changes (env vars, volumes, permissions, schema) go into `UPGRADE.md`.
- Update `README.md` when commands or configuration change.
