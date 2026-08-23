package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// poracleWebAdmins is the typed admins sub-object: the discord/telegram admin
// id lists (always non-nil arrays, never null — matching the legacy coercion).
type poracleWebAdmins struct {
	Discord  []string `json:"discord" doc:"Discord admin user ids"`
	Telegram []string `json:"telegram" doc:"Telegram admin user ids"`
}

// poracleWebResponse is the typed body for GET /api/config/poracleWeb. It
// mirrors buildPoracleWebResponse's exact key set, types, and nesting. Field
// declaration order is deliberately alphabetical by json tag so the serialised
// bytes are IDENTICAL to the legacy map[string]any (Go marshals map keys in
// sorted order), keeping existing clients (PoracleWeb) byte-compatible.
type poracleWebResponse struct {
	AddressFormat                string           `json:"addressFormat" doc:"Geocoding address format string"`
	Admins                       poracleWebAdmins `json:"admins" doc:"Discord/Telegram admin id lists"`
	ChannelNotesContainsCategory bool             `json:"channelNotesContainsCategory" doc:"Whether channel notes carry the category (check_role + update_channel_notes)"`
	DefaultDistance              int              `json:"defaultDistance" doc:"Default tracking distance (m)"`
	DefaultPvpCap                int              `json:"defaultPvpCap" doc:"Default user PVP level cap"`
	DefaultTemplateName          string           `json:"defaultTemplateName" doc:"Default DTS template name"`
	DisabledHooks                []string         `json:"disabledHooks" doc:"Webhook types disabled on this server"`
	EverythingFlagPermissions    string           `json:"everythingFlagPermissions" doc:"Permission mode for the !track everything keyword"`
	GymBattles                   bool             `json:"gymBattles" doc:"Whether gym battle tracking is enabled"`
	Locale                       string           `json:"locale" doc:"Default server locale"`
	MaxDistance                  int              `json:"maxDistance" doc:"Maximum allowed tracking distance (m)"`
	Prefix                       string           `json:"prefix" doc:"Discord command prefix"`
	Provider                     string           `json:"provider" doc:"Geocoding provider type (nominatim || photon)"`
	ProviderURL                  string           `json:"providerURL" doc:"Geocoding provider URL"`
	PvpCaps                      []int            `json:"pvpCaps" doc:"Configured PVP level caps"`
	PvpFilterGreatMinCP          int              `json:"pvpFilterGreatMinCP" doc:"Minimum CP floor for great-league PVP filters"`
	PvpFilterLittleMinCP         int              `json:"pvpFilterLittleMinCP" doc:"Minimum CP floor for little-league PVP filters"`
	PvpFilterMaxRank             int              `json:"pvpFilterMaxRank" doc:"Maximum PVP rank allowed in filters"`
	PvpFilterUltraMinCP          int              `json:"pvpFilterUltraMinCP" doc:"Minimum CP floor for ultra-league PVP filters"`
	PvpLittleLeagueAllowed       bool             `json:"pvpLittleLeagueAllowed" doc:"Whether little-league PVP filters are allowed"`
	PvpRequiresMinCp             bool             `json:"pvpRequiresMinCp" doc:"Whether PVP filters require a min CP (force_min_cp + webhook data source)"`
	StaticKey                    []string         `json:"staticKey" doc:"Tileserver static map keys (always an array)"`
	Status                       string           `json:"status" doc:"Always \"ok\""`
	Version                      string           `json:"version" doc:"Processor version"`
}

// poracleWebOutput is the typed huma output for GET /api/config/poracleWeb.
type poracleWebOutput struct {
	Body poracleWebResponse
}

