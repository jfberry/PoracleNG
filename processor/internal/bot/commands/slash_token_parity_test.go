package commands

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
)

// TestSlashTrackTokensAreAllRecognized drives the /track mapper's output
// through the SAME ArgMatcher the text bot uses and asserts nothing lands in
// Unrecognized.
//
// This is the assertion that was missing when #238 shipped. TestSlashTextParity
// pins the mapper's output STRING, so reverting a mapper to emit a bare token
// and re-pinning the string leaves the suite green while the command aborts in
// production — which is exactly how the size bug survived. ReportUnrecognized
// fails the whole command on a single unknown token, so an unrecognised token is
// never cosmetic.
//
// It also pins the implicit contract that each hardcoded prefix in the mapper
// ("size:", "form:", "costume:", "template:", "d", "iv", …) still resolves under
// ArgMatcher. Renaming the matching arg.prefix.* value in en.json is an
// otherwise-silent break.
//
// SCOPE — this catches token GRAMMAR only, not value semantics. Reverting the
// size fix fails it; reverting the form-lowercasing fix does NOT, because
// "form:Alola" is a perfectly recognised token and the failure happens later, in
// filterByForm. Value correctness is covered per-option in the mappers package
// (TestTrackMapperLowercasesForm and friends). Both layers are needed.
func TestSlashTrackTokensAreAllRecognized(t *testing.T) {
	ctx, _ := invasionTestCtx(t) // shares the ArgMatcher/GameData wiring
	params := trackParams(ctx)

	cases := []struct {
		name string
		opts []*discordgo.ApplicationCommandInteractionDataOption
	}{
		{"size class", []*discordgo.ApplicationCommandInteractionDataOption{
			sopt("pokemon", "25"), sopt("size", "xxl"),
		}},
		{"iv range", []*discordgo.ApplicationCommandInteractionDataOption{
			sopt("pokemon", "25"), sopt("iv", "90-100"),
		}},
		{"distance and clean", []*discordgo.ApplicationCommandInteractionDataOption{
			sopt("pokemon", "25"), iopt("distance", 250), bopt("clean", true),
		}},
		{"template", []*discordgo.ApplicationCommandInteractionDataOption{
			sopt("pokemon", "25"), sopt("template", "pvp"),
		}},
		{"capitalised form", []*discordgo.ApplicationCommandInteractionDataOption{
			sopt("pokemon", "25"), sopt("form", "Alola"),
		}},
		{"pvp leagues", []*discordgo.ApplicationCommandInteractionDataOption{
			sopt("pokemon", "25"), iopt("great_rank", 5), iopt("ultra_rank", 10),
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokens, err := mappers.Track(c.opts)
			if err != nil {
				t.Fatalf("mapper: %v", err)
			}
			// The pokemon name is resolved separately by the command, not by
			// ArgMatcher, so it is expected in Unrecognized.
			parsed := ctx.ArgMatcher.Match(tokens[1:], params, "en")
			if len(parsed.Unrecognized) != 0 {
				t.Errorf("tokens %v produced Unrecognized %v — ReportUnrecognized aborts the whole command on these",
					tokens, parsed.Unrecognized)
			}
		})
	}
}

// sopt/iopt/bopt build slash option values. Mirrors the helpers in the mappers
// package tests, which are not exported.
func sopt(name, val string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: name, Type: discordgo.ApplicationCommandOptionString, Value: val,
	}
}

func iopt(name string, val int) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: name, Type: discordgo.ApplicationCommandOptionInteger, Value: float64(val),
	}
}

func bopt(name string, val bool) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name: name, Type: discordgo.ApplicationCommandOptionBoolean, Value: val,
	}
}
