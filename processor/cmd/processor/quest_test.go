package main

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// AR and non-AR quests on the same stop must produce different dedup
// keys even when the rewards happen to match — otherwise the second
// firing collides with the first and the user only sees one alert.
func TestBuildQuestRewardsKey_ARSegregation(t *testing.T) {
	rewards := []webhook.QuestReward{
		{Type: 2, Info: map[string]any{"item_id": float64(701), "amount": float64(6)}},
	}

	stdKey := buildQuestRewardsKey(false, rewards)
	arKey := buildQuestRewardsKey(true, rewards)

	if stdKey == arKey {
		t.Fatalf("AR and non-AR quest with identical rewards produced same key %q — collision will silently drop the second webhook", stdKey)
	}
}

// Same AR flag + same rewards must collapse — that's the dedup the
// cache exists for in the first place (Golbat resends every minute).
func TestBuildQuestRewardsKey_SameFlagSameRewardsCollide(t *testing.T) {
	rewards := []webhook.QuestReward{
		{Type: 7, Info: map[string]any{"pokemon_id": float64(25)}},
	}

	if buildQuestRewardsKey(false, rewards) != buildQuestRewardsKey(false, rewards) {
		t.Errorf("identical (withAR=false, rewards) inputs must produce identical keys")
	}
	if buildQuestRewardsKey(true, rewards) != buildQuestRewardsKey(true, rewards) {
		t.Errorf("identical (withAR=true, rewards) inputs must produce identical keys")
	}
}

// Different rewards under the same AR flag must still produce different
// keys (regression: AR prefix must not swallow the rewards segment).
func TestBuildQuestRewardsKey_DifferentRewardsDifferentKey(t *testing.T) {
	a := []webhook.QuestReward{{Type: 7, Info: map[string]any{"pokemon_id": float64(25)}}}
	b := []webhook.QuestReward{{Type: 7, Info: map[string]any{"pokemon_id": float64(132)}}}

	if buildQuestRewardsKey(true, a) == buildQuestRewardsKey(true, b) {
		t.Errorf("different rewards under AR=true produced colliding keys")
	}
}
