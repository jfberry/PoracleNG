package bot_test

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

func TestLoadParityFixtures(t *testing.T) {
	fixtures, err := bot.LoadParityFixtures("testdata/parity.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures loaded")
	}
	for _, f := range fixtures {
		if f.Name == "" {
			t.Errorf("nameless fixture: %+v", f)
		}
		if f.Command == "" {
			t.Errorf("%s: command empty", f.Name)
		}
		if f.Slash.Name == "" {
			t.Errorf("%s: slash.name empty", f.Name)
		}
	}
}
