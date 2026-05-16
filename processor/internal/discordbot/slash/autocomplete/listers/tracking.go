// Package listers contains concrete UserStateLister implementations used by
// the slash-command autocomplete registry. Each lister is a pure function
// over BotDeps + userID + UserStateHint and returns []autocomplete.Choice.
package listers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// ListTracking enumerates the user's tracking rules for the subtype named in
// hint.Subtype. Each Choice label is the rowtext description plus an
// "[id:N]" suffix; the Value is the UID as a decimal string so the caller
// can pass it to a delete handler. Subtypes outside the known set return
// (nil, nil) so the caller can fall through to FilterAndCap without error.
//
// The lister calls Humans.GetLite to discover the user's current profile
// number. A nil human (unregistered user) is treated as profile 0 — the
// caller's permission checks decide whether anything is actually shown.
func ListTracking(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	profileNo := 0
	if deps.Humans != nil {
		human, err := deps.Humans.GetLite(userID)
		if err != nil {
			return nil, err
		}
		if human != nil {
			profileNo = human.CurrentProfileNo
		}
	}

	tr := translatorFor(deps, userID)

	switch hint.Subtype {
	case "pokemon":
		rows, err := deps.Tracking.Monsters.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.MonsterRowText(tr, toMonsterTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "raid":
		rows, err := deps.Tracking.Raids.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.RaidRowText(tr, toRaidTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "egg":
		rows, err := deps.Tracking.Eggs.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.EggRowText(tr, toEggTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "quest":
		rows, err := deps.Tracking.Quests.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.QuestRowText(tr, toQuestTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "invasion":
		rows, err := deps.Tracking.Invasions.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.InvasionRowText(tr, toInvasionTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "lure":
		rows, err := deps.Tracking.Lures.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.LureRowText(tr, toLureTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "nest":
		rows, err := deps.Tracking.Nests.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.NestRowText(tr, toNestTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "gym":
		rows, err := deps.Tracking.Gyms.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.GymRowText(tr, toGymTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "fort":
		rows, err := deps.Tracking.Forts.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.FortUpdateRowText(tr, toFortTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	case "maxbattle":
		rows, err := deps.Tracking.Maxbattles.SelectByIDProfile(userID, profileNo)
		if err != nil {
			return nil, err
		}
		out := make([]autocomplete.Choice, 0, len(rows))
		for i := range rows {
			desc := deps.RowText.MaxbattleRowText(tr, toMaxbattleTracking(&rows[i]))
			out = append(out, buildChoice(desc, rows[i].UID))
		}
		return out, nil

	default:
		return nil, nil
	}
}

// translatorFor resolves the translator for the given user. Falls back to
// the bundle's default locale when the user is unregistered or has no
// language set.
func translatorFor(deps *bot.BotDeps, userID string) *i18n.Translator {
	lang := ""
	if deps.Humans != nil {
		if h, err := deps.Humans.GetLite(userID); err == nil && h != nil {
			lang = h.Language
		}
	}
	if deps.Cfg != nil && lang == "" {
		lang = deps.Cfg.General.Locale
	}
	return deps.Translations.For(lang)
}

// buildChoice composes a Choice from a description and UID, attaching the
// "[id:N]" suffix the caller can rely on for delete actions and which
// FilterAndCap preserves when truncating long labels.
func buildChoice(desc string, uid int64) autocomplete.Choice {
	return autocomplete.Choice{
		Label: fmt.Sprintf("%s [id:%d]", desc, uid),
		Value: strconv.FormatInt(uid, 10),
	}
}

// --- API -> Tracking converters (mirror the unexported ones in api/) ---

func toMonsterTracking(api *db.MonsterTrackingAPI) *db.MonsterTracking {
	return &db.MonsterTracking{
		ID:               api.ID,
		ProfileNo:        api.ProfileNo,
		Ping:             api.Ping,
		Clean:            api.Clean,
		Distance:         api.Distance,
		Template:         api.Template,
		PokemonID:        api.PokemonID,
		Form:             api.Form,
		MinIV:            api.MinIV,
		MaxIV:            api.MaxIV,
		MinCP:            api.MinCP,
		MaxCP:            api.MaxCP,
		MinLevel:         api.MinLevel,
		MaxLevel:         api.MaxLevel,
		ATK:              api.ATK,
		DEF:              api.DEF,
		STA:              api.STA,
		MaxATK:           api.MaxATK,
		MaxDEF:           api.MaxDEF,
		MaxSTA:           api.MaxSTA,
		Gender:           api.Gender,
		MinWeight:        api.MinWeight,
		MaxWeight:        api.MaxWeight,
		MinTime:          api.MinTime,
		Rarity:           api.Rarity,
		MaxRarity:        api.MaxRarity,
		Size:             api.Size,
		MaxSize:          api.MaxSize,
		PVPRankingLeague: api.PVPRankingLeague,
		PVPRankingBest:   api.PVPRankingBest,
		PVPRankingWorst:  api.PVPRankingWorst,
		PVPRankingMinCP:  api.PVPRankingMinCP,
		PVPRankingCap:    api.PVPRankingCap,
	}
}

func toRaidTracking(api *db.RaidTrackingAPI) *db.RaidTracking {
	return &db.RaidTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       api.Clean,
		Distance:    api.Distance,
		Template:    api.Template,
		Team:        api.Team,
		PokemonID:   api.PokemonID,
		Form:        api.Form,
		Level:       api.Level,
		Exclusive:   bool(api.Exclusive),
		Move:        api.Move,
		Evolution:   api.Evolution,
		GymID:       api.GymID.NullString,
		RSVPChanges: api.RSVPChanges,
	}
}

func toEggTracking(api *db.EggTrackingAPI) *db.EggTracking {
	return &db.EggTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       api.Clean,
		Distance:    api.Distance,
		Template:    api.Template,
		Team:        api.Team,
		Level:       api.Level,
		Exclusive:   bool(api.Exclusive),
		GymID:       api.GymID.NullString,
		RSVPChanges: api.RSVPChanges,
	}
}

func toQuestTracking(api *db.QuestTrackingAPI) *db.QuestTracking {
	return &db.QuestTracking{
		ID:         api.ID,
		ProfileNo:  api.ProfileNo,
		Ping:       api.Ping,
		Clean:      api.Clean,
		Distance:   api.Distance,
		Template:   api.Template,
		RewardType: api.RewardType,
		Reward:     api.Reward,
		Form:       api.Form,
		Shiny:      bool(api.Shiny),
		Amount:     api.Amount,
	}
}

func toInvasionTracking(api *db.InvasionTrackingAPI) *db.InvasionTracking {
	return &db.InvasionTracking{
		ID:        api.ID,
		ProfileNo: api.ProfileNo,
		Ping:      api.Ping,
		Clean:     api.Clean,
		Distance:  api.Distance,
		Template:  api.Template,
		Gender:    api.Gender,
		GruntType: api.GruntType,
	}
}

func toLureTracking(api *db.LureTrackingAPI) *db.LureTracking {
	return &db.LureTracking{
		ID:        api.ID,
		ProfileNo: api.ProfileNo,
		Ping:      api.Ping,
		Clean:     api.Clean,
		Distance:  api.Distance,
		Template:  api.Template,
		LureID:    api.LureID,
	}
}

func toNestTracking(api *db.NestTrackingAPI) *db.NestTracking {
	return &db.NestTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       api.Clean,
		Distance:    api.Distance,
		Template:    api.Template,
		PokemonID:   api.PokemonID,
		MinSpawnAvg: api.MinSpawnAvg,
		Form:        api.Form,
	}
}

func toGymTracking(api *db.GymTrackingAPI) *db.GymTracking {
	return &db.GymTracking{
		ID:            api.ID,
		ProfileNo:     api.ProfileNo,
		Ping:          api.Ping,
		Clean:         api.Clean,
		Distance:      api.Distance,
		Template:      api.Template,
		Team:          api.Team,
		SlotChanges:   bool(api.SlotChanges),
		BattleChanges: bool(api.BattleChanges),
		GymID:         api.GymID,
	}
}

func toFortTracking(api *db.FortTrackingAPI) *db.FortTracking {
	return &db.FortTracking{
		ID:           api.ID,
		ProfileNo:    api.ProfileNo,
		Ping:         api.Ping,
		Distance:     api.Distance,
		Template:     api.Template,
		FortType:     api.FortType,
		IncludeEmpty: bool(api.IncludeEmpty),
		ChangeTypes:  api.ChangeTypes,
	}
}

func toMaxbattleTracking(api *db.MaxbattleTrackingAPI) *db.MaxbattleTracking {
	return &db.MaxbattleTracking{
		ID:        api.ID,
		ProfileNo: api.ProfileNo,
		Ping:      api.Ping,
		Clean:     api.Clean,
		Distance:  api.Distance,
		Template:  api.Template,
		PokemonID: api.PokemonID,
		Form:      api.Form,
		Level:     api.Level,
		Move:      api.Move,
		Gmax:      api.Gmax,
		Evolution: api.Evolution,
		StationID: api.StationID,
	}
}
