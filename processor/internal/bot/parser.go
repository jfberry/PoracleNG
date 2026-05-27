package bot

import (
	"regexp"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// ParsedCommand represents one command invocation after parsing.
type ParsedCommand struct {
	CommandKey   string   // identifier key (e.g. "cmd.track"), empty if unrecognised
	Args         []string // remaining arguments (lowercased, underscores→spaces)
	RawArgs      []string // remaining arguments preserving original case (underscores→spaces still applied to unquoted tokens). Use for output that should keep what the user typed — e.g. autocreate channel/category names.
	LanguageHint string   // language code from available_languages (e.g. "de" from "!dasporacle")
}

// Parser handles text → structured commands.
type Parser struct {
	prefix     string
	commandMap map[string]string // lowercased translated name → identifier key
	langMap    map[string]string // lowercased command word → language code (from available_languages)
}

var tokenRe = regexp.MustCompile(`"([^"]*)"|\S+`)

// NewParser builds the multi-language command lookup table.
// For each cmd.* key in the bundle, all translations across the given languages
// are mapped to the identifier key. If two languages translate different commands
// to the same word, the first language in the list wins.
func NewParser(prefix string, bundle *i18n.Bundle, languages []string, availableLanguages map[string]config.LanguageEntry) *Parser {
	cmdMap := make(map[string]string)
	for _, lang := range languages {
		tr := bundle.For(lang)
		if tr == nil {
			continue
		}
		for key, val := range tr.Messages() {
			if !strings.HasPrefix(key, "cmd.") {
				continue
			}
			// Only register top-level command keys (cmd.track, cmd.poracle)
			// not subcommand labels (cmd.info.sub.poracle) or message strings
			// (cmd.track.usage). A command key has exactly one dot after "cmd.".
			parts := strings.SplitN(key[4:], ".", 2) // strip "cmd." prefix
			if len(parts) > 1 {
				continue // has sub-key — not a command name
			}
			lower := strings.ToLower(val)
			if lower == "" {
				continue
			}
			// First mapping wins — earlier languages have priority
			if _, exists := cmdMap[lower]; !exists {
				cmdMap[lower] = key
			}
		}
	}

	// Register available_languages poracle/help aliases.
	// These map language-specific command words (e.g. "dasporacle") to the
	// standard command keys. The language code is stored in langMap so the
	// poracle command can auto-set the user's language on registration.
	langMap := make(map[string]string) // command word → language code
	for langCode, entry := range availableLanguages {
		if entry.Poracle != "" {
			word := strings.ToLower(entry.Poracle)
			if _, exists := cmdMap[word]; !exists {
				cmdMap[word] = "cmd.poracle"
			}
			langMap[word] = langCode
		}
		if entry.Help != "" {
			word := strings.ToLower(entry.Help)
			if _, exists := cmdMap[word]; !exists {
				cmdMap[word] = "cmd.help"
			}
			langMap[word] = langCode
		}
	}

	return &Parser{prefix: strings.ToLower(prefix), commandMap: cmdMap, langMap: langMap}
}

// Parse splits raw message text into one or more ParsedCommands.
func (p *Parser) Parse(text string) []ParsedCommand {
	var results []ParsedCommand

	// Multi-line: split by newlines, each line is independent
	lines := strings.SplitSeq(text, "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check prefix (case-insensitive)
		if len(line) < len(p.prefix) {
			continue
		}
		if strings.ToLower(line[:len(p.prefix)]) != p.prefix {
			continue
		}
		line = line[len(p.prefix):]

		// Tokenize preserving quoted strings
		tokens := tokenize(line)
		if len(tokens) == 0 {
			continue
		}

		// Look up command name (first token, already lowercased by tokenize)
		cmdWord := tokens[0].Value
		cmdKey := p.commandMap[cmdWord]
		langHint := p.langMap[cmdWord] // non-empty if from available_languages

		// Remaining args: underscore→space, but only for unquoted tokens.
		// Users can wrap a value in double quotes to preserve its underscores
		// (e.g. area names like "gent_centrum"). The autocreate template
		// expander relies on this to round-trip names through the parser.
		// rawArgs preserves the original case so output paths that echo the
		// user's input (autocreate channel names, etc.) don't collapse it.
		args := make([]string, 0, len(tokens)-1)
		rawArgs := make([]string, 0, len(tokens)-1)
		for _, tok := range tokens[1:] {
			val := tok.Value
			raw := tok.Raw
			if !tok.Quoted {
				val = strings.ReplaceAll(val, "_", " ")
				raw = strings.ReplaceAll(raw, "_", " ")
			}
			args = append(args, val)
			rawArgs = append(rawArgs, raw)
		}

		// Pipe splitting: split args by "|" into groups sharing the same command
		groups, rawGroups := splitByPipePaired(args, rawArgs)
		if len(groups) == 0 {
			results = append(results, ParsedCommand{CommandKey: cmdKey, Args: nil, LanguageHint: langHint})
		} else {
			for i := range groups {
				results = append(results, ParsedCommand{
					CommandKey:   cmdKey,
					Args:         groups[i],
					RawArgs:      rawGroups[i],
					LanguageHint: langHint,
				})
			}
		}
	}

	return results
}

// token is a single lex unit produced by tokenize. Quoted tracks whether
// the source text used double quotes — callers that strip underscores or
// otherwise normalise tokens use this to leave quoted values alone. Raw
// preserves the original case (Value is lowercased) for callers that need
// to echo the user's input verbatim, e.g. autocreate channel/category
// names that should keep "GentCentrum" not collapse to "gentcentrum".
type token struct {
	Value  string
	Raw    string
	Quoted bool
}

// tokenize splits text into tokens, preserving quoted strings.
// Quotes are stripped from the result. Value is lowercased; Raw retains
// the original case.
func tokenize(text string) []token {
	matches := tokenRe.FindAllStringSubmatch(text, -1)
	tokens := make([]token, 0, len(matches))
	for _, m := range matches {
		// A quoted match starts and ends with " (handles empty "" too).
		quoted := len(m[0]) >= 2 && m[0][0] == '"' && m[0][len(m[0])-1] == '"'
		val := m[0]
		if quoted {
			val = m[1]
		}
		tokens = append(tokens, token{
			Value:  strings.ToLower(val),
			Raw:    val,
			Quoted: quoted,
		})
	}
	return tokens
}

// MergeApplyGroups consolidates consecutive cmd.apply ParsedCommands back into
// a single invocation. The parser pipe-splits "!apply t1 | track pikachu" into
// separate ParsedCommands, but apply needs all pipe groups at once to resolve
// targets and execute sub-commands.
func MergeApplyGroups(cmds []ParsedCommand) []ParsedCommand {
	if len(cmds) <= 1 {
		return cmds
	}

	var result []ParsedCommand
	i := 0
	for i < len(cmds) {
		if cmds[i].CommandKey != "cmd.apply" {
			result = append(result, cmds[i])
			i++
			continue
		}

		// Collect consecutive cmd.apply entries
		merged := ParsedCommand{CommandKey: "cmd.apply"}
		merged.Args = append(merged.Args, cmds[i].Args...)
		i++
		for i < len(cmds) && cmds[i].CommandKey == "cmd.apply" {
			merged.Args = append(merged.Args, "|")
			merged.Args = append(merged.Args, cmds[i].Args...)
			i++
		}
		result = append(result, merged)
	}
	return result
}

// splitByPipePaired splits args + rawArgs in lockstep so the index of each
// group in the lowercased slice matches the corresponding group in the
// raw-case slice. Pipe positions are detected on the lowercased side.
func splitByPipePaired(args, rawArgs []string) (groups, rawGroups [][]string) {
	if len(args) == 0 {
		return nil, nil
	}
	current := make([]string, 0)
	currentRaw := make([]string, 0)
	for i, a := range args {
		if a == "|" {
			if len(current) > 0 {
				groups = append(groups, current)
				rawGroups = append(rawGroups, currentRaw)
			}
			current = make([]string, 0)
			currentRaw = make([]string, 0)
		} else {
			current = append(current, a)
			currentRaw = append(currentRaw, rawArgs[i])
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
		rawGroups = append(rawGroups, currentRaw)
	}
	return groups, rawGroups
}
