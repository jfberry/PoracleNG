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
	gameMasterURL    = "https://raw.githubusercontent.com/WatWowMap/Masterfile-Generator/master/master-latest-poracle-v2.json"
	rawMasterURL     = "https://raw.githubusercontent.com/WatWowMap/Masterfile-Generator/master/master-latest-raw.json"
	gruntsURL        = "https://raw.githubusercontent.com/WatWowMap/event-info/main/grunts/classic.json"
	localeIndex      = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/index.json"
	localeBaseURL    = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/static/enRefMerged/"
	localesBaseURL   = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/static/locales/"
	manualBaseURL    = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/static/manual/"
)

// Download fetches game data and locale files into the resources directory.
// On fetch failure, existing cached files are preserved.
func Download(baseDir string) error {
	dataDir := filepath.Join(baseDir, "resources", "data")
	rawDir := filepath.Join(baseDir, "resources", "rawdata")
	localeDir := filepath.Join(baseDir, "resources", "locale")
	gameLocaleDir := filepath.Join(baseDir, "resources", "gamelocale")

	for _, dir := range []string{dataDir, rawDir, localeDir, gameLocaleDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Poracle-v2 data + enRefMerged locales: used by the alerter only.
	// Remove these once the alerter is fully migrated to use processor enrichment.
	downloadGameMaster(dataDir)
	downloadLocales(localeDir)

	// Grunt data (classic.json format) saved to rawdata/ for the processor.
	downloadGrunts(rawDir)

	// Raw masterfile + identifier-key locales: used by the processor.
	downloadRawMaster(rawDir)
	downloadGameLocales(gameLocaleDir)

	return nil
}

func downloadGameMaster(dataDir string) {
	log.Info("[Resources] Fetching latest Game Master (poracle-v2)...")

	body, err := httpGet(gameMasterURL)
	if err != nil {
		log.Warnf("[Resources] Could not fetch Game Master, using existing: %s", err)
		return
	}

	// Game master is an object with category keys (monsters, moves, items, etc.)
	var master map[string]json.RawMessage
	if err := json.Unmarshal(body, &master); err != nil {
		log.Warnf("[Resources] Could not parse Game Master: %s", err)
		return
	}

	for category, data := range master {
		path := filepath.Join(dataDir, category+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Warnf("[Resources] Could not write %s: %s", path, err)
		}
	}

	log.Infof("[Resources] Game Master (poracle-v2) saved (%d categories)", len(master))
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

// downloadLocales fetches enRefMerged locale files (English-as-key format) for the alerter.
func downloadLocales(localeDir string) {
	log.Info("[Resources] Fetching locales (enRefMerged)...")

	body, err := httpGet(localeIndex)
	if err != nil {
		log.Warnf("[Resources] Could not fetch locale index, using existing: %s", err)
		return
	}

	var locales []string
	if err := json.Unmarshal(body, &locales); err != nil {
		log.Warnf("[Resources] Could not parse locale index: %s", err)
		return
	}

	for _, locale := range locales {
		data, err := httpGet(localeBaseURL + locale)
		if err != nil {
			log.Warnf("[Resources] Could not fetch locale %s: %s", locale, err)
			continue
		}

		path := filepath.Join(localeDir, locale)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Warnf("[Resources] Could not write %s: %s", path, err)
			continue
		}
	}

	log.Infof("[Resources] Locales (enRefMerged) saved (%d files)", len(locales))
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