// RegisterConfigPoracleWeb registers GET /api/config/poracleWeb, returning the
// configuration subset PoracleWeb needs for its UI. Replaces gin
// HandleConfigPoracleWeb. The response is a typed config object; the serialised
// bytes are identical to the legacy {"status":"ok", ...} map.
func RegisterConfigPoracleWeb(api huma.API, cfg *config.Config) {
	// Build the response once since config is immutable after load — mirror
	// the legacy handler's pre-computation exactly.
	resp := buildPoracleWebResponse(cfg)
	huma.Register(api, huma.Operation{
		OperationID: "get-config-poracleweb", Method: "GET", Path: "/config/poracleWeb",
		Summary:     "Server config for web UI",
		Description: "Returns the configuration subset PoracleWeb needs for its UI.",
		Tags:        []string{"config"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*poracleWebOutput, error) {
		return &poracleWebOutput{Body: resp}, nil
	})
}

// configValuesInput carries the optional section query param for the values
// read, matching the legacy ?section= filter.
type configValuesInput struct {
	Section string `query:"section"`
}

// configValuesResponse is the typed body for GET /api/config/values. The outer
// envelope is named/documented; `values` stays OPEN (it is the reflection-built
// ExtractValues map of dynamically-keyed sections/fields). Field order is
// deliberately alphabetical by json tag (overridden, status, values) so the
// serialised bytes are IDENTICAL to the legacy map[string]any response (Go
// marshals map keys in sorted order).
type configValuesResponse struct {
	Overridden []string       `json:"overridden" doc:"Always an empty list; retained for editor backward compatibility"`
	Status     string         `json:"status" doc:"Always \"ok\""`
	Values     map[string]any `json:"values" doc:"Current merged config values: a dynamically-keyed {section: {field: value}} map (open shape)"`
}

// configValuesOutput is the typed huma output for GET /api/config/values.
type configValuesOutput struct {
	Body configValuesResponse
}

// RegisterConfigValues registers GET /api/config/values, returning current
// merged config values (reflection-built map). Replaces gin HandleConfigValues.
// The envelope is typed; `values` stays open (the ExtractValues reflection map).
// The "overridden" field is always an empty list, preserved for editor backward
// compatibility.
func RegisterConfigValues(api huma.API, deps ConfigDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-config-values", Method: "GET", Path: "/config/values",
		Summary:     "Current merged config values",
		Description: "Returns current merged config values. The outer envelope ({status, values, overridden}) is typed; `values` is open (the reflection-built {section:{field:value}} map). The `overridden` field is always an empty list, retained for editor backward compatibility.",
		Tags:        []string{"config"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *configValuesInput) (*configValuesOutput, error) {
		values := ExtractValues(deps.Cfg, in.Section)
		return &configValuesOutput{Body: configValuesResponse{
			Overridden: []string{},
			Status:     "ok",
			Values:     values,
		}}, nil
	})
}

// configWriteInput carries the freeform config-values request. The body is open
// because the editor POSTs a dynamically-keyed {section: {field: value}} map.
type configWriteInput struct {
	Body openJSON
}

// configSaveResponse is the typed body for POST /api/config/values. The request
// stays OPEN (configWriteInput.Body openJSON — the nested {section:{field:value}}
// editor map), but the response envelope is named/documented. Field order is
// deliberately alphabetical by json tag (backup, restart_fields, restart_required,
// saved, status) so the serialised bytes are IDENTICAL to the legacy
// map[string]any response. `restart_fields` is omitempty to match the legacy
// handler, which only sets the key when at least one restart-required field was
// changed.
type configSaveResponse struct {
	Backup          string   `json:"backup" doc:"Path (relative to config/) of the pre-write backup of config.toml"`
	RestartFields   []string `json:"restart_fields,omitempty" doc:"Changed fields that require a restart to take effect (omitted when none)"`
	RestartRequired bool     `json:"restart_required" doc:"Whether a restart is needed for the saved changes to take effect"`
	Saved           int      `json:"saved" doc:"Number of config fields written"`
	Status          string   `json:"status" doc:"Always \"ok\""`
}

// configSaveOutput is the typed huma output for POST /api/config/values.
type configSaveOutput struct {
	Body configSaveResponse
}

