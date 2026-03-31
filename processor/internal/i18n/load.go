package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Load creates a Bundle with translations from multiple sources, merged in
// order (later wins):
//
//  1. resources/gamelocale/*.json — game data translations (pogo-translations identifier keys)
//  2. Embedded locale JSON       — bundled processor-specific messages (can override game data)
//  3. config/custom.*.json       — admin overrides per locale
func Load(baseDir string) *Bundle {
	b := NewBundle()

	if baseDir != "" {
		// 1. resources/gamelocale/*.json (identifier-key game data translations)
		gameLocaleDir := filepath.Join(baseDir, "resources", "gamelocale")
		if err := b.LoadJSONDir(gameLocaleDir); err != nil {
			log.Debugf("i18n: no gamelocale dir at %s", gameLocaleDir)
		}
	}

	// 2. Embedded defaults (processor/internal/i18n/locale/*.json)
	// Loaded AFTER gamelocale so processor-specific overrides win
	// (e.g. weather_1: "Sunny" overrides gamelocale's "Clear")
	sub, err := fs.Sub(embeddedLocales, "locale")
	if err != nil {
		log.Errorf("i18n: failed to access embedded locales: %s", err)
		return b
	}
	if err := b.LoadJSONFS(sub); err != nil {
		log.Errorf("i18n: failed to load embedded locales: %s", err)
	}

	if baseDir == "" {
		return b
	}

	// 3. config/custom.{locale}.json (admin overrides)
	configDir := filepath.Join(baseDir, "config")
	entries, err := os.ReadDir(configDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, "custom.") || !strings.HasSuffix(name, ".json") {
				continue
			}
			// custom.de.json → locale "de"
			locale := strings.TrimSuffix(strings.TrimPrefix(name, "custom."), ".json")
			if locale == "" {
				continue
			}
			path := filepath.Join(configDir, name)
			if err := b.LoadCustomFile(path, locale); err != nil {
				log.Warnf("i18n: failed to load custom locale %s: %s", path, err)
			} else {
				log.Infof("i18n: loaded custom overrides from %s", path)
			}
		}
	}

	return b
}
