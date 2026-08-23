package commands

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// TestResolveDTSType_QuestSummary locks in a real defect found while wiring
// the "quest-summary" !poracle-test type (superpowers/sdd task-5): the
// Parser lowercases every arg before PoracleTestCommand.Run ever sees it
// (see internal/bot/parser.go's tokenize), so a validHooks entry can only
// ever be matched in its already-lowercase form. Multi-word CLI names must
// therefore use the lowercase-dash spelling ("quest-summary", mirroring the
// existing "fort-update"/"max-battle" entries) — never the camelCase DTS
// template-type name ("questSummary") a user could never actually type.
// resolveDTSType is the seam that maps the post-ReplaceAll wire spelling
// ("quest_summary") back to the real, case-sensitive registered DTS
// template type ("questSummary") for template-existence validation.
func TestResolveDTSType_QuestSummary(t *testing.T) {
	if got := resolveDTSType("quest_summary", nil); got != "questSummary" {
		t.Errorf(`resolveDTSType("quest_summary", nil) = %q, want "questSummary"`, got)
	}
}

// TestResolveDTSType_MonsterChanged mirrors TestResolveDTSType_QuestSummary
// for the "monster-changed" !poracle-test type (superpowers/sdd task-6): the
// CLI-typeable spelling is lowercase-dash ("monster-changed", never the
// camelCase DTS template-type name "monsterChanged" a user could never type),
// and resolveDTSType maps the post-ReplaceAll wire spelling
// ("monster_changed") back to the registered DTS template type
// ("monsterChanged") for template-existence validation.
func TestResolveDTSType_MonsterChanged(t *testing.T) {
	if got := resolveDTSType("monster_changed", nil); got != "monsterChanged" {
		t.Errorf(`resolveDTSType("monster_changed", nil) = %q, want "monsterChanged"`, got)
	}
}

// TestResolveDTSType_RsvpChanges mirrors TestResolveDTSType_MonsterChanged
// for the "rsvp-changes" !poracle-test type (superpowers/sdd task-7 — the
// last derived type): the CLI-typeable spelling is lowercase-dash
// ("rsvp-changes", never the camelCase DTS template-type name "rsvpChanges"
// a user could never type), and resolveDTSType maps the post-ReplaceAll wire
// spelling ("rsvp_changes") back to the registered DTS template type
// ("rsvpChanges") for template-existence validation.
func TestResolveDTSType_RsvpChanges(t *testing.T) {
	if got := resolveDTSType("rsvp_changes", nil); got != "rsvpChanges" {
		t.Errorf(`resolveDTSType("rsvp_changes", nil) = %q, want "rsvpChanges"`, got)
	}
}

