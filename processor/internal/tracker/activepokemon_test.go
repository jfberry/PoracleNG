package tracker

import (
	"testing"
	"time"
)

func TestActivePokemonTracker_RegisterAndGet(t *testing.T) {
	apt := NewActivePokemonTracker(50)

	future := time.Now().Unix() + 3600

	apt.Register("cell1", "user1", "enc1", ActivePokemon{
		PokemonID:     1,
		Form:          0,
		IV:            98.0,
		CP:            2500,
		Latitude:      40.0,
		Longitude:     -74.0,
		DisappearTime: future,
		Weather:       1,            // boosted by clear
		Types:         []int{4, 12}, // poison, grass — grass boosted by clear (1)
	})

	apt.Register("cell1", "user1", "enc2", ActivePokemon{
		PokemonID:     7,
		Form:          0,
		IV:            89.0,
		CP:            1800,
		Latitude:      40.1,
		Longitude:     -74.1,
		DisappearTime: future,
		Weather:       0,        // not boosted
		Types:         []int{7}, // water — boosted by rainy (2)
	})

	// Weather changes from clear (1) to rainy (2)
	// - Bulbasaur (grass): was boosted by clear, rainy doesn't boost grass → affected (losing boost)
	// - Squirtle (water): was not boosted, rainy boosts water → affected (gaining boost)
	affected := apt.GetAffectedPokemon("cell1", "user1", 1, 2, 10)
	if len(affected) != 2 {
		t.Fatalf("expected 2 affected, got %d", len(affected))
	}

	// Weather changes from clear (1) to cloudy (4)
	// - Bulbasaur (poison/grass): was boosted by clear (grass), cloudy boosts poison → still boosted, NOT affected
	// - Squirtle (water): was not boosted, cloudy doesn't boost water → NOT affected
	affected = apt.GetAffectedPokemon("cell1", "user1", 1, 4, 10)
	if len(affected) != 0 {
		t.Fatalf("expected 0 affected for cloudy, got %d", len(affected))
	}
}

func TestActivePokemonTracker_ExpiryEviction(t *testing.T) {
	apt := NewActivePokemonTracker(50)

	past := time.Now().Unix() - 10
	future := time.Now().Unix() + 3600

	apt.Register("cell1", "user1", "enc_expired", ActivePokemon{
		PokemonID:     1,
		DisappearTime: past,
		Weather:       1,
		Types:         []int{12},
	})
	apt.Register("cell1", "user1", "enc_active", ActivePokemon{
		PokemonID:     7,
		DisappearTime: future,
		Weather:       0,
		Types:         []int{7},
	})

	// The expired entry should be pruned; only the active water pokemon should show
	affected := apt.GetAffectedPokemon("cell1", "user1", 1, 2, 10)
	if len(affected) != 1 {
		t.Fatalf("expected 1 affected after expiry, got %d", len(affected))
	}
	if affected[0].PokemonID != 7 {
		t.Errorf("expected pokemon 7, got %d", affected[0].PokemonID)
	}
}

func TestActivePokemonTracker_MaxPerUser(t *testing.T) {
	apt := NewActivePokemonTracker(3)

	future := time.Now().Unix() + 3600

	for i := range 5 {
		apt.Register("cell1", "user1", encID(i), ActivePokemon{
			PokemonID:     i + 1,
			DisappearTime: future + int64(i),
			Weather:       0,
			Types:         []int{7},
		})
	}

	// Should have at most 3 entries
	apt.mu.Lock()
	count := len(apt.cells["cell1"]["user1"])
	apt.mu.Unlock()

	if count > 3 {
		t.Errorf("expected at most 3 entries, got %d", count)
	}
}

func TestActivePokemonTracker_MaxCount(t *testing.T) {
	apt := NewActivePokemonTracker(50)

	future := time.Now().Unix() + 3600

	for i := range 10 {
		apt.Register("cell1", "user1", encID(i), ActivePokemon{
			PokemonID:     i + 1,
			DisappearTime: future,
			Weather:       0,
			Types:         []int{7}, // water, boosted by rainy
		})
	}

	affected := apt.GetAffectedPokemon("cell1", "user1", 1, 2, 3)
	if len(affected) != 3 {
		t.Errorf("expected maxCount=3, got %d", len(affected))
	}
}

func TestActivePokemonTracker_EmptyCellUser(t *testing.T) {
	apt := NewActivePokemonTracker(50)

	affected := apt.GetAffectedPokemon("nonexistent", "nobody", 1, 2, 10)
	if affected != nil {
		t.Errorf("expected nil for nonexistent cell/user, got %v", affected)
	}
}

func encID(i int) string {
	return "enc" + string(rune('A'+i))
}
