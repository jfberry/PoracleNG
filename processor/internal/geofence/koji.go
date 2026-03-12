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

// FetchKojiGeofences downloads geofences from HTTP URLs with Koji authentication.
func FetchKojiGeofences(paths []string, bearerToken, cacheDir string) error {
	if bearerToken == "" {
		log.Info("[KŌJI] Kōji bearer token not found, skipping")
		return nil
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	for _, path := range paths {
		if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
			continue
		}

		log.Infof("[KŌJI] Fetching %s...", path)

		if err := fetchKojiFence(path, bearerToken, cacheDir); err != nil {
			log.Warnf("[KŌJI] Could not process %s: %v", path, err)
			continue
		}
	}

	return nil
}

func fetchKojiFence(url, bearerToken, cacheDir string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Extract .data field from response
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	// Write to cache file
	cacheFile := filepath.Join(cacheDir, sanitizeURL(url)+".json")
	if err := os.WriteFile(cacheFile, response.Data, 0644); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}

	log.Infof("[KŌJI] Cached %s to %s", url, cacheFile)
	return nil
}

// sanitizeURL converts a URL to a safe filename by replacing slashes with double underscores.
func sanitizeURL(url string) string {
	return strings.ReplaceAll(url, "/", "__")
}
