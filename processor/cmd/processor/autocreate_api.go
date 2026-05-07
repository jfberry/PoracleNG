package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot"
)

// autocreateRunRequest is the POST /api/autocreate/run body.
type autocreateRunRequest struct {
	Rule     string `json:"rule"` // empty → all rules
	DryRun   bool   `json:"dry_run"`
	Reset    bool   `json:"reset"`
	Removals bool   `json:"removals"`
	Force    bool   `json:"force"`
}

// handleAutocreateRun implements POST /api/autocreate/run. Authenticated
// via the same x-poracle-secret middleware applied to the /api/* group.
//
// Body:  {"rule": "uk-areas", "dry_run": false, ...}  ("rule" empty → all rules)
// Reply: {"status": "ok", "rules": [SyncOneRuleResult, ...]}
func handleAutocreateRun(cfg *config.Config, bot *discordbot.Bot) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req autocreateRunRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		if bot == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "discord bot not running"})
			return
		}

		rules := cfg.Autocreate.Rules
		if req.Rule != "" {
			var matched []config.AutocreateRule
			for _, r := range rules {
				if r.Name == req.Rule {
					matched = append(matched, r)
					break
				}
			}
			if len(matched) == 0 {
				c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "rule not found"})
				return
			}
			rules = matched
		}
		if len(rules) == 0 {
			c.JSON(http.StatusOK, gin.H{"status": "ok", "rules": []discordbot.SyncOneRuleResult{}})
			return
		}

		opts := discordbot.SyncRuleOptions{
			DryRun:   req.DryRun,
			Reset:    req.Reset,
			Removals: req.Removals,
			Force:    req.Force,
		}

		results := make([]discordbot.SyncOneRuleResult, 0, len(rules))
		session := bot.Session()
		for _, r := range rules {
			results = append(results, bot.SyncOneRule(session, r, opts))
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "rules": results})
	}
}
