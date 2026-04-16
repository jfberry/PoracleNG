package config

import (
	"encoding/json"
	"fmt"
)

// FlexStrings accepts either a single string or an array of strings in TOML
// and JSON. A single scalar is normalised to a one-element slice, matching
// PoracleJS's behaviour where fields like community.location_fence could be
// written as "wholenewyork" or ["wholenewyork", "greaternewyork"].
type FlexStrings []string

// UnmarshalTOML implements BurntSushi/toml's Unmarshaler interface.
func (f *FlexStrings) UnmarshalTOML(data any) error {
	switch v := data.(type) {
	case nil:
		*f = nil
		return nil
	case string:
		*f = FlexStrings{v}
		return nil
	case []any:
		out := make(FlexStrings, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("element %d: expected string, got %T", i, item)
			}
			out = append(out, s)
		}
		*f = out
		return nil
	}
	return fmt.Errorf("expected string or []string, got %T", data)
}

// UnmarshalJSON lets overrides.json (and any other JSON input) accept the
// same flexibility.
func (f *FlexStrings) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*f = nil
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = FlexStrings{s}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*f = FlexStrings(arr)
	return nil
}
