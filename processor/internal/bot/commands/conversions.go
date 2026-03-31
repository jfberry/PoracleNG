package commands

import (
	"database/sql"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func raidAPIToTracking(a *db.RaidTrackingAPI) *db.RaidTracking {
	return &db.RaidTracking{
		ID:          a.ID,
		ProfileNo:   a.ProfileNo,
		PokemonID:   a.PokemonID,
		Level:       a.Level,
		Team:        a.Team,
		Exclusive:   bool(a.Exclusive),
		Form:        a.Form,
		Evolution:   a.Evolution,
		Move:        a.Move,
		GymID:       sql.NullString{String: a.GymID.String, Valid: a.GymID.Valid},
		Distance:    a.Distance,
		Template:    a.Template,
		Clean:       bool(a.Clean),
		Ping:        a.Ping,
		RSVPChanges: a.RSVPChanges,
	}
}

func questAPIToTracking(a *db.QuestTrackingAPI) *db.QuestTracking {
	return &db.QuestTracking{
		ID:         a.ID,
		ProfileNo:  a.ProfileNo,
		RewardType: a.RewardType,
		Reward:     a.Reward,
		Form:       a.Form,
		Amount:     a.Amount,
		Shiny:      bool(a.Shiny),
		Distance:   a.Distance,
		Template:   a.Template,
		Clean:      bool(a.Clean),
		Ping:       a.Ping,
	}
}

func invasionAPIToTracking(a *db.InvasionTrackingAPI) *db.InvasionTracking {
	return &db.InvasionTracking{
		ID:        a.ID,
		ProfileNo: a.ProfileNo,
		GruntType: a.GruntType,
		Gender:    a.Gender,
		Distance:  a.Distance,
		Template:  a.Template,
		Clean:     bool(a.Clean),
		Ping:      a.Ping,
	}
}

func lureAPIToTracking(a *db.LureTrackingAPI) *db.LureTracking {
	return &db.LureTracking{
		ID:        a.ID,
		ProfileNo: a.ProfileNo,
		LureID:    a.LureID,
		Distance:  a.Distance,
		Template:  a.Template,
		Clean:     bool(a.Clean),
		Ping:      a.Ping,
	}
}

func gymAPIToTracking(a *db.GymTrackingAPI) *db.GymTracking {
	return &db.GymTracking{
		ID:            a.ID,
		ProfileNo:     a.ProfileNo,
		Team:          a.Team,
		SlotChanges:   bool(a.SlotChanges),
		BattleChanges: bool(a.BattleChanges),
		GymID:         a.GymID,
		Distance:      a.Distance,
		Template:      a.Template,
		Clean:         bool(a.Clean),
		Ping:          a.Ping,
	}
}

func nestAPIToTracking(a *db.NestTrackingAPI) *db.NestTracking {
	return &db.NestTracking{
		ID:          a.ID,
		ProfileNo:   a.ProfileNo,
		PokemonID:   a.PokemonID,
		Form:        a.Form,
		MinSpawnAvg: a.MinSpawnAvg,
		Distance:    a.Distance,
		Template:    a.Template,
		Clean:       bool(a.Clean),
		Ping:        a.Ping,
	}
}

func fortAPIToTracking(a *db.FortTrackingAPI) *db.FortTracking {
	return &db.FortTracking{
		ID:           a.ID,
		ProfileNo:    a.ProfileNo,
		FortType:     a.FortType,
		ChangeTypes:  a.ChangeTypes,
		IncludeEmpty: bool(a.IncludeEmpty),
		Distance:     a.Distance,
		Template:     a.Template,
		Ping:         a.Ping,
	}
}

func maxbattleAPIToTracking(a *db.MaxbattleTrackingAPI) *db.MaxbattleTracking {
	return &db.MaxbattleTracking{
		ID:        a.ID,
		ProfileNo: a.ProfileNo,
		PokemonID: a.PokemonID,
		Form:      a.Form,
		Level:     a.Level,
		Move:      a.Move,
		Gmax:      int(a.Gmax),
		Evolution: a.Evolution,
		StationID: a.StationID,
		Distance:  a.Distance,
		Template:  a.Template,
		Clean:     bool(a.Clean),
		Ping:      a.Ping,
	}
}
