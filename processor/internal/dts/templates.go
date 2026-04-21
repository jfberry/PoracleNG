package dts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	raymond "github.com/mailgun/raymond/v2"
	log "github.com/sirupsen/logrus"
)

// DTSEntry represents a single DTS template entry from the dts.json file.
type DTSEntry struct {
	Type         string `json:"type"`
	ID           jsonID `json:"id"`
	Platform     string `json:"platform"`
	Language     string `json:"language"`
	Default      bool   `json:"default"`
	Hidden       bool   `json:"hidden"`
	Readonly     bool   `json:"readonly,omitempty"`
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
	Template     any    `json:"template"`
	TemplateFile string `json:"templateFile,omitempty"`

	// sourceFile tracks where this entry was loaded from (not serialized).
	// Used to remove the entry from its original file when saving elsewhere.
	sourceFile string
}

// SourceFile returns the file path this entry was loaded from.
func (e *DTSEntry) SourceFile() string { return e.sourceFile }

// jsonID handles DTS id fields that may be either a JSON string or number.
type jsonID string

func (j *jsonID) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*j = jsonID(s)
		return nil
	}
	// Try number
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*j = jsonID(n.String())
		return nil
	}
	return fmt.Errorf("dts id: cannot unmarshal %s", string(data))
}

func (j jsonID) String() string { return string(j) }

// TemplateStore holds parsed DTS entries and a cache of compiled templates.
type TemplateStore struct {
	mu          sync.RWMutex
	entries     []DTSEntry
	cache       map[string]*raymond.Template
	sourceCache map[string]string
	tileUsage   map[string]bool
	partials    map[string]string
	configDir   string
	fallbackDir string
}

// LoadTemplates reads dts.json from configDir (preferred) or fallbackDir.
func LoadTemplates(configDir, fallbackDir string) (*TemplateStore, error) {
	ts := &TemplateStore{
		cache:       make(map[string]*raymond.Template),
		sourceCache: make(map[string]string),
		tileUsage:   make(map[string]bool),
		configDir:   configDir,
		fallbackDir: fallbackDir,
	}

	ts.partials = loadPartials(configDir, fallbackDir)

	entries, err := loadEntries(configDir, fallbackDir)
	if err != nil {
		return nil, err
	}
	ts.entries = entries
	return ts, nil
}

// loadPartials loads Handlebars partials from partials.json.
// Config dir takes precedence over fallback dir.
func loadPartials(configDir, fallbackDir string) map[string]string {
	path := filepath.Join(configDir, "partials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Join(fallbackDir, "partials.json")
		data, err = os.ReadFile(path)
		if err != nil {
			log.Debug("No partials.json found")
			return nil
		}
	}

	var partials map[string]string
	if err := json.Unmarshal(data, &partials); err != nil {
		log.Warnf("dts: failed to parse partials.json: %v", err)
		return nil
	}

	return partials
}

