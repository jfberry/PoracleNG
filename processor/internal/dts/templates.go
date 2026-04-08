package dts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	TemplateFile string `json:"templateFile"`

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
	partials    map[string]string
	configDir   string
	fallbackDir string
}

// LoadTemplates reads dts.json from configDir (preferred) or fallbackDir.
func LoadTemplates(configDir, fallbackDir string) (*TemplateStore, error) {
	ts := &TemplateStore{
		cache:       make(map[string]*raymond.Template),
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

	log.Infof("dts: loaded %d partials from %s", len(partials), path)
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
func selectEntryPass(entries []DTSEntry, templateType, platform, idLower, language string, readonly bool) *DTSEntry {
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
	result := make(map[string]any)

	for _, e := range ts.entries {
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
		found := false
		for _, existing := range byPlatform[e.Platform] {
			if existing == id {
				found = true
				break
			}
		}
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

	log.Infof("DTS loaded: %d templates (%d discord, %d telegram)", total, discordCount, telegramCount)

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
	for k, v := range ts.partials {
		result[k] = v
	}
	return result
}

// FilteredEntries returns DTS entries matching the given filters.
// Empty filter values match all entries for that dimension.
func (ts *TemplateStore) FilteredEntries(filterType, filterPlatform, filterLanguage, filterID string) []DTSEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// First pass: record (type|platform) combos that have at least one user
	// (non-readonly) entry. Fallback (readonly) entries for those combos are
	// suppressed in the second pass — the user has already taken ownership of
	// that surface in the editor and the bundled defaults would just be noise.
	hasUser := make(map[string]bool)
	for _, e := range ts.entries {
		if !e.Readonly {
			hasUser[e.Type+"|"+e.Platform] = true
		}
	}

	var result []DTSEntry
	for _, e := range ts.entries {
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
		if e.Readonly && hasUser[e.Type+"|"+e.Platform] {
			continue
		}
		result = append(result, e)
	}
	return result
}

// entryKey returns the matching key for a DTSEntry.
func entryKey(e *DTSEntry) string {
	return e.Type + "|" + e.Platform + "|" + e.Language + "|" + strings.ToLower(e.ID.String())
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

	// Find existing entries. There may be multiple matches (e.g. one from
	// fallback readonly + one from config/dts.json). We need to:
	// - Note if any are readonly (so we know to append an override)
	// - Update the first non-readonly match in place
	// - Record the old source file so we can clean it up on disk
	var oldSourceFile string
	found := false
	isOverride := false
	incKey := entryKey(&inc)
	for i := range ts.entries {
		e := &ts.entries[i]
		if entryKey(e) != incKey {
			continue
		}
		if e.Readonly {
			isOverride = true
			continue // keep looking for a non-readonly copy to update
		}
		oldSourceFile = e.sourceFile
		// Update in place
		e.Template = inc.Template
		e.TemplateFile = inc.TemplateFile
		e.Name = inc.Name
		e.Description = inc.Description
		e.Default = inc.Default
		e.Hidden = inc.Hidden
		found = true
		break
	}

	if !found {
		// New entry (or override of readonly)
		ts.entries = append(ts.entries, inc)
		if isOverride {
			log.Infof("dts: creating override for readonly template %s", entryFilename(&inc))
		}
	}

	// Determine the save path
	dtsDir := filepath.Join(ts.configDir, "dts")
	savePath := filepath.Join(dtsDir, entryFilename(&inc))

	// Update the source file on the entry
	for i := range ts.entries {
		if entryKey(&ts.entries[i]) == incKey {
			ts.entries[i].sourceFile = savePath
			break
		}
	}

	// Clear template cache
	ts.cache = make(map[string]*raymond.Template)
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

	// Remove from previous source file (if it was in a config file, not fallback)
	if oldSourceFile != "" && oldSourceFile != savePath {
		if err := removeEntryFromFile(oldSourceFile, incKey, configDir); err != nil {
			log.Warnf("dts: failed to remove old entry from %s: %v", oldSourceFile, err)
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
