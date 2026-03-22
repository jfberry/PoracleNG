// Package uicons resolves icon URLs from UICONS-compatible icon repositories.
// It fetches and caches the repository's index.json on a schedule, then uses
// it to resolve the most specific available icon for a given entity.
package uicons

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	refreshInterval = 1 * time.Hour
	fetchTimeout    = 10 * time.Second
)

// Index holds sets of available icon filenames per category.
type Index struct {
	Pokemon        map[string]bool
	Gym            map[string]bool
	Weather        map[string]bool
	Pokestop       map[string]bool
	Invasion       map[string]bool
	Type           map[string]bool
	Team           map[string]bool
	Egg            map[string]bool
	RewardItem     map[string]bool
	RewardStardust map[string]bool
	RewardCandy    map[string]bool
	RewardXlCandy  map[string]bool
	RewardMega     map[string]bool
}

// Uicons resolves icon URLs from a UICONS-compatible icon repository.
type Uicons struct {
	url       string
	imageType string
	index     atomic.Pointer[Index]
	client    *http.Client
}

// NewWithIndex creates a Uicons with a pre-loaded index, for testing.
// No HTTP fetch or background refresh is started.
func NewWithIndex(url, imageType string, idx *Index) *Uicons {
	url = strings.TrimRight(url, "/")
	if imageType == "" {
		imageType = "png"
	}
	u := &Uicons{url: url, imageType: imageType}
	if idx != nil {
		u.index.Store(idx)
	}
	return u
}

// New creates a new Uicons resolver. Fetches index.json immediately and
// refreshes on a 1-hour schedule.
func New(url, imageType string) *Uicons {
	url = strings.TrimRight(url, "/")
	if imageType == "" {
		imageType = "png"
	}
	u := &Uicons{
		url:       url,
		imageType: imageType,
		client:    &http.Client{Timeout: fetchTimeout},
	}
	// Fetch immediately, then schedule refresh
	u.fetchIndex()
	go u.refreshLoop()
	return u
}

func (u *Uicons) refreshLoop() {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for range ticker.C {
		u.fetchIndex()
	}
}

