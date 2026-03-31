package commands

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

// WeatherCommand implements !weather — track weather changes.
// NOTE: The weather tracking table exists in the DB (weather: id, ping, template,
// clean, condition, cell, uid, profile_no) but there are no Go tracking API
// functions (SelectWeather, InsertWeather) yet. This is a stub that informs users
// the command is not yet available in the processor. Weather tracking is handled
// by the alerter for now.
type WeatherCommand struct{}

func (c *WeatherCommand) Name() string      { return "cmd.weather" }
func (c *WeatherCommand) Aliases() []string { return nil }

func (c *WeatherCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	return []bot.Reply{{React: "🙅", Text: "Weather tracking is not yet available via this command. Use the alerter command instead."}}
}
