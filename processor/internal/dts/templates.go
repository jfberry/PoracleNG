package dts

import (
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
	Template     any    `json:"template"`
	TemplateFile string `json:"templateFile"`
}

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
	entries, err := loadEntries(configDir, fallbackDir)
	if err != nil {
		return nil, err
	}
	ts.entries = entries
	return ts, nil
}

func loadEntries(configDir, fallbackDir string) ([]DTSEntry, error) {
	path := filepath.Join(configDir, "dts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read dts.json from config: %w", err)
		}
		path = filepath.Join(fallbackDir, "dts.json")
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read dts.json from fallback: %w", err)
		}
	}
	var entries []DTSEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse dts.json: %w", err)
	}
	return entries, nil
}

// Reload re-reads dts.json and clears the template cache.
func (ts *TemplateStore) Reload(configDir, fallbackDir string) error {
	entries, err := loadEntries(configDir, fallbackDir)
	if err != nil {
		return err
	}
	ts.mu.Lock()
	ts.entries = entries
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

	// Cache under write lock
	ts.mu.Lock()
	ts.cache[key] = compiled
	ts.mu.Unlock()

	return compiled
}

func cacheKey(templateType, platform, templateID, language string) string {
	return templateType + " " + platform + " " + templateID + " " + language
}

// selectEntry applies the selection chain to find the best matching entry.
func (ts *TemplateStore) selectEntry(templateType, platform, templateID, language string) *DTSEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	idLower := strings.ToLower(templateID)

	// 1. type + id + platform + language (exact)
	for i := range ts.entries {
		e := &ts.entries[i]
		if e.Type == templateType &&
			strings.ToLower(e.ID.String()) == idLower &&
			e.Platform == platform &&
			e.Language == language {
			return e
		}
	}

	// 2. type + id + platform (no language — entry has empty language)
	for i := range ts.entries {
		e := &ts.entries[i]
		if e.Type == templateType &&
			strings.ToLower(e.ID.String()) == idLower &&
			e.Platform == platform &&
			e.Language == "" {
			return e
		}
	}

	// 3. default + type + platform + language
	for i := range ts.entries {
		e := &ts.entries[i]
		if e.Default &&
			e.Type == templateType &&
			e.Platform == platform &&
			e.Language == language {
			return e
		}
	}

	// 4. default + type + platform (no language — entry has empty language)
	for i := range ts.entries {
		e := &ts.entries[i]
		if e.Default &&
			e.Type == templateType &&
			e.Platform == platform &&
			e.Language == "" {
			return e
		}
	}

	// 5. default + type + platform (any language — last resort)
	for i := range ts.entries {
		e := &ts.entries[i]
		if e.Default &&
			e.Type == templateType &&
			e.Platform == platform {
			return e
		}
	}

	return nil
}

// resolveTemplate produces the Handlebars template string from a DTSEntry.
func resolveTemplate(entry DTSEntry, configDir string) (string, error) {
	var templateObj any

	if entry.TemplateFile != "" {
		// Read external template file
		path := filepath.Join(configDir, entry.TemplateFile)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read templateFile %s: %w", entry.TemplateFile, err)
		}
		if err := json.Unmarshal(data, &templateObj); err != nil {
			return "", fmt.Errorf("parse templateFile %s: %w", entry.TemplateFile, err)
		}
	} else {
		templateObj = entry.Template
	}

	if templateObj == nil {
		return "", fmt.Errorf("entry has no template or templateFile")
	}

	// Join arrays and resolve @include directives
	templateObj = processTemplateValue(templateObj, configDir)

	// JSON.stringify the processed template object
	result, err := json.Marshal(templateObj)
	if err != nil {
		return "", fmt.Errorf("marshal template: %w", err)
	}

	return string(result), nil
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
		// Join array elements to a single string
		var sb strings.Builder
		for _, elem := range val {
			sb.WriteString(fmt.Sprintf("%v", elem))
		}
		return resolveIncludes(sb.String(), configDir)
	case string:
		return resolveIncludes(val, configDir)
	default:
		return val
	}
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
