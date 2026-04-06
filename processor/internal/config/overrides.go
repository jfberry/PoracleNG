package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	log "github.com/sirupsen/logrus"
)

// LoadOverrides reads config/overrides.json and returns the parsed map.
// Returns nil (not an error) if the file doesn't exist.
func LoadOverrides(configDir string) (map[string]any, error) {
	path := filepath.Join(configDir, "overrides.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read overrides.json: %w", err)
	}

	var overrides map[string]any
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, fmt.Errorf("parse overrides.json: %w", err)
	}

	log.Infof("config: loaded %d override sections from %s", len(overrides), path)
	return overrides, nil
}

// SaveOverrides reads the existing overrides.json, deep-merges the updates,
// and writes back. Creates the file if it doesn't exist.
func SaveOverrides(configDir string, updates map[string]any) error {
	path := filepath.Join(configDir, "overrides.json")

	// Read existing
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parse existing overrides.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing overrides.json: %w", err)
	}

	// Deep merge updates into existing
	deepMerge(existing, updates)

	// Write back
	buf, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal overrides: %w", err)
	}

	if err := os.WriteFile(path, buf, 0644); err != nil {
		return fmt.Errorf("write overrides.json: %w", err)
	}

	log.Infof("config: saved overrides to %s", path)
	return nil
}

// deepMerge merges src into dst. For nested maps, recurses. For everything
// else (scalars, arrays), src replaces dst.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
}

// ApplyOverrides walks the override map and sets matching fields on the
// Config struct using reflection. Fields are matched by TOML tag name.
func ApplyOverrides(cfg *Config, overrides map[string]any) {
	if overrides == nil {
		return
	}
	applyToStruct(reflect.ValueOf(cfg).Elem(), overrides)
}

// applyToStruct recursively applies override values to a struct.
func applyToStruct(v reflect.Value, overrides map[string]any) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldVal := v.Field(i)

		// Get TOML tag name
		tag := field.Tag.Get("toml")
		if tag == "" || tag == "-" {
			continue
		}
		// Strip tag options (e.g., "omitempty")
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}

		override, ok := overrides[tag]
		if !ok {
			continue
		}

		// If both the field and override are maps/structs, recurse
		if fieldVal.Kind() == reflect.Struct {
			if subMap, ok := override.(map[string]any); ok {
				applyToStruct(fieldVal, subMap)
				continue
			}
		}

		// Set the field value via JSON round-trip (handles type coercion)
		jsonBytes, err := json.Marshal(override)
		if err != nil {
			log.Warnf("config: override %s: marshal error: %v", tag, err)
			continue
		}

		target := reflect.New(fieldVal.Type())
		if err := json.Unmarshal(jsonBytes, target.Interface()); err != nil {
			log.Warnf("config: override %s: type mismatch: %v", tag, err)
			continue
		}

		fieldVal.Set(target.Elem())
		log.Debugf("config: applied override for %s", tag)
	}
}
