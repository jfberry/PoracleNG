package state

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// summarizeMonsterBuckets returns a one-line human-readable summary of the
// monster index's bucket sizes. The "everything" bucket (pokemon_id=0) is
// the catch-all rule list scanned for every pokemon spawn, so operators
// want to see its size at a glance; "top-pokemon" lists the 5 most-tracked
// species so configuration hotspots are visible.
func summarizeMonsterBuckets(idx *db.MonsterIndex) string {
	if idx == nil {
		return "monsters=nil"
	}
	everything := len(idx.ByPokemonID[0])

	type bucket struct {
		id   int
		size int
	}
	perSpecies := make([]bucket, 0, len(idx.ByPokemonID))
	for id, slice := range idx.ByPokemonID {
		if id == 0 {
			continue
		}
		perSpecies = append(perSpecies, bucket{id, len(slice)})
	}
	sort.Slice(perSpecies, func(i, j int) bool { return perSpecies[i].size > perSpecies[j].size })
	top := perSpecies
	if len(top) > 5 {
		top = top[:5]
	}
	topStrs := make([]string, 0, len(top))
	for _, b := range top {
		topStrs = append(topStrs, fmt.Sprintf("%d=%d", b.id, b.size))
	}

	pvpSpec := 0
	for _, slice := range idx.PVPSpecific {
		pvpSpec += len(slice)
	}
	pvpEvery := 0
	for _, slice := range idx.PVPEverything {
		pvpEvery += len(slice)
	}

	return fmt.Sprintf(
		"monsters: total=%d everything=%d top-pokemon=[%s] pvp-specific=%d pvp-everything=%d",
		idx.Total, everything, strings.Join(topStrs, ","), pvpSpec, pvpEvery,
	)
}
