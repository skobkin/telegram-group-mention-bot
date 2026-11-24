# Upgrade notes

Operator-facing changes, newest first.

## Unreleased

- `.env` and the compiled binary are no longer tracked in git. Copy `.env.example` to `.env` locally.
  If you ever used a real token from the previously committed `.env`, rotate it via @BotFather.
- Project layout changed: build with `go build ./cmd/bot` (was `go build .`).
- Dependencies updated (Go 1.27, telego 1.14, gorm 1.31); building from source needs Go 1.27+.
- SQLite is now CGO-free (`modernc.org/sqlite`); existing database files keep working. No C toolchain is needed to build.
- The Docker image now runs as a **non-root user (uid/gid 10001)**. Make an existing `/data` volume writable for it, e.g.
  `chown -R 10001:10001 ./data` on the host, otherwise the bot cannot open its database.
- Logs no longer contain message text or user names, and the bot token is scrubbed from all log output.
