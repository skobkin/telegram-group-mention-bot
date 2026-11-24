// Package buildinfo exposes build-time metadata.
package buildinfo

// AppName is the application name used in logs and release artifacts.
const AppName = "telegram-group-mention-bot"

// Version is set at build time via -ldflags "-X telegram-group-mention-bot/internal/buildinfo.Version=...".
var Version = "dev"
