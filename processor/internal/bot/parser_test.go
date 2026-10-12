package bot

import (
	"slices"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func newTestParser() *Parser {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"cmd.track":   "track",
		"cmd.raid":    "raid",
		"cmd.egg":     "egg",
		"cmd.start":   "start",
		"cmd.area":    "area",
		"cmd.stop":    "stop",
		"cmd.tracked": "tracked",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"cmd.track":   "verfolgen",
		"cmd.raid":    "raid",
		"cmd.egg":     "ei",
		"cmd.start":   "starten",
		"cmd.area":    "gebiet",
		"cmd.stop":    "stoppen",
		"cmd.tracked": "status",
	}))
	return NewParser("!", bundle, []string{"en", "de"}, nil)
}

func TestParserBasic(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!track pikachu iv100")
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "cmd.track" {
		t.Errorf("key = %q, want cmd.track", cmds[0].CommandKey)
	}
	if len(cmds[0].Args) != 2 {
		t.Errorf("args = %v, want 2 args", cmds[0].Args)
	}
	if cmds[0].Args[0] != "pikachu" {
		t.Errorf("args[0] = %q, want pikachu", cmds[0].Args[0])
	}
	if cmds[0].Args[1] != "iv100" {
		t.Errorf("args[1] = %q, want iv100", cmds[0].Args[1])
	}
}

func TestParserGermanCommand(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!verfolgen relaxo iv100")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "cmd.track" {
		t.Errorf("key = %q, want cmd.track", cmds[0].CommandKey)
	}
	if len(cmds[0].Args) != 2 {
		t.Errorf("args = %v", cmds[0].Args)
	}
}

func TestParserPipeSplit(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!track pikachu | bulbasaur")
	if len(cmds) != 2 {
		t.Fatalf("expected 2, got %d", len(cmds))
	}
	if cmds[0].Args[0] != "pikachu" {
		t.Errorf("first = %v", cmds[0].Args)
	}
	if cmds[1].Args[0] != "bulbasaur" {
		t.Errorf("second = %v", cmds[1].Args)
	}
	if cmds[0].CommandKey != "cmd.track" {
		t.Errorf("first key = %q", cmds[0].CommandKey)
	}
	if cmds[1].CommandKey != "cmd.track" {
		t.Errorf("second key = %q", cmds[1].CommandKey)
	}
}

func TestParserMultiLine(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!track pikachu\n!raid level5")
	if len(cmds) != 2 {
		t.Fatalf("expected 2, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "cmd.track" {
		t.Errorf("first = %q", cmds[0].CommandKey)
	}
	if cmds[1].CommandKey != "cmd.raid" {
		t.Errorf("second = %q", cmds[1].CommandKey)
	}
}

func TestParserQuotedArgs(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse(`!track "mr. mime" iv100`)
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if len(cmds[0].Args) != 2 {
		t.Fatalf("args = %v, want 2", cmds[0].Args)
	}
	if cmds[0].Args[0] != "mr. mime" {
		t.Errorf("arg = %q, want 'mr. mime'", cmds[0].Args[0])
	}
}

func TestParserUnderscoreToSpace(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!track slot_changes")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].Args[0] != "slot changes" {
		t.Errorf("arg = %q, want 'slot changes'", cmds[0].Args[0])
	}
}

// Quoted tokens preserve underscores — this is what lets autocreate templates
// round-trip area names like "gent_centrum" through the bot parser without
// the underscore→space conversion mangling them.
func TestParserQuotedPreservesUnderscores(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse(`!track "gent_centrum"`)
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].Args[0] != "gent_centrum" {
		t.Errorf("arg = %q, want 'gent_centrum'", cmds[0].Args[0])
	}
}

// RawArgs preserves the original case the user typed so callers that echo
// arguments back (autocreate channel/category names) don't collapse
// "GentCentrum" to "gentcentrum". Args stays lowercase for matching paths.
func TestParserRawArgsPreservesCase(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!track Gent GentCentrum")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	wantArgs := []string{"gent", "gentcentrum"}
	wantRaw := []string{"Gent", "GentCentrum"}
	for i, w := range wantArgs {
		if cmds[0].Args[i] != w {
			t.Errorf("Args[%d] = %q, want %q", i, cmds[0].Args[i], w)
		}
		if cmds[0].RawArgs[i] != wantRaw[i] {
			t.Errorf("RawArgs[%d] = %q, want %q", i, cmds[0].RawArgs[i], wantRaw[i])
		}
	}
}

func TestParserUnknownCommand(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!notacommand hello")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "" {
		t.Errorf("expected empty key, got %q", cmds[0].CommandKey)
	}
}

func TestParserNoPrefix(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("track pikachu")
	if len(cmds) != 0 {
		t.Errorf("expected 0, got %d", len(cmds))
	}
}

