package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/backup"
	"github.com/pokemon/poracleng/processor/internal/buttonactions"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// dtsEmojiLookup is the minimal emoji-resolution surface the /dts/emoji read
// needs. *dts.EmojiLookup satisfies it; the interface keeps the Register
// signature testable.
type dtsEmojiLookup interface {
	Defaults() map[string]string
	PlatformOverrides() map[string]map[string]string
	MergedFor(platform string) map[string]string
}

// dtsTemplateReader is the minimal template-store surface the DTS read
// endpoints need. *dts.TemplateStore satisfies it; the interface keeps the
// Register signatures testable since TemplateStore has unexported fields and
// can't be populated from outside the dts package.
type dtsTemplateReader interface {
	FilteredEntries(filterType, filterPlatform, filterLanguage, filterID string) []dts.DTSEntry
	ResolveEntryContent(entry dts.DTSEntry) (any, string)
	DeleteEntry(filterType, filterPlatform, filterLanguage, filterID string) error
	GetEntry(filterType, filterPlatform, filterLanguage, filterID string) *dts.DTSEntry
	Partials() map[string]string
	ClearCache()
}

// dtsEmojiInput carries the optional platform query param.
type dtsEmojiInput struct {
	Platform string `query:"platform"`
}

// RegisterDTSEmoji registers GET /api/dts/emoji. With a platform query it
// returns the merged flat map for that platform; otherwise the full
// defaults+overrides set. Replaces gin HandleDTSEmoji. Success JSON preserved.
//
// The response is left open (freeform): the body shape depends on the query —
// with a `platform` it is {status, platform, emoji}, without it is {status,
// defaults, platforms} — so there is no single fixed schema.
func RegisterDTSEmoji(api huma.API, emoji dtsEmojiLookup) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dts-emoji", Method: "GET", Path: "/dts/emoji",
		Summary:     "Emoji lookup map for template editing",
		Description: "Returns emoji lookup data for template editing. The response body is left open (freeform): with a `platform` query it is {status, platform, emoji}; without it is {status, defaults, platforms}.",
		Tags:        []string{"dts"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsEmojiInput) (*anyBodyOutput, error) {
		if in.Platform != "" {
			return &anyBodyOutput{Body: map[string]any{
				"status":   "ok",
				"platform": in.Platform,
				"emoji":    emoji.MergedFor(in.Platform),
			}}, nil
		}
		return &anyBodyOutput{Body: map[string]any{
			"status":    "ok",
			"defaults":  emoji.Defaults(),
			"platforms": emoji.PlatformOverrides(),
		}}, nil
	})
}

// dtsTemplatesQueryInput carries the optional filter query params.
type dtsTemplatesQueryInput struct {
	Type     string `query:"type"`
	Platform string `query:"platform"`
	Language string `query:"language"`
	ID       string `query:"id"`
}

// dtsEntryWithContent mirrors the anonymous struct the gin handler used: a
// DTSEntry with an optional resolved templateFileContent.
type dtsEntryWithContent struct {
	dts.DTSEntry
	TemplateFileContent string `json:"templateFileContent,omitempty"`
}

// dtsGetTemplatesOutput is the typed body for GET /api/dts/templates: a status
// envelope plus the filtered DTS entries (with resolved templateFile content).
type dtsGetTemplatesOutput struct {
	Body struct {
		Status    string                `json:"status"`
		Templates []dtsEntryWithContent `json:"templates"`
	}
}

// RegisterDTSGetTemplates registers GET /api/dts/templates, returning filtered
// DTS entries with resolved content. Replaces gin HandleDTSGetTemplates.
func RegisterDTSGetTemplates(api huma.API, ts dtsTemplateReader) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dts-templates", Method: "GET", Path: "/dts/templates",
		Summary: "DTS template entries with full content", Tags: []string{"dts"},
		Description: "Returns {status, templates}: the loaded DTS template entries (optionally filtered by the type/platform/language/id query params) with resolved content, including the text of any referenced templateFile. The template-editor read endpoint.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsTemplatesQueryInput) (*dtsGetTemplatesOutput, error) {
		entries := ts.FilteredEntries(in.Type, in.Platform, in.Language, in.ID)
		result := make([]dtsEntryWithContent, len(entries))
		for i, e := range entries {
			resolved, fileContent := ts.ResolveEntryContent(e)
			if resolved != nil {
				e.Template = resolved
			}
			result[i].DTSEntry = e
			if fileContent != "" {
				result[i].TemplateFileContent = fileContent
			}
		}
		out := &dtsGetTemplatesOutput{}
		out.Body.Status = "ok"
		out.Body.Templates = result
		return out, nil
	})
}

