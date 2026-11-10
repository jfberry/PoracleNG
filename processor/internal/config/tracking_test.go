package config

import (
	"testing"
	"time"
)

func TestTrackingPokemonEditDefaults(t *testing.T) {
	cfg, err := loadFromReader(t, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Tracking.PokemonEdit {
		t.Error("pokemon_edit should default to true")
	}
	if cfg.Tracking.PokemonEditWindowMins != 5 {
		t.Errorf("pokemon_edit_window_mins should default to 5, got %d", cfg.Tracking.PokemonEditWindowMins)
	}
	if got := cfg.Tracking.PokemonEditMaxAge(); got != 5*time.Minute {
		t.Errorf("PokemonEditMaxAge() = %v, want 5m", got)
	}
}

func TestTrackingPokemonEditOverrides(t *testing.T) {
	cfg, err := loadFromReader(t, "[tracking]\npokemon_edit = false\npokemon_edit_window_mins = 12\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tracking.PokemonEdit {
		t.Error("pokemon_edit = false not honoured")
	}
	if got := cfg.Tracking.PokemonEditMaxAge(); got != 12*time.Minute {
		t.Errorf("PokemonEditMaxAge() = %v, want 12m", got)
	}
}

func TestPokemonEditMaxAgeNoLimit(t *testing.T) {
	for _, mins := range []int{0, -3} {
		tc := TrackingConfig{PokemonEditWindowMins: mins}
		if got := tc.PokemonEditMaxAge(); got != 0 {
			t.Errorf("window %d: PokemonEditMaxAge() = %v, want 0 (no limit)", mins, got)
		}
	}
}

func TestTrackingDefaultEdit(t *testing.T) {
	cfg, err := loadFromReader(t, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tracking.DefaultEditFor("pokemon") {
		t.Error("default_edit should be empty by default")
	}

	cfg, err = loadFromReader(t, "[tracking]\ndefault_edit = [\"pokemon\", \" Egg \", \"gym\"]\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for alertType, want := range map[string]bool{"pokemon": true, "egg": true, "raid": false, "lure": false} {
		if got := cfg.Tracking.DefaultEditFor(alertType); got != want {
			t.Errorf("DefaultEditFor(%q) = %v, want %v", alertType, got, want)
		}
	}
	if bad := cfg.Tracking.UnsupportedDefaultEdit(); len(bad) != 1 || bad[0] != "gym" {
		t.Errorf("UnsupportedDefaultEdit() = %v, want [gym]", bad)
	}
}
