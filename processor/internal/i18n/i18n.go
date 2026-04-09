// Package i18n provides message translation for the processor.
//
// Translations use flat key-value JSON files — the same format as the alerter's
// locale files and pogo-translations. This means one format across the Go
// processor, Node alerter, and React frontend.
//
// Placeholders use {0}, {1}, ... syntax (matching the alerter's convention).
//
// Merge order (later wins):
//  1. Embedded locale JSON  (bundled defaults for processor-specific messages)
//  2. External locale dir   (e.g. resources/locale/ — game data + shared strings)
//  3. Alerter locale dir    (e.g. alerter/locale/ — alerter message strings)
//  4. Custom overrides      (e.g. config/custom.{locale}.json — admin overrides)
//
// Supported by Crowdin, Transifex, Weblate, POEditor, and most i18n platforms.
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var placeholderRe = regexp.MustCompile(`\{(\d+)\}`)

// Translator resolves translated strings for a single locale.
type Translator struct {
	lang     string
	messages map[string]string // key → translated string
	fallback *Translator       // English fallback for missing keys (nil for English itself)
}

// T returns the translation of key, falling back to English, or key itself if
// neither has it.
func (t *Translator) T(key string) string {
	if t == nil || t.messages == nil {
		return key
	}
	if s, ok := t.messages[key]; ok && s != "" {
		return s
	}
	if t.fallback != nil {
		if s, ok := t.fallback.messages[key]; ok && s != "" {
			return s
		}
	}
	return key
}

// Tf translates key and replaces {0}, {1}, ... with the given args.
func (t *Translator) Tf(key string, args ...any) string {
	return Format(t.T(key), args...)
}

// TfNamed translates a key and substitutes %{name} placeholders from the map.
func (t *Translator) TfNamed(key string, values map[string]string) string {
	return FormatNamed(t.T(key), values)
}

// Lang returns the locale code for this translator.
func (t *Translator) Lang() string {
	if t == nil {
		return "en"
	}
	return t.lang
}

// FormatNamed replaces %{name} placeholders in a string with values from a map.
// Used for gamelocale translations that use named placeholders like %{amount_0}, %{pokemon}.
func FormatNamed(s string, values map[string]string) string {
	for k, v := range values {
		s = strings.ReplaceAll(s, "%{"+k+"}", v)
	}
	return s
}

// Format replaces {0}, {1}, ... placeholders in s with the given args.
func Format(s string, args ...any) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(match string) string {
		var idx int
		if _, err := fmt.Sscanf(match, "{%d}", &idx); err == nil && idx < len(args) {
			return fmt.Sprintf("%v", args[idx])
		}
		return match
	})
}

// NewTranslator creates a Translator for the given locale with pre-populated messages.
// Useful for testing or building translators outside the Bundle merge pipeline.
func NewTranslator(lang string, msgs map[string]string) *Translator {
	return &Translator{lang: lang, messages: msgs}
}

// Bundle holds translators for all loaded locales.
type Bundle struct {
	mu          sync.RWMutex
	translators map[string]*Translator // locale code → Translator
}

// NewBundle creates an empty bundle.
func NewBundle() *Bundle {
	return &Bundle{
		translators: make(map[string]*Translator),
	}
}

// AddTranslator adds a pre-built Translator to the bundle.
// If a translator for the same locale already exists, its messages are merged
// (new keys override existing ones).
func (b *Bundle) AddTranslator(t *Translator) {
	if t == nil {
		return
	}
	b.merge(t.lang, t.messages)
}

// LoadJSONDir loads all .json files from a directory, merging into existing
// translators. File names should be "{locale}.json" (e.g. "de.json", "fr.json").
// Keys from later loads override earlier ones (merge semantics).
func (b *Bundle) LoadJSONDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading locale dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		locale := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(dir, e.Name())
		msgs, err := loadJSONFile(path)
		if err != nil {
			return fmt.Errorf("loading %s: %w", path, err)
		}
		b.merge(locale, msgs)
	}
	return nil
}

// LoadJSONFS loads all .json files from an embedded filesystem, merging into
// existing translators.
func (b *Bundle) LoadJSONFS(fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		locale := strings.TrimSuffix(filepath.Base(path), ".json")
		f, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		var msgs map[string]string
		if err := json.NewDecoder(f).Decode(&msgs); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		b.merge(locale, msgs)
		return nil
	})
}

// LoadCustomFile loads a single custom.{locale}.json override file.
func (b *Bundle) LoadCustomFile(path string, locale string) error {
	msgs, err := loadJSONFile(path)
	if err != nil {
		return err
	}
	b.merge(locale, msgs)
	return nil
}

// For returns the translator for the given locale, falling back to English.
func (b *Bundle) For(locale string) *Translator {
	if locale == "" {
		locale = "en"
	}
	b.mu.RLock()
	t, ok := b.translators[locale]
	b.mu.RUnlock()
	if ok {
		return t
	}
	// Try base language (e.g. "pt" from "pt-br")
	if idx := strings.IndexAny(locale, "-_"); idx > 0 {
		b.mu.RLock()
		t, ok = b.translators[locale[:idx]]
		b.mu.RUnlock()
		if ok {
			return t
		}
	}
	// Fall back to English
	b.mu.RLock()
	t = b.translators["en"]
	b.mu.RUnlock()
	if t != nil {
		return t
	}
	return &Translator{lang: "en"}
}

// merge adds msgs into the translator for locale, creating it if needed.
func (b *Bundle) merge(locale string, msgs map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	t, ok := b.translators[locale]
	if !ok {
		t = &Translator{lang: locale, messages: make(map[string]string)}
		// Non-English translators fall back to English for missing keys
		if locale != "en" {
			t.fallback = b.translators["en"]
		}
		b.translators[locale] = t
	}
	for k, v := range msgs {
		t.messages[k] = v
	}
}

// linkFallbacks sets the English fallback on all non-English translators.
// Called after all locale files are loaded to handle the case where English
// was loaded after another language.
func (b *Bundle) LinkFallbacks() {
	b.mu.Lock()
	defer b.mu.Unlock()
	en := b.translators["en"]
	if en == nil {
		return
	}
	for locale, t := range b.translators {
		if locale != "en" {
			t.fallback = en
		}
	}
}

func loadJSONFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}