// dtsDeleteTemplateInput carries the key fields identifying the entry to delete.
type dtsDeleteTemplateInput struct {
	Type     string `query:"type"`
	Platform string `query:"platform"`
	Language string `query:"language"`
	ID       string `query:"id"`
}

// RegisterDTSDeleteTemplate registers DELETE /api/dts/templates, removing the
// keyed entry from memory and disk. Replaces gin HandleDTSDeleteTemplate.
// Missing type/platform/id yields 400; "not found" yields 404; readonly or
// other store errors yield 403.
func RegisterDTSDeleteTemplate(api huma.API, ts dtsTemplateReader) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-dts-template", Method: "DELETE", Path: "/dts/templates",
		Summary: "Delete a DTS template entry", Tags: []string{"dts"},
		Description: "Removes the DTS entry identified by the type/platform/language/id query keys from memory and disk. Unknown keys yield 404; readonly fallback entries (and other store rejections) yield 403.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsDeleteTemplateInput) (*statusOKOutput, error) {
		// Platform is required except for platform-agnostic types (e.g.
		// help), whose entries carry an empty platform — mirroring the save
		// endpoint's exemption so a saved override can also be deleted.
		if in.Type == "" || in.ID == "" ||
			(in.Platform == "" && !dts.IsPlatformAgnosticType(in.Type)) {
			return nil, huma.Error400BadRequest("type, platform, and id query parameters are required")
		}
		if err := ts.DeleteEntry(in.Type, in.Platform, in.Language, in.ID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				return nil, huma.Error404NotFound(err.Error())
			}
			return nil, huma.Error403Forbidden(err.Error())
		}
		out := &statusOKOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}

// dtsTemplateFileWriteInput carries the entry key fields plus the new content.
type dtsTemplateFileWriteInput struct {
	Type     string `query:"type"`
	Platform string `query:"platform"`
	Language string `query:"language"`
	ID       string `query:"id"`
	Body     struct {
		Content string `json:"content"`
	}
}

// RegisterDTSTemplateFileWrite registers PUT /api/dts/templates/file, updating
// the raw content of a templateFile entry. The path is derived from the entry's
// key fields — no client paths are used. Replaces gin HandleDTSTemplateFileWrite.
// Missing entry → 404; non-templateFile entry → 400; readonly → 403; path
// traversal → 403; filesystem errors → 500.
// dtsTemplateFileWriteOutput is the typed body for PUT /api/dts/templates/file:
// {status, templateFile, backup}.
type dtsTemplateFileWriteOutput struct {
	Body struct {
		Status       string `json:"status"`
		TemplateFile string `json:"templateFile"`
		Backup       string `json:"backup"`
	}
}

func RegisterDTSTemplateFileWrite(api huma.API, ts dtsTemplateReader, configDir string) {
	huma.Register(api, huma.Operation{
		OperationID: "put-dts-template-file", Method: "PUT", Path: "/dts/templates/file",
		Summary: "Update raw templateFile content", Tags: []string{"dts"},
		Description: "Overwrites the raw Handlebars text of an external templateFile referenced by DTS entries (path relative to config/, e.g. dts/fort_update.txt). Returns {status, templateFile, backup}; a backup copy is written to config/backups/ before the file is replaced.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsTemplateFileWriteInput) (*dtsTemplateFileWriteOutput, error) {
		entry := ts.GetEntry(in.Type, in.Platform, in.Language, in.ID)
		if entry == nil {
			return nil, huma.Error404NotFound("template not found")
		}
		if entry.TemplateFile == "" {
			return nil, huma.Error400BadRequest("template uses inline JSON, not a templateFile")
		}
		if entry.Readonly {
			return nil, huma.Error403Forbidden("template is readonly (bundled default)")
		}

		path := filepath.Join(configDir, entry.TemplateFile)
		// Safety: ensure resolved path stays under configDir.
		absPath, _ := filepath.Abs(path)
		absConfig, _ := filepath.Abs(configDir)
		if !strings.HasPrefix(absPath, absConfig+string(filepath.Separator)) {
			return nil, huma.Error403Forbidden("invalid template file path")
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, huma.Error500InternalServerError("create directory: " + err.Error())
		}

		// Snapshot the existing file (if any) before overwriting.
		backupRel, err := backup.Save(configDir, entry.TemplateFile)
		if err != nil {
			return nil, huma.Error500InternalServerError("backup existing: " + err.Error())
		}

		if err := os.WriteFile(path, []byte(in.Body.Content), 0644); err != nil { //nolint:gosec // operator-writable template content, same perms as legacy handler
			return nil, huma.Error500InternalServerError("write file: " + err.Error())
		}

		ts.ClearCache()

		log.Infof("dts: updated template file %s via API", entry.TemplateFile)
		out := &dtsTemplateFileWriteOutput{}
		out.Body.Status = "ok"
		out.Body.TemplateFile = entry.TemplateFile
		out.Body.Backup = backupRel
		return out, nil
	})
}

