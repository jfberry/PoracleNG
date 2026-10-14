package commands

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/store"
)

const shinyFeedBase = `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true}}]}`
const shinyFeedLondon = `{"rates":[{"pokemon":25,"form":0,"costume":0,"prediction":{"probability":0.1,"supported":true},"overrides":{"Europe/London":{"probability":0.05,"supported":true}}}]}`

func loadedShinyRates(t *testing.T, feed string) *personalshiny.Rates {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed)) }))
	t.Cleanup(server.Close)
	r := personalshiny.NewRates(server.URL, "", 0)
	r.Start(context.Background())
	t.Cleanup(r.Stop)
	for i := 0; !r.Loaded(); i++ {
		if i > 2000 {
			t.Fatal("feed never loaded")
		}
		time.Sleep(time.Millisecond)
	}
	return r
}

func TestShinyFeedInfoPokemon(t *testing.T) {
	ctx := infoCostumeCtx(t)
	cmd := &InfoCommand{}
	baseline := cmd.Run(ctx, []string{"pikachu"})[0].Text

	// Unconfigured and nil feeds leave the output identical.
	ctx.ShinyRates = personalshiny.NewRates("", "", 0)
	if got := cmd.Run(ctx, []string{"pikachu"})[0].Text; got != baseline {
		t.Fatalf("unconfigured feed changed output:\n%q\n%q", got, baseline)
	}
	if strings.Contains(baseline, "Expected shiny rate") {
		t.Fatal("baseline has expected line")
	}

	// Base rate, user with a location.
	ctx.ShinyRates = loadedShinyRates(t, shinyFeedBase)
	ctx.HasLocation = true
	h, _ := ctx.Humans.Get("user1")
	h.Latitude, h.Longitude = 51.5, 0
	ctx.Humans.(*store.MockHumanStore).AddHuman(h)
	out := cmd.Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(out, "Expected shiny rate: 1:10") || strings.Contains(out, "right now") {
		t.Fatalf("base line missing: %q", out)
	}

	// Local override.
	ctx.ShinyRates = loadedShinyRates(t, shinyFeedLondon)
	out = cmd.Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(out, "Expected shiny rate: 1:10 (1:20 at your location right now)") {
		t.Fatalf("local line missing: %q", out)
	}
}

func TestShinyFeedInfoShinyBoosted(t *testing.T) {
	ctx := infoCostumeCtx(t)
	cmd := &InfoCommand{}
	before := cmd.Run(ctx, []string{"shiny"})

	ctx.ShinyRates = personalshiny.NewRates("", "", 0)
	after := cmd.Run(ctx, []string{"shiny"})
	if len(before) != len(after) || before[0].Text != after[0].Text {
		t.Fatalf("unconfigured feed changed output: %v vs %v", before, after)
	}
	if strings.Contains(after[0].Text, "Boosted right now") {
		t.Fatal("unexpected header")
	}

	ctx.ShinyRates = loadedShinyRates(t, shinyFeedBase)
	out := cmd.Run(ctx, []string{"shiny"})[0].Text
	if !strings.Contains(out, "Boosted right now") || !strings.Contains(out, "Pikachu: 1:10") {
		t.Fatalf("boosted section missing: %q", out)
	}
}

// The feed keys Pikachu under its NORMAL form id (598); form 0 lookups must
// resolve through the species' default form, and that form must not be
// spelled out in the "boosted" list.
const shinyFeedNormalForm = `{"rates":[{"pokemon":25,"form":598,"costume":0,"prediction":{"probability":0.1,"supported":true}}]}`

func TestShinyFeedInfoUsesDefaultForm(t *testing.T) {
	ctx := infoCostumeCtx(t)
	ctx.GameData.Monsters[gamedata.MonsterKey{ID: 25, Form: 0}].DefaultFormID = 598
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":  "Pikachu",
		"form_598": "Normal",
	}))
	ctx.ShinyRates = loadedShinyRates(t, shinyFeedNormalForm)
	cmd := &InfoCommand{}

	out := cmd.Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(out, "Expected shiny rate: 1:10") {
		t.Fatalf("default-form rate missing: %q", out)
	}

	out = cmd.Run(ctx, []string{"shiny"})[0].Text
	if !strings.Contains(out, "Pikachu: 1:10") || strings.Contains(out, "(Normal)") {
		t.Fatalf("boosted list should omit the default form suffix: %q", out)
	}
}
