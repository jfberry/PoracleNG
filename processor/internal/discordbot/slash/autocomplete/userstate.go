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
	// IsAdmin reports whether the invoking user is a configured platform
	// admin. Listers that consult AreaLogic need it: admins see every
	// fence, not just the user-selectable ones.
	IsAdmin bool
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
// labels to at most 100 bytes via truncateChoiceLabel — which preserves
// both the leading prefix (where the human-identifiable name lives, e.g.
// the pokemon name) AND the trailing "[id:N]" / "[#N]" selector. The
// middle (filter recitations) is what gets elided.
func FilterAndCap(choices []Choice, focused string) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.ToLower(strings.TrimSpace(focused))
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	for _, c := range choices {
		if focused != "" && !strings.Contains(strings.ToLower(c.Label), focused) {
			continue
		}
		out = append(out, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateChoiceLabel(c.Label),
			Value: c.Value,
		})
		if len(out) == 25 {
			break
		}
	}
	return out
}

// truncateChoiceLabel clips a label to Discord's 100-byte choice-name
// limit. When the label fits, it's returned unchanged. When truncation
// is needed, the function tries to preserve both ends of the string:
// pokemon-name-style prefixes are how humans identify the rule, while
// suffix-anchored selectors like " [id:N]" or " [#N]" carry the load-
// bearing UID the user wants to pick. The middle — long filter
// recitations like "iv: 90%-100% | cp: 0-9000 | level: 0-55" — is what
// gets elided with a single rune ellipsis.
//
// Falls back to a tail-preserving truncation when no recognisable
// selector suffix is present, since the suffix is more likely to carry
// the unambiguous selector than the prefix for general labels.
func truncateChoiceLabel(label string) string {
	const max = 100
	if len(label) <= max {
		return label
	}
	const ellipsis = "…" // 3 bytes UTF-8
	// Look for a trailing selector marker. We accept " [id:" or " [#"
	// (matching how listers/tracking and listers/profiles compose
	// labels). Whatever follows the marker is treated as the selector
	// suffix and preserved verbatim.
	for _, marker := range []string{" [id:", " [#"} {
		if idx := strings.LastIndex(label, marker); idx > 0 {
			suffix := label[idx:]
			if len(suffix) >= max-len(ellipsis)-1 {
				// Suffix alone almost fills the budget — fall back to
				// the tail-only form.
				break
			}
			prefixBudget := max - len(suffix) - len(ellipsis)
			return label[:prefixBudget] + ellipsis + suffix
		}
	}
	// No selector marker — keep the head, ellipsis at the end.
	return label[:max-len(ellipsis)] + ellipsis
}

// maxChoiceValue is Discord's hard limit on an autocomplete choice's
// value (100 characters). FilterAndCapMulti composes values, so it has to
// respect the ceiling rather than let the API reject the whole response.
const maxChoiceValue = 100

// FilterAndCapMulti is FilterAndCap for options that hold a
// comma-separated list rather than one value — the tracker `areas:`
// option, which maps to a single `area:X,Y,Z` token.
//
// Discord replaces the entire option text with the picked choice's value,
// so a plain single-value list makes the second pick overwrite the first.
// Instead we split the typed text at the last comma: everything before it
// is the user's committed selection, the remainder is what they're
// currently typing. Choices are filtered by that remainder, already-picked
// values are dropped, and each choice's value is the whole composed field
// ("London,Paris") so picking appends instead of replacing. Labels stay
// the bare name so the list is scannable.
//
// Segments are re-joined with a bare "," (no space): the value travels to
// the text parser as one `area:` token either way, and the tighter form
// leaves more room under the 100-character value limit.
func FilterAndCapMulti(choices []Choice, focused string) []*discordgo.ApplicationCommandOptionChoice {
	committed, typing := SplitLastSegment(focused)

	chosen := make(map[string]bool, len(committed))
	for _, c := range committed {
		chosen[strings.ToLower(c)] = true
	}
	prefix := strings.Join(committed, ",")

	typing = strings.ToLower(strings.TrimSpace(typing))
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	for _, c := range choices {
		if chosen[strings.ToLower(c.Value)] {
			continue
		}
		if typing != "" && !strings.Contains(strings.ToLower(c.Label), typing) {
			continue
		}
		value := c.Value
		if prefix != "" {
			value = prefix + "," + c.Value
		}
		if len(value) > maxChoiceValue {
			continue
		}
		out = append(out, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateChoiceLabel(c.Label),
			Value: value,
		})
		if len(out) == 25 {
			break
		}
	}
	return out
}

// SplitLastSegment splits comma-separated text into the segments the user
// has committed (everything before the last comma, trimmed, with empty
// segments dropped) and the segment they are still typing. Text with no
// comma is entirely "still typing".
func SplitLastSegment(focused string) (committed []string, typing string) {
	idx := strings.LastIndex(focused, ",")
	if idx < 0 {
		return nil, focused
	}
	for _, seg := range strings.Split(focused[:idx], ",") {
		if seg = strings.TrimSpace(seg); seg != "" {
			committed = append(committed, seg)
		}
	}
	return committed, focused[idx+1:]
}