// TestResolveHookType_DTSNames covers superpowers/sdd task-9: !poracle-test's
// leading type token must resolve by DTS template-type name (e.g. "monster",
// "maxbattle"), not just by webhook wire type or CLI-display hyphenated
// name. Tokens are given already-lowercased, matching what
// internal/bot/parser.go's tokenize hands the command (a real "monsterChanged"
// user input arrives as "monsterchanged" — see TestResolveDTSType_MonsterChanged's
// doc comment for the same lowercasing constraint applied to resolveDTSType).
func TestResolveHookType_DTSNames(t *testing.T) {
	cases := []struct {
		token string
		want  string
	}{
		{"monster", "pokemon"},                // DTS name -> pokemon wire type
		{"monsternoiv", "pokemon"},            // DTS name -> pokemon wire type
		{"maxbattle", "max_battle"},           // DTS name (no hyphen) -> wire type
		{"monsterchanged", "monster_changed"}, // camelCase, lowercased by the parser
		{"rsvpchanges", "rsvp_changes"},
		{"questsummary", "quest_summary"},
		{"egg", "raid"}, // DTS name -> shared "raid" wire type/testdata bucket
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			got, ok := resolveHookType(tc.token)
			if !ok {
				t.Fatalf("resolveHookType(%q) ok = false, want true", tc.token)
			}
			if got != tc.want {
				t.Errorf("resolveHookType(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

// TestResolveHookType_ExistingFormsUnchanged locks in backward compatibility:
// every raw webhook-type spelling and CLI-display hyphenated derived-type
// name that already worked before task-9 must keep resolving to the exact
// same wire type.
func TestResolveHookType_ExistingFormsUnchanged(t *testing.T) {
	cases := []struct {
		token string
		want  string
	}{
		{"pokemon", "pokemon"},
		{"raid", "raid"},
		{"pokestop", "pokestop"},
		{"incident", "incident"},
		{"gym", "gym"},
		{"nest", "nest"},
		{"quest", "quest"},
		{"showcase", "showcase"},
		{"weatherchange", "weatherchange"},
		{"quest-summary", "quest_summary"},
		{"monster-changed", "monster_changed"},
		{"rsvp-changes", "rsvp_changes"},
		{"fort-update", "fort_update"},
		{"max-battle", "max_battle"},
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			got, ok := resolveHookType(tc.token)
			if !ok {
				t.Fatalf("resolveHookType(%q) ok = false, want true", tc.token)
			}
			if got != tc.want {
				t.Errorf("resolveHookType(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

// TestResolveHookType_Unknown confirms a nonsense token is rejected, exactly
// as validHooks rejected it before task-9 (the caller falls back to the
// existing "unknown type" usage message — see TestPoracleTest_UnknownType).
func TestResolveHookType_Unknown(t *testing.T) {
	if _, ok := resolveHookType("not-a-real-type"); ok {
		t.Errorf(`resolveHookType("not-a-real-type") ok = true, want false`)
	}
}

// --- End-to-end Run() coverage -------------------------------------------

// fakeTestProcessor records the webhookType and raw payload passed to
// ProcessTest, so tests can assert !poracle-test resolved the right testdata
// bucket for a DTS-name token without needing a fully-wired ProcessorService
// (dtsRenderer/renderCh/dispatcher — see cmd/processor/incident_test.go's
// TestProcessTest_DispatchesIncident doc comment for why that's impractical
// at this layer).
type fakeTestProcessor struct {
	gotWebhookType string
	gotRaw         json.RawMessage
	gotTarget      bot.TestTarget
	err            error
}

func (f *fakeTestProcessor) ProcessTest(webhookType string, raw json.RawMessage, target bot.TestTarget) error {
	f.gotWebhookType = webhookType
	f.gotRaw = raw
	f.gotTarget = target
	return f.err
}

// repoRoot walks up from this package (processor/internal/bot/commands) to
// the repo root, where fallbacks/testdata.json lives — mirrors
// cmd/processor/enrich_test.go's repoRoot helper.
func repoRoot() string {
	return filepath.Join("..", "..", "..", "..")
}

// poracleTestCmdCtx builds a CommandContext wired well enough to exercise
// PoracleTestCommand.Run end-to-end: admin (Run 🙅-blocks non-admins),
// Config.BaseDir pointing at the real repo root (loadTestdata reads
// fallbacks/testdata.json from there), a minimal ArgMatcher for the
// template:/language: prefixes, and a fakeTestProcessor to capture the
// resolved dispatch. Named distinctly from poracle_test.go's poracleTestCtx
// (that one builds a context for the unrelated !poracle registration
// command).
func poracleTestCmdCtx(t *testing.T) (*bot.CommandContext, *fakeTestProcessor) {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Config = &config.Config{BaseDir: repoRoot()}

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Moves:    map[int]*gamedata.Move{},
		Types:    map[int]*gamedata.TypeInfo{},
	}
	resolver := bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.ArgMatcher = bot.NewArgMatcher(ctx.Translations, gd, resolver, []string{"en"})

	fake := &fakeTestProcessor{}
	ctx.TestProcessor = fake
	return ctx, fake
}

func runPoracleTest(t *testing.T, ctx *bot.CommandContext, args ...string) []bot.Reply {
	t.Helper()
	cmd := &PoracleTestCommand{}
	return cmd.Run(ctx, args)
}

// TestPoracleTest_DTSNameLoadsSameEntryAsWireType is the core task-9
// scenario: "!poracle-test monster hundo" must resolve the same testdata
// bucket and dispatch the same wire type as the existing
// "!poracle-test pokemon hundo".
func TestPoracleTest_DTSNameLoadsSameEntryAsWireType(t *testing.T) {
	ctx, fake := poracleTestCmdCtx(t)
	replies := runPoracleTest(t, ctx, "monster", "hundo")
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("monster,hundo: replies = %+v, want a ✅ ack", replies)
	}
	if fake.gotWebhookType != "pokemon" {
		t.Errorf(`monster,hundo: ProcessTest webhookType = %q, want "pokemon"`, fake.gotWebhookType)
	}
	gotHundo := fake.gotRaw

	ctx2, fake2 := poracleTestCmdCtx(t)
	runPoracleTest(t, ctx2, "pokemon", "hundo")
	if fake2.gotWebhookType != "pokemon" {
		t.Errorf(`pokemon,hundo: ProcessTest webhookType = %q, want "pokemon"`, fake2.gotWebhookType)
	}
	if normalizeTestPayload(t, gotHundo) != normalizeTestPayload(t, fake2.gotRaw) {
		t.Errorf("monster,hundo and pokemon,hundo loaded different payloads:\n%s\nvs\n%s", gotHundo, fake2.gotRaw)
	}
}

// normalizeTestPayload parses a !poracle-test webhook payload and strips the
// timestamp fields that PoracleTestCommand freshens from time.Now() at dispatch
// (disappear_time for pokemon; start/end for raid/rsvp). Two loads of the same
// testdata entry are byte-identical except for these, so comparing the
// normalized forms proves "same entry" without the flaky wall-clock race that
// made this test fail when the two loads straddled a one-second boundary.
func normalizeTestPayload(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("normalizeTestPayload: unmarshal: %v", err)
	}
	for _, k := range []string{"disappear_time", "start", "end"} {
		delete(m, k)
	}
	b, err := json.Marshal(m) // map keys marshal in sorted order → canonical
	if err != nil {
		t.Fatalf("normalizeTestPayload: marshal: %v", err)
	}
	return string(b)
}

// TestNormalizeTestPayload_IgnoresFreshenedTimestamps reproduces the exact CI
// flake deterministically: two loads of the same entry that differ only in
// disappear_time (stamped from time.Now() a second apart) must normalize equal,
// while a genuine field difference must still be detected.
func TestNormalizeTestPayload_IgnoresFreshenedTimestamps(t *testing.T) {
	a := json.RawMessage(`{"pokemon_id":102,"cp":1041,"disappear_time":1784720004}`)
	b := json.RawMessage(`{"pokemon_id":102,"cp":1041,"disappear_time":1784720005}`)
	if normalizeTestPayload(t, a) != normalizeTestPayload(t, b) {
		t.Errorf("payloads differing only by disappear_time should normalize equal:\n%s\nvs\n%s",
			normalizeTestPayload(t, a), normalizeTestPayload(t, b))
	}

	// A real difference (cp) must NOT be masked by normalization.
	c := json.RawMessage(`{"pokemon_id":102,"cp":9999,"disappear_time":1784720004}`)
	if normalizeTestPayload(t, a) == normalizeTestPayload(t, c) {
		t.Error("normalizeTestPayload must not mask a real field difference (cp)")
	}
}

// TestPoracleTest_MaxbattleDTSName covers the "maxbattle" (no hyphen, DTS
// template-type name) form alongside the pre-existing "max-battle"
// (CLI-display hyphenated) form — both must resolve to the "max_battle"
// wire type and find the same "level1" testdata entry.
func TestPoracleTest_MaxbattleDTSName(t *testing.T) {
	ctx, fake := poracleTestCmdCtx(t)
	replies := runPoracleTest(t, ctx, "maxbattle", "level1")
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("maxbattle,level1: replies = %+v, want a ✅ ack", replies)
	}
	if fake.gotWebhookType != "max_battle" {
		t.Errorf(`maxbattle,level1: ProcessTest webhookType = %q, want "max_battle"`, fake.gotWebhookType)
	}

	ctx2, fake2 := poracleTestCmdCtx(t)
	runPoracleTest(t, ctx2, "max-battle", "level1")
	if fake2.gotWebhookType != "max_battle" {
		t.Errorf(`max-battle,level1: ProcessTest webhookType = %q, want "max_battle"`, fake2.gotWebhookType)
	}
}

// TestPoracleTest_MonsterChangedLowercasedDTSName covers the camelCase DTS
// name as it actually arrives at Run: lowercased by the parser (see
// resolveHookType's doc comment). "!poracle-test monsterChanged,<id>" from a
// real user reaches PoracleTestCommand.Run as the token "monsterchanged".
func TestPoracleTest_MonsterChangedLowercasedDTSName(t *testing.T) {
	ctx, fake := poracleTestCmdCtx(t)
	replies := runPoracleTest(t, ctx, "monsterchanged", "ditto-reveal")
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("monsterchanged,ditto-reveal: replies = %+v, want a ✅ ack", replies)
	}
	if fake.gotWebhookType != "monster_changed" {
		t.Errorf(`monsterchanged,ditto-reveal: ProcessTest webhookType = %q, want "monster_changed"`, fake.gotWebhookType)
	}
}

// TestPoracleTest_EggDTSName covers "egg" — a DTS template-type name that
// isn't its own wire type (fallbacks/testdata.json has no `type:"egg"`
// entries; egg samples live under `type:"raid"`, disambiguated by
// pokemon_id — see resolveDTSType's "raid" case). Both "egg" and "raid"
// must resolve to the "raid" testdata bucket and find the same "egg5" entry.
func TestPoracleTest_EggDTSName(t *testing.T) {
	ctx, fake := poracleTestCmdCtx(t)
	replies := runPoracleTest(t, ctx, "egg", "egg5")
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("egg,egg5: replies = %+v, want a ✅ ack", replies)
	}
	if fake.gotWebhookType != "raid" {
		t.Errorf(`egg,egg5: ProcessTest webhookType = %q, want "raid"`, fake.gotWebhookType)
	}
}

// TestPoracleTest_UnknownType confirms an unrecognized leading token still
// produces the pre-existing "unknown type" usage reply (no regression: an
// unresolvable token must not silently fall through to ProcessTest).
func TestPoracleTest_UnknownType(t *testing.T) {
	ctx, fake := poracleTestCmdCtx(t)
	replies := runPoracleTest(t, ctx, "not-a-real-type", "whatever")
	if len(replies) == 0 {
		t.Fatal("not-a-real-type: expected a usage reply, got none")
	}
	if replies[0].React == "✅" {
		t.Errorf("not-a-real-type: got a ✅ ack, want the unknown-type usage message")
	}
	if fake.gotWebhookType != "" {
		t.Errorf("not-a-real-type: ProcessTest was called with webhookType %q, want no call", fake.gotWebhookType)
	}
}
