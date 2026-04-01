package resources

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

const (
	rawMasterURL   = "https://raw.githubusercontent.com/WatWowMap/Masterfile-Generator/master/master-latest-raw.json"
	gruntsURL      = "https://raw.githubusercontent.com/WatWowMap/event-info/main/grunts/classic.json"
	localeIndex    = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/index.json"
	localesBaseURL = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/static/locales/"
	manualBaseURL  = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/static/manual/"
)

// Download fetches game data and locale files into the resources directory.
// On fetch failure, existing cached files are preserved.
func Download(baseDir string) error {
	rawDir := filepath.Join(baseDir, "resources", "rawdata")
	gameLocaleDir := filepath.Join(baseDir, "resources", "gamelocale")

	for _, dir := range []string{rawDir, gameLocaleDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Grunt data (classic.json format) saved to rawdata/ for the processor.
	downloadGrunts(rawDir)

	// Raw masterfile + identifier-key locales: used by the processor.
	downloadRawMaster(rawDir)
	downloadGameLocales(gameLocaleDir)

	return nil
}

func downloadRawMaster(rawDir string) {
	log.Info("[Resources] Fetching latest Raw Game Master...")

	body, err := httpGet(rawMasterURL)
	if err != nil {
		log.Warnf("[Resources] Could not fetch Raw Game Master, using existing: %s", err)
		return
	}

	var master map[string]json.RawMessage
	if err := json.Unmarshal(body, &master); err != nil {
		log.Warnf("[Resources] Could not parse Raw Game Master: %s", err)
		return
	}

	for category, data := range master {
		// Skip invasions — we use classic.json from downloadGrunts instead
		// of the raw masterfile's formatted invasion data
		if category == "invasions" {
			continue
		}
		path := filepath.Join(rawDir, category+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Warnf("[Resources] Could not write %s: %s", path, err)
		}
	}

	log.Infof("[Resources] Raw Game Master saved (%d categories)", len(master))
}

func downloadGrunts(rawDir string) {
	log.Info("[Resources] Fetching latest invasions (classic.json)...")

	body, err := httpGet(gruntsURL)
	if err != nil {
		log.Warnf("[Resources] Could not fetch grunts, using existing: %s", err)
		return
	}

	path := filepath.Join(rawDir, "invasions.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		log.Warnf("[Resources] Could not write %s: %s", path, err)
		return
	}

	log.Info("[Resources] Grunts saved (classic.json → rawdata/invasions.json)")
}

// downloadGameLocales fetches identifier-key locale files (locales/ + manual/) for the processor.
// For each language, downloads both locales/{lang}.json and manual/{lang}.json,
// merges them (manual overrides locales), and writes to resources/gamelocale/{lang}.json.
func downloadGameLocales(gameLocaleDir string) {
	log.Info("[Resources] Fetching game locales (identifier-key)...")

	// Get available locales from the index
	body, err := httpGet(localeIndex)
	if err != nil {
		log.Warnf("[Resources] Could not fetch locale index for game locales, using existing: %s", err)
		return
	}

	var locales []string
	if err := json.Unmarshal(body, &locales); err != nil {
		log.Warnf("[Resources] Could not parse locale index: %s", err)
		return
	}

	count := 0
	for _, locale := range locales {
		lang := strings.TrimSuffix(locale, ".json")
		if lang == "" {
			continue
		}

		// Download locales/{lang}.json (auto-generated identifier keys)
		localeData, err := httpGet(localesBaseURL + locale)
		if err != nil {
			log.Debugf("[Resources] Could not fetch game locale %s: %s", locale, err)
			continue
		}

		var merged map[string]string
		if err := json.Unmarshal(localeData, &merged); err != nil {
			log.Warnf("[Resources] Could not parse game locale %s: %s", locale, err)
			continue
		}

		// Download manual/{lang}.json (hand-curated overrides)
		manualData, err := httpGet(manualBaseURL + locale)
		if err == nil {
			var manual map[string]string
			if err := json.Unmarshal(manualData, &manual); err == nil {
				// Manual overrides locales
				for k, v := range manual {
					merged[k] = v
				}
			}
		}

		// Write merged result
		outData, err := json.Marshal(merged)
		if err != nil {
			log.Warnf("[Resources] Could not marshal game locale %s: %s", locale, err)
			continue
		}

		path := filepath.Join(gameLocaleDir, locale)
		if err := os.WriteFile(path, outData, 0o644); err != nil {
			log.Warnf("[Resources] Could not write %s: %s", path, err)
			continue
		}
		count++
	}

	log.Infof("[Resources] Game locales (identifier-key) saved (%d files)", count)
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
