package nlp

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// ParseOption represents an alternative command the user might have meant.
type ParseOption struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

// ParseResult holds the output of the NLP parser.
type ParseResult struct {
	Status  string        `json:"status"`
	Command string        `json:"command,omitempty"`
	Error   string        `json:"error,omitempty"`
	Message string        `json:"message,omitempty"`
	Options []ParseOption `json:"options,omitempty"`
}

// Parser is the main NLP parser that converts natural language into Poracle commands.
type Parser struct {
	vocabs          *Vocabularies
	invasionEvents  map[string]bool
}

// NewParser creates a new Parser from a translator and base directory.
// invasionEvents is a set of known invasion event names (e.g. "kecleon", "showcase").
func NewParser(tr *i18n.Translator, baseDir string, invasionEvents map[string]bool) *Parser {
	vocabs := BuildVocabularies(tr, baseDir)
	if invasionEvents == nil {
		invasionEvents = map[string]bool{}
	}
	return &Parser{
		vocabs:         vocabs,
		invasionEvents: invasionEvents,
	}
}

// shortcutPhrases maps common natural language phrases to direct commands.
// Checked before the main NLP pipeline.
var shortcutPhrases = map[string]string{
	"stop":                          "!stop",
	"stop alerts":                   "!stop",
	"pause":                         "!stop",
	"pause alerts":                  "!stop",
	"show me what i'm tracking":     "!tracked",
	"show me what im tracking":      "!tracked",
	"show tracking":                 "!tracked",
	"what am i tracking":            "!tracked",
	"whats tracked":                 "!tracked",
	"list tracking":                 "!tracked",
	"my tracking":                   "!tracked",
	"tracked":                       "!tracked",
	"help":                          "!help",
	"start":                         "!poracle",
	"register":                      "!poracle",
}

// Parse converts natural language input into Poracle command(s).
func (p *Parser) Parse(input string) ParseResult {
	if strings.TrimSpace(input) == "" {
		return ParseResult{
			Status: "error",
			Error:  "empty input",
		}
	}

	// Check shortcut phrases first
	lowered := strings.ToLower(strings.TrimSpace(input))
	if cmd, ok := shortcutPhrases[lowered]; ok {
		return ParseResult{Status: "ok", Command: cmd}
	}

	// Step 1: Normalize
	normalized := Normalize(input)
	if normalized == "" {
		return ParseResult{
			Status: "error",
			Error:  "input contains only filler words",
		}
	}

	// Step 2: Detect intent
	intent := DetectIntent(normalized, p.invasionEvents)

	// Step 3: Match tokens
	matched := matchTokens(intent.Remaining, intent.CommandType, p.vocabs, p.invasionEvents)

	// Step 4: Assemble commands
	commands := assemble(intent.CommandType, intent.IsRemove, matched)

	if len(commands) == 0 {
		return ParseResult{
			Status: "error",
			Error:  "could not parse command",
		}
	}

	return ParseResult{
		Status:  "ok",
		Command: strings.Join(commands, "\n"),
	}
}

// PokemonCount returns the number of pokemon in the vocabulary.
func (p *Parser) PokemonCount() int {
	if p.vocabs == nil || p.vocabs.Pokemon == nil {
		return 0
	}
	return len(p.vocabs.Pokemon.single)
}
