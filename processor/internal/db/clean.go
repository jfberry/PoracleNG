package db

// Clean flag bitmask values:
//   0 = no tracking
//   1 = clean (auto-delete on TTH expiry)
//   2 = edit (track for message editing)
//   3 = edit + clean
//   4 = summary (buffered + grouped delivery)

// IsClean returns true if the clean flag has the auto-delete bit set.
func IsClean(clean int) bool { return clean&1 != 0 }

// IsEdit returns true if the clean flag has the edit-tracking bit set.
func IsEdit(clean int) bool { return clean&2 != 0 }

// IsSummary reports whether bit 4 of clean is set, opting the rule
// into the summary scheduler (buffered + grouped delivery).
func IsSummary(clean int) bool { return clean&4 != 0 }