// dtsFieldTypesOutput is the typed body for GET /api/dts/fields: {status, types}.
type dtsFieldTypesOutput struct {
	Body struct {
		Status string   `json:"status"`
		Types  []string `json:"types"`
	}
}

// RegisterDTSFieldTypes registers GET /api/dts/fields, returning the list of
// available DTS type names. Replaces gin HandleDTSFieldTypes.
func RegisterDTSFieldTypes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dts-fields", Method: "GET", Path: "/dts/fields",
		Summary: "List all DTS type names", Tags: []string{"dts"},
		Description: "Returns {status, types}: every DTS template type name that has field metadata (pokemon, raid, quest, ...). Use GET /dts/fields/{type} for the per-type field list.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*dtsFieldTypesOutput, error) {
		types := make([]string, 0, len(fieldsByType))
		for t := range fieldsByType {
			types = append(types, t)
		}
		out := &dtsFieldTypesOutput{}
		out.Body.Status = "ok"
		out.Body.Types = types
		return out, nil
	})
}

// dtsFieldsInput carries the type path param.
type dtsFieldsInput struct {
	Type string `path:"type"`
}

// dtsFieldsOutput is the typed body for GET /api/dts/fields/{type}: {status,
// type, fields} plus optional blockScopes/snippets (present only when non-empty,
// matching the legacy conditional keys via omitempty).
type dtsFieldsOutput struct {
	Body struct {
		Status      string       `json:"status"`
		Type        string       `json:"type"`
		Fields      []FieldDef   `json:"fields"`
		BlockScopes []BlockScope `json:"blockScopes,omitempty"`
		Snippets    []Snippet    `json:"snippets,omitempty"`
	}
}

// RegisterDTSFields registers GET /api/dts/fields/{type}, returning the field
// surface for a DTS type. Unknown types return just the common fields (200, not
// 404 — matching the gin handler). Replaces gin HandleDTSFields.
func RegisterDTSFields(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dts-fields-type", Method: "GET", Path: "/dts/fields/{type}",
		Summary: "Template fields, block scopes, and snippets for a type", Tags: []string{"dts"},
		Description: "Returns {status, type, fields, blockScopes, snippets}: the template fields available to DTS templates of the given type, plus block scopes and starter snippets — the data behind template-editor autocomplete. 404 for an unknown type.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsFieldsInput) (*dtsFieldsOutput, error) {
		out := &dtsFieldsOutput{}
		out.Body.Status = "ok"
		out.Body.Type = in.Type
		entry, ok := fieldsByType[in.Type]
		if !ok {
			out.Body.Fields = commonFields
			return out, nil
		}
		out.Body.Fields = entry.Fields
		if len(entry.BlockScopes) > 0 {
			out.Body.BlockScopes = entry.BlockScopes
		}
		if len(entry.Snippets) > 0 {
			out.Body.Snippets = entry.Snippets
		}
		return out, nil
	})
}

// dtsPartialsOutput is the typed body for GET /api/dts/partials: {status,
// partials} where partials maps partial name → Handlebars source.
type dtsPartialsOutput struct {
	Body struct {
		Status   string            `json:"status"`
		Partials map[string]string `json:"partials"`
	}
}

// RegisterDTSPartials registers GET /api/dts/partials, returning the Handlebars
// partials map. Replaces gin HandleDTSPartials.
func RegisterDTSPartials(api huma.API, ts dtsTemplateReader) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dts-partials", Method: "GET", Path: "/dts/partials",
		Summary: "Handlebars partials for client-side rendering", Tags: []string{"dts"},
		Description: "Returns {status, partials}: the registered Handlebars partials as a name → source map, letting clients reproduce server-side rendering (e.g. live template preview).",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*dtsPartialsOutput, error) {
		out := &dtsPartialsOutput{}
		out.Body.Status = "ok"
		out.Body.Partials = ts.Partials()
		return out, nil
	})
}

// dtsTestdataInput carries the optional type/dtsType filter query params.
type dtsTestdataInput struct {
	Type    string `query:"type" doc:"Legacy filter: exact raw webhook type (e.g. pokemon, raid, pokestop). Returns every entry of that type, untagged. Takes precedence over dtsType if both are set."`
	DtsType string `query:"dtsType" doc:"DTS template type name (e.g. monster, egg, invasion, lure, incident, monsterChanged) or raw webhook type, resolved via the shared alias table. Returns only the entries that preview that type — including the server-side pokestop invasion/lure and raid/egg splits — each tagged with dtsType."`
}