func loadEntries(configDir, fallbackDir string) ([]DTSEntry, error) {
	var entries []DTSEntry

	// 1. Load fallback dts.json (readonly — bundled defaults)
	fallbackPath := filepath.Join(fallbackDir, "dts.json")
	if data, err := os.ReadFile(fallbackPath); err == nil {
		var fallbackEntries []DTSEntry
		if err := json.Unmarshal(data, &fallbackEntries); err != nil {
			return nil, fmt.Errorf("parse fallback dts.json: %w", err)
		}
		for i := range fallbackEntries {
			fallbackEntries[i].sourceFile = fallbackPath
			fallbackEntries[i].Readonly = true
		}
		entries = append(entries, fallbackEntries...)
	}

	// 1b. Load fallback dts/**/*.json (readonly — bundled help, info,
	// info sub-topic files; kept out of the monolithic fallbacks/dts.json
	// so each topic is one small file that operators can copy verbatim
	// into config/dts/ to customise).
	fallbackDtsDir := filepath.Join(fallbackDir, "dts")
	filepath.WalkDir(fallbackDtsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("dts: failed to read %s: %s", path, err)
			return nil
		}
		var extraEntries []DTSEntry
		if err := json.Unmarshal(data, &extraEntries); err != nil {
			log.Warnf("dts: failed to parse %s: %s", path, err)
			return nil
		}
		for i := range extraEntries {
			extraEntries[i].sourceFile = path
			extraEntries[i].Readonly = true
		}
		entries = append(entries, extraEntries...)
		log.Debugf("dts: loaded %d fallback entries from %s", len(extraEntries), path)
		return nil
	})

	// 2. Load config dts.json (user's main config, editable)
	configPath := filepath.Join(configDir, "dts.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var configEntries []DTSEntry
		if err := json.Unmarshal(data, &configEntries); err != nil {
			return nil, fmt.Errorf("parse config dts.json: %w", err)
		}
		for i := range configEntries {
			configEntries[i].sourceFile = configPath
		}
		entries = append(entries, configEntries...)
	} else if entries == nil {
		// Neither fallback nor config found
		return nil, fmt.Errorf("no dts.json found in %s or %s", configDir, fallbackDir)
	}

	// 3. Load additional DTS files from config/dts/ directory.
	// Each JSON file is an array of DTSEntry objects, concatenated to the main list.
	// Later entries override earlier ones via the template selection chain.
	dtsDir := filepath.Join(configDir, "dts")
	dirEntries, err := os.ReadDir(dtsDir)
	if err == nil {
		for _, f := range dirEntries {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			extraPath := filepath.Join(dtsDir, f.Name())
			extraData, err := os.ReadFile(extraPath)
			if err != nil {
				log.Warnf("dts: failed to read %s: %s", extraPath, err)
				continue
			}
			var extraEntries []DTSEntry
			if err := json.Unmarshal(extraData, &extraEntries); err != nil {
				log.Warnf("dts: failed to parse %s: %s", extraPath, err)
				continue
			}
			for i := range extraEntries {
				extraEntries[i].sourceFile = extraPath
			}
			entries = append(entries, extraEntries...)
			log.Debugf("dts: loaded %d entries from %s", len(extraEntries), f.Name())
		}
	}

	return entries, nil
}

// Reload re-reads dts.json and partials.json, then clears the template cache.
func (ts *TemplateStore) Reload(configDir, fallbackDir string) error {
	entries, err := loadEntries(configDir, fallbackDir)
	if err != nil {
		return err
	}
	partials := loadPartials(configDir, fallbackDir)
	ts.mu.Lock()
	ts.entries = entries
	ts.partials = partials
	ts.cache = make(map[string]*raymond.Template)
	ts.sourceCache = make(map[string]string)
	ts.tileUsage = make(map[string]bool)
	ts.configDir = configDir
	ts.fallbackDir = fallbackDir
	ts.mu.Unlock()
	return nil
}

// Get finds and returns a compiled template using the selection chain.
// Returns nil if no matching entry exists or if compilation fails.
func (ts *TemplateStore) Get(templateType, platform, templateID, language string) *raymond.Template {
	ts.mu.RLock()
	key := cacheKey(templateType, platform, templateID, language)
	if cached, ok := ts.cache[key]; ok {
		ts.mu.RUnlock()
		return cached
	}
	ts.mu.RUnlock()

	// Find matching entry via selection chain
	entry := ts.selectEntry(templateType, platform, templateID, language)
	if entry == nil {
		return nil
	}

	// Resolve and compile
	tmplStr, err := resolveTemplate(*entry, ts.configDir)
	if err != nil {
		log.Errorf("dts: resolve template %s/%s/%s/%s: %v", templateType, platform, templateID, language, err)
		return nil
	}

	compiled, err := raymond.Parse(tmplStr)
	if err != nil {
		log.Errorf("dts: compile template %s/%s/%s/%s: %v", templateType, platform, templateID, language, err)
		return nil
	}

	// Register partials on this template instance (not globally, so reloads work)
	ts.mu.RLock()
	if len(ts.partials) > 0 {
		compiled.RegisterPartials(ts.partials)
	}
	ts.mu.RUnlock()

	// Cache under write lock
	ts.mu.Lock()
	ts.cache[key] = compiled
	ts.sourceCache[key] = tmplStr
	ts.mu.Unlock()

	return compiled
}