// configValidateResponse is the typed body for POST /api/config/validate. The
// request stays OPEN (the config map); the response envelope is named. Field
// order is deliberately alphabetical by json tag (issues, status) so the
// serialised bytes are IDENTICAL to the legacy map[string]any response.
type configValidateResponse struct {
	Issues []ValidationIssue `json:"issues" doc:"Per-field validation issues (each {field, severity, message}); empty/absent when all values are valid"`
	Status string            `json:"status" doc:"Always \"ok\""`
}

// configValidateOutput is the typed huma output for POST /api/config/validate.
type configValidateOutput struct {
	Body configValidateResponse
}

// RegisterConfigSave registers POST /api/config/values, rewriting
// config/config.toml in place (with a backup to config/backups/). Replaces gin
// HandleConfigSave. The request body is open (a nested {section:{field:value}}
// map). Parse, validate, save, and reload behaviour — plus the config.toml
// rewrite side effect — are preserved exactly from the legacy handler.
func RegisterConfigSave(api huma.API, deps ConfigDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "post-config-values", Method: "POST", Path: "/config/values",
		Summary:     "Save config changes",
		Description: "Rewrites config/config.toml in place (previous version backed up to config/backups/). Request body stays open: a nested {section: {field: value}} map of editor updates. Response envelope ({status, saved, restart_required, backup, restart_fields?}) is typed.",
		Tags:        []string{"config"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *configWriteInput) (*configSaveOutput, error) {
		var updates map[string]any
		if err := json.Unmarshal(in.Body, &updates); err != nil {
			return nil, huma.Error400BadRequest("invalid request body: " + err.Error())
		}

		if len(updates) == 0 {
			return nil, huma.Error400BadRequest("no changes provided")
		}

		// Validate that all sections/fields exist in schema.
		if err := validateUpdates(updates); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}

		// Per-field value validation (colour format, array length, paths).
		issues := validateConfigValues(updates, deps.ConfigDir)
		var errorIssues []ValidationIssue
		for _, iss := range issues {
			if iss.Severity == "error" {
				errorIssues = append(errorIssues, iss)
			}
		}
		if len(errorIssues) > 0 {
			details := make([]error, 0, len(errorIssues))
			for _, iss := range errorIssues {
				details = append(details, &huma.ErrorDetail{
					Message:  iss.Message,
					Location: iss.Field,
				})
			}
			return nil, huma.Error400BadRequest("validation failed", details...)
		}

		// Strip masked sensitive values ("****") so the editor can resubmit a
		// form without wiping secrets the user didn't touch.
		stripMaskedSensitiveValues(updates)

		// Convert flat table-row fields (discord_channels etc.) back into the
		// nested struct shape before persisting.
		nestTableUpdates(updates)

		// Save directly to config.toml. The previous file is backed up to
		// config/backups/ before the rewrite.
		backupRel, err := writeConfigTOML(deps.ConfigDir, updates)
		if err != nil {
			log.Errorf("config save: %v", err)
			return nil, huma.Error500InternalServerError("save failed: " + err.Error())
		}

		// Apply to in-memory config.
		config.ApplyOverrides(deps.Cfg, updates)

		// Check if restart is required.
		restartRequired, restartFields := checkRestartRequired(updates)

		// Trigger hot-reload if applicable.
		if !restartRequired && deps.ReloadFn != nil {
			deps.ReloadFn()
		}

		saved := countFields(updates)
		log.Infof("config: saved %d field(s) via API (restart_required=%v, backup=%s)", saved, restartRequired, backupRel)

		return &configSaveOutput{Body: configSaveResponse{
			Backup:          backupRel,
			RestartFields:   restartFields,
			RestartRequired: restartRequired,
			Saved:           saved,
			Status:          "ok",
		}}, nil
	})
}

