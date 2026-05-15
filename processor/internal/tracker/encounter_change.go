package tracker

// ChangeType describes which dimension of an encounter changed.
type ChangeType int

const (
	ChangeNone ChangeType = iota
	ChangeSpecies
	ChangeForm
	ChangeGender
	ChangeEncountered  // non-IV sighting filled in with CP/IVs
	ChangeWeatherBoost // post-encounter weather change that moved CP
	ChangeStats        // raw IV (atk/def/sta) drift between two encountered webhooks
)

func (c ChangeType) String() string {
	switch c {
	case ChangeSpecies:
		return "species"
	case ChangeForm:
		return "form"
	case ChangeGender:
		return "gender"
	case ChangeEncountered:
		return "encountered"
	case ChangeWeatherBoost:
		return "weather_boost"
	case ChangeStats:
		return "stats"
	}
	return "none"
}
