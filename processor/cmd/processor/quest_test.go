package main

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/tracker"
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

// bufferQuestMatches must put one entry per user into the summary
// buffer, preserving the identity tuple (RewardType, Reward, PokestopID,
// WithAR) and a verbatim copy of the raw webhook payload.
func TestBufferQuestMatches_WritesIdentityTuple(t *testing.T) {
	ps := &ProcessorService{summaryBuffer: tracker.NewSummaryBuffer("")}

	quest := &webhook.QuestWebhook{
		PokestopID: "stop-A",
		Latitude:   51.0,
		Longitude:  0.5,
		WithAR:     true,
	}
	rewards := []matching.QuestRewardData{
		{Type: 7, PokemonID: 25}, // pokemon encounter
	}
	raw := []byte(`{"pokestop_id":"stop-A","with_ar":true}`)
	users := []webhook.MatchedUser{{ID: "user-1", Clean: 4}}

	ps.bufferQuestMatches(users, quest, rewards, raw)

	got := ps.summaryBuffer.List("user-1", "quest")
	if len(got) != 1 {
		t.Fatalf("List len = %d, want 1", len(got))
	}
	q := got[0]
	if q.PokestopID != "stop-A" {
		t.Errorf("PokestopID = %q, want stop-A", q.PokestopID)
	}
	if !q.WithAR {
		t.Errorf("WithAR = false, want true")
	}
	if q.RewardType != 7 || q.Reward != 25 {
		t.Errorf("(RewardType, Reward) = (%d, %d), want (7, 25)", q.RewardType, q.Reward)
	}
	if string(q.Payload) != string(raw) {
		t.Errorf("Payload = %q, want %q", q.Payload, raw)
	}
	if q.ExpiresAt == 0 {
		t.Errorf("ExpiresAt should be set (end-of-day timestamp), got 0")
	}
	if q.CreatedAt == 0 {
		t.Errorf("CreatedAt should be set, got 0")
	}
}

// Multiple users in one buffered slice each get their own entry, all
// keyed under the same (rewardType, reward, pokestopID, withAR) tuple.
func TestBufferQuestMatches_OneEntryPerUser(t *testing.T) {
	ps := &ProcessorService{summaryBuffer: tracker.NewSummaryBuffer("")}

	quest := &webhook.QuestWebhook{
		PokestopID: "stop-B",
		Latitude:   51.0, Longitude: 0.5,
	}
	rewards := []matching.QuestRewardData{{Type: 3, Amount: 200}} // stardust
	raw := []byte(`{"pokestop_id":"stop-B"}`)

	users := []webhook.MatchedUser{
		{ID: "user-1", Clean: 4},
		{ID: "user-2", Clean: 4},
	}
	ps.bufferQuestMatches(users, quest, rewards, raw)

	for _, id := range []string{"user-1", "user-2"} {
		got := ps.summaryBuffer.List(id, "quest")
		if len(got) != 1 {
			t.Errorf("user %s: List len = %d, want 1", id, len(got))
		}
		if len(got) == 1 && got[0].PokestopID != "stop-B" {
			t.Errorf("user %s: PokestopID = %q, want stop-B", id, got[0].PokestopID)
		}
	}

	// Stardust uses Amount as the bufferKey reward field so repeat
	// firings of the same stop with the same amount still dedup.
	if got := ps.summaryBuffer.List("user-1", "quest"); got[0].RewardType != 3 || got[0].Reward != 200 {
		t.Errorf("stardust keying: (RewardType, Reward) = (%d, %d), want (3, 200)", got[0].RewardType, got[0].Reward)
	}
}

// Nil buffer is a soft guard — bufferQuestMatches must not panic even
// if the buffer wasn't constructed (defensive against early-startup
// quest webhooks).
func TestBufferQuestMatches_NilBufferIsSafe(t *testing.T) {
	ps := &ProcessorService{summaryBuffer: nil}

	quest := &webhook.QuestWebhook{PokestopID: "stop-A"}
	rewards := []matching.QuestRewardData{{Type: 7, PokemonID: 25}}
	users := []webhook.MatchedUser{{ID: "user-1", Clean: 4}}

	// Should not panic.
	ps.bufferQuestMatches(users, quest, rewards, []byte(`{}`))
}
