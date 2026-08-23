package gamedata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCostumeTranslationKey(t *testing.T) {
	if got := CostumeTranslationKey(1); got != "costume_1" {
		t.Errorf("CostumeTranslationKey(1) = %q, want costume_1", got)
	}
}

func TestCostumesLoaded(t *testing.T) {
	gd := loadTestGameData(t) // reuse the existing gamedata test loader
	if gd.Costumes == nil || gd.Costumes[1].Name == "" {
		t.Fatalf("costume 1 not loaded; got %+v", gd.Costumes[1])
	}
}

// rawFilesRequiredByLoad mirrors the filenames Load reads directly from
// resources/rawdata (everything except costumes.json, which is loaded
// separately by design and must degrade gracefully on its own — that's
// exactly the path these negative tests exercise).
var rawFilesRequiredByLoad = []string{
	"pokemon.json",
	"forms.json",
	"moves.json",
	"types.json",
	"items.json",
	"invasions.json",
	"weather.json",
}

// newTempResourceDirWithoutCostumes builds a temp baseDir whose
// resources/rawdata symlinks to the real fixture files Load needs
// (pokemon.json alone is >1MB, so symlinking avoids copying fixtures),
// deliberately omitting costumes.json so callers can control it directly.
func newTempResourceDirWithoutCostumes(t *testing.T, realBaseDir string) string {
	t.Helper()
	// Symlink targets are resolved relative to the symlink's own directory,
	// not the process cwd, so the source paths must be absolute — realBaseDir
	// as returned by testBaseDir(t) is relative ("../../../").
	absRealBaseDir, err := filepath.Abs(realBaseDir)
	if err != nil {
		t.Fatalf("resolve absolute path for %s: %v", realBaseDir, err)
	}

	tempBase := t.TempDir()
	tempRawDir := filepath.Join(tempBase, "resources", "rawdata")
	if err := os.MkdirAll(tempRawDir, 0o755); err != nil {
		t.Fatalf("mkdir temp rawdata dir: %v", err)
	}
	realRawDir := filepath.Join(absRealBaseDir, "resources", "rawdata")
	for _, name := range rawFilesRequiredByLoad {
		if err := os.Symlink(filepath.Join(realRawDir, name), filepath.Join(tempRawDir, name)); err != nil {
			t.Fatalf("symlink %s: %v", name, err)
		}
	}
	return tempBase
}

// TestCostumesLoad_MissingFile is the negative-path counterpart to
// TestCostumesLoaded: a missing costumes.json must not fail Load() or
// panic — costume data is additive, so it should degrade to an empty
// GameData.Costumes rather than taking down the whole processor.
func TestCostumesLoad_MissingFile(t *testing.T) {
	realBaseDir := testBaseDir(t)
	tempBase := newTempResourceDirWithoutCostumes(t, realBaseDir)
	// Deliberately do not create costumes.json in the temp rawdata dir.

	gd, err := Load(tempBase)
	if err != nil {
		t.Fatalf("Load() should degrade gracefully when costumes.json is missing, got error: %v", err)
	}
	if len(gd.Costumes) != 0 {
		t.Fatalf("expected no costumes loaded when costumes.json is missing, got %d entries", len(gd.Costumes))
	}
}

// TestCostumesLoad_MalformedFile covers the parse-failure half of the same
// graceful-degradation contract: invalid JSON in costumes.json must not
// fail Load() or panic either.
func TestCostumesLoad_MalformedFile(t *testing.T) {
	realBaseDir := testBaseDir(t)
	tempBase := newTempResourceDirWithoutCostumes(t, realBaseDir)
	costumesPath := filepath.Join(tempBase, "resources", "rawdata", "costumes.json")
	if err := os.WriteFile(costumesPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write malformed costumes.json: %v", err)
	}

	gd, err := Load(tempBase)
	if err != nil {
		t.Fatalf("Load() should degrade gracefully when costumes.json is malformed, got error: %v", err)
	}
	if len(gd.Costumes) != 0 {
		t.Fatalf("expected no costumes loaded when costumes.json is malformed, got %d entries", len(gd.Costumes))
	}
}
