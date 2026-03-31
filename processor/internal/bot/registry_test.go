package bot

import "testing"

// testCmd implements Command for testing.
type testCmd struct {
	name    string
	aliases []string
}

func (c *testCmd) Name() string      { return c.name }
func (c *testCmd) Aliases() []string { return c.aliases }
func (c *testCmd) Run(_ *CommandContext, _ []string) []Reply {
	return []Reply{{Text: c.name}}
}

func TestRegistryLookup(t *testing.T) {
	r := NewRegistry()
	cmd := &testCmd{name: "cmd.track", aliases: []string{"cmd.pokemon"}}
	r.Register(cmd)

	if r.Lookup("cmd.track") != cmd {
		t.Error("cmd.track should find the command")
	}
	if r.Lookup("cmd.pokemon") != cmd {
		t.Error("cmd.pokemon alias should find the same command")
	}
	if r.Lookup("cmd.raid") != nil {
		t.Error("cmd.raid should return nil")
	}
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()
	r.Register(&testCmd{name: "cmd.track", aliases: []string{"cmd.pokemon"}})
	r.Register(&testCmd{name: "cmd.raid"})

	all := r.All()
	if len(all) != 2 {
		t.Errorf("expected 2 unique commands, got %d", len(all))
	}
}

func TestRegistryAllDeduplicatesAliases(t *testing.T) {
	r := NewRegistry()
	r.Register(&testCmd{name: "cmd.track", aliases: []string{"cmd.pokemon", "cmd.mon"}})

	all := r.All()
	if len(all) != 1 {
		t.Errorf("expected 1 unique command (not 3), got %d", len(all))
	}
}
