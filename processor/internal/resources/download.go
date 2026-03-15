package resources

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

const (
	gameMasterURL = "https://raw.githubusercontent.com/WatWowMap/Masterfile-Generator/master/master-latest-poracle-v2.json"
	gruntsURL     = "https://raw.githubusercontent.com/WatWowMap/event-info/main/grunts/formatted.json"
	localeIndex   = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/index.json"
	localeBaseURL = "https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/static/enRefMerged/"
)

// Download fetches game data and locale files into the resources directory.
// On fetch failure, existing cached files are preserved.
func Download(baseDir string) error {
	dataDir := filepath.Join(baseDir, "resources", "data")
	localeDir := filepath.Join(baseDir, "resources", "locale")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(localeDir, 0o755); err != nil {
		return fmt.Errorf("create locale dir: %w", err)
	}

	downloadGameMaster(dataDir)
	downloadGrunts(dataDir)
	downloadLocales(localeDir)

	return nil
}

func downloadGameMaster(dataDir string) {
	log.Info("[Resources] Fetching latest Game Master...")

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

	log.Infof("[Resources] Game Master saved (%d categories)", len(master))
}

func downloadGrunts(dataDir string) {
	log.Info("[Resources] Fetching latest invasions...")

	body, err := httpGet(gruntsURL)
	if err != nil {
		log.Warnf("[Resources] Could not fetch grunts, using existing: %s", err)
		return
	}

	path := filepath.Join(dataDir, "grunts.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		log.Warnf("[Resources] Could not write %s: %s", path, err)
		return
	}

	log.Info("[Resources] Grunts saved")
}

func downloadLocales(localeDir string) {
	log.Info("[Resources] Fetching locales...")

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

	log.Infof("[Resources] Locales saved (%d files)", len(locales))
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