func (u *Uicons) fetchIndex() {
	indexURL := u.url + "/index.json"
	resp, err := u.client.Get(indexURL)
	if err != nil {
		log.Warnf("uicons: failed to fetch %s: %v", indexURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Debugf("uicons: got 404 for %s (no index available)", indexURL)
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Warnf("uicons: unexpected status %d for %s", resp.StatusCode, indexURL)
		return
	}

	var raw struct {
		Pokemon  []string `json:"pokemon"`
		Gym      []string `json:"gym"`
		Weather  []string `json:"weather"`
		Pokestop []string `json:"pokestop"`
		Invasion []string `json:"invasion"`
		Type     []string `json:"type"`
		Team     []string `json:"team"`
		Raid     *struct {
			Egg []string `json:"egg"`
		} `json:"raid"`
		Reward *struct {
			Item     []string `json:"item"`
			Stardust []string `json:"stardust"`
			Candy    []string `json:"candy"`
			XlCandy  []string `json:"xl_candy"`
			Mega     []string `json:"mega_resource"`
		} `json:"reward"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Warnf("uicons: failed to decode index from %s: %v", indexURL, err)
		return
	}

	idx := &Index{
		Pokemon:  toSet(raw.Pokemon),
		Gym:      toSet(raw.Gym),
		Weather:  toSet(raw.Weather),
		Pokestop: toSet(raw.Pokestop),
		Invasion: toSet(raw.Invasion),
		Type:     toSet(raw.Type),
		Team:     toSet(raw.Team),
	}
	if raw.Raid != nil {
		idx.Egg = toSet(raw.Raid.Egg)
	} else {
		idx.Egg = map[string]bool{}
	}
	if raw.Reward != nil {
		idx.RewardItem = toSet(raw.Reward.Item)
		idx.RewardStardust = toSet(raw.Reward.Stardust)
		idx.RewardCandy = toSet(raw.Reward.Candy)
		idx.RewardXlCandy = toSet(raw.Reward.XlCandy)
		idx.RewardMega = toSet(raw.Reward.Mega)
	} else {
		idx.RewardItem = map[string]bool{}
		idx.RewardStardust = map[string]bool{}
		idx.RewardCandy = map[string]bool{}
		idx.RewardXlCandy = map[string]bool{}
		idx.RewardMega = map[string]bool{}
	}

	log.Infof("uicons: loaded index from %s (%d pokemon icons)", indexURL, len(idx.Pokemon))
	u.index.Store(idx)
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

// PokemonIcon resolves the URL for a pokemon icon.
func (u *Uicons) PokemonIcon(pokemonID, form, evolution, gender, costume, alignment int, shiny bool) string {
	if idx := u.index.Load(); idx != nil {
		file := resolvePokemonIcon(idx.Pokemon, u.imageType, pokemonID, form, evolution, gender, costume, alignment, shiny)
		return u.url + "/pokemon/" + file
	}
	// Fallback path (no index available)
	evoSuffix := ""
	if evolution > 0 {
		evoSuffix = fmt.Sprintf("_%d", evolution)
	}
	formStr := "00"
	if form > 0 {
		formStr = fmt.Sprintf("%d", form)
	}
	return fmt.Sprintf("%s/pokemon_icon_%03d_%s%s.%s", u.url, pokemonID, formStr, evoSuffix, u.imageType)
}

// EggIcon resolves the URL for a raid egg icon.
func (u *Uicons) EggIcon(level int, hatched, ex bool) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveEggIcon(idx.Egg, u.imageType, level, hatched, ex)
		return u.url + "/raid/egg/" + file
	}
	return fmt.Sprintf("%s/egg%d.%s", u.url, level, u.imageType)
}

// GymIcon resolves the URL for a gym icon.
func (u *Uicons) GymIcon(teamID, trainerCount int, inBattle, ex bool) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveGymIcon(idx.Gym, u.imageType, teamID, trainerCount, inBattle, ex)
		return u.url + "/gym/" + file
	}
	return ""
}

// WeatherIcon resolves the URL for a weather icon.
func (u *Uicons) WeatherIcon(weatherID int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveSimpleIcon(idx.Weather, u.imageType, weatherID)
		return u.url + "/weather/" + file
	}
	return ""
}

// InvasionIcon resolves the URL for a Team Rocket invasion icon.
func (u *Uicons) InvasionIcon(gruntType int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveSimpleIcon(idx.Invasion, u.imageType, gruntType)
		return u.url + "/invasion/" + file
	}
	return ""
}

// PokestopIcon resolves the URL for a pokestop icon.
func (u *Uicons) PokestopIcon(lureID int, invasionActive bool, incidentDisplayType int, questActive bool) string {
	if idx := u.index.Load(); idx != nil {
		file := resolvePokestopIcon(idx.Pokestop, u.imageType, lureID, invasionActive, incidentDisplayType, questActive)
		return u.url + "/pokestop/" + file
	}
	return ""
}

// TypeIcon resolves the URL for a type icon.
func (u *Uicons) TypeIcon(typeID int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveSimpleIcon(idx.Type, u.imageType, typeID)
		return u.url + "/type/" + file
	}
	return ""
}

// TeamIcon resolves the URL for a team icon.
func (u *Uicons) TeamIcon(teamID int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveSimpleIcon(idx.Team, u.imageType, teamID)
		return u.url + "/team/" + file
	}
	return ""
}

// RewardItemIcon resolves the URL for a reward item icon.
func (u *Uicons) RewardItemIcon(itemID, amount int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveItemIcon(idx.RewardItem, u.imageType, itemID, amount)
		return u.url + "/reward/item/" + file
	}
	return fmt.Sprintf("%s/rewards/reward_%d_1.%s", u.url, itemID, u.imageType)
}

// RewardStardustIcon resolves the URL for a stardust reward icon.
func (u *Uicons) RewardStardustIcon(amount int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveItemIcon(idx.RewardStardust, u.imageType, amount, 0)
		return u.url + "/reward/stardust/" + file
	}
	return fmt.Sprintf("%s/rewards/reward_stardust.%s", u.url, u.imageType)
}

// RewardCandyIcon resolves the URL for a candy reward icon.
func (u *Uicons) RewardCandyIcon(pokemonID, amount int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveItemIcon(idx.RewardCandy, u.imageType, pokemonID, amount)
		return u.url + "/reward/candy/" + file
	}
	return fmt.Sprintf("%s/rewards/reward_candy_%d.%s", u.url, pokemonID, u.imageType)
}

// RewardXlCandyIcon resolves the URL for an XL candy reward icon.
func (u *Uicons) RewardXlCandyIcon(pokemonID, amount int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveItemIcon(idx.RewardXlCandy, u.imageType, pokemonID, amount)
		return u.url + "/reward/xl_candy/" + file
	}
	return ""
}

// RewardMegaEnergyIcon resolves the URL for a mega energy reward icon.
func (u *Uicons) RewardMegaEnergyIcon(pokemonID, amount int) string {
	if idx := u.index.Load(); idx != nil {
		file := resolveItemIcon(idx.RewardMega, u.imageType, pokemonID, amount)
		return u.url + "/reward/mega_resource/" + file
	}
	return fmt.Sprintf("%s/rewards/reward_mega_energy_%d.%s", u.url, pokemonID, u.imageType)
}

// --- resolve functions ---
// These match the JS implementation's fallback chains exactly.

func resolvePokemonIcon(avail map[string]bool, imageType string, pokemonID, form, evolution, gender, costume, alignment int, shiny bool) string {
	evolutionSuffixes := []string{""}
	if evolution != 0 {
		evolutionSuffixes = []string{fmt.Sprintf("_e%d", evolution), ""}
	}
	formSuffixes := []string{""}
	if form != 0 {
		formSuffixes = []string{fmt.Sprintf("_f%d", form), ""}
	}
	costumeSuffixes := []string{""}
	if costume != 0 {
		costumeSuffixes = []string{fmt.Sprintf("_c%d", costume), ""}
	}
	genderSuffixes := []string{""}
	if gender != 0 {
		genderSuffixes = []string{fmt.Sprintf("_g%d", gender), ""}
	}
	alignmentSuffixes := []string{""}
	if alignment != 0 {
		alignmentSuffixes = []string{fmt.Sprintf("_a%d", alignment), ""}
	}
	shinySuffixes := []string{""}
	if shiny {
		shinySuffixes = []string{"_s", ""}
	}

	ext := "." + imageType
	for _, evoSfx := range evolutionSuffixes {
		for _, formSfx := range formSuffixes {
			for _, costumeSfx := range costumeSuffixes {
				for _, genderSfx := range genderSuffixes {
					for _, alignSfx := range alignmentSuffixes {
						for _, shinySfx := range shinySuffixes {
							result := fmt.Sprintf("%d%s%s%s%s%s%s%s", pokemonID, evoSfx, formSfx, costumeSfx, genderSfx, alignSfx, shinySfx, ext)
							if avail[result] {
								return result
							}
						}
					}
				}
			}
		}
	}
	return "0" + ext
}

func resolveGymIcon(avail map[string]bool, imageType string, teamID, trainerCount int, inBattle, ex bool) string {
	trainerSuffixes := []string{""}
	if trainerCount != 0 {
		trainerSuffixes = []string{fmt.Sprintf("_t%d", trainerCount), ""}
	}
	inBattleSuffixes := []string{""}
	if inBattle {
		inBattleSuffixes = []string{"_b", ""}
	}
	exSuffixes := []string{""}
	if ex {
		exSuffixes = []string{"_ex", ""}
	}

	ext := "." + imageType
	for _, tSfx := range trainerSuffixes {
		for _, bSfx := range inBattleSuffixes {
			for _, exSfx := range exSuffixes {
				result := fmt.Sprintf("%d%s%s%s%s", teamID, tSfx, bSfx, exSfx, ext)
				if avail[result] {
					return result
				}
			}
		}
	}
	return "0" + ext
}

func resolveEggIcon(avail map[string]bool, imageType string, level int, hatched, ex bool) string {
	hatchedSuffixes := []string{""}
	if hatched {
		hatchedSuffixes = []string{"_h", ""}
	}
	exSuffixes := []string{""}
	if ex {
		exSuffixes = []string{"_ex", ""}
	}

	ext := "." + imageType
	for _, hSfx := range hatchedSuffixes {
		for _, exSfx := range exSuffixes {
			result := fmt.Sprintf("%d%s%s%s", level, hSfx, exSfx, ext)
			if avail[result] {
				return result
			}
		}
	}
	return "0" + ext
}

func resolveSimpleIcon(avail map[string]bool, imageType string, id int) string {
	result := fmt.Sprintf("%d.%s", id, imageType)
	if avail[result] {
		return result
	}
	return "0." + imageType
}

func resolvePokestopIcon(avail map[string]bool, imageType string, lureID int, invasionActive bool, incidentDisplayType int, questActive bool) string {
	invasionSuffixes := []string{""}
	if invasionActive {
		invasionSuffixes = []string{"_i", ""}
	}
	incidentSuffixes := []string{""}
	if incidentDisplayType != 0 {
		incidentSuffixes = []string{fmt.Sprintf("%d", incidentDisplayType), ""}
	}
	questSuffixes := []string{""}
	if questActive {
		questSuffixes = []string{"_q", ""}
	}

	ext := "." + imageType
	for _, iSfx := range invasionSuffixes {
		for _, idtSfx := range incidentSuffixes {
			for _, qSfx := range questSuffixes {
				result := fmt.Sprintf("%d%s%s%s%s", lureID, iSfx, idtSfx, qSfx, ext)
				if avail[result] {
					return result
				}
			}
		}
	}
	return "0" + ext
}

func resolveItemIcon(avail map[string]bool, imageType string, id, amount int) string {
	ext := "." + imageType
	if amount != 0 {
		resultAmount := fmt.Sprintf("%d_a%d%s", id, amount, ext)
		if avail[resultAmount] {
			return resultAmount
		}
	}
	result := fmt.Sprintf("%d%s", id, ext)
	if avail[result] {
		return result
	}
	return "0" + ext
}