// RegisterDTSTestdata registers GET /api/dts/testdata, returning test webhook
// scenarios merged from config + fallback testdata.json. Replaces gin
// HandleDTSTestdata. A missing testdata.json yields 404.
// dtsTestdataOutput is the typed body for GET /api/dts/testdata: {status,
// testdata, types}. testdata is null when a type/dtsType filter matches
// nothing (preserved via a nil slice marshalling to null, matching the
// legacy handler). types is the full DTS-type -> source map (see dtsmap),
// always present, so clients can discover every addressable DTS type name
// without a separate request.
type dtsTestdataOutput struct {
	Body struct {
		Status   string                 `json:"status"`
		Testdata []TestDataEntry        `json:"testdata"`
		Types    map[string]dtsTypeInfo `json:"types"`
	}
}

func RegisterDTSTestdata(api huma.API, configDir, fallbackDir string) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dts-testdata", Method: "GET", Path: "/dts/testdata",
		Summary: "Test webhook scenarios from testdata.json", Tags: []string{"dts"},
		Description: "Returns {status, testdata, types}: the named test webhook scenarios from the bundled fallbacks/testdata.json merged with the operator's config/testdata.json — the same scenarios poracle-test and POST /test use. ?dtsType=<name> resolves via the shared DTS-type alias table (including the pokestop invasion/lure and raid/egg payload-shape splits) and tags each returned entry with dtsType; the legacy ?type=<webhookType> filter is unchanged and untagged. types is the full DTS-type -> source map for client discovery.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *dtsTestdataInput) (*dtsTestdataOutput, error) {
		entries := loadTestdata(configDir, fallbackDir)
		if entries == nil {
			return nil, huma.Error404NotFound("testdata.json not found")
		}
		switch {
		case in.Type != "":
			var filtered []TestDataEntry
			for _, e := range entries {
				if e.Type == in.Type {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		case in.DtsType != "":
			entries = filterByDTSType(entries, in.DtsType)
		}
		out := &dtsTestdataOutput{}
		out.Body.Status = "ok"
		out.Body.Testdata = entries
		out.Body.Types = dtsTypeInfoMap()
		return out, nil
	})
}

// dtsActionsOutput is the typed body for GET /api/dts/actions: {actions} (no
// status envelope, matching the legacy handler).
type dtsActionsOutput struct {
	Body struct {
		Actions []ActionInfo `json:"actions"`
	}
}

// RegisterButtonActions registers GET /api/dts/actions, returning the list of
// registered button actions and their editor metadata. Replaces gin
// HandleButtonActionsList. A nil registry yields 503.
func RegisterButtonActions(api huma.API, reg *buttonactions.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "get-dts-actions", Method: "GET", Path: "/dts/actions",
		Summary: "List registered button actions + their scopes/params", Tags: []string{"dts"},
		Description: "Returns the registered alert-button actions (mute, unsubscribe, redeliver, render, ...) with each action's allowed scopes, whether a scope is required, and its known params — so editors can render per-action button configuration UI.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*dtsActionsOutput, error) {
		if reg == nil {
			return nil, huma.Error503ServiceUnavailable("button actions not configured")
		}
		names := reg.Names()
		actions := make([]ActionInfo, 0, len(names))
		for _, n := range names {
			actions = append(actions, describeAction(n))
		}
		out := &dtsActionsOutput{}
		out.Body.Actions = actions
		return out, nil
	})
}

// RegisterDTSReads registers the in-block DTS editor read endpoints — the ones
// that must stay gated behind dtsRenderer != nil. /dts/actions is registered
// separately (RegisterButtonActions) because it lives outside that block.
func RegisterDTSReads(api huma.API, emoji dtsEmojiLookup, ts dtsTemplateReader, configDir, fallbackDir string) {
	RegisterDTSEmoji(api, emoji)
	RegisterDTSGetTemplates(api, ts)
	RegisterDTSDeleteTemplate(api, ts)
	RegisterDTSTemplateFileWrite(api, ts, configDir)
	RegisterDTSFieldTypes(api)
	RegisterDTSFields(api)
	RegisterDTSPartials(api, ts)
	RegisterDTSTestdata(api, configDir, fallbackDir)
}

// compile-time assertions that the concrete types satisfy the read interfaces.
var (
	_ dtsEmojiLookup    = (*dts.EmojiLookup)(nil)
	_ dtsTemplateReader = (*dts.TemplateStore)(nil)
)
