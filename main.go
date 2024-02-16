package main

import (
	"fmt"
	"github.com/mymmrac/telego"
	"os"
)

func main() {
	botToken := os.Getenv("TELEGRAM_TOKEN")

	bot, err := telego.NewBot(botToken, telego.WithDefaultDebugLogger())
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}
