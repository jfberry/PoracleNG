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
		cmdWord := tokens[0]
		cmdKey := p.commandMap[cmdWord]
		langHint := p.langMap[cmdWord] // non-empty if from available_languages

		// Remaining args: underscore→space
		args := make([]string, 0, len(tokens)-1)
		for _, tok := range tokens[1:] {
			args = append(args, strings.ReplaceAll(tok, "_", " "))
		}

		// Pipe splitting: split args by "|" into groups sharing the same command
		groups := splitByPipe(args)
		if len(groups) == 0 {
			results = append(results, ParsedCommand{CommandKey: cmdKey, Args: nil, LanguageHint: langHint})
		} else {
			for _, group := range groups {
				results = append(results, ParsedCommand{CommandKey: cmdKey, Args: group, LanguageHint: langHint})
			}
		}
	}

	return results
}

// tokenize splits text into tokens, preserving quoted strings.
// Quotes are stripped from the result. All tokens are lowercased.
func tokenize(text string) []string {
	matches := tokenRe.FindAllStringSubmatch(text, -1)
	tokens := make([]string, 0, len(matches))
	for _, m := range matches {
		tok := m[0]
		if m[1] != "" {
			tok = m[1] // captured quoted content (without quotes)
		}
		tokens = append(tokens, strings.ToLower(tok))
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

// splitByPipe splits args by the "|" token into groups.
// Each group gets its own command invocation.
func splitByPipe(args []string) [][]string {
	if len(args) == 0 {
		return nil
	}
	var groups [][]string
	current := make([]string, 0)
	for _, a := range args {
		if a == "|" {
			if len(current) > 0 {
				groups = append(groups, current)
			}
			current = make([]string, 0)
		} else {
			current = append(current, a)
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}
