package bot

import "strings"

// adminOnlyHelpTopics — non-admins asking "!help enable" get the 🙅
// unknown-topic reply rather than the admin command surface.
//
// Lives in this package rather than next to HelpCommand because the slash
// autocomplete needs the same set to avoid suggesting topics it would then
// refuse, and internal/bot/commands already imports internal/discordbot/slash
// — so the autocomplete package cannot import commands without a cycle.
var adminOnlyHelpTopics = map[string]bool{
	"enable":        true,
	"disable":       true,
	"broadcast":     true,
	"userlist":      true,
	"community":     true,
	"apply":         true,
	"backup":        true,
	"restore":       true,
	"poracle-admin": true,
	"pa":            true,
}

// IsAdminOnlyHelpTopic reports whether a help topic is restricted to admins.
// Matching is case-insensitive, mirroring the command path, which lowercases
// the topic before looking it up.
func IsAdminOnlyHelpTopic(topic string) bool {
	return adminOnlyHelpTopics[strings.ToLower(strings.TrimSpace(topic))]
}
