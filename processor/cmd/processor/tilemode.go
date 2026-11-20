package main

import (
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/logref"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// tileMode determines whether we need a tile and in what form.
// Called after matching, before enrichment.
//
// templateType is whatever DTS type the renderer will look up — the
// function is type-agnostic (e.g. "monster", "quest", "questSummary"),
// so newly added types automatically work here as long as the per-user
// templates exist. The questSummary path doesn't currently route
// through this helper because BuildQuestSummaryView builds the URL
// synchronously, but tileMode handles "questSummary" correctly if a
// future caller does pre-resolve summary tiles.
func (ps *ProcessorService) tileMode(templateType string, matched []webhook.MatchedUser, ref string) int {
	return ps.tileModeFor([]tileAudience{{templateType: templateType, users: matched}}, ref)
}

// tileAudience is a set of recipients rendered with one DTS template type.
type tileAudience struct {
	templateType string
	users        []webhook.MatchedUser
}

// tileModeFor is tileMode for a batch whose recipients are rendered with
// different template types, e.g. a pokemon event that renders `monster` (or
// `monsterNoIv`) for matched users and `monsterChanged` for prior-only users.
// Each recipient is checked against the template it will actually get;
// checking them all against one type missed a `monsterChanged` template that
// uses the tile when `monster` doesn't, so change alerts went out without it.
func (ps *ProcessorService) tileModeFor(audiences []tileAudience, ref string) int {
	l := logref.WithOptional(ref)
	if ps.dtsRenderer == nil {
		return enrichment.TileModeSkip
	}
	ts := ps.dtsRenderer.Templates()

	var anyNeedsTile, anyNeedsURL, anyDiscordUpload bool
	templateType, users := "", 0

	for _, a := range audiences {
		if len(a.users) == 0 {
			continue
		}
		if templateType == "" {
			templateType = a.templateType
		} else if templateType != a.templateType {
			templateType += "+" + a.templateType
		}
		users += len(a.users)
		for _, u := range a.users {
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

			tt := a.templateType
			// The renderer falls back from monsterNoIv to monster when there
			// is no monsterNoIv template; UsesTile would otherwise answer a
			// conservative "yes" for the missing template.
			if tt == "monsterNoIv" && ts.Get(tt, platform, tmplID, lang) == nil {
				tt = "monster"
			}

			if !ts.UsesTile(tt, platform, tmplID, lang) {
				continue // this user's template doesn't use staticMap
			}

			anyNeedsTile = true

			if canUploadInline(u.Type, ps.cfg.Discord.UploadEmbedImages) {
				anyDiscordUpload = true
			} else {
				anyNeedsURL = true
			}
		}
	}

	switch {
	case !anyNeedsTile:
		l.Debugf("tileMode: %s → skip (no template uses staticMap)", templateType)
		metrics.TileModeTotal.WithLabelValues("skip").Inc()
		return enrichment.TileModeSkip
	case anyNeedsURL && anyDiscordUpload:
		l.Debugf("tileMode: %s → url_with_bytes (%d users, mixed URL-needers + Discord upload)", templateType, users)
		metrics.TileModeTotal.WithLabelValues("url_with_bytes").Inc()
		return enrichment.TileModeURLWithBytes
	case anyNeedsURL:
		l.Debugf("tileMode: %s → url (%d users, at least one needs fetchable URL)", templateType, users)
		metrics.TileModeTotal.WithLabelValues("url").Inc()
		return enrichment.TileModeURL
	default:
		l.Debugf("tileMode: %s → inline (%d users, all support upload)", templateType, users)
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