// Exists checks whether a template with the given ID exists for the type and
// platform. A default entry's ID also counts as valid. This does NOT fall back
// to the default template for unknown IDs — use Get for that (rendering).
func (ts *TemplateStore) Exists(templateType, platform, templateID, language string) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	idLower := strings.ToLower(templateID)

	for i := range ts.entries {
		e := &ts.entries[i]
		if e.Type != templateType || e.Platform != platform {
			continue
		}
		if strings.ToLower(e.ID.String()) == idLower {
			return true
		}
	}
	return false
}

// ResolveEntryContent resolves @include directives and joins string arrays
// in the entry's template content. Returns a copy with the resolved template.
// For templateFile entries, returns the resolved file content as a string.
// Used by the API to return fully expanded templates to the editor.
func (ts *TemplateStore) ResolveEntryContent(entry DTSEntry) (any, string) {
	ts.mu.RLock()
	configDir := ts.configDir
	ts.mu.RUnlock()

	if entry.TemplateFile != "" {
		path := filepath.Join(configDir, entry.TemplateFile)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, ""
		}
		return nil, resolveIncludes(strings.TrimSpace(string(data)), configDir)
	}
	if entry.Template != nil {
		return processTemplateValue(entry.Template, configDir), ""
	}
	return nil, ""
}

// ClearCache drops all compiled templates so the next render re-parses from
// source. Used after template file content is updated via the API.
func (ts *TemplateStore) ClearCache() {
	ts.mu.Lock()
	ts.cache = make(map[string]*raymond.Template)
	ts.sourceCache = make(map[string]string)
	ts.tileUsage = make(map[string]bool)
	ts.mu.Unlock()
}

// UsesTile checks whether the resolved template for the given parameters
// references staticMap/staticmap. Uses the same selection chain as Get().
// Returns true (conservative) if the template can't be found.
func (ts *TemplateStore) UsesTile(templateType, platform, templateID, language string) bool {
	key := cacheKey(templateType, platform, templateID, language)

	ts.mu.RLock()
	if result, ok := ts.tileUsage[key]; ok {
		ts.mu.RUnlock()
		return result
	}
	ts.mu.RUnlock()

	// Trigger template resolution (which populates sourceCache)
	tmpl := ts.Get(templateType, platform, templateID, language)
	if tmpl == nil {
		return true // conservative
	}

	ts.mu.RLock()
	source, ok := ts.sourceCache[key]
	ts.mu.RUnlock()
	if !ok {
		return true // conservative
	}

	uses := strings.Contains(strings.ToLower(source), "staticmap")

	ts.mu.Lock()
	ts.tileUsage[key] = uses
	ts.mu.Unlock()

	return uses
}

func cacheKey(templateType, platform, templateID, language string) string {
	return templateType + " " + platform + " " + templateID + " " + language
}

// selectEntry applies the selection chain to find the best matching entry.
//
// User entries (non-readonly, from config/dts.json or config/dts/) always
// beat fallback entries (readonly, bundled defaults). The chain is run twice:
// first over user entries only, then — if nothing matched — over readonly
// entries. Within each pass, the priority order is:
//
//  1. type + id + platform + language  (exact)
//  2. type + id + platform              (entry has empty language)
//  3. default + type + platform + language
//  4. default + type + platform         (entry has empty language)
//  5. default + type + platform         (any language — last resort)
//
// Within each level the LAST match wins, so config/dts/ overrides
// config/dts.json since later-loaded files are appended to the entries slice.
func (ts *TemplateStore) selectEntry(templateType, platform, templateID, language string) *DTSEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	idLower := strings.ToLower(templateID)
	if e := selectEntryPass(ts.entries, templateType, platform, idLower, language, false); e != nil {
		return e
	}
	return selectEntryPass(ts.entries, templateType, platform, idLower, language, true)
}

// selectEntryPass walks the priority chain over a subset of entries
// (readonly==false → user entries; readonly==true → fallback entries).
//
// Runs the priority chain twice: first against entries whose Platform
// matches the request exactly, then — if nothing matched — against
// entries with Platform: "" (the platform wildcard). Matches the same
// pattern that empty Language already uses as a wildcard inside the
// chain. Lets authors ship one entry that serves both Discord and
// Telegram for simple text content (help pages, info dumps) while
// still honouring platform-specific templates where they exist.
func selectEntryPass(entries []DTSEntry, templateType, platform, idLower, language string, readonly bool) *DTSEntry {
	if m := selectEntryPassForPlatform(entries, templateType, platform, idLower, language, readonly); m != nil {
		return m
	}
	if platform != "" {
		return selectEntryPassForPlatform(entries, templateType, "", idLower, language, readonly)
	}
	return nil
}

