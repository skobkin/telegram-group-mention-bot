# Telegram Group Mention Bot

[![build status](https://ci.skobk.in/api/badges/skobkin/telegram-group-mention-bot/status.svg)](https://ci.skobk.in/skobkin/telegram-group-mention-bot) [![latest release](https://git.skobk.in/skobkin/telegram-group-mention-bot/badges/release.svg)](https://git.skobk.in/skobkin/telegram-group-mention-bot/releases)

A Telegram bot that allows users to create mention groups and mention all members of a group at once.

## Features

- Create mention groups in any chat
- Join/leave mention groups
- Mention all members of a group at once
- Support for users with and without usernames
- List group members without mentioning them
- Delete empty groups
- Anti-spam: per-group and per-user mention cooldowns (chat admins are exempt) and an optional cap on pinged members
- Case-insensitive group names, plus aliases and renaming
- Inline button pickers when a command is used without a group name (only the requester can use them), and Join/Leave buttons under every mention post

## Commands

| Command | Description |
|---------|-------------|
| `/new <name>` | Create a new mention group |
| `/join <name>` | Join an existing mention group |
| `/leave <name>` | Leave a mention group |
| `/mention <name>`, `/m <name>`, `/call <name>` | Mention all members of a group |
| `/show <name>` | Show all members of a group without mentioning them |
| `/del <name>` | Delete a group (only if it has no members) |
| `/del <name> force` | Chat admins: delete a group together with its members (asks for confirmation) |
| `/add <group>`, `/remove <group>` | Chat admins: add/remove the user you reply to (or `@mention`) |
| `/list` | Show all groups in this chat |
| `/my` | Show groups you've joined in this chat |
| `/clear` | Delete all empty groups in this chat |
| `/rename <old> <new>` | Rename a group, keeping its members |
| `/alias <group> <alias>` | Add a short alias for a group (usable in `/m` and as `@alias`) |
| `/unalias <alias>` | Remove an alias |
| `/help` | Show this help message |

## Getting Started

### From source

1. Copy `.env.example` to `.env` (it is git-ignored) and fill in the variables:
   ```
   TELEGRAM_BOT_TOKEN=your_bot_token_here
   DATABASE_PATH=data.sqlite
   ```
2. Run the bot:
   ```bash
   go run ./cmd/bot
   ```

### Using Docker

You can also run the bot using Docker:

```bash
docker run -d \
  -e TELEGRAM_BOT_TOKEN=your_bot_token_here \
  -v ./data:/data \
  skobkin/telegram-group-mention-bot
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `TELEGRAM_BOT_TOKEN` | Your Telegram bot token from [@BotFather](https://t.me/botfather) | (required) |
| `DATABASE_PATH` | Path to the SQLite database file | `data.sqlite` |
| `MENTION_GROUP_COOLDOWN_SECONDS` | Minimum seconds between two mentions of the same group in a chat (0 disables) | `30` |
| `MENTION_USER_COOLDOWN_SECONDS` | Minimum seconds between two mentions by the same user in a chat (0 disables) | `10` |
| `MENTION_MAX_MEMBERS` | Maximum members pinged per mention post; the rest are reported as skipped (0 = unlimited) | `0` |
| `BOT_OWNER_IDS` | Comma-separated Telegram user IDs allowed to use `/stats` (private chat only); empty disables owner tools | empty |
| `LOG_LEVEL` | Logging level (`debug`, `info`, `warn`, `error`) | `warn` |

You can also control logging verbosity using command-line flags:
- `-v` - Enable verbose logging (LevelInfo)
- `-vv` - Enable very verbose logging (LevelDebug)

Note: Command-line flags take precedence over the `LOG_LEVEL` environment variable.

## Requirements

- Go 1.27 or later (see `go.mod`)
- No C toolchain or SQLite library needed (pure-Go SQLite driver)
- Telegram Bot Token (get it from [@BotFather](https://t.me/botfather))

## Dependencies

- [telego](https://github.com/mymmrac/telego) - Telegram Bot API library
- [gorm](https://gorm.io) - ORM library, with the CGO-free [glebarez/sqlite](https://github.com/glebarez/sqlite) driver
- [godotenv](https://github.com/joho/godotenv) - Environment variables loader 

## Development

```bash
go build ./cmd/bot
go vet ./...
go test ./...
golangci-lint fmt --diff ./cmd/bot ./internal
golangci-lint run ./cmd/bot/... ./internal/...
```

### CI

CI runs on Woodpecker (`.woodpecker/ci.yaml`): fmt, lint, vet, test and build on every pull request and on pushes
to `main`; tags additionally publish a Forgejo release (GoReleaser) and a Docker image. Required repository secrets:

| Secret | Used for |
|--------|----------|
| `GITEA_TOKEN` | Creating the Forgejo release |
| `DOCKER_REPO` | Docker image repository |
| `DOCKER_LOGIN`, `DOCKER_TOKEN` | Docker registry credentials |
