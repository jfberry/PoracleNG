package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/dts"
)

// HandleDTSEmoji returns the merged emoji map for the requested platform, or
// the full set when no platform is given.
//
// GET /api/dts/emoji                  → {"defaults": {...}, "platforms": {discord: {...}, telegram: {...}}}
// GET /api/dts/emoji?platform=discord → {"platform": "discord", "emoji": {key: value, ...}}
//
// The flat per-platform map is the merge of defaults (from util.json) overlaid
// with that platform's overrides (from emoji.json), so editors can resolve
// {{getEmoji "..."}} the same way the renderer does.
func HandleDTSEmoji(emoji *dts.EmojiLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		platform := c.Query("platform")
		if platform != "" {
			c.JSON(http.StatusOK, gin.H{
				"status":   "ok",
				"platform": platform,
				"emoji":    emoji.MergedFor(platform),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"defaults":  emoji.Defaults(),
			"platforms": emoji.PlatformOverrides(),
		})
	}
}
