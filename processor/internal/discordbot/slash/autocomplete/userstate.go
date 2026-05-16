// Package autocomplete provides primitives for Discord slash command
// autocomplete handlers. The Registry maps a lister name (referenced from
// command definitions) to a UserStateLister that produces Choice slices
// for the focused option. FilterAndCap performs the final
// substring-filter + cap-at-25 + truncate-to-100-bytes step before
// returning choices to discordgo.
package autocomplete

import (
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// Choice is a single autocomplete suggestion. Label is shown to the user;
// Value is what discordgo sends back when the user picks it.
type Choice struct {
	Label string
	Value string
}

// UserStateHint carries optional context from the slash command into the
// lister. Subtype lets one lister (e.g. "tracking") serve multiple command
// trees by branching on the parent command's subtype. Focused is the
// current partial text the user has typed for the autocomplete field.
type UserStateHint struct {
	Subtype string
	Focused string
}

// UserStateLister produces autocomplete choices for a given user.
// Implementations should be safe to call concurrently and should respect
// ctx cancellation for any I/O they perform.
type UserStateLister func(ctx context.Context, deps *bot.BotDeps, userID string, hint UserStateHint) ([]Choice, error)

// Registry maps lister names to their UserStateLister implementations.
type Registry struct {
	listers map[string]UserStateLister
}

// NewRegistry returns an empty Registry ready for Register calls.
func NewRegistry() *Registry { return &Registry{listers: map[string]UserStateLister{}} }

// Register adds a lister under the given name, overwriting any previous
// entry under the same name.
func (r *Registry) Register(name string, lister UserStateLister) { r.listers[name] = lister }

// Lookup returns the lister registered under name, or nil if none.
func (r *Registry) Lookup(name string) UserStateLister { return r.listers[name] }

// FilterAndCap substring-filters by focused (case-insensitive against
// Label), caps the result at Discord's 25-choice limit, and truncates
// labels to at most 100 bytes. When truncation is needed the tail of the
// label is kept (prefixed with a one-rune ellipsis) so suffix-anchored
// selectors like "[id:N]" or "[#N]" remain visible to the user.
func FilterAndCap(choices []Choice, focused string) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.ToLower(strings.TrimSpace(focused))
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	for _, c := range choices {
		if focused != "" && !strings.Contains(strings.ToLower(c.Label), focused) {
			continue
		}
		label := c.Label
		if len(label) > 100 {
			// "…" is 3 bytes in UTF-8; keep the last 97 bytes so the
			// total comes to exactly 100 bytes, preserving any
			// suffix-anchored selector like "[id:42]".
			label = "…" + label[len(label)-97:]
		}
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: label, Value: c.Value})
		if len(out) == 25 {
			break
		}
	}
	return out
}
