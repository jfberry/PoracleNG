package enrichment

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/uicons"
)

func testUicons() *uicons.Uicons {
	return uicons.NewWithIndex("https://icons.test", "png", &uicons.Index{
		Pokemon:        map[string]bool{"25.png": true, "0.png": true},
		RewardItem:     map[string]bool{"1.png": true, "0.png": true},
		RewardStardust: map[string]bool{"500.png": true, "0.png": true},
		RewardMega:     map[string]bool{"6.png": true, "0.png": true},
		RewardCandy:    map[string]bool{"25.png": true, "0.png": true},
		Gym:            map[string]bool{"0.png": true, "1.png": true},
		Invasion:       map[string]bool{"41.png": true, "0.png": true},
		Pokestop:       map[string]bool{"501.png": true, "0.png": true},
		Egg:            map[string]bool{"5.png": true, "0.png": true},
		Weather:        map[string]bool{"3.png": true, "0.png": true},
	})
}

func TestAddQuestIconURLs_PokemonReward(t *testing.T) {
	e := &Enricher{ImgUicons: testUicons()}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 7, PokemonID: 25, FormID: 0},
	}
	e.addQuestIconURLs(m, rewards)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("expected imgUrl to be set for pokemon reward")
	}
	if !strings.Contains(imgUrl, "/pokemon/") {
		t.Errorf("pokemon reward imgUrl = %q, want it to contain /pokemon/", imgUrl)
	}
}

func TestAddQuestIconURLs_ItemReward(t *testing.T) {
	e := &Enricher{ImgUicons: testUicons()}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 2, ItemID: 1, Amount: 3},
	}
	e.addQuestIconURLs(m, rewards)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("expected imgUrl to be set for item reward")
	}
	if !strings.Contains(imgUrl, "/reward/item/") {
		t.Errorf("item reward imgUrl = %q, want it to contain /reward/item/", imgUrl)
	}
}

func TestAddQuestIconURLs_StardustReward(t *testing.T) {
	e := &Enricher{ImgUicons: testUicons()}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 3, Amount: 500},
	}
	e.addQuestIconURLs(m, rewards)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("expected imgUrl to be set for stardust reward")
	}
	if !strings.Contains(imgUrl, "/reward/stardust/") {
		t.Errorf("stardust reward imgUrl = %q, want it to contain /reward/stardust/", imgUrl)
	}
}

func TestAddQuestIconURLs_MegaEnergyReward(t *testing.T) {
	e := &Enricher{ImgUicons: testUicons()}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 12, PokemonID: 6, Amount: 40},
	}
	e.addQuestIconURLs(m, rewards)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("expected imgUrl to be set for mega energy reward")
	}
	if !strings.Contains(imgUrl, "/reward/mega_resource/") {
		t.Errorf("mega energy reward imgUrl = %q, want it to contain /reward/mega_resource/", imgUrl)
	}
}

func TestAddQuestIconURLs_CandyReward(t *testing.T) {
	e := &Enricher{ImgUicons: testUicons()}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 4, PokemonID: 25, Amount: 3},
	}
	e.addQuestIconURLs(m, rewards)

	imgUrl, ok := m["imgUrl"].(string)
	if !ok || imgUrl == "" {
		t.Fatal("expected imgUrl to be set for candy reward")
	}
	if !strings.Contains(imgUrl, "/reward/candy/") {
		t.Errorf("candy reward imgUrl = %q, want it to contain /reward/candy/", imgUrl)
	}
}

func TestAddQuestIconURLs_NilUicons(t *testing.T) {
	e := &Enricher{ImgUicons: nil}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 7, PokemonID: 25, FormID: 0},
	}
	e.addQuestIconURLs(m, rewards)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl when ImgUicons is nil")
	}
}

func TestAddQuestIconURLs_EmptyRewards(t *testing.T) {
	e := &Enricher{ImgUicons: testUicons()}
	m := make(map[string]any)

	e.addQuestIconURLs(m, nil)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl for empty rewards")
	}
}

func TestAddQuestIconURLs_AltAndStickerUicons(t *testing.T) {
	alt := uicons.NewWithIndex("https://alt.test", "png", &uicons.Index{
		Pokemon: map[string]bool{"25.png": true, "0.png": true},
	})
	sticker := uicons.NewWithIndex("https://sticker.test", "webp", &uicons.Index{
		Pokemon: map[string]bool{"25.webp": true, "0.webp": true},
	})
	e := &Enricher{
		ImgUicons:     testUicons(),
		ImgUiconsAlt:  alt,
		StickerUicons: sticker,
	}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 7, PokemonID: 25, FormID: 0},
	}
	e.addQuestIconURLs(m, rewards)

	if _, ok := m["imgUrl"].(string); !ok {
		t.Error("expected imgUrl to be set")
	}
	if _, ok := m["imgUrlAlt"].(string); !ok {
		t.Error("expected imgUrlAlt to be set")
	}
	if _, ok := m["stickerUrl"].(string); !ok {
		t.Error("expected stickerUrl to be set")
	}
}

func TestAddQuestIconURLs_ShinyPokemonReward(t *testing.T) {
	// When Shiny is true on the reward, icon should request shiny variant
	idx := &uicons.Index{
		Pokemon: map[string]bool{"25_s.png": true, "25.png": true, "0.png": true},
	}
	e := &Enricher{
		ImgUicons: uicons.NewWithIndex("https://icons.test", "png", idx),
	}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 7, PokemonID: 25, FormID: 0, Shiny: true},
	}
	e.addQuestIconURLs(m, rewards)

	imgUrl := m["imgUrl"].(string)
	// The shiny icon should resolve to the _s variant
	if !strings.Contains(imgUrl, "25_s.png") {
		t.Errorf("shiny pokemon reward imgUrl = %q, want it to contain 25_s.png", imgUrl)
	}
}

func TestAddQuestIconURLs_RequestShinyImages(t *testing.T) {
	// When RequestShinyImages is true on the enricher, pokemon rewards should use shiny icons
	idx := &uicons.Index{
		Pokemon: map[string]bool{"25_s.png": true, "25.png": true, "0.png": true},
	}
	e := &Enricher{
		ImgUicons:          uicons.NewWithIndex("https://icons.test", "png", idx),
		RequestShinyImages: true,
	}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 7, PokemonID: 25, FormID: 0, Shiny: false},
	}
	e.addQuestIconURLs(m, rewards)

	imgUrl := m["imgUrl"].(string)
	if !strings.Contains(imgUrl, "25_s.png") {
		t.Errorf("RequestShinyImages imgUrl = %q, want it to contain 25_s.png", imgUrl)
	}
}

func TestAddQuestIconURLs_UnknownRewardType(t *testing.T) {
	e := &Enricher{ImgUicons: testUicons()}
	m := make(map[string]any)

	rewards := []matching.QuestRewardData{
		{Type: 99},
	}
	e.addQuestIconURLs(m, rewards)

	if _, ok := m["imgUrl"]; ok {
		t.Error("expected no imgUrl for unknown reward type")
	}
}
