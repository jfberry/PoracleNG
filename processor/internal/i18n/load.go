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
//  1. Embedded locale JSON       — bundled processor-specific messages
//  2. resources/gamelocale/*.json — game data translations (pogo-translations identifier keys)
//  3. resources/locale/*.json    — game data translations (enRefMerged, for backward compat)
//  4. alerter/locale/*.json      — alerter message translations
//  5. config/custom.*.json       — admin overrides per locale
func Load(baseDir string) *Bundle {
	b := NewBundle()

	// 1. Embedded defaults (processor/internal/i18n/locale/*.json)
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

	// 2. resources/gamelocale/*.json (identifier-key game data translations)
	gameLocaleDir := filepath.Join(baseDir, "resources", "gamelocale")
	if err := b.LoadJSONDir(gameLocaleDir); err != nil {
		log.Debugf("i18n: no gamelocale dir at %s", gameLocaleDir)
	}

	// 3. resources/locale/*.json (enRefMerged game data translations, backward compat)
	resourcesDir := filepath.Join(baseDir, "resources", "locale")
	if err := b.LoadJSONDir(resourcesDir); err != nil {
		log.Debugf("i18n: no resources locale dir at %s", resourcesDir)
	}

	// 4. alerter/locale/*.json (alerter message translations)
	alerterDir := filepath.Join(baseDir, "alerter", "locale")
	if err := b.LoadJSONDir(alerterDir); err != nil {
		log.Debugf("i18n: no alerter locale dir at %s", alerterDir)
	}

	// 5. config/custom.{locale}.json (admin overrides)
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
