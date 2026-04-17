package i18n

import "embed"

// Embed both the UI strings (locale/*.json) and the Pokemon-name set
// (locale/pokemon/*.json). LoadJSONFS walks the tree and merges every file
// into the translator whose locale matches the filename, so UI and Pokemon
// entries end up in the same per-locale translator without any extra plumbing.
//
//go:embed locale/*.json locale/pokemon/*.json
var embeddedLocales embed.FS
