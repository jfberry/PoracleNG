// Package reconcile provides shared types and sentinel errors for Discord
// role reconciliation. It is a leaf package (no imports from discordbot or
// commands) so both can import it without creating a cycle.
package reconcile

import "errors"

// ErrDisabled is returned by the BotDeps Reconciler and RunReconcile closures
// when Discord reconciliation is not configured (Telegram-only deploy or
// check_role=false). Callers use errors.Is to detect this sentinel and emit a
// friendly operator message instead of a stack trace.
//
// Mirrors the pattern used by discordbot/slash.ErrSlashNotConfigured.
var ErrDisabled = errors.New("discord reconciliation is not enabled")
