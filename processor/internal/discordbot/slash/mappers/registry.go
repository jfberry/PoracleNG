package mappers

import "github.com/bwmarrin/discordgo"

// Mapper transforms slash command options into text-command tokens.
// Returns ([]string, nil) on success or (nil, *MapperError) on validation failure.
type Mapper func(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error)

// MapperError is a typed mapper error carrying an i18n key + format args.
// The dispatcher translates this to the user's language before sending.
type MapperError struct {
	Key  string
	Args []any
}

func (e *MapperError) Error() string { return e.Key }

// Lookup returns the mapper registered for a given slash command name.
// Returns nil for unknown commands.
func Lookup(commandName string) Mapper {
	return registry[commandName]
}

// registry maps slash command name → mapper. Seeded with the always-on
// /version mapper; additional commands register themselves via init() in
// their respective files.
var registry = map[string]Mapper{
	"version": Version,
}
