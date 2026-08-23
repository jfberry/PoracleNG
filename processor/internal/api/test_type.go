package api

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/dtsmap"
)

// resolveTestWireType resolves POST /api/test's `type` field — a DTS
// template-type name (e.g. "monster", "monsterChanged", "maxbattle"), a raw
// webhook wire type (e.g. "pokemon", "monster_changed"), or the CLI-display
// hyphenated spelling (e.g. "fort-update", "max-battle") — to the wire type
// bot.TestProcessor.ProcessTest's switch (cmd/processor/test.go) dispatches
// on. Resolution is case-insensitive via dtsmap.AliasFold, mirroring
// !poracle-test's resolveHookType (internal/bot/commands/poracletest.go) so a
// client can pass any of the three spellings interchangeably, same as the bot
// command — without it, a JSON caller sending the natural DTS name
// ("monsterChanged") got ProcessTest's raw "unsupported test webhook type"
// 500, since ProcessTest's switch only recognizes wire spellings.
//
// The "pokestop" handling deliberately does NOT mirror resolveHookType's:
// resolveHookType resolves "invasion"/"lure" to "pokestop" because
// !poracle-test needs the WIRE TYPE THAT MATCHES TESTDATA.JSON'S BUCKET KEY
// (testdata.json only has "pokestop"-typed entries, never "invasion"- or
// "lure"-typed ones — see resolveHookType's doc comment). This function feeds
// ProcessTest's switch directly instead (no testdata.json lookup happens on
// this HTTP path — the caller supplies the raw webhook body themselves), and
// that switch already has its own direct "invasion" case (mirrors
// enrichForType's identically-motivated pokestop-rewrite guard,
// cmd/processor/enrich.go) — so collapsing "invasion" to "pokestop" would be
// a no-op at best. Collapsing "lure" to "pokestop" would actually CHANGE
// behavior: ProcessTest's switch has no "lure" case at all today, so a
// literal "lure" input already falls through to "unsupported webhook type"
// pre-fix; fixing that gap is a separate concern outside this task's scope.
// Both are therefore left resolving to their own token, unaffected by this
// change either way — "pokestop" itself (which has no dtsmap identity entry
// by design; see dtsmap's doc comment) is special-cased to keep resolving to
// itself, since it's a valid raw wire type ProcessTest's own "pokestop" case
// already peeks the payload to disambiguate.
//
// An unresolved name is returned unchanged — the resolution step must never
// mask an unknown type behind a different error; ProcessTest's own
// "unsupported test webhook type: %s" case still fires with the caller's
// original string.
func resolveTestWireType(name string) string {
	lower := strings.ToLower(name)
	if lower == "pokestop" {
		return "pokestop"
	}

	src, ok := dtsmap.AliasFold(name)
	if !ok {
		return name
	}
	if src.WebhookType == "pokestop" {
		// invasion/lure — see doc comment above for why these stay as their
		// own token instead of collapsing to "pokestop".
		return lower
	}
	return src.WebhookType
}