// RegisterConfigValidate registers POST /api/config/validate, a dry-run of the
// config-values validation pass (no save). Replaces gin HandleConfigValidate.
// The request body is open (a nested {section:{field:value}} map). The response
// is {"status":"ok","issues":[...]} where issues is the full validation list.
func RegisterConfigValidate(api huma.API, deps ConfigDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "post-config-validate", Method: "POST", Path: "/config/validate",
		Summary:     "Dry-run config validation",
		Description: "Runs the config-values validation pass without saving. Request body stays open: a nested {section: {field: value}} map. Response envelope ({status, issues}) is typed; each issue is {field, severity, message}.",
		Tags:        []string{"config"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *configWriteInput) (*configValidateOutput, error) {
		var updates map[string]any
		if err := json.Unmarshal(in.Body, &updates); err != nil {
			return nil, huma.Error400BadRequest("invalid request body: " + err.Error())
		}

		issues := validateConfigValues(updates, deps.ConfigDir)
		return &configValidateOutput{Body: configValidateResponse{
			Issues: issues,
			Status: "ok",
		}}, nil
	})
}

// buildPoracleWebResponse mirrors HandleConfigPoracleWeb's response
// construction exactly, returning the typed poracleWebResponse. The nil-slice →
// [] coercions (staticKey, admins, pvpCaps) are preserved so existing clients
// keep seeing empty arrays rather than null.
func buildPoracleWebResponse(cfg *config.Config) poracleWebResponse {
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

	defaultTemplateName := "1"
	if cfg.General.DefaultTemplateName != nil {
		defaultTemplateName = fmt.Sprintf("%v", cfg.General.DefaultTemplateName)
	}

	pvpCaps := cfg.PVP.LevelCaps
	if len(pvpCaps) == 0 {
		pvpCaps = []int{50}
	}

	pvpRequiresMinCp := cfg.PVP.ForceMinCP && cfg.PVP.DataSource == "webhook"
	channelNotesContainsCategory := cfg.Discord.CheckRole && cfg.Reconciliation.Discord.UpdateChannelNotes

	staticKeys := cfg.Geocoding.StaticKey
	if staticKeys == nil {
		staticKeys = []string{}
	}

	discordAdmins := cfg.Discord.Admins
	if discordAdmins == nil {
		discordAdmins = []string{}
	}
	telegramAdmins := cfg.Telegram.Admins
	if telegramAdmins == nil {
		telegramAdmins = []string{}
	}

	return poracleWebResponse{
		Status:                       "ok",
		Version:                      Version,
		Locale:                       cfg.General.Locale,
		Prefix:                       cfg.Discord.Prefix,
		Provider:                     cfg.Geocoding.Provider,
		ProviderURL:                  cfg.Geocoding.ProviderURL,
		AddressFormat:                cfg.Locale.AddressFormat,
		StaticKey:                    staticKeys,
		PvpFilterMaxRank:             cfg.PVP.PVPFilterMaxRank,
		PvpFilterGreatMinCP:          cfg.PVP.PVPFilterGreatMinCP,
		PvpFilterUltraMinCP:          cfg.PVP.PVPFilterUltraMinCP,
		PvpFilterLittleMinCP:         cfg.PVP.PVPFilterLittleMinCP,
		PvpLittleLeagueAllowed:       true,
		PvpCaps:                      pvpCaps,
		PvpRequiresMinCp:             pvpRequiresMinCp,
		DefaultPvpCap:                cfg.Tracking.DefaultUserTrackingLevelCap,
		DefaultTemplateName:          defaultTemplateName,
		ChannelNotesContainsCategory: channelNotesContainsCategory,
		Admins: poracleWebAdmins{
			Discord:  discordAdmins,
			Telegram: telegramAdmins,
		},
		MaxDistance:               cfg.Tracking.MaxDistance,
		DefaultDistance:           cfg.Tracking.DefaultDistance,
		EverythingFlagPermissions: cfg.Tracking.EverythingFlagPermissions,
		DisabledHooks:             disabledHooks,
		GymBattles:                cfg.Tracking.EnableGymBattle,
	}
}