// selectEntryPassForPlatform walks the 5-level priority chain for a
// specific platform value (possibly "" to target platform-agnostic
// entries). Extracted from selectEntryPass so the caller can retry with
// platform: "" when no platform-specific match exists.
func selectEntryPassForPlatform(entries []DTSEntry, templateType, platform, idLower, language string, readonly bool) *DTSEntry {
	var match *DTSEntry

	// 1. type + id + platform + language (exact)
	for i := range entries {
		e := &entries[i]
		if e.Readonly != readonly {
			continue
		}
		if e.Type == templateType &&
			strings.ToLower(e.ID.String()) == idLower &&
			e.Platform == platform &&
			e.Language == language {
			match = e
		}
	}
	if match != nil {
		return match
	}

	// 2. type + id + platform (entry has empty language)
	for i := range entries {
		e := &entries[i]
		if e.Readonly != readonly {
			continue
		}
		if e.Type == templateType &&
			strings.ToLower(e.ID.String()) == idLower &&
			e.Platform == platform &&
			e.Language == "" {
			match = e
		}
	}
	if match != nil {
		return match
	}

	// 3. default + type + platform + language
	for i := range entries {
		e := &entries[i]
		if e.Readonly != readonly {
			continue
		}
		if e.Default &&
			e.Type == templateType &&
			e.Platform == platform &&
			e.Language == language {
			match = e
		}
	}

	// 4. default + type + platform (entry has empty language)
	for i := range entries {
		e := &entries[i]
		if e.Readonly != readonly {
			continue
		}
		if e.Default &&
			e.Type == templateType &&
			e.Platform == platform &&
			e.Language == "" {
			match = e
		}
	}
	if match != nil {
		return match
	}

	// 5. default + type + platform (any language — last resort)
	for i := range entries {
		e := &entries[i]
		if e.Readonly != readonly {
			continue
		}
		if e.Default &&
			e.Type == templateType &&
			e.Platform == platform {
			match = e
		}
	}
	return match
}

// resolveTemplate produces the Handlebars template string from a DTSEntry.
func resolveTemplate(entry DTSEntry, configDir string) (string, error) {
	if entry.TemplateFile != "" {
		// templateFile: read as raw text — the file IS the Handlebars template.
		// Unlike inline templates (JSON objects that get stringified), templateFiles
		// may contain non-JSON constructs like unquoted Handlebars expressions in
		// JSON value positions (e.g. "color": {{#eq fortType 'pokestop'}}123{{/eq}}).
		path := filepath.Join(configDir, entry.TemplateFile)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read templateFile %s: %w", entry.TemplateFile, err)
		}
		return resolveIncludes(strings.TrimSpace(string(data)), configDir), nil
	}

	templateObj := entry.Template
	if templateObj == nil {
		return "", fmt.Errorf("entry has no template or templateFile")
	}

	// Join arrays and resolve @include directives
	templateObj = processTemplateValue(templateObj, configDir)

	// JSON.stringify the processed template object.
	// Use Encoder with SetEscapeHTML(false) to preserve <, >, & in Handlebars
	// expressions like {{#compare x '<' 100}}.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(templateObj); err != nil {
		return "", fmt.Errorf("marshal template: %w", err)
	}

	return strings.TrimSpace(buf.String()), nil
}

