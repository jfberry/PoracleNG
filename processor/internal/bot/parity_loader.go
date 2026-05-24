// Parity-fixture loader for slash↔text command parity tests.
//
// Each fixture is a (slash invocation, text command, expected tokens) triple.
// The slash mapper and the text parser must both produce the same token set.
// Fixtures live in testdata/parity.yaml and are loaded by parity_test.go and
// the coverage meta-test.
package bot

import (
	"os"

	"gopkg.in/yaml.v3"
)

// ParityFixture pins a single (slash, text, tokens) parity case.
//
// Slash.Name is the canonical English short slash command name (e.g. "track").
// Slash.Options uses the dotted-key convention for sub-commands:
//
//	options:
//	  pokemon: pikachu        # flat option on a top-level command
//	  add.area: london        # sub-command "add" with option "area"="london"
//	  show: true              # bare sub-command with no options
//
// ExpectedTokens is the token slice the mapper should produce. Order does
// not matter — the parity runner compares as a multiset (sorted) so we can
// add new options to a mapper without re-pinning every fixture's order.
type ParityFixture struct {
	Name           string          `yaml:"name"`
	Description    string          `yaml:"description"`
	Command        string          `yaml:"command"`
	Slash          SlashInvocation `yaml:"slash"`
	Text           string          `yaml:"text"`
	ExpectedTokens []string        `yaml:"expected_tokens"`
}

// SlashInvocation captures the slash command shape used to drive the mapper.
type SlashInvocation struct {
	Name    string         `yaml:"name"`
	Options map[string]any `yaml:"options"`
}

// LoadParityFixtures reads a YAML file containing a list of ParityFixture
// entries. Returns the parsed fixtures or any read/unmarshal error.
func LoadParityFixtures(path string) ([]ParityFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fixtures []ParityFixture
	if err := yaml.Unmarshal(data, &fixtures); err != nil {
		return nil, err
	}
	return fixtures, nil
}
