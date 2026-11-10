package config

import (
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestExampleConfigHasNoUnknownKeys decodes config/config.example.toml into
// the real Config struct and fails if any key is not consumed. Operators copy
// the example verbatim, so a key the processor doesn't read is a support trap:
// it looks like a setting but silently does nothing.
func TestExampleConfigHasNoUnknownKeys(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	examplePath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config", "config.example.toml")

	var cfg Config
	md, err := toml.DecodeFile(examplePath, &cfg)
	if err != nil {
		t.Fatalf("decode %s: %v", examplePath, err)
	}

	var unknown []string
	for _, k := range md.Undecoded() {
		unknown = append(unknown, k.String())
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		t.Errorf("config.example.toml key %q is not read by the processor config", k)
	}
}
