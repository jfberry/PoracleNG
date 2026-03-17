package geofence

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

// FetchKojiGeofences downloads geofence data from HTTP URLs using a Koji bearer token,
// caching the results locally. If a fetch fails, the cached version (if any) is preserved.
func FetchKojiGeofences(paths []string, bearerToken string, cacheDir string) error {
	if bearerToken == "" {
		log.Debug("Koji bearer token not configured, skipping geofence fetch")
		return nil
	}

	if cacheDir == "" {
		cacheDir = "config/.cache/geofences"
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create koji cache dir %s: %w", cacheDir, err)
	}

	for _, p := range paths {
		if !strings.HasPrefix(p, "http://") && !strings.HasPrefix(p, "https://") {
			continue
		}
		if err := fetchKojiFence(p, bearerToken, cacheDir); err != nil {
			cachePath := filepath.Join(cacheDir, sanitizeURL(p)+".json")
			if _, statErr := os.Stat(cachePath); statErr == nil {
				log.Warnf("[KOJI] Failed to fetch %s, using cached version: %s", p, err)
			} else {
				log.Warnf("[KOJI] Failed to fetch %s and no cache available: %s", p, err)
			}
		}
	}
	return nil
}

func fetchKojiFence(url, bearerToken, cacheDir string) error {
	log.Infof("[KOJI] Fetching %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	// Koji wraps the GeoJSON in a { "data": ... } envelope
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if envelope.Data == nil {
		return fmt.Errorf("response has no 'data' field")
	}

	cachePath := filepath.Join(cacheDir, sanitizeURL(url)+".json")
	if err := os.WriteFile(cachePath, envelope.Data, 0o644); err != nil {
		return fmt.Errorf("write cache %s: %w", cachePath, err)
	}

	log.Infof("[KOJI] Cached %s -> %s", url, cachePath)
	return nil
}

func sanitizeURL(url string) string {
	return strings.ReplaceAll(url, "/", "__")
}
