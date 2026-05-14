package discordbot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/backup"
)

// channelTemplatePath returns the on-disk location of channelTemplate.json
// rooted at the project's config dir.
func channelTemplatePath(baseDir string) string {
	return filepath.Join(baseDir, "config", "channelTemplate.json")
}

// LoadChannelTemplatesRaw returns the raw bytes of channelTemplate.json,
// or []byte("[]") if the file doesn't exist. Used by the editor API to
// pass-through unknown fields losslessly.
func LoadChannelTemplatesRaw(baseDir string) ([]byte, error) {
	data, err := os.ReadFile(channelTemplatePath(baseDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("read channel templates: %w", err)
	}
	return data, nil
}

// SaveChannelTemplatesRaw validates raw, snapshots the existing file
// into config/backups/, and atomically replaces the live file with raw.
// Returns the backup path relative to the config dir (e.g.
// "backups/channelTemplate.json.bak.2026-05-07_113402"), or "" when
// there was no existing file to back up.
//
// Pretty-prints the JSON before writing so hand-edits and editor diffs
// stay friendly.
func SaveChannelTemplatesRaw(baseDir string, raw []byte) (string, error) {
	if errs := ValidateChannelTemplatesRaw(raw); len(errs) > 0 {
		return "", fmt.Errorf("validation failed: %s", errs[0].Message)
	}

	// Re-encode through the typed shape so we get stable indentation and
	// a single pass of normalisation. Preserves key order via the typed
	// struct; unknown fields are dropped on this path. If preserving
	// unknown fields ever becomes a requirement, marshal back from a
	// json.RawMessage parse instead.
	var typed []ChannelTemplate
	if err := json.Unmarshal(raw, &typed); err != nil {
		return "", fmt.Errorf("parse for normalisation: %w", err)
	}
	pretty, err := json.MarshalIndent(typed, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}

	path := channelTemplatePath(baseDir)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	// Snapshot the existing file (if any) into config/backups/ so the
	// operator can roll back. Returns "" when there's no live file yet.
	backupRel, err := backup.Save(dir, "channelTemplate.json")
	if err != nil {
		return "", fmt.Errorf("backup existing: %w", err)
	}

	// Atomic write: unique tmp + rename.
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(pretty); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename tmp: %w", err)
	}
	return backupRel, nil
}

// TemplateValidationError is one issue found during channel-template
// validation. Path is a JSON-pointer-ish locator (e.g.
// "templates[0].definition.channels[1].channelType") so the editor can
// highlight the offending field. Severity distinguishes blocking errors
// from informational warnings.
type TemplateValidationError struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" (blocks save) or "warning" (informational)
}

// ValidateChannelTemplatesRaw runs the rules from the editor schema doc
// against raw template-array JSON. Errors block save; warnings don't.
// Returns nil on a clean array.
func ValidateChannelTemplatesRaw(raw []byte) []TemplateValidationError {
	var templates []ChannelTemplate
	if err := json.Unmarshal(raw, &templates); err != nil {
		return []TemplateValidationError{{Path: "templates", Severity: "error", Message: "invalid JSON: " + err.Error()}}
	}
	var out []TemplateValidationError
	seen := map[string]bool{}
	for i, t := range templates {
		base := fmt.Sprintf("templates[%d]", i)
		if t.Name == "" {
			out = append(out, TemplateValidationError{Path: base + ".name", Severity: "error", Message: "name is required"})
		} else {
			if strings.ContainsAny(t.Name, " \t") {
				out = append(out, TemplateValidationError{Path: base + ".name", Severity: "error", Message: "name must not contain whitespace"})
			}
			if seen[t.Name] {
				out = append(out, TemplateValidationError{Path: base + ".name", Severity: "error", Message: fmt.Sprintf("duplicate name %q (must be unique within the array)", t.Name)})
			}
			seen[t.Name] = true
		}
		if len(t.Definition.Channels) == 0 {
			out = append(out, TemplateValidationError{Path: base + ".definition.channels", Severity: "error", Message: "at least one channel is required"})
		}
		if t.Definition.Category != nil {
			for ri, r := range t.Definition.Category.Roles {
				rp := fmt.Sprintf("%s.definition.category.roles[%d]", base, ri)
				if r.Name == "" {
					out = append(out, TemplateValidationError{Path: rp + ".name", Severity: "error", Message: "role name is required"})
				}
			}
		}
		for ci, ch := range t.Definition.Channels {
			cp := fmt.Sprintf("%s.definition.channels[%d]", base, ci)
			if ch.ChannelName == "" {
				out = append(out, TemplateValidationError{Path: cp + ".channelName", Severity: "error", Message: "channelName is required"})
			}
			switch ch.ChannelType {
			case "", "text", "voice":
				// ok
			default:
				out = append(out, TemplateValidationError{Path: cp + ".channelType", Severity: "error", Message: fmt.Sprintf("channelType must be \"text\" or \"voice\" (got %q)", ch.ChannelType)})
			}
			switch ch.ControlType {
			case "", "bot", "webhook":
				// ok
			default:
				out = append(out, TemplateValidationError{Path: cp + ".controlType", Severity: "error", Message: fmt.Sprintf("controlType must be \"\", \"bot\", or \"webhook\" (got %q)", ch.ControlType)})
			}
			for ri, r := range ch.Roles {
				rp := fmt.Sprintf("%s.roles[%d]", cp, ri)
				if r.Name == "" {
					out = append(out, TemplateValidationError{Path: rp + ".name", Severity: "error", Message: "role name is required"})
				}
			}
			for ti, th := range ch.Threads {
				tp := fmt.Sprintf("%s.threads[%d]", cp, ti)
				if th.Name == "" {
					out = append(out, TemplateValidationError{Path: tp + ".name", Severity: "error", Message: "thread name is required"})
				}
				switch th.ButtonStyle {
				case "", "primary", "secondary", "success", "danger":
					// ok
				default:
					out = append(out, TemplateValidationError{Path: tp + ".buttonStyle", Severity: "error", Message: fmt.Sprintf("buttonStyle must be one of primary/secondary/success/danger (got %q)", th.ButtonStyle)})
				}
			}
			// Warnings.
			if ch.ChannelType == "voice" && (ch.Topic != "" || len(ch.Commands) > 0 || len(ch.Threads) > 0 || ch.ThreadPicker != nil) {
				out = append(out, TemplateValidationError{Path: cp, Severity: "warning", Message: "voice channels don't support topic/commands/threads/threadPicker — those fields will be ignored"})
			}
			if ch.ThreadPicker != nil && len(ch.Threads) == 0 {
				out = append(out, TemplateValidationError{Path: cp + ".threadPicker", Severity: "warning", Message: "threadPicker is set but no threads[] entries — picker will have no buttons to render"})
			}
		}
	}
	return out
}

