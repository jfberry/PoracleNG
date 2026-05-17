package slash

import "errors"

// ErrSlashNotConfigured is returned by the BotDeps slash closures when
// Discord is not configured or slash commands are not enabled. Callers
// use errors.Is to detect this sentinel and emit a friendly operator
// message instead of a stack trace.
//
// Mirrors the pattern used by discordbot.ErrReconciliationDisabled.
var ErrSlashNotConfigured = errors.New("slash commands are not configured or Discord is not enabled")