// processTemplateValue recursively walks the template object, joining arrays
// to strings and resolving @include directives in string values.
func processTemplateValue(v any, configDir string) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = processTemplateValue(child, configDir)
		}
		return out
	case []any:
		// Only join arrays of strings (DTS convention for multi-line descriptions).
		// Arrays containing objects (like embed.fields) must be preserved as arrays.
		allStrings := true
		for _, elem := range val {
			if _, ok := elem.(string); !ok {
				allStrings = false
				break
			}
		}
		if allStrings {
			var sb strings.Builder
			for _, elem := range val {
				sb.WriteString(elem.(string))
			}
			return resolveIncludes(sb.String(), configDir)
		}
		// Recurse into non-string arrays (e.g. fields array)
		out := make([]any, len(val))
		for i, elem := range val {
			out[i] = processTemplateValue(elem, configDir)
		}
		return out
	case string:
		return resolveIncludes(val, configDir)
	default:
		return val
	}
}

// TemplateInfo holds metadata about a single template for API responses.
type TemplateInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// TemplateMetadata returns template metadata grouped by platform → type → language.
// Hidden entries are excluded. When includeDescriptions is false, each language maps
// to a list of ID strings. When true, maps to a list of TemplateInfo objects.
// Empty language strings are replaced with "%" (matching alerter convention).
func (ts *TemplateStore) TemplateMetadata(includeDescriptions bool) map[string]any {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// platform -> type -> language -> list
	// Pre-seed both platforms so clients always see discord/telegram keys.
	result := map[string]any{
		"discord":  make(map[string]any),
		"telegram": make(map[string]any),
	}

	// Deduplicate so the dropdown never lists the same (type,platform,
	// language,id) twice when an override in config/dts/ shadows an entry
	// in config/dts.json.
	for _, e := range dedupEntriesPreferLast(ts.entries) {
		if e.Hidden {
			continue
		}

		platform := e.Platform
		lang := e.Language
		if lang == "" {
			lang = "%"
		}

		// Get or create platform map
		platformMap, ok := result[platform].(map[string]any)
		if !ok {
			platformMap = make(map[string]any)
			result[platform] = platformMap
		}

		// Get or create type map
		typeMap, ok := platformMap[e.Type].(map[string]any)
		if !ok {
			typeMap = make(map[string]any)
			platformMap[e.Type] = typeMap
		}

		if includeDescriptions {
			existing, _ := typeMap[lang].([]TemplateInfo)
			typeMap[lang] = append(existing, TemplateInfo{
				ID:          e.ID.String(),
				Name:        e.Name,
				Description: e.Description,
			})
		} else {
			existing, _ := typeMap[lang].([]string)
			typeMap[lang] = append(existing, e.ID.String())
		}
	}

	return result
}

// UserTemplateInfo holds template info for user-facing display.
type UserTemplateInfo struct {
	ID          string
	Name        string
	Description string
	IsDefault   bool
}

// ListForPlatform returns non-hidden templates for a given platform, grouped by type.
// Deduplicates by ID within each type (same ID can appear for different languages).
func (ts *TemplateStore) ListForPlatform(platform string) map[string][]UserTemplateInfo {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	type seen struct{ typ, id string }
	dedup := make(map[seen]bool)
	result := make(map[string][]UserTemplateInfo)

	for _, e := range ts.entries {
		if e.Hidden || e.Platform != platform {
			continue
		}
		id := e.ID.String()
		if id == "" {
			id = "default"
		}
		key := seen{e.Type, id}
		if dedup[key] {
			continue
		}
		dedup[key] = true

		result[e.Type] = append(result[e.Type], UserTemplateInfo{
			ID:          id,
			Name:        e.Name,
			Description: e.Description,
			IsDefault:   e.Default,
		})
	}
	return result
}

// TemplateSummary returns a map of type → platform → count for loaded templates.
// Hidden entries are excluded.
func (ts *TemplateStore) TemplateSummary() map[string]map[string]int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	result := make(map[string]map[string]int)
	for _, e := range ts.entries {
		if e.Hidden {
			continue
		}
		byPlatform, ok := result[e.Type]
		if !ok {
			byPlatform = make(map[string]int)
			result[e.Type] = byPlatform
		}
		byPlatform[e.Platform]++
	}
	return result
}

