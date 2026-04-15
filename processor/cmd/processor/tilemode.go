package main

import (
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// tileMode determines whether we need a tile and in what form.
// Called after matching, before enrichment.
func (ps *ProcessorService) tileMode(templateType string, matched []webhook.MatchedUser) int {
	if ps.dtsRenderer == nil {
		return enrichment.TileModeSkip
	}
	ts := ps.dtsRenderer.Templates()

	var anyNeedsTile, anyNeedsURL, anyDiscordUpload bool

	for _, u := range matched {
		// Resolve the template ID the same way the renderer does
		tmplID := u.Template
		if tmplID == "" {
			tmplID = ps.dtsRenderer.ResolveTemplate("")
		}

		lang := u.Language
		if lang == "" {
			lang = ps.cfg.General.Locale
		}

		platform := delivery.PlatformFromType(u.Type)

		if !ts.UsesTile(templateType, platform, tmplID, lang) {
			continue // this user's template doesn't use staticMap
		}

		anyNeedsTile = true

		if canUploadInline(u.Type, ps.cfg.Discord.UploadEmbedImages) {
			anyDiscordUpload = true
		} else {
			anyNeedsURL = true
		}
	}

	switch {
	case !anyNeedsTile:
		log.Debugf("tileMode: %s → skip (no template uses staticMap)", templateType)
		metrics.TileModeTotal.WithLabelValues("skip").Inc()
		return enrichment.TileModeSkip
	case anyNeedsURL && anyDiscordUpload:
		log.Debugf("tileMode: %s → url_with_bytes (%d users, mixed URL-needers + Discord upload)", templateType, len(matched))
		metrics.TileModeTotal.WithLabelValues("url_with_bytes").Inc()
		return enrichment.TileModeURLWithBytes
	case anyNeedsURL:
		log.Debugf("tileMode: %s → url (%d users, at least one needs fetchable URL)", templateType, len(matched))
		metrics.TileModeTotal.WithLabelValues("url").Inc()
		return enrichment.TileModeURL
	default:
		log.Debugf("tileMode: %s → inline (%d users, all support upload)", templateType, len(matched))
		metrics.TileModeTotal.WithLabelValues("inline").Inc()
		return enrichment.TileModeInline
	}
}

// canUploadInline returns true if this destination type supports receiving
// uploaded image bytes instead of a fetchable URL.
func canUploadInline(userType string, uploadEmbedImages bool) bool {
	platform := delivery.PlatformFromType(userType)
	switch platform {
	case "discord":
		return uploadEmbedImages
	default:
		return false // Telegram always needs URL
	}
}