// PermissionFlag describes one permission overwrite key for the editor.
// Group is "general" / "voice" / "admin" so the editor can render them
// in sensible sections.
type PermissionFlag struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Group string `json:"group"`
}

// permissionFlagMeta layers display metadata on top of rolePermissionFlags.
// Source of truth for the keys themselves stays in rolePermissionFlags;
// any additions there must also be added here or the editor will silently
// hide them.
var permissionFlagMeta = map[string]struct {
	Label string
	Group string
}{
	"view":                 {"View Channel", "general"},
	"viewHistory":          {"Read Message History", "general"},
	"send":                 {"Send Messages", "general"},
	"react":                {"Add Reactions", "general"},
	"pingEveryone":         {"Mention @everyone", "general"},
	"embedLinks":           {"Embed Links", "general"},
	"attachFiles":          {"Attach Files", "general"},
	"sendTTS":              {"Send TTS", "general"},
	"externalEmoji":        {"Use External Emoji", "general"},
	"externalStickers":     {"Use External Stickers", "general"},
	"createPublicThreads":  {"Create Public Threads", "general"},
	"createPrivateThreads": {"Create Private Threads", "general"},
	"sendThreads":          {"Send in Threads", "general"},
	"slashCommands":        {"Use Slash Commands", "general"},
	"createInvite":         {"Create Invites", "general"},
	"connect":              {"Connect", "voice"},
	"speak":                {"Speak", "voice"},
	"autoMic":              {"Use Voice Activation", "voice"},
	"stream":               {"Video / Stream", "voice"},
	"vcActivities":         {"Use Activities", "voice"},
	"prioritySpeaker":      {"Priority Speaker", "voice"},
	"mute":                 {"Mute Members", "voice"},
	"deafen":               {"Deafen Members", "voice"},
	"move":                 {"Move Members", "voice"},
	"channels":             {"Manage Channels", "admin"},
	"messages":             {"Manage Messages", "admin"},
	"roles":                {"Manage Roles", "admin"},
	"webhooks":             {"Manage Webhooks", "admin"},
	"threads":              {"Manage Threads", "admin"},
	"events":               {"Manage Events", "admin"},
}

// PermissionFlagsList returns the editor-facing list of role permission
// keys, sorted by group then name. Source of truth for the key set is
// rolePermissionFlags; any new key without a permissionFlagMeta entry
// is reported with the raw key as label and group "other".
func PermissionFlagsList() []PermissionFlag {
	out := make([]PermissionFlag, 0, len(rolePermissionFlags))
	for k := range rolePermissionFlags {
		meta, ok := permissionFlagMeta[k]
		if !ok {
			meta.Label = k
			meta.Group = "other"
		}
		out = append(out, PermissionFlag{Name: k, Label: meta.Label, Group: meta.Group})
	}
	groupOrder := map[string]int{"general": 0, "voice": 1, "admin": 2, "other": 3}
	sort.Slice(out, func(i, j int) bool {
		gi, gj := groupOrder[out[i].Group], groupOrder[out[j].Group]
		if gi != gj {
			return gi < gj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ChannelTemplate, ChannelDefinition, etc. are exported aliases of the
// existing unexported types so the editor / API can decode the same
// shape without us renaming every internal use of the lowercase name.
// (Aliases share a single underlying type — methods, JSON tags, and
// nil-pointer semantics remain identical.)
type (
	ChannelTemplate    = channelTemplate
	ChannelDefinition  = channelDefinition
	CategoryDefinition = categoryDefinition
	ChannelEntry       = channelEntry
	ThreadPickerDef    = threadPickerDef
	ThreadEntry        = threadEntry
	RoleEntry          = roleEntry
)

// DeleteChannelTemplate removes the named template from the file and
// saves (with backup). Returns "" backupBase + os.ErrNotExist if the
// template wasn't in the array; the caller should map that to 404.
func DeleteChannelTemplate(baseDir, name string) (string, error) {
	raw, err := LoadChannelTemplatesRaw(baseDir)
	if err != nil {
		return "", err
	}
	var templates []ChannelTemplate
	if err := json.Unmarshal(raw, &templates); err != nil {
		return "", fmt.Errorf("parse current templates: %w", err)
	}
	out := templates[:0]
	found := false
	for _, t := range templates {
		if t.Name == name {
			found = true
			continue
		}
		out = append(out, t)
	}
	if !found {
		return "", os.ErrNotExist
	}
	newRaw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("re-encode after delete: %w", err)
	}
	return SaveChannelTemplatesRaw(baseDir, newRaw)
}
