package enrichment

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/uicons"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// tappableTypePrefix is stripped from Golbat's tappable type for display and
// translation keys (TAPPABLE_TYPE_BOX → box).
const tappableTypePrefix = "TAPPABLE_TYPE_"

// Tappable builds the base enrichment for a tappable webhook. rewards is the
// slice the handler parsed once (matching.ParseTappableRewards).
func (e *Enricher) Tappable(tw *webhook.TappableWebhook, rewards []matching.TappableRewardData, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)
	lat, lon := tw.Latitude, tw.Longitude

	// Identity
	m["encounter_id"] = tw.EncounterID
	if tw.SpawnpointID != "" && tw.SpawnpointID != webhook.TappableNone {
		m["spawnpoint_id"] = tw.SpawnpointID
	}
	if tw.HasPokestop() {
		m["pokestop_id"] = tw.PokestopID
		if tw.PokestopName != nil {
			m["pokestop_name"] = *tw.PokestopName
		}
	}
	m["tappable_type"] = tw.Type

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	cellID := tracker.GetWeatherCellID(lat, lon)
	if e.WeatherProvider != nil {
		m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)
		forecast := e.GetForecast(cellID)
		m["weatherForecastCurrent"] = forecast.Current
		m["weatherForecastNext"] = forecast.Next
	}
	m["nextHourTimestamp"] = tracker.GetNextHourTimestamp()

	// Expiry. disappear_time 0 means unknown: no TTH fields, so clean-deletion
	// takes the default path other unknown-expiry types use.
	m["disappear_time_verified"] = tw.DisappearTimeVerified
	m["confirmedTime"] = tw.DisappearTimeVerified
	if tw.DisappearTime > 0 {
		m["disappear_time"] = tw.DisappearTime
		m["disappearTime"] = geo.FormatTime(tw.DisappearTime, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(tw.DisappearTime)
	}

	// Rewards (first reward as flat fields, all of them as a list).
	addTappableRewardFields(m, rewards)
	m["rewards"] = tappableRewardList(rewards, nil, nil)

	// Map URLs: a fort tappable links to its pokestop; a spawn tappable has
	// no map entity, so only coordinate links.
	if tw.HasPokestop() {
		e.addMapURLs(m, lat, lon, "pokestops", tw.PokestopID)
	} else {
		e.addCoordinateMapURLs(m, lat, lon)
	}

	e.addTappableIconURLs(m, rewards)

	e.addLocationFields(m, lat, lon)

	pending := e.addStaticMap(m, "tappable", lat, lon, nil, tileMode, tw.EncounterID)

	m["color"] = "4CAF50"

	e.setFallbackImg(m, e.FallbackImgURL)

	return m, pending
}

// addCoordinateMapURLs adds the map links that need only coordinates (no
// scanner entity id).
func (e *Enricher) addCoordinateMapURLs(m map[string]any, lat, lon float64) {
	m["appleMapUrl"] = geo.AppleMapURL(lat, lon)
	m["googleMapUrl"] = geo.GoogleMapURL(lat, lon)
	m["wazeMapUrl"] = geo.WazeMapURL(lat, lon)
	if e.MapConfig != nil && e.MapConfig.RocketMadURL != "" {
		m["rocketMadUrl"] = normalizeTrailingSlash(e.MapConfig.RocketMadURL) + "?lat=" + geo.FormatCoord(lat) + "&lon=" + geo.FormatCoord(lon) + "&zoom=18.0"
	}
}

// tappableRewardColumn returns the value a tappable rule's `reward` column
// holds for this reward: the item id, the pokemon id, or the amount for
// stardust / pokecoins (the quest quirk).
func tappableRewardColumn(r matching.TappableRewardData) int {
	switch r.Type {
	case matching.TappableRewardItem:
		return r.ItemID
	case matching.TappableRewardStardust, matching.TappableRewardPokecoin:
		return r.Amount
	case matching.TappableRewardCandy, matching.TappableRewardXLCandy, matching.TappableRewardMegaEnergy:
		return r.PokemonID
	}
	return 0
}

// addTappableRewardFields sets the flat fields describing the first reward.
func addTappableRewardFields(m map[string]any, rewards []matching.TappableRewardData) {
	m["rewardCount"] = len(rewards)
	if len(rewards) == 0 {
		return
	}
	r := rewards[0]
	m["rewardType"] = r.Key
	m["rewardTypeId"] = r.Type
	m["reward"] = tappableRewardColumn(r)
	m["rewardAmount"] = r.Amount
	m["rewardPokemonId"] = r.PokemonID
	m["rewardItemId"] = r.ItemID
}