// TemplateSummaryDetailed returns type → platform → list of template IDs.
func (ts *TemplateStore) TemplateSummaryDetailed() map[string]map[string][]string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	result := make(map[string]map[string][]string)
	for _, e := range ts.entries {
		if e.Hidden {
			continue
		}
		byPlatform, ok := result[e.Type]
		if !ok {
			byPlatform = make(map[string][]string)
			result[e.Type] = byPlatform
		}
		id := string(e.ID)
		if id == "" {
			id = "default"
		}
		// Deduplicate (same ID can appear for different languages)
		found := slices.Contains(byPlatform[e.Platform], id)
		if !found {
			byPlatform[e.Platform] = append(byPlatform[e.Platform], id)
		}
	}
	return result
}

// LogSummary logs a summary of loaded templates and warns about types missing defaults.
func (ts *TemplateStore) LogSummary() {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	total := len(ts.entries)
	discordCount := 0
	telegramCount := 0
	for _, e := range ts.entries {
		switch e.Platform {
		case "discord":
			discordCount++
		case "telegram":
			telegramCount++
		}
	}

	partialCount := len(ts.partials)
	log.Infof("DTS loaded: %d templates (%d discord, %d telegram), %d partials", total, discordCount, telegramCount, partialCount)

	// Check for types missing default templates per platform
	// Collect all (type, platform) pairs that have entries
	type typePlatform struct {
		typ      string
		platform string
	}
	seen := make(map[typePlatform]bool)
	hasDefault := make(map[typePlatform]bool)
	for _, e := range ts.entries {
		key := typePlatform{e.Type, e.Platform}
		seen[key] = true
		if e.Default {
			hasDefault[key] = true
		}
	}
	for key := range seen {
		if !hasDefault[key] {
			log.Warnf("DTS: no default template for type=%q platform=%q", key.typ, key.platform)
		}
	}
}

// Partials returns the registered Handlebars partials as a name→template map.
func (ts *TemplateStore) Partials() map[string]string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	result := make(map[string]string, len(ts.partials))
	maps.Copy(result, ts.partials)
	return result
}

// FilteredEntries returns DTS entries matching the given filters.
// Empty filter values match all entries for that dimension.
func (ts *TemplateStore) FilteredEntries(filterType, filterPlatform, filterLanguage, filterID string) []DTSEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Deduplicate so the editor never sees two entries with the same key
	// (e.g. one in config/dts.json plus an override in config/dts/*.json).
	// dedupEntriesPreferLast keeps the last-loaded copy, which is the same
	// one selectEntry would pick at render time.
	deduped := dedupEntriesPreferLast(ts.entries)

	var result []DTSEntry
	for _, e := range deduped {
		if filterType != "" && e.Type != filterType {
			continue
		}
		if filterPlatform != "" && e.Platform != filterPlatform {
			continue
		}
		if filterLanguage != "" && e.Language != filterLanguage {
			continue
		}
		if filterID != "" && strings.ToLower(e.ID.String()) != strings.ToLower(filterID) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// GetEntry returns a copy of the entry matching the four key fields, or nil
// if not found. Used by the API to look up an entry without exposing internals.
// Returns the LAST matching entry in load order — the same one selectEntry
// would resolve at render time — so an override in config/dts/ correctly
// shadows an earlier entry in config/dts.json.
func (ts *TemplateStore) GetEntry(filterType, filterPlatform, filterLanguage, filterID string) *DTSEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	target := entryKey(&DTSEntry{
		Type: filterType, Platform: filterPlatform,
		Language: filterLanguage, ID: jsonID(filterID),
	})
	for i := len(ts.entries) - 1; i >= 0; i-- {
		if entryKey(&ts.entries[i]) == target {
			e := ts.entries[i]
			return &e
		}
	}
	return nil
}

// entryKey returns the matching key for a DTSEntry.
func entryKey(e *DTSEntry) string {
	return e.Type + "|" + e.Platform + "|" + e.Language + "|" + strings.ToLower(e.ID.String())
}