func TestParserCaseInsensitive(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!TRACK Pikachu IV100")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "cmd.track" {
		t.Errorf("key = %q", cmds[0].CommandKey)
	}
	if cmds[0].Args[0] != "pikachu" {
		t.Errorf("arg = %q, want pikachu", cmds[0].Args[0])
	}
	if cmds[0].Args[1] != "iv100" {
		t.Errorf("arg = %q, want iv100", cmds[0].Args[1])
	}
}

func TestParserEmptyInput(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("")
	if len(cmds) != 0 {
		t.Errorf("expected 0, got %d", len(cmds))
	}
}

func TestParserPrefixOnly(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!")
	if len(cmds) != 0 {
		t.Errorf("expected 0, got %d", len(cmds))
	}
}

func TestParserCommandNoArgs(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!start")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "cmd.start" {
		t.Errorf("key = %q", cmds[0].CommandKey)
	}
	if len(cmds[0].Args) != 0 {
		t.Errorf("args = %v, want empty", cmds[0].Args)
	}
}

func TestParserGermanCommandNoArgs(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!starten")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "cmd.start" {
		t.Errorf("key = %q, want cmd.start", cmds[0].CommandKey)
	}
}

func TestParserMultiPipe(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!track pikachu | bulbasaur | charmander")
	if len(cmds) != 3 {
		t.Fatalf("expected 3, got %d", len(cmds))
	}
	for _, c := range cmds {
		if c.CommandKey != "cmd.track" {
			t.Errorf("key = %q", c.CommandKey)
		}
	}
	if cmds[2].Args[0] != "charmander" {
		t.Errorf("third = %v", cmds[2].Args)
	}
}

func TestParserSharedCommandDifferentLanguages(t *testing.T) {
	// "raid" is the same in English and German — should still resolve
	p := newTestParser()
	cmds := p.Parse("!raid level5")
	if len(cmds) != 1 {
		t.Fatalf("expected 1, got %d", len(cmds))
	}
	if cmds[0].CommandKey != "cmd.raid" {
		t.Errorf("key = %q, want cmd.raid", cmds[0].CommandKey)
	}
}

func TestParserMultiLineWithBlankLines(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse("!track pikachu\n\n!start\n")
	if len(cmds) != 2 {
		t.Fatalf("expected 2, got %d", len(cmds))
	}
}

func TestParserPipeWithSpaces(t *testing.T) {
	// Pipe with extra spaces around it
	p := newTestParser()
	cmds := p.Parse("!track pikachu  |  bulbasaur")
	if len(cmds) != 2 {
		t.Fatalf("expected 2, got %d", len(cmds))
	}
}

func TestParserEnsurePrefix(t *testing.T) {
	p := newTestParser()
	cases := map[string]string{
		"track pikachu iv90":          "!track pikachu iv90",
		"!track pikachu iv90":         "!track pikachu iv90",
		"track pikachu\n!raid level5": "!track pikachu\n!raid level5",
		"  tracked  ":                 "!tracked",
		"track a\n\nraid b":           "!track a\n\n!raid b",
	}
	for in, want := range cases {
		if got := p.EnsurePrefix(in); got != want {
			t.Errorf("EnsurePrefix(%q) = %q, want %q", in, got, want)
		}
	}
	cmds := p.Parse(p.EnsurePrefix("track pikachu iv90"))
	if len(cmds) != 1 || cmds[0].CommandKey != "cmd.track" {
		t.Fatalf("parse after EnsurePrefix = %+v", cmds)
	}
}

func TestParseVerbatimArgs(t *testing.T) {
	p := newTestParser()
	cmds := p.Parse(`!track User_ABC "Quoted_Value" x | Second_One`)
	if len(cmds) != 2 {
		t.Fatalf("got %d commands", len(cmds))
	}
	if got := cmds[0].VerbatimArgs; len(got) != 3 || got[0] != "User_ABC" || got[1] != "Quoted_Value" || got[2] != "x" {
		t.Fatalf("verbatim[0] = %q", got)
	}
	if got := cmds[1].VerbatimArgs; len(got) != 1 || got[0] != "Second_One" {
		t.Fatalf("verbatim[1] = %q", got)
	}
	if cmds[0].Args[0] != "user abc" {
		t.Fatalf("Args unchanged: %q", cmds[0].Args)
	}
}

func TestMergeApplyGroupsKeepsVerbatimArgs(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"cmd.apply": "apply", "cmd.account": "account"}))
	p := NewParser("!", bundle, []string{"en"}, nil)
	cmds := MergeApplyGroups(p.Parse("!apply u2 | account add User_ABC Main"))
	if len(cmds) != 1 || cmds[0].CommandKey != "cmd.apply" {
		t.Fatalf("merged = %+v", cmds)
	}
	want := []string{"u2", "|", "account", "add", "User_ABC", "Main"}
	if !slices.Equal(cmds[0].VerbatimArgs, want) {
		t.Fatalf("VerbatimArgs = %q, want %q", cmds[0].VerbatimArgs, want)
	}
	if len(cmds[0].RawArgs) != len(cmds[0].Args) {
		t.Fatalf("RawArgs %q not in lockstep with Args %q", cmds[0].RawArgs, cmds[0].Args)
	}
}
