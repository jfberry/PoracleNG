package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// Version is the processor version reported by /api/config/poracleWeb.
// It can be overridden at build time via -ldflags.
var Version = "0.0.0"

// HandleConfigPoracleWeb returns a handler for GET /api/config/poracleWeb.
// It returns the configuration subset that PoracleWeb needs for its UI.
func HandleConfigPoracleWeb(cfg *config.Config) http.HandlerFunc {
	// Pre-compute the disabled hooks list from config flags.
	type hookFlag struct {
		Name    string
		Disable bool
	}
	hookTypes := []hookFlag{
		{"pokemon", cfg.General.DisablePokemon},
		{"raid", cfg.General.DisableRaid},
		{"pokestop", cfg.General.DisablePokestop},
		{"invasion", cfg.General.DisableInvasion},
		{"lure", cfg.General.DisableLure},
		{"quest", cfg.General.DisableQuest},
		{"weather", cfg.General.DisableWeather},
		{"nest", cfg.General.DisableNest},
		{"gym", cfg.General.DisableGym},
		{"maxbattle", cfg.General.DisableMaxBattle},
	}
	disabledHooks := make([]string, 0)
	for _, h := range hookTypes {
		if h.Disable {
			disabledHooks = append(disabledHooks, h.Name)
		}
	}

	// Resolve default template name to a string.
	defaultTemplateName := "1"
	if cfg.General.DefaultTemplateName != nil {
		defaultTemplateName = fmt.Sprintf("%v", cfg.General.DefaultTemplateName)
	}

	// pvpCaps defaults to [50] if empty.
	pvpCaps := cfg.PVP.LevelCaps
	if len(pvpCaps) == 0 {
		pvpCaps = []int{50}
	}

	// pvpRequiresMinCp matches alerter logic: forceMinCp && dataSource === "webhook".
	pvpRequiresMinCp := cfg.PVP.ForceMinCP && cfg.PVP.DataSource == "webhook"

	// channelNotesContainsCategory matches alerter logic.
	channelNotesContainsCategory := cfg.Discord.CheckRole && cfg.Reconciliation.Discord.UpdateChannelNotes

	// Resolve static key — use first entry if configured as an array.
	staticKey := ""
	if len(cfg.Geocoding.StaticKey) > 0 {
		staticKey = cfg.Geocoding.StaticKey[0]
	}

	// Build the response once since config is immutable after load.
	resp := map[string]any{
		"status":                       "ok",
		"version":                      Version,
		"locale":                       cfg.General.Locale,
		"prefix":                       cfg.Discord.Prefix,
		"providerURL":                  cfg.Geocoding.ProviderURL,
		"addressFormat":                cfg.Locale.AddressFormat,
		"staticKey":                    staticKey,
		"pvpFilterMaxRank":             cfg.PVP.PVPFilterMaxRank,
		"pvpFilterGreatMinCP":          cfg.PVP.PVPFilterGreatMinCP,
		"pvpFilterUltraMinCP":          cfg.PVP.PVPFilterUltraMinCP,
		"pvpFilterLittleMinCP":         cfg.PVP.PVPFilterLittleMinCP,
		"pvpLittleLeagueAllowed":       true,
		"pvpCaps":                      pvpCaps,
		"pvpRequiresMinCp":             pvpRequiresMinCp,
		"defaultPvpCap":                cfg.Tracking.DefaultUserTrackingLevelCap,
		"defaultTemplateName":          defaultTemplateName,
		"channelNotesContainsCategory": channelNotesContainsCategory,
		"admins": map[string]any{
			"discord":  cfg.Discord.Admins,
			"telegram": cfg.Telegram.Admins,
		},
		"maxDistance":                cfg.Tracking.MaxDistance,
		"defaultDistance":            cfg.Tracking.DefaultDistance,
		"everythingFlagPermissions":  cfg.Tracking.EverythingFlagPermissions,
		"disabledHooks":             disabledHooks,
		"gymBattles":                cfg.Tracking.EnableGymBattle,
	}

	body, _ := json.Marshal(resp)

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}
