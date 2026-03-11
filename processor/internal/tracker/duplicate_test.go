package tracker

import (
	"testing"
	"time"
)

func TestDuplicateCachePokemon(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	disappear := time.Now().Unix() + 600

	// First time - not duplicate
	isDup := dc.CheckPokemon("enc1", true, 500, disappear)
	if isDup {
		t.Error("Expected first sighting to not be duplicate")
	}

	// Same key - duplicate
	isDup = dc.CheckPokemon("enc1", true, 500, disappear)
	if !isDup {
		t.Error("Expected second sighting to be duplicate")
	}

	// Different verified state - not duplicate
	isDup = dc.CheckPokemon("enc1", false, 500, disappear)
	if isDup {
		t.Error("Expected different verified state to not be duplicate")
	}

	// Different CP - not duplicate
	isDup = dc.CheckPokemon("enc1", true, 600, disappear)
	if isDup {
		t.Error("Expected different CP to not be duplicate")
	}
}

func TestDuplicateCacheRaid(t *testing.T) {
	dc := NewDuplicateCache()
	defer dc.Close()

	end := time.Now().Unix() + 3600

	// First time
	isDup, isFirst := dc.CheckRaid("gym1", end, 150, nil)
	if isDup {
		t.Error("Expected first raid to not be duplicate")
	}
	if !isFirst {
		t.Error("Expected first notification to be true")
	}

	// Same key - duplicate
	isDup, isFirst = dc.CheckRaid("gym1", end, 150, nil)
	if !isDup {
		t.Error("Expected second raid to be duplicate")
	}
	if isFirst {
		t.Error("Expected first notification to be false")
	}

	// Different pokemon - not duplicate
	isDup, isFirst = dc.CheckRaid("gym1", end, 151, nil)
	if isDup {
		t.Error("Expected different pokemon to not be duplicate")
	}
}
