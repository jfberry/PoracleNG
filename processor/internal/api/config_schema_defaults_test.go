package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// loadDefaultConfig runs config.Load against a minimal config.toml so every
// built-in default (the struct literal plus Load's post-processing) is applied.
func loadDefaultConfig(t *testing.T) *config.Config {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	const minimal = "[database]\nuser = \"u\"\ndatabase = \"d\"\n"
	if err := os.WriteFile(filepath.Join(base, "config", "config.toml"), []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORACLE_HOST", "")
	cfg, err := config.Load(base)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

// TestConfigSchemaDefaultsMatchCode fails when a schema field has no backing
// config key, or when its advertised Default differs from what the processor
// actually uses when the key is omitted from config.toml. The config editor
// pre-fills these defaults, so drift silently changes behaviour on save.
func TestConfigSchemaDefaultsMatchCode(t *testing.T) {
	// Fields whose default is applied at the point of use rather than in
	// config.Load (the config struct holds 0/"" and the consumer substitutes
	// the documented value). The schema shows the effective value.
	appliedAtUse := map[string]bool{
		"geofence.default_name":                true, // geofence loader: "" -> "city"
		"geocoding.provider":                   true, // "" and "none" both disable geocoding
		"geocoding.static_provider":            true, // "" and "none" both disable tiles
		"tuning.tileserver_concurrency":        true, // staticmap.New fills tuning defaults
		"tuning.tileserver_timeout":            true,
		"tuning.tileserver_failure_threshold":  true,
		"tuning.tileserver_cooldown_ms":        true,
		"tuning.tileserver_queue_size":         true,
		"tuning.tileserver_deadline":           true,
		"tuning.tileserver_pregen_ttl":         true,
		"tracking.everything_flag_permissions": true, // only an explicit "deny" restricts; "" behaves as allow-any
		"logging.level":                        true, // logging.resolveLevel: "" -> info
	}
	// Relative path defaults that config.Load resolves to absolute paths;
	// the loaded value must end with the schema default.
	resolvedPath := map[string]bool{
		"geofence.paths":          true,
		"logging.filename":        true,
		"snapshots.path":          true,
		"webhookLogging.filename": true,
	}

	cfg := loadDefaultConfig(t)
	for _, section := range configSchema {
		sv := findConfigSection(cfg, section.Name)
		if !sv.IsValid() {
			t.Errorf("schema section %q has no matching config section", section.Name)
			continue
		}
		for _, field := range section.Fields {
			key := section.Name + "." + field.Name
			fv := getFieldByTag(sv, field.Name)
			if !fv.IsValid() {
				t.Errorf("schema field %s has no matching config key", key)
				continue
			}
			if appliedAtUse[key] || field.Default == nil {
				continue
			}
			want, _ := json.Marshal(field.Default)
			got, _ := json.Marshal(fv.Interface())
			if string(got) == "null" && (string(want) == "[]" || string(want) == "{}") {
				continue
			}
			if resolvedPath[key] {
				if !pathsHaveSuffix(fv.Interface(), field.Default) {
					t.Errorf("schema default for %s = %s, resolved code default = %s", key, want, got)
				}
				continue
			}
			if string(want) != string(got) {
				t.Errorf("schema default for %s = %s, code default = %s", key, want, got)
			}
		}
	}
}

// pathsHaveSuffix reports whether each resolved path (string or []string)
// ends with the corresponding relative schema default.
func pathsHaveSuffix(got, want any) bool {
	toSlice := func(v any) []string {
		switch x := v.(type) {
		case string:
			return []string{x}
		case []string:
			return x
		}
		return nil
	}
	g, w := toSlice(got), toSlice(want)
	if len(g) != len(w) || len(g) == 0 {
		return false
	}
	for i := range g {
		if !strings.HasSuffix(g[i], filepath.FromSlash(w[i])) {
			return false
		}
	}
	return true
}