// tappableRewardList builds the `rewards` array. tr / enTr may be nil (base
// layer, untranslated); with translators each entry also carries name /
// nameEng (the item or pokemon) and text / textEng ("6 Zubat Candy").
func tappableRewardList(rewards []matching.TappableRewardData, tr, enTr *i18n.Translator) []map[string]any {
	list := make([]map[string]any, 0, len(rewards))
	for _, r := range rewards {
		entry := map[string]any{
			"type":      r.Key,
			"typeId":    r.Type,
			"amount":    r.Amount,
			"pokemonId": r.PokemonID,
			"itemId":    r.ItemID,
		}
		if tr != nil {
			entry["name"] = tappableRewardSubject(tr, r)
			entry["nameEng"] = tappableRewardSubject(enTr, r)
			entry["text"] = tappableRewardText(tr, r)
			entry["textEng"] = tappableRewardText(enTr, r)
		}
		list = append(list, entry)
	}
	return list
}

// tappableRewardSubject is the thing rewarded: the item or pokemon name, or
// the reward label for stardust / pokecoins / untrackable rewards.
func tappableRewardSubject(tr *i18n.Translator, r matching.TappableRewardData) string {
	switch r.Type {
	case matching.TappableRewardItem:
		return TranslateItemName(tr, r.ItemID)
	case matching.TappableRewardCandy, matching.TappableRewardXLCandy, matching.TappableRewardMegaEnergy:
		if r.PokemonID == 0 {
			return ""
		}
		return tr.T(gamedata.PokemonTranslationKey(r.PokemonID))
	}
	return tappableRewardLabel(tr, r)
}

// tappableRewardLabel is the reward kind ("Candy", "Stardust", "XL Candy").
// Quest's quest_reward_N keys are reused where they exist; untrackable
// rewards fall back to their prettified oneof name.
func tappableRewardLabel(tr *i18n.Translator, r matching.TappableRewardData) string {
	switch r.Type {
	case matching.TappableRewardStardust, matching.TappableRewardCandy,
		matching.TappableRewardPokecoin, matching.TappableRewardMegaEnergy:
		return tr.T(fmt.Sprintf("quest_reward_%d", r.Type))
	case matching.TappableRewardXLCandy:
		return translateOr(tr, "quest_reward_9", translateOr(tr, "tappable.reward.xl_candy", "XL Candy"))
	}
	switch r.Key {
	case "experience":
		return translateOr(tr, "quest_reward_1", prettifyIdentifier(r.Key))
	case "sticker_id":
		return translateOr(tr, "quest_reward_11", prettifyIdentifier(r.Key))
	}
	return prettifyIdentifier(r.Key)
}

// tappableRewardText phrases one reward the way quest does: "6 Zubat Candy",
// "Rare Candy" / "2 Rare Candy", "500 Stardust", "20 Charizard Mega Energy".
func tappableRewardText(tr *i18n.Translator, r matching.TappableRewardData) string {
	switch r.Type {
	case matching.TappableRewardItem:
		name := TranslateItemName(tr, r.ItemID)
		if name == "" {
			return ""
		}
		if r.Amount > 1 {
			return fmt.Sprintf("%d %s", r.Amount, name)
		}
		return name
	case matching.TappableRewardCandy, matching.TappableRewardXLCandy, matching.TappableRewardMegaEnergy:
		label := tappableRewardLabel(tr, r)
		if r.PokemonID == 0 {
			return joinNonEmpty(amountPrefix(r.Amount), label)
		}
		return joinNonEmpty(amountPrefix(r.Amount), tr.T(gamedata.PokemonTranslationKey(r.PokemonID)), label)
	}
	return joinNonEmpty(amountPrefix(r.Amount), tappableRewardLabel(tr, r))
}

