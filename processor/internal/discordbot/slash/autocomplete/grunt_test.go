package autocomplete

import (
	"context"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// gruntTestDeps wires a minimal BotDeps covering each Grunt source:
//   - typed grunt (Fire, type 10) and (Water, type 11)
//   - boss leader (Giovanni)
//   - pokestop event (Kecleon)
func gruntTestDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_type_10": "Fire",
		"poke_type_11": "Water",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Grunts: map[int]*gamedata.Grunt{
			1: {Template: "CHARACTER_FIRE_GRUNT_MALE", TypeID: 10},
			2: {Template: "CHARACTER_WATER_GRUNT_FEMALE", TypeID: 11},
			3: {Template: "CHARACTER_GIOVANNI", Boss: true},
		},
		Util: &gamedata.UtilData{
			PokestopEvent: map[int]gamedata.EventInfo{
				7: {Name: "Kecleon"},
				8: {Name: "Showcase"},
			},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd}
}

func TestGrunt_EmptyFocusedReturnsAllCategories(t *testing.T) {
	deps := gruntTestDeps(t)
	out := Grunt(context.Background(), deps, "", "en")
	if len(out) == 0 {
		t.Fatal("expected non-empty grunt choices")
	}
	names := map[string]bool{}
	for _, c := range out {
		names[c.Name] = true
	}
	for _, want := range []string{"Everything", "Giovanni", "Fire Grunt", "Water Grunt", "Kecleon", "Showcase"} {
		if !names[want] {
			t.Errorf("missing expected entry %q in %+v", want, names)
		}
	}
}

func TestGrunt_TypedFilter(t *testing.T) {
	deps := gruntTestDeps(t)
	out := Grunt(context.Background(), deps, "giov", "en")
	if len(out) != 1 || out[0].Name != "Giovanni" {
		t.Errorf("expected single Giovanni for 'giov' filter, got %+v", out)
	}
	if v, _ := out[0].Value.(string); v != "giovanni" {
		t.Errorf("Giovanni value=%q, want lowercase canonical 'giovanni'", v)
	}
}

func TestGrunt_TypeNameValueIsCanonical(t *testing.T) {
	deps := gruntTestDeps(t)
	out := Grunt(context.Background(), deps, "fire", "en")
	found := false
	for _, c := range out {
		if c.Name == "Fire Grunt" {
			found = true
			if v, _ := c.Value.(string); v != "fire" {
				t.Errorf("Fire Grunt value=%q, want 'fire' (text bot's canonical type name)", v)
			}
		}
	}
	if !found {
		t.Errorf("expected 'Fire Grunt' in 'fire' results, got %+v", out)
	}
}

// Bosses come before typed grunts come before incidents. Verifies the
// sort-by-group ordering so the dropdown reads top-down with the most
// salient categories first.
func TestGrunt_CategoryOrdering(t *testing.T) {
	deps := gruntTestDeps(t)
	out := Grunt(context.Background(), deps, "", "en")
	var firstBoss, firstType, firstIncident int = -1, -1, -1
	for i, c := range out {
		switch c.Name {
		case "Giovanni":
			if firstBoss == -1 {
				firstBoss = i
			}
		case "Fire Grunt", "Water Grunt":
			if firstType == -1 {
				firstType = i
			}
		case "Kecleon", "Showcase":
			if firstIncident == -1 {
				firstIncident = i
			}
		}
	}
	if firstBoss == -1 || firstType == -1 || firstIncident == -1 {
		t.Fatalf("missing category samples: boss=%d type=%d incident=%d", firstBoss, firstType, firstIncident)
	}
	if !(firstBoss < firstType && firstType < firstIncident) {
		t.Errorf("category ordering wrong: boss=%d type=%d incident=%d (want boss<type<incident)", firstBoss, firstType, firstIncident)
	}
}

func TestGrunt_NilDepsReturnsNil(t *testing.T) {
	if out := Grunt(context.Background(), nil, "", "en"); out != nil {
		t.Errorf("expected nil for nil deps, got %+v", out)
	}
}

func TestGrunt_GenderOptionDoesNotLeakIntoGruntChoices(t *testing.T) {
	// The autocomplete only returns grunt names — gender filtering is a
	// separate option on /invasion, not embedded in grunt_type values.
	deps := gruntTestDeps(t)
	out := Grunt(context.Background(), deps, "", "en")
	for _, c := range out {
		if strings.Contains(strings.ToLower(c.Name), "male") || strings.Contains(strings.ToLower(c.Name), "female") {
			t.Errorf("gender leaked into grunt_type choice %q", c.Name)
		}
	}
}
