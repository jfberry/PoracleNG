package i18n

import "embed"

//go:embed locale/*.json
var embeddedLocales embed.FS