func amountPrefix(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

func joinNonEmpty(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// translateOr returns tr.T(key), or fallback when the key is untranslated.
func translateOr(tr *i18n.Translator, key, fallback string) string {
	if s := tr.T(key); s != key {
		return s
	}
	return fallback
}

// prettifyIdentifier turns an identifier like "route_pin" / "ROUTE_PIN" into
// "Route Pin".
func prettifyIdentifier(s string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ToLower(s), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// TappableTypeName translates a Golbat tappable type (TAPPABLE_TYPE_BOX) via
// the tappable.type.<name> key, falling back to the prettified name for types
// with no translation.
func TappableTypeName(tr *i18n.Translator, tappableType string) string {
	short := strings.ToLower(strings.TrimPrefix(strings.ToUpper(tappableType), tappableTypePrefix))
	if short == "" {
		return ""
	}
	return translateOr(tr, "tappable.type."+short, prettifyIdentifier(short))
}

// addTappableIconURLs sets imgUrl / imgUrlAlt / stickerUrl from the first
// reward (mirrors addQuestIconURLs).
func (e *Enricher) addTappableIconURLs(m map[string]any, rewards []matching.TappableRewardData) {
	if len(rewards) == 0 {
		return
	}
	r := rewards[0]
	var icon func(u iconSource) string
	switch {
	case r.Type == matching.TappableRewardItem && r.ItemID > 0:
		icon = func(u iconSource) string { return u.RewardItemIcon(r.ItemID, r.Amount) }
	case r.Type == matching.TappableRewardStardust:
		icon = func(u iconSource) string { return u.RewardStardustIcon(r.Amount) }
	case r.Type == matching.TappableRewardPokecoin:
		icon = func(u iconSource) string { return u.RewardPokecoinIcon(r.Amount) }
	case r.Type == matching.TappableRewardCandy && r.PokemonID > 0:
		icon = func(u iconSource) string { return u.RewardCandyIcon(r.PokemonID, r.Amount) }
	case r.Type == matching.TappableRewardXLCandy && r.PokemonID > 0:
		icon = func(u iconSource) string { return u.RewardXlCandyIcon(r.PokemonID, r.Amount) }
	case r.Type == matching.TappableRewardMegaEnergy && r.PokemonID > 0:
		icon = func(u iconSource) string { return u.RewardMegaEnergyIcon(r.PokemonID, r.Amount) }
	default:
		return
	}
	// An empty URL (no icon for this reward, e.g. XL candy without an index)
	// is left unset so setFallbackImg can fill imgUrl.
	for key, u := range map[string]*uicons.Uicons{"imgUrl": e.ImgUicons, "imgUrlAlt": e.ImgUiconsAlt, "stickerUrl": e.StickerUicons} {
		if u == nil {
			continue
		}
		if url := icon(u); url != "" {
			m[key] = url
		}
	}
}

// iconSource is the reward-icon subset of *uicons.Uicons.
type iconSource interface {
	RewardItemIcon(itemID, amount int) string
	RewardStardustIcon(amount int) string
	RewardPokecoinIcon(amount int) string
	RewardCandyIcon(pokemonID, amount int) string
	RewardXlCandyIcon(pokemonID, amount int) string
	RewardMegaEnergyIcon(pokemonID, amount int) string
}

// TappableTranslate adds the per-language fields: the translated reward
// list and string, the first reward's subject name, the tappable type name,
// weather name and localized address.
func (e *Enricher) TappableTranslate(base map[string]any, tw *webhook.TappableWebhook, rewards []matching.TappableRewardData, lang string) map[string]any {
	if e.Translations == nil || tw == nil {
		return nil
	}

	m := make(map[string]any, 16)
	defer e.addLocalizedGeoResult(m, tw.Latitude, tw.Longitude, lang)

	tr := e.Translations.For(lang)
	enTr := e.Translations.For("en")

	m["tappableTypeName"] = TappableTypeName(tr, tw.Type)
	m["tappableTypeNameEng"] = TappableTypeName(enTr, tw.Type)

	m["rewards"] = tappableRewardList(rewards, tr, enTr)

	var parts, partsEng []string
	for _, r := range rewards {
		if s := tappableRewardText(tr, r); s != "" {
			parts = append(parts, s)
		}
		if s := tappableRewardText(enTr, r); s != "" {
			partsEng = append(partsEng, s)
		}
	}
	m["rewardString"] = strings.Join(parts, ", ")
	m["rewardStringEng"] = strings.Join(partsEng, ", ")
	if len(rewards) > 0 {
		m["rewardName"] = tappableRewardSubject(tr, rewards[0])
		m["rewardNameEng"] = tappableRewardSubject(enTr, rewards[0])
	}

	gameWeatherID := toInt(base["gameWeatherId"])
	m["gameWeatherName"] = TranslateWeatherName(tr, gameWeatherID)
	m["gameWeatherNameEng"] = TranslateWeatherName(enTr, gameWeatherID)
	if gameWeatherID > 0 && e.GameData != nil {
		if wInfo, ok := e.GameData.Util.Weather[gameWeatherID]; ok {
			m["gameWeatherEmojiKey"] = wInfo.Emoji
		}
	}

	return m
}
