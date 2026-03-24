// Package rowtext generates human-readable descriptions of tracking rules.
// Ported from the JS tracked.js functions in the alerter.
package rowtext

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/scanner"
)

// Generator produces row text strings for tracking rules.
type Generator struct {
	GD                  *gamedata.GameData
	Translations        *i18n.Bundle
	DefaultTemplateName string
	Scanner             scanner.Scanner
}

// standardText appends template and clean indicators.
func standardText(tr *i18n.Translator, template, defaultTemplate string, clean bool) string {
	var text string
	if template != defaultTemplate {
		text += " " + tr.Tf("tracking.template_fmt", template)
	}
	if clean {
		text += " " + tr.T("tracking.clean")
	}
	return text
}

// translateMonsterName returns the translated pokemon name and form name.
func translateMonsterName(tr *i18n.Translator, gd *gamedata.GameData, pokemonID, form int) (name, formName string) {
	if pokemonID == 0 {
		return tr.T("tracking.everything"), ""
	}

	mon := gd.GetMonster(pokemonID, form)
	if mon == nil {
		return fmt.Sprintf("%s %d", tr.T("tracking.unknown_monster"), pokemonID), fmt.Sprintf("%d", form)
	}

	name = tr.T(gamedata.PokemonTranslationKey(pokemonID))
	formName = tr.T(gamedata.FormTranslationKey(form))

	if formName == gamedata.FormTranslationKey(form) {
		// Translation key returned as-is means no translation found
		formName = ""
	} else if form == 0 && enrichment.IsNormalForm(formName) {
		formName = ""
	} else if enrichment.IsNormalForm(formName) {
		formName = ""
	}

	return name, formName
}

// translateMoveName returns a translated "MoveName/TypeName" string, or "" if
// the move is 0 or 9000 (wildcard).
func translateMoveName(tr *i18n.Translator, gd *gamedata.GameData, moveID int) string {
	if moveID == 0 || moveID == 9000 {
		return ""
	}
	move := gd.GetMove(moveID)
	if move == nil {
		return ""
	}
	return tr.T(gamedata.MoveTranslationKey(moveID)) + "/" + tr.T(gamedata.TypeTranslationKey(move.TypeID))
}

// resolveGymName looks up a gym name from the scanner, falling back to the raw ID.
func resolveGymName(s scanner.Scanner, gymID string) string {
	if gymID == "" {
		return ""
	}
	if s == nil {
		return gymID
	}
	name, err := s.GetGymName(gymID)
	if err != nil || name == "" {
		return gymID
	}
	return name
}

// resolvePokestopName looks up a pokestop/station name from the scanner, falling back to the raw ID.
func resolvePokestopName(s scanner.Scanner, stationID string) string {
	if stationID == "" {
		return ""
	}
	if s == nil {
		return stationID
	}
	name, err := s.GetPokestopName(stationID)
	if err != nil || name == "" {
		return stationID
	}
	return name
}

// rsvpText translates the rsvp_changes value.
func rsvpText(tr *i18n.Translator, rsvpChanges int) string {
	switch rsvpChanges {
	case 0:
		return tr.T("tracking.rsvp_without")
	case 1:
		return tr.T("tracking.rsvp_including")
	case 2:
		return tr.T("tracking.rsvp_only")
	default:
		return ""
	}
}

// ucFirst returns s with its first rune uppercased.
func ucFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
