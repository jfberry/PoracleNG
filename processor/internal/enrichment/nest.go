package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Nest builds enrichment fields for a nest webhook.
func (e *Enricher) Nest(nest *webhook.NestWebhook) map[string]any {
	m := make(map[string]any)

	expiration := nest.ResetTime + 7*24*60*60
	tz := geo.GetTimezone(nest.Latitude, nest.Longitude)

	m["tth"] = geo.ComputeTTH(expiration)
	m["disappearTime"] = geo.FormatTime(expiration, tz, e.TimeLayout)
	m["disappearDate"] = geo.FormatTime(expiration, tz, e.DateLayout)
	m["resetTime"] = geo.FormatTime(nest.ResetTime, tz, e.TimeLayout)
	m["resetDate"] = geo.FormatTime(nest.ResetTime, tz, e.DateLayout)

	// Game data enrichment
	if e.GameData != nil {
		monster := e.GameData.GetMonster(nest.PokemonID, nest.Form)
		if monster != nil {
			m["types"] = monster.Types
			m["color"] = e.GameData.GetTypeColor(monster.Types)
			m["typeEmojiKeys"] = e.GameData.GetTypeEmojiKeys(monster.Types)
		}
	}

	return m
}

// NestTranslate adds per-language translated fields.
func (e *Enricher) NestTranslate(base map[string]any, pokemonID, form int, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return base
	}

	m := make(map[string]any, len(base)+5)
	for k, v := range base {
		m[k] = v
	}

	tr := e.Translations.For(lang)
	TranslateMonsterNamesEng(m, e.GameData, tr, e.Translations, pokemonID, form, 0)

	monster := e.GameData.GetMonster(pokemonID, form)
	if monster != nil {
		TranslateTypeNames(m, tr, monster.Types)
	}

	return m
}
