package db

// HumanIDExtractor returns the human ID for a tracking row. Implemented
// by each tracking type below.
type HumanIDExtractor[T any] func(*T) string

// PartitionByHuman groups a tracking-rule pointer slice by the human ID
// returned from extract. Returns a map[humanID][]*T sharing the input
// pointers (no copies, no allocations of T).
func PartitionByHuman[T any](rows []*T, extract HumanIDExtractor[T]) map[string][]*T {
	out := map[string][]*T{}
	for _, r := range rows {
		if r == nil {
			continue
		}
		id := extract(r)
		if id == "" {
			continue
		}
		out[id] = append(out[id], r)
	}
	return out
}
