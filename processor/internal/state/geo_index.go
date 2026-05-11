package state

import (
	"github.com/tidwall/rtree"

	"github.com/pokemon/poracleng/processor/internal/db"
)

// HumanGeoIndex pre-computes which humans are geographically capable of
// receiving an alert for a given spawn location. Two index structures:
//
//   - byArea: areaName → set of humanIDs whose Area list contains it
//   - byAreaRestriction: same shape but only humans with a strict-mode
//     AreaRestriction set (used when strict mode is enabled)
//   - distanceTree: R-tree of (humanLocation, maxRuleDistance) bounding
//     boxes for distance-based rules
//
// Built once per state load; never mutated after BuildHumanGeoIndex returns.
// Concurrent reads from many matcher goroutines are safe because the
// underlying maps and rtree are read-only.
type HumanGeoIndex struct {
	byArea             map[string]map[string]bool
	byAreaRestriction  map[string]map[string]bool
	distanceTree       rtree.RTreeG[string]
	humansWithDistance map[string]bool
	humansWithArea     map[string]bool
	humansWithRestriction map[string]bool
}

// BuildHumanGeoIndex constructs the index from the loaded humans map and a
// per-human max-tracking-distance map. perHumanMaxDist holds the max distance
// (in metres) across all of that human's tracking rules; humans with only
// area-based rules (distance == 0 on every rule) are omitted from
// perHumanMaxDist and don't enter the distance tree.
//
// Disabled or admin-disabled humans are excluded; their rules can't fire
// regardless.
func BuildHumanGeoIndex(humans map[string]*db.Human, perHumanMaxDist map[string]int) *HumanGeoIndex {
	idx := &HumanGeoIndex{
		byArea:             map[string]map[string]bool{},
		byAreaRestriction:  map[string]map[string]bool{},
		humansWithDistance: map[string]bool{},
		humansWithArea:     map[string]bool{},
		humansWithRestriction: map[string]bool{},
	}
	for id, h := range humans {
		if h == nil || !h.Enabled || h.AdminDisable {
			continue
		}
		for _, a := range h.Area {
			if a == "" {
				continue
			}
			if idx.byArea[a] == nil {
				idx.byArea[a] = map[string]bool{}
			}
			idx.byArea[a][id] = true
			idx.humansWithArea[id] = true
		}
		for _, a := range h.AreaRestriction {
			if a == "" {
				continue
			}
			if idx.byAreaRestriction[a] == nil {
				idx.byAreaRestriction[a] = map[string]bool{}
			}
			idx.byAreaRestriction[a][id] = true
			idx.humansWithRestriction[id] = true
		}
		if d, ok := perHumanMaxDist[id]; ok && d > 0 {
			// Convert metres to degrees using a uniform approximation:
			// 1 degree ≈ 111320m.  This produces false positives at high
			// latitudes (longitude degrees are shorter), but false positives
			// are acceptable here — the exact haversine check in
			// ValidateHumans* is the authoritative gate.
			dDeg := float64(d) / 111320.0
			minLat := h.Latitude - dDeg
			maxLat := h.Latitude + dDeg
			minLon := h.Longitude - dDeg
			maxLon := h.Longitude + dDeg
			idx.distanceTree.Insert([2]float64{minLon, minLat}, [2]float64{maxLon, maxLat}, id)
			idx.humansWithDistance[id] = true
		}
	}
	return idx
}

// ApplicableHumans returns the set of human IDs whose geography (area
// selection and/or rule-distance circle) overlaps the spawn at (lat, lon)
// in any of matchedAreas. In strictMode, an area match additionally
// requires the human's AreaRestriction to overlap matchedAreas.
func (idx *HumanGeoIndex) ApplicableHumans(
	lat, lon float64,
	matchedAreas map[string]bool,
	strictMode bool,
) map[string]bool {
	out := map[string]bool{}
	if idx == nil {
		return out
	}

	// Area-based hits
	for area := range matchedAreas {
		for id := range idx.byArea[area] {
			if strictMode {
				if !humanHasRestrictionOverlap(idx, id, matchedAreas) {
					continue
				}
			}
			out[id] = true
		}
	}

	// Distance-based hits — only consider humans for whom we inserted a
	// bbox. The rtree query returns candidates; haversine confirms.
	idx.distanceTree.Search(
		[2]float64{lon, lat}, [2]float64{lon, lat},
		func(_, _ [2]float64, id string) bool {
			if out[id] {
				return true // already applicable via area path
			}
			if strictMode && !humanHasRestrictionOverlap(idx, id, matchedAreas) {
				return true
			}
			out[id] = true
			return true
		},
	)
	return out
}

// humanHasRestrictionOverlap returns true if the human either has no
// AreaRestriction set (no constraint) or if at least one of their restriction
// areas is present in matchedAreas.
//
// This matches the semantics of ValidateHumansGeneric strict-mode: humans
// without a restriction are always considered to pass.
func humanHasRestrictionOverlap(idx *HumanGeoIndex, humanID string, matchedAreas map[string]bool) bool {
	if !idx.humansWithRestriction[humanID] {
		return true  // no restriction set → unrestricted
	}
	for area := range matchedAreas {
		if idx.byAreaRestriction[area][humanID] {
			return true
		}
	}
	return false
}