// dedupEntriesPreferLast returns a slice containing each entry keyed by
// entryKey, keeping only the last occurrence. Load order is
// fallback → config/dts.json → config/dts/*.json, so the "last" entry is
// the override that also wins at template selection time (selectEntryPass
// uses last-match-wins). Readonly fallback entries are dropped if any
// non-readonly override exists for the same (type,platform) combo to
// match FilteredEntries' existing "user took ownership of this surface"
// rule. Preserves the relative order of the retained entries.
func dedupEntriesPreferLast(entries []DTSEntry) []DTSEntry {
	hasUser := make(map[string]bool)
	for _, e := range entries {
		if !e.Readonly {
			hasUser[e.Type+"|"+e.Platform] = true
		}
	}

	// Walk backwards to find the last occurrence of each key; remember which
	// indices win so we can emit results in the original order.
	seen := make(map[string]int, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := &entries[i]
		if e.Readonly && hasUser[e.Type+"|"+e.Platform] {
			continue
		}
		k := entryKey(e)
		if _, ok := seen[k]; !ok {
			seen[k] = i
		}
	}

	result := make([]DTSEntry, 0, len(seen))
	for i, e := range entries {
		k := entryKey(&e)
		if idx, ok := seen[k]; ok && idx == i {
			result = append(result, e)
		}
	}
	return result
}

// entryFilename generates a filename for saving an entry to config/dts/.
// Format: {type}-{id}-{platform}[-{lang}].json
func entryFilename(e *DTSEntry) string {
	id := strings.ToLower(e.ID.String())
	if id == "" {
		id = "default"
	}
	name := e.Type + "-" + id + "-" + e.Platform
	if e.Language != "" {
		name += "-" + e.Language
	}
	return name + ".json"
}

// SaveEntry saves a single DTS entry: updates in-memory state, writes to its
// own file in config/dts/, and removes it from its previous source file.
// If the existing entry is readonly (e.g. from fallbacks), a new override
// entry is appended — the readonly original is left untouched, and the
// last-match-wins selection chain ensures the new entry takes precedence.
func (ts *TemplateStore) SaveEntry(inc DTSEntry) error {
	ts.mu.Lock()

	// Determine the save path up front so we can decide per-duplicate
	// whether its source file needs cleaning.
	dtsDir := filepath.Join(ts.configDir, "dts")
	savePath := filepath.Join(dtsDir, entryFilename(&inc))

	// Find all entries matching the incoming key. We treat the in-memory
	// state as authoritative about where duplicates live: every
	// non-readonly match whose source file isn't the target savePath gets
	// cleaned up, so stale copies in config/dts.json AND in differently-
	// named files under config/dts/ are both removed — not just the first
	// match we happen to encounter in load order.
	incKey := entryKey(&inc)
	isOverride := false
	oldSources := make(map[string]struct{})
	newIndices := make([]int, 0, 2)
	for i := range ts.entries {
		e := &ts.entries[i]
		if entryKey(e) != incKey {
			continue
		}
		if e.Readonly {
			isOverride = true
			continue
		}
		if e.sourceFile != "" && e.sourceFile != savePath {
			oldSources[e.sourceFile] = struct{}{}
		}
		newIndices = append(newIndices, i)
	}

	if len(newIndices) == 0 {
		// New entry (possibly overriding a readonly fallback).
		inc.sourceFile = savePath
		ts.entries = append(ts.entries, inc)
		if isOverride {
			log.Infof("dts: creating override for readonly template %s", entryFilename(&inc))
		}
	} else {
		// Update the first match in place and drop any later duplicates.
		keep := newIndices[0]
		e := &ts.entries[keep]
		e.Template = inc.Template
		e.TemplateFile = inc.TemplateFile
		e.Name = inc.Name
		e.Description = inc.Description
		e.Default = inc.Default
		e.Hidden = inc.Hidden
		e.sourceFile = savePath

		if len(newIndices) > 1 {
			drop := make(map[int]struct{}, len(newIndices)-1)
			for _, i := range newIndices[1:] {
				drop[i] = struct{}{}
			}
			filtered := ts.entries[:0]
			for i, entry := range ts.entries {
				if _, skip := drop[i]; skip {
					continue
				}
				filtered = append(filtered, entry)
			}
			ts.entries = filtered
		}
	}

	// Clear template cache
	ts.cache = make(map[string]*raymond.Template)
	ts.sourceCache = make(map[string]string)
	ts.tileUsage = make(map[string]bool)
	configDir := ts.configDir
	ts.mu.Unlock()

	// Ensure config/dts/ directory exists
	if err := os.MkdirAll(dtsDir, 0755); err != nil {
		return fmt.Errorf("create dts dir: %w", err)
	}

	// Write the entry to its own file (single-element array)
	if err := writeEntryFile(savePath, inc); err != nil {
		return err
	}

	// Clean up every old source file that referenced this key.
	for oldPath := range oldSources {
		if err := removeEntryFromFile(oldPath, incKey, configDir); err != nil {
			log.Warnf("dts: failed to remove old entry from %s: %v", oldPath, err)
		}
	}

	log.Infof("dts: saved template %s to %s", entryFilename(&inc), savePath)
	return nil
}

