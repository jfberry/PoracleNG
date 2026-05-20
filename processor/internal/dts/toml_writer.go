package dts

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

// encodeTOML produces the wire-format TOML bytes for a list of DTS
// entries. Used by the config editor save path (Option C from #110):
// the editor speaks JSON internally; this is the bridge that writes
// back to TOML for entries whose sourceFormat is "toml".
//
// Multi-line template bodies and inline response templates are kept as
// strings — the BurntSushi/toml encoder picks up Go's multi-line
// string heuristic and emits """...""" for strings containing newlines,
// which is exactly what operators want.
//
// Comments and operator-chosen key ordering are NOT preserved. Editors
// that depend on those should treat each save as a re-emit; the
// rewrite-backup mechanism captures the prior file so operators can
// recover hand-authored layouts if a round-trip clobbers them. See the
// editor design decision in docs/buttons-and-snapshots/DESIGN.md.
func encodeTOML(entries []DTSEntry) ([]byte, error) {
	wrap := tomlFile{Entries: entries}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(wrap); err != nil {
		return nil, fmt.Errorf("toml encode: %w", err)
	}
	return buf.Bytes(), nil
}
