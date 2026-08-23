package main

import (
	"github.com/pokemon/poracleng/processor/internal/discordbot"
)

// channelTemplatesEnums is the static metadata the editor needs to render
// dropdowns + permission-flag pickers. Returned by GET .../schema.
type channelTemplatesEnums struct {
	ChannelTypes     []string                    `json:"channelTypes"`
	ControlTypes     []string                    `json:"controlTypes"`
	ButtonStyles     []string                    `json:"buttonStyles"`
	PermissionFlags  []discordbot.PermissionFlag `json:"permissionFlags"`
	PlaceholderHelp  map[string]string           `json:"placeholderHelp"`
	BackupNamePrefix string                      `json:"backupNamePrefix"`
}

// hasBlockingErrors reports whether any validation error has "error" severity
// (a blocking error that must prevent a write).
func hasBlockingErrors(errs []discordbot.TemplateValidationError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}

// nonBlocking returns the subset of validation errors that are warnings (not
// blocking). The result reuses the input backing array.
func nonBlocking(errs []discordbot.TemplateValidationError) []discordbot.TemplateValidationError {
	out := errs[:0]
	for _, e := range errs {
		if e.Severity != "error" {
			out = append(out, e)
		}
	}
	return out
}
