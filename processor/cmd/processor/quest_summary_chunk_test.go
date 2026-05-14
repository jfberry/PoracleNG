package main

import "testing"

func TestChunkPerMessage(t *testing.T) {
	mk := func(n int) []map[string]any {
		out := make([]map[string]any, n)
		for i := range out {
			out[i] = map[string]any{"i": i}
		}
		return out
	}
	cases := []struct {
		name    string
		entries int
		size    int
		want    []int // expected entries-per-chunk
	}{
		{"empty", 0, 25, nil},
		{"under cap", 5, 25, []int{5}},
		{"exactly cap", 25, 25, []int{25}},
		{"split into 2", 30, 25, []int{25, 5}},
		{"split into 3", 75, 25, []int{25, 25, 25}},
		{"size disabled (0) → single chunk", 100, 0, []int{100}},
		{"size disabled (-1) → single chunk", 100, -1, []int{100}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := chunkPerMessage(mk(c.entries), c.size)
			if len(got) != len(c.want) {
				t.Fatalf("chunk count = %d, want %d", len(got), len(c.want))
			}
			for i, ch := range got {
				if len(ch.entries) != c.want[i] {
					t.Errorf("chunk[%d] size = %d, want %d", i, len(ch.entries), c.want[i])
				}
				if ch.index != i+1 {
					t.Errorf("chunk[%d] index = %d, want %d", i, ch.index, i+1)
				}
				if ch.total != len(c.want) {
					t.Errorf("chunk[%d] total = %d, want %d", i, ch.total, len(c.want))
				}
			}
		})
	}
}
