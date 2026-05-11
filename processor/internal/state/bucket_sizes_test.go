package state

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestSummarizeMonsterBuckets(t *testing.T) {
	idx := &db.MonsterIndex{
		ByPokemonID: map[int][]*db.MonsterTracking{
			0:   make([]*db.MonsterTracking, 5000),
			25:  make([]*db.MonsterTracking, 200),
			150: make([]*db.MonsterTracking, 150),
			12:  make([]*db.MonsterTracking, 80),
		},
		PVPSpecific: map[int][]*db.MonsterTracking{
			500:  make([]*db.MonsterTracking, 100),
			1500: make([]*db.MonsterTracking, 200),
		},
		PVPEverything: map[int][]*db.MonsterTracking{
			1500: make([]*db.MonsterTracking, 400),
		},
		Total: 5930,
	}
	s := summarizeMonsterBuckets(idx)
	if !strings.Contains(s, "everything=5000") {
		t.Errorf("expected everything bucket size in summary, got %q", s)
	}
	if !strings.Contains(s, "top-pokemon=") {
		t.Errorf("expected top-pokemon list in summary, got %q", s)
	}
	if !strings.Contains(s, "pvp-everything=") {
		t.Errorf("expected pvp-everything in summary, got %q", s)
	}
}
