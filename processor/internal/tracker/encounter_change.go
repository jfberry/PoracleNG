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
	}
	return "none"
}