// DeleteEntry removes a single entry matching all four key fields.
// Removes from in-memory state and from the source file on disk.
// Returns an error if the entry is readonly or not found.
func (ts *TemplateStore) DeleteEntry(filterType, filterPlatform, filterLanguage, filterID string) error {
	ts.mu.Lock()

	target := DTSEntry{Type: filterType, Platform: filterPlatform, Language: filterLanguage, ID: jsonID(filterID)}
	targetKey := entryKey(&target)

	var sourceFile string
	found := false
	for i := range ts.entries {
		e := &ts.entries[i]
		if entryKey(e) == targetKey {
			if e.Readonly {
				ts.mu.Unlock()
				return fmt.Errorf("template %s/%s/%s/%s is readonly", e.Type, e.Platform, e.ID, e.Language)
			}
			sourceFile = e.sourceFile
			ts.entries = append(ts.entries[:i], ts.entries[i+1:]...)
			ts.cache = make(map[string]*raymond.Template)
			ts.sourceCache = make(map[string]string)
			ts.tileUsage = make(map[string]bool)
			found = true
			break
		}
	}

	configDir := ts.configDir
	ts.mu.Unlock()

	if !found {
		return fmt.Errorf("template not found")
	}

	// Remove from source file on disk
	if sourceFile != "" {
		if err := removeEntryFromFile(sourceFile, targetKey, configDir); err != nil {
			log.Warnf("dts: failed to remove entry from %s: %v", sourceFile, err)
		}
	}

	return nil
}

// writeEntryFile writes a single DTSEntry as a one-element JSON array.
func writeEntryFile(path string, entry DTSEntry) error {
	// Don't serialize internal fields
	clean := entry
	clean.sourceFile = ""
	clean.Readonly = false

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode([]DTSEntry{clean}); err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// removeEntryFromFile removes an entry (by key) from a JSON file containing
// an array of DTSEntry objects. If the file becomes empty, it is deleted
// (unless it's the main dts.json).
func removeEntryFromFile(filePath, targetKey, configDir string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var entries []DTSEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse %s: %w", filePath, err)
	}

	// Filter out the target entry
	var remaining []DTSEntry
	for _, e := range entries {
		if entryKey(&e) != targetKey {
			remaining = append(remaining, e)
		}
	}

	if len(remaining) == len(entries) {
		return nil // entry wasn't in this file
	}

	mainDTS := filepath.Join(configDir, "dts.json")
	if len(remaining) == 0 && filePath != mainDTS {
		// File is empty and it's not the main dts.json — delete it
		return os.Remove(filePath)
	}

	// Write back the remaining entries
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(remaining); err != nil {
		return fmt.Errorf("marshal remaining: %w", err)
	}
	return os.WriteFile(filePath, buf.Bytes(), 0644)
}

// resolveIncludes replaces @include directives in s with the file contents.
// Format: @include filename (the rest of the line after @include is the filename).
func resolveIncludes(s string, configDir string) string {
	for {
		idx := strings.Index(s, "@include ")
		if idx == -1 {
			return s
		}
		// Find the filename — goes to end of line or end of string
		start := idx + len("@include ")
		end := strings.IndexByte(s[start:], '\n')
		var filename string
		if end == -1 {
			filename = strings.TrimSpace(s[start:])
			end = len(s)
		} else {
			filename = strings.TrimSpace(s[start : start+end])
			end = start + end
		}
		// Read the include file
		path := filepath.Join(configDir, "dts", filename)
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warnf("dts: @include %s: %v", filename, err)
			// Remove the directive but keep going
			s = s[:idx] + s[end:]
			continue
		}
		s = s[:idx] + string(data) + s[end:]
	}
}
