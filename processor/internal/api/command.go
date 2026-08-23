package api

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

type commandRequest struct {
	Text      string `json:"text"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Platform  string `json:"platform"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	IsDM      bool   `json:"is_dm"`
}

type commandResponse struct {
	Status  string      `json:"status"`
	Replies []bot.Reply `json:"replies"`
}
