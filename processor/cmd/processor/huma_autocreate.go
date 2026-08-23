package main

import (
	"context"
	"errors"
	"os"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/discordbot"
)

// autocreateBodyOutput is a huma output whose body re-marshals an arbitrary
// value to the same JSON the legacy gin handlers produced via c.JSON.
type autocreateBodyOutput struct {
	Body any
}

// autocreateRunInput wraps the POST /autocreate/run body. Body fields mirror
// autocreateRunRequest and are all optional (the legacy gin handler used
// ShouldBindJSON, which treats every field as optional and accepts partial /
// empty bodies). required:"false" keeps huma from emitting a spurious 422.
type autocreateRunInput struct {
	Body struct {
		Rule     string `json:"rule" required:"false"` // empty → all rules
		DryRun   bool   `json:"dry_run" required:"false"`
		Reset    bool   `json:"reset" required:"false"`
		Removals bool   `json:"removals" required:"false"`
		Force    bool   `json:"force" required:"false"`
	}
}

// deleteTemplateInput carries the {name} path param for the delete op.
type deleteTemplateInput struct {
	Name string `path:"name"`
}

// autocreateDeleteOutput is the typed body for DELETE
// /api/autocreate/templates/{name}: {status, backup}.
type autocreateDeleteOutput struct {
	Body struct {
		Status string `json:"status"`
		Backup string `json:"backup"`
	}
}

// autocreateSchemaOutput is the typed body for GET
// /api/autocreate/templates/schema: {status, schema}.
type autocreateSchemaOutput struct {
	Body struct {
		Status string                `json:"status"`
		Schema channelTemplatesEnums `json:"schema"`
	}
}

// registerAutocreateHuma registers the autocreate run / delete-template /
// templates-schema ops on the shared huma instance, in place — same paths,
// same success JSON as the legacy gin handlers; error paths surface as
// problem+json. The three MODERATE template endpoints (GET/POST templates,
// POST templates/validate) intentionally stay on gin and are NOT registered
// here.
func registerAutocreateHuma(humaAPI huma.API, cfg *config.Config, bot *discordbot.Bot) {
	// POST /api/autocreate/run — sync one or all autocreate rules.
	huma.Register(humaAPI, huma.Operation{
		OperationID: "post-autocreate-run", Method: "POST", Path: "/autocreate/run",
		Summary:     "Run autocreate rule sync",
		Description: "Syncs one or all autocreate rules. The response body is left open (freeform): {status, rules:[]SyncOneRuleResult} where each result carries per-fence created/reused/orphan/removed/skipped logs and a free-form errors list, so it has no fixed schema.",
		Tags:        []string{"autocreate"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *autocreateRunInput) (*autocreateBodyOutput, error) {
		req := in.Body

		if bot == nil {
			return nil, huma.Error503ServiceUnavailable("discord bot not running")
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
				return nil, huma.Error404NotFound("rule not found")
			}
			rules = matched
		}
		if len(rules) == 0 {
			return &autocreateBodyOutput{Body: map[string]any{"status": "ok", "rules": []discordbot.SyncOneRuleResult{}}}, nil
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
		return &autocreateBodyOutput{Body: map[string]any{"status": "ok", "rules": results}}, nil
	})

	// DELETE /api/autocreate/templates/{name} — remove one channel template.
	huma.Register(humaAPI, huma.Operation{
		OperationID: "delete-autocreate-template", Method: "DELETE", Path: "/autocreate/templates/{name}",
		Summary: "Delete a channel template", Tags: []string{"autocreate"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *deleteTemplateInput) (*autocreateDeleteOutput, error) {
		if in.Name == "" {
			return nil, huma.Error400BadRequest("template name is required")
		}
		backup, err := discordbot.DeleteChannelTemplate(cfg.BaseDir, in.Name)
		if errors.Is(err, os.ErrNotExist) {
			return nil, huma.Error404NotFound("template not found")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &autocreateDeleteOutput{}
		out.Body.Status = "ok"
		out.Body.Backup = backup
		return out, nil
	})

	// GET /api/autocreate/templates/schema — static editor metadata.
	huma.Register(humaAPI, huma.Operation{
		OperationID: "get-autocreate-templates-schema", Method: "GET", Path: "/autocreate/templates/schema",
		Summary: "Get channel templates schema", Tags: []string{"autocreate"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*autocreateSchemaOutput, error) {
		schema := channelTemplatesEnums{
			ChannelTypes:    []string{"text", "voice"},
			ControlTypes:    []string{"", "bot", "webhook"},
			ButtonStyles:    []string{"primary", "secondary", "success", "danger"},
			PermissionFlags: discordbot.PermissionFlagsList(),
			PlaceholderHelp: map[string]string{
				"interactive": "{N} indexes args[N+1] from !autocreate <template> <args...> — note the off-by-one (args[0] is the template name itself).",
				"bulk":        "[[autocreate.rules]] params[] elements render per-fence then become positional args in order. Whitespace inside a rendered element splits it into multiple args; \"quoted segments\" stay as one.",
			},
			BackupNamePrefix: "channelTemplate.json.bak.",
		}
		out := &autocreateSchemaOutput{}
		out.Body.Status = "ok"
		out.Body.Schema = schema
		return out, nil
	})
}
