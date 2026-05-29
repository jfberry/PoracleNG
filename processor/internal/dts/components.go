package dts

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/buttons"
)

// CustomIDPrefix is the namespace used for every poracle-emitted button.
// Identifies our buttons in the gateway InteractionCreate handler so we
// don't accidentally claim clicks meant for some other bot integration
// in the same channel. Format: poracle:btn:<actionID>.
const CustomIDPrefix = "poracle:btn:"

// MaxButtonsPerMessage is Discord's hard cap on components per message
// (5 buttons per action row × 5 action rows). Operators authoring beyond
// this don't get a more useful failure mode — Discord rejects the whole
// message. We truncate with a warn so a single overflow doesn't lose the
// entire alert.
const MaxButtonsPerMessage = 25

// discordButtonStyle maps the operator-friendly style strings to Discord's
// numeric style codes. Reference:
//
//	1 = primary, 2 = secondary, 3 = success, 4 = danger, 5 = link
//
// (We don't expose link style — link buttons can't have a custom_id and
// therefore can't dispatch to our handlers.)
var discordButtonStyle = map[string]int{
	buttons.StylePrimary:   1,
	buttons.StyleSecondary: 2,
	buttons.StyleSuccess:   3,
	buttons.StyleDanger:    4,
}

// InjectDiscordComponents takes a rendered message body (Discord JSON) and
// returns it with a `components` array appended for the given buttons.
// Buttons that fail the AppliesTo or show_if check are dropped. Returns
// the original body unchanged when no buttons should be attached, when
// the body isn't a JSON object, or when components emission is disabled
// for the platform.
//
// Operates on raw JSON bytes (not on a templated struct) because the
// renderer's output is already serialised — re-parsing into a typed
// struct would be expensive and would have to handle every Discord
// embed shape. A surgical addition to the top-level object is enough.
//
// view is the resolved LayeredView used to evaluate show_if expressions
// against. Pass the same one the renderer used so the predicate sees
// the fields the operator wrote into their template.
//
// recipientIsAdmin gates render-time hiding of admin-only buttons on DM
// destinations: when targetType=="dm" and !recipientIsAdmin, buttons
// with visible_to="admin" are dropped here so non-admin recipients
// never see a button they couldn't use. Channel destinations can't be
// filtered per-viewer; the click-time gate remains the only enforcement
// there.
func InjectDiscordComponents(messageBody json.RawMessage, defs []buttons.Def, view any, targetType string, recipientIsAdmin bool, evalShowIf ShowIfEvaluator, logReference string) json.RawMessage {
	if len(defs) == 0 || !json.Valid(messageBody) {
		return messageBody
	}

	// Decode the top-level object so we can write the components key
	// back in without disturbing existing fields.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(messageBody, &top); err != nil {
		// Body isn't an object — likely a string content (e.g. compact
		// Telegram-shaped message that's not really discord-shaped).
		// We don't add components to non-object messages.
		return messageBody
	}

	// Filter buttons by applies_to + show_if + per-recipient visibility
	// first so we know whether any survive before serialising
	// components JSON.
	var kept []buttons.Def
	for _, def := range defs {
		if !def.AppliesToTarget(targetType) {
			continue
		}
		if targetType == "dm" && def.EffectiveVisibility() == buttons.VisibleAdmin && !recipientIsAdmin {
			continue
		}
		if def.ShowIf != "" && evalShowIf != nil {
			if ok, err := evalShowIf(def.ShowIf, view); err != nil {
				log.Warnf("[%s] dts: show_if eval for button %q: %v — dropping", logReference, def.ID, err)
				continue
			} else if !ok {
				continue
			}
		}
		kept = append(kept, def)
		if len(kept) >= MaxButtonsPerMessage {
			log.Warnf("[%s] dts: button list exceeds Discord's per-message cap (%d); truncating", logReference, MaxButtonsPerMessage)
			break
		}
	}
	if len(kept) == 0 {
		return messageBody
	}

	components, err := buildDiscordComponents(kept)
	if err != nil {
		log.Warnf("[%s] dts: build components: %v — sending without buttons", logReference, err)
		return messageBody
	}

	top["components"] = components
	out, err := json.Marshal(top)
	if err != nil {
		log.Warnf("[%s] dts: marshal message with components: %v — sending without buttons", logReference, err)
		return messageBody
	}
	return out
}

// ShowIfEvaluator is the renderer's hook for evaluating a Handlebars
// expression against a view. Injected at construction so the dts package
// doesn't take a hard dependency on raymond at this layer.
type ShowIfEvaluator func(expr string, view any) (bool, error)

// buildDiscordComponents groups defs into action rows (5 per row max)
// and emits the components array Discord expects:
//
//	[{type: 1, components: [<button>, <button>...]}, ...]
//
// One row is always emitted (the empty case is filtered upstream).
func buildDiscordComponents(defs []buttons.Def) (json.RawMessage, error) {
	const buttonsPerRow = 5
	var rows []map[string]any
	for i := 0; i < len(defs); i += buttonsPerRow {
		end := min(i+buttonsPerRow, len(defs))
		row := map[string]any{
			"type":       1,
			"components": []map[string]any{},
		}
		btns := make([]map[string]any, 0, end-i)
		for _, def := range defs[i:end] {
			btns = append(btns, map[string]any{
				"type":      2, // Discord Button
				"style":     discordButtonStyle[def.EffectiveStyle()],
				"label":     def.Label,
				"custom_id": CustomIDPrefix + def.ID,
			})
		}
		row["components"] = btns
		rows = append(rows, row)
	}
	return json.Marshal(rows)
}

// SplitCustomID parses a poracle button custom_id back into its actionID.
// Returns empty string + false for any value not produced by
// InjectDiscordComponents (other bots' buttons, malformed values, etc.).
func SplitCustomID(customID string) (actionID string, ok bool) {
	if !strings.HasPrefix(customID, CustomIDPrefix) {
		return "", false
	}
	actionID = strings.TrimPrefix(customID, CustomIDPrefix)
	if actionID == "" {
		return "", false
	}
	return actionID, true
}

// FormatComponentError emits a uniform error string for logs when a
// component-related operation fails on a known boundary (DTS schema,
// Discord API). Keeps log lines greppable.
func FormatComponentError(stage, detail string) string {
	return fmt.Sprintf("dts/components: %s: %s", stage, detail)
}
