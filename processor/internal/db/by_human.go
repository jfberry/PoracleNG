package db

// idAccessor constrains *T to implement GetID() string, allowing
// PartitionByHuman to call GetID on each element without a separate
// extractor function.
type idAccessor[T any] interface {
	*T
	GetID() string
}

// PartitionByHuman groups a tracking-rule pointer slice by the human ID
// returned from GetID(). Returns a map[humanID][]P sharing the input
// pointers (no copies, no allocations of T).
func PartitionByHuman[T any, P idAccessor[T]](rows []P) map[string][]P {
	out := map[string][]P{}
	for _, r := range rows {
		if any(r) == nil {
			continue
		}
		id := r.GetID()
		if id == "" {
			continue
		}
		out[id] = append(out[id], r)
	}
	return out
}
