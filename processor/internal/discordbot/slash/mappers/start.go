package mappers

import "github.com/bwmarrin/discordgo"

// Start maps /start to its empty-token text-command form.
// /start takes no options — it resumes alert delivery for the invoker.
func Start(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	return nil, nil
}

// Stop maps /stop similarly — pauses alert delivery, no options.
func Stop(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	return nil, nil
}

func init() {
	registry["start"] = Start
	registry["stop"] = Stop
}
