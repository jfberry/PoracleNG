package api

import (
	"context"
	"sort"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// costumeEntry is one costume in the masterdata costumes map.
type costumeEntry struct {
	ID   int    `json:"id" doc:"Costume id (game-master), the value stored in a tracking rule's costume field"`
	Name string `json:"name" doc:"Display name in the requested locale, falling back to the raw masterfile name when the costume_<id> key is untranslated"`
}

// masterdataCostumesInput carries the optional locale query param, matching
// GET /masterdata/monsters.
type masterdataCostumesInput struct {
	Locale string `query:"locale"`
}

// masterdataCostumesOutput is keyed by costume id (string) → costumeEntry. The
// dynamic keys make huma emit an object-with-additionalProperties schema that
// documents the VALUE shape while leaving keys arbitrary — the same treatment
// masterdataGruntsOutput gets, and directly usable as an id → name lookup.
type masterdataCostumesOutput struct {
	Body map[string]costumeEntry
}

// RegisterMasterdataCostumes registers GET /api/masterdata/costumes.
//
// Costume names existed only inside the processor before this: the
// costume_<id> gamelocale keys are fully translated and gd.Costumes is loaded
// at startup, but no endpoint served either, so a client holding a costume id
// (from a tracking rule, or from the activity endpoint) could not render it in
// ANY language. See #212.
func RegisterMasterdataCostumes(api huma.API, gd *gamedata.GameData, translations *i18n.Bundle) {
	huma.Register(api, huma.Operation{
		OperationID: "get-masterdata-costumes", Method: "GET", Path: "/masterdata/costumes",
		Summary: "All costumes with localised names",
		Description: "Returns costumes keyed by costume id (an empty object when game data is unavailable). " +
			"`?locale=` selects the language (default `en`); a costume with no translation in that locale falls " +
			"back to the raw masterfile name rather than the bare translation key.",
		Tags:     []string{"masterdata"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *masterdataCostumesInput) (*masterdataCostumesOutput, error) {
		if gd == nil {
			return &masterdataCostumesOutput{Body: map[string]costumeEntry{}}, nil
		}
		out := make(map[string]costumeEntry, len(gd.Costumes))
		locale := in.Locale
		if locale == "" {
			locale = "en"
		}
		tr := translations.For(locale)

		ids := make([]int, 0, len(gd.Costumes))
		for id := range gd.Costumes {
			ids = append(ids, id)
		}
		sort.Ints(ids)

		for _, id := range ids {
			out[strconv.Itoa(id)] = costumeEntry{ID: id, Name: costumeDisplayName(gd, tr, id)}
		}
		return &masterdataCostumesOutput{Body: out}, nil
	})
}

// costumeDisplayName resolves a costume id to a display name, mirroring
// info.go's costumeName and rowtext's monster costume branch: the translated
// costume_<id> key, else the raw masterfile name, else the key itself.
func costumeDisplayName(gd *gamedata.GameData, tr *i18n.Translator, id int) string {
	key := gamedata.CostumeTranslationKey(id)
	name := tr.T(key)
	if name != key {
		return name
	}
	if gd != nil {
		if info, ok := gd.Costumes[id]; ok && info.Name != "" {
			return info.Name
		}
	}
	return name
}
