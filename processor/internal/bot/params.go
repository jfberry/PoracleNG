package bot

// ParamType identifies what kind of argument a parameter matches.
type ParamType int

const (
	ParamPrefixRange   ParamType = iota // iv100, iv50-100, cp2500-3000, level20-30, atk15
	ParamPrefixSingle  ParamType = iota // d500, t60, gen3, cap50, miniv90, maxcp3000
	ParamPrefixString  ParamType = iota // form:alola, template:2, move:hydropump, name:foo
	ParamKeyword       ParamType = iota // remove, everything, clean, ex, shiny, individually
	ParamTeam          ParamType = iota // valor/red, mystic/blue, instinct/yellow, harmony/gray
	ParamGender        ParamType = iota // male, female, genderless
	ParamPokemonName   ParamType = iota // pikachu, relaxo, 25, "mr. mime"
	ParamTypeName      ParamType = iota // grass, fire, dragon
	ParamLureType      ParamType = iota // glacial, mossy, magnetic, rainy, sparkly, normal
	ParamRaidLevelName ParamType = iota // legendary, mega, shadow, ultra beast
	ParamPVPLeague     ParamType = iota // great5, ultra10-50, greathigh3, greatcp1400
	ParamLatLon        ParamType = iota // 51.28,1.08
)

// ParamDef defines a parameter that a command accepts.
type ParamDef struct {
	Type ParamType
	// Key is the translation identifier for the prefix or keyword.
	// For PrefixRange/PrefixSingle: "arg.prefix.iv", "arg.prefix.d", etc.
	// For PrefixString: "arg.prefix.form", "arg.prefix.template", etc.
	// For Keyword: "arg.remove", "arg.everything", etc.
	// For Team/Gender/PokemonName/TypeName/LureType/RaidLevelName/PVPLeague/LatLon: unused.
	Key string
}

// Range holds a min-max pair from a range parameter like iv50-100 or level20-30.
// When the user provides a single value (e.g. iv50), only Min is set and HasMax is false.
type Range struct {
	Min    int
	Max    int
	HasMax bool // true when user explicitly provided a max (e.g. iv50-100)
}

// PVPFilter holds PVP league ranking parameters.
type PVPFilter struct {
	Best  int // best (lowest) rank, default 1
	Worst int // worst (highest) rank
	MinCP int // minimum CP for PVP
}

// LatLon holds parsed coordinates.
type LatLon struct {
	Lat float64
	Lon float64
}

// ParsedArgs holds structured results from argument matching.
type ParsedArgs struct {
	// Ranges from PrefixRange params: "iv" → {Min:50, Max:100}
	Ranges map[string]Range
	// Singles from PrefixSingle params: "d" → 500, "gen" → 3
	Singles map[string]int
	// Strings from PrefixString params: "form" → "alola", "template" → "2"
	Strings map[string]string
	// Keywords matched: "arg.remove" → true, "arg.clean" → true
	Keywords map[string]bool
	// Team ID (0=harmony, 1=mystic, 2=valor, 3=instinct, 4=unset)
	Team int
	// Gender (0=unset, 1=male, 2=female, 3=genderless)
	Gender int
	// Pokemon matched by name/ID
	Pokemon []ResolvedPokemon
	// Type IDs matched by name
	Types []int
	// Lure type ID (0=any/unset, 501-506)
	LureType int
	// Raid levels matched by name (e.g. "legendary" → [5])
	RaidLevels []int
	// PVP filters by league key: "great" → {Best:1, Worst:5, MinCP:0}
	PVP map[string]PVPFilter
	// Parsed coordinates
	Coords *LatLon
	// Tokens that matched nothing
	Unrecognized []string
}

// NewParsedArgs creates an empty ParsedArgs with initialized maps.
func NewParsedArgs() *ParsedArgs {
	return &ParsedArgs{
		Ranges:   make(map[string]Range),
		Singles:  make(map[string]int),
		Strings:  make(map[string]string),
		Keywords: make(map[string]bool),
		PVP:      make(map[string]PVPFilter),
		Team:     4, // default: unset
	}
}

// HasKeyword returns true if the given identifier key was matched.
func (p *ParsedArgs) HasKeyword(key string) bool {
	return p.Keywords[key]
}
