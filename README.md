# Telegram Group Mention Bot

A Telegram bot that allows users to create mention groups and mention all members of a group at once.

## Features

- Create mention groups in any chat
- Join/leave mention groups
- Mention all members of a group at once
- Support for users with and without usernames
- List group members without mentioning them
- Delete empty groups

## Commands

| Command | Description |
|---------|-------------|
| `/new <name>` | Create a new mention group |
| `/join <name>` | Join an existing mention group |
| `/leave <name>` | Leave a mention group |
| `/mention <name>`, `/m <name>`, `/call <name>` | Mention all members of a group |
| `/show <name>` | Show all members of a group without mentioning them |
| `/del <name>` | Delete a group (only if it has no members) |
| `/list` | Show all groups in this chat |
| `/help` | Show this help message |

## Getting Started

1. Create a `.env` file with the following variables:
   ```
   TELEGRAM_BOT_TOKEN=your_bot_token_here
   DATABASE_PATH=data.sqlite
   ```
2. Run the bot:
   ```bash
   go run main.go
   ```

## Requirements

- Go 1.16 or later
- SQLite3
- Telegram Bot Token (get it from [@BotFather](https://t.me/botfather))

## Dependencies

- [telego](https://github.com/mymmrac/telego) - Telegram Bot API library
- [gorm](https://gorm.io) - ORM library
- [godotenv](https://github.com/joho/godotenv) - Environment variables loader 