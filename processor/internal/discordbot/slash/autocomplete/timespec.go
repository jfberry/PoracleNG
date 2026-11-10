package autocomplete

import (
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// timeShapes are the three forms the settime grammar supports, as the time
// half of a spec (the day prefix is prepended separately). Each carries a
// plain-English gloss because the point of this autocomplete is to teach the
// syntax, not just to save typing.
var timeShapes = []struct {
	Time  string
	Gloss string
}{
	{"07:30", "once at 07:30"},
	{"07:00-18:00", "hourly, 07:00 → 18:00"},
	{"08:00-20:00/2", "every 2 hours, 08:00 → 20:00"},
}

// dayGloss renders a human description of a day prefix for the choice label.
func dayGloss(prefix string) string {
	switch prefix {
	case "":
		return "every day"
	case "weekday":
		return "Mon–Fri"
	case "weekend":
		return "Sat–Sun"
	case "every", "everyday":
		return "every day"
	default:
		return prefix
	}
}

// TimeSpec returns autocomplete choices for the `times` option on
// /profile settime and /summary … settime.
//
// The grammar is `[<dayPrefix>[:]]HH[:MM][-HH[:MM][/step]]` — easy to use and
// hard to recall, which is what this exists to fix. Suggestions are complete,
// valid specs; labels gloss what each one means.
//
// Following the IVRange precedent, whatever the user has typed is offered
// back as the first choice so a known-good spec is always committable and the
// picker never traps free text.
//
// tr may be nil; translated day prefixes are then omitted (English is always
// accepted by the parser).
func TimeSpec(focused string, tr *i18n.Translator) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.ToLower(strings.TrimSpace(focused))

	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	seen := make(map[string]bool)
	add := func(value, label string) bool {
		if seen[value] || len(out) >= 25 {
			return len(out) < 25
		}
		seen[value] = true
		out = append(out, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateChoiceLabel(label),
			Value: value,
		})
		return len(out) < 25
	}

	if focused != "" {
		add(focused, focused)
	}

	// Digits first: the user is writing the time half, so completing day
	// prefixes would only get in the way.
	if focused != "" && focused[0] >= '0' && focused[0] <= '9' {
		for _, shape := range timeShapes {
			if strings.HasPrefix(shape.Time, focused) {
				add(shape.Time, shape.Time+" — "+dayGloss("")+", "+shape.Gloss)
			}
		}
		return out
	}

	// Otherwise the user is either at the start or partway through a day
	// prefix. Offer bare times only when nothing has been typed, since a
	// partial prefix can never be completed by a bare time.
	prefixes := []string{""}
	for _, p := range bot.DayPrefixNames(tr) {
		if focused == "" || strings.HasPrefix(p, focused) {
			prefixes = append(prefixes, p)
		}
	}

	// Breadth before depth: one suggestion per prefix first, so a wide match
	// like "w" shows weekday AND weekend rather than three weekday variants.
	for shapeIdx := range timeShapes {
		for _, p := range prefixes {
			if p == "" && focused != "" {
				continue // a bare time cannot extend a typed prefix
			}
			shape := timeShapes[shapeIdx]
			if !add(p+shape.Time, p+shape.Time+" — "+dayGloss(p)+", "+shape.Gloss) {
				return out
			}
		}
	}
	return out
}
