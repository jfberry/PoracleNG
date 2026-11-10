package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/enrichment"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/validation"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// recordingValidator denies the listed human IDs and records every call.
type recordingValidator struct {
	mu    sync.Mutex
	deny  map[string]bool
	calls []validation.Request
}

func (v *recordingValidator) Validate(_ context.Context, req validation.Request) validation.Decision {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls = append(v.calls, req)
	return validation.Decision{Allow: !v.deny[req.Human.ID]}
}
func (v *recordingValidator) Concurrency() int { return 1 }
func (v *recordingValidator) Enabled() bool    { return true }

func (v *recordingValidator) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.calls)
}

func platformCfg(discord, telegram bool) *config.Config {
	cfg := &config.Config{General: config.GeneralConfig{Locale: "en"}}
	cfg.Discord.Enabled, cfg.Telegram.Enabled = discord, telegram
	return cfg
}

var summaryTestQuest = &webhook.QuestWebhook{PokestopID: "stop-A", Latitude: 51.0, Longitude: 0.5}

// #311: a disabled platform's humans must never be buffered.
func TestBufferQuestMatches_SkipsDisabledPlatform(t *testing.T) {
	ps := &ProcessorService{summaryBuffer: tracker.NewSummaryBuffer(""), cfg: platformCfg(false, true)}
	users := []webhook.MatchedUser{
		{ID: "d1", Type: "discord:user", Clean: 4},
		{ID: "w1", Type: "webhook", Clean: 4},
		{ID: "t1", Type: "telegram:user", Clean: 4},
	}
	ps.bufferQuestMatches(users, summaryTestQuest, []matching.QuestRewardData{{Type: 3, Amount: 200}}, []byte(`{}`), nil)

	for _, id := range []string{"d1", "w1"} {
		if got := ps.summaryBuffer.List(id, AlertTypeQuest); len(got) != 0 {
			t.Errorf("%s (discord disabled): buffered %d entries, want 0", id, len(got))
		}
	}
	if got := ps.summaryBuffer.List("t1", AlertTypeQuest); len(got) != 1 {
		t.Errorf("t1 (telegram enabled): buffered %d entries, want 1", len(got))
	}
}

// Validation judges the match (webhook + areas), so it runs at buffer time.
func TestBufferQuestMatches_AppliesValidation(t *testing.T) {
	v := &recordingValidator{deny: map[string]bool{"u2": true}}
	ps := &ProcessorService{summaryBuffer: tracker.NewSummaryBuffer(""), validator: v, ctx: context.Background()}
	users := []webhook.MatchedUser{{ID: "u1", Type: "discord:user"}, {ID: "u2", Type: "discord:user"}}
	areas := []webhook.MatchedArea{{Name: "Town"}}
	ps.bufferQuestMatches(users, summaryTestQuest, []matching.QuestRewardData{{Type: 3, Amount: 200}}, []byte(`{}`), areas)

	if got := ps.summaryBuffer.List("u1", AlertTypeQuest); len(got) != 1 {
		t.Errorf("u1 allowed: buffered %d, want 1", len(got))
	}
	if got := ps.summaryBuffer.List("u2", AlertTypeQuest); len(got) != 0 {
		t.Errorf("u2 denied: buffered %d, want 0", len(got))
	}
	if v.callCount() != 2 || len(v.calls[0].Areas) != 1 || v.calls[0].Areas[0] != "Town" {
		t.Errorf("validator calls = %+v, want 2 calls carrying matched areas", v.calls)
	}
}

// The rule UID and matched areas are captured for the fire-time mute check.
func TestBufferQuestMatches_CapturesRuleUIDAndAreas(t *testing.T) {
	ps := &ProcessorService{summaryBuffer: tracker.NewSummaryBuffer("")}
	users := []webhook.MatchedUser{{ID: "u1", Type: "discord:user", RuleUID: 42}}
	ps.bufferQuestMatches(users, summaryTestQuest, []matching.QuestRewardData{{Type: 3, Amount: 200}}, []byte(`{}`),
		[]webhook.MatchedArea{{Name: "Town"}, {Name: "Park"}})

	got := ps.summaryBuffer.List("u1", AlertTypeQuest)
	if len(got) != 1 {
		t.Fatalf("buffered %d, want 1", len(got))
	}
	if len(got[0].RuleUIDs) != 1 || got[0].RuleUIDs[0] != 42 {
		t.Errorf("RuleUIDs = %v, want [42]", got[0].RuleUIDs)
	}
	if len(got[0].Areas) != 2 || got[0].Areas[0] != "Town" || got[0].Areas[1] != "Park" {
		t.Errorf("Areas = %v, want [Town Park]", got[0].Areas)
	}
}

// A mute active at fire time suppresses the muted user's matching entries
// only — other entries and other users are untouched.
func TestUnmutedSummaryRows(t *testing.T) {
	ms := mute.NewStore()
	exp := time.Now().Add(time.Hour).Unix()
	ms.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopePokestop, ScopeValue: "stop-A", ExpiresAt: exp})
	ms.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeArea, ScopeValue: "Park", ExpiresAt: exp})
	ms.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeTracking, RuleTable: "quest", ScopeValue: "7", ExpiresAt: exp})
	ps := &ProcessorService{muteStore: ms}

	rows := func() []tracker.BufferedQuest {
		return []tracker.BufferedQuest{
			{PokestopID: "stop-A"},                          // pokestop mute
			{PokestopID: "stop-B", Areas: []string{"Park"}}, // area mute
			{PokestopID: "stop-C", RuleUIDs: []int64{7}},    // tracking mute
			{PokestopID: "stop-D", RuleUIDs: []int64{7, 8}}, // only one of its rules muted
			{PokestopID: "stop-E", Areas: []string{"Town"}, RuleUIDs: []int64{9}},
		}
	}
	stops := func(rs []tracker.BufferedQuest) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.PokestopID)
		}
		return out
	}

	got := stops(ps.unmutedSummaryRows("u1", "discord:user", rows()))
	if len(got) != 2 || got[0] != "stop-D" || got[1] != "stop-E" {
		t.Errorf("u1 kept %v, want [stop-D stop-E]", got)
	}
	if got := ps.unmutedSummaryRows("u2", "discord:user", rows()); len(got) != 5 {
		t.Errorf("u2 (no mutes) kept %v, want all 5", stops(got))
	}
}

// fireTimeFixture buffers one quest for a Discord user and returns a service
// whose validator records whether dispatch reached the delivery stages.
func fireTimeFixture(t *testing.T, cfg *config.Config) (*ProcessorService, *recordingValidator) {
	t.Helper()
	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "Ash", Enabled: true})
	v := &recordingValidator{deny: map[string]bool{"u1": true}} // deny: stop before rendering
	ps := &ProcessorService{
		summaryBuffer: tracker.NewSummaryBuffer(""),
		humans:        humans,
		cfg:           cfg,
		validator:     v,
		ctx:           context.Background(),
		enricher:      &enrichment.Enricher{},
	}
	ps.summaryBuffer.Append("u1", AlertTypeQuest, tracker.BufferedQuest{
		RewardType: 3, Reward: 200, PokestopID: "stop-A",
		Payload:   []byte(`{"pokestop_id":"stop-A","latitude":51,"longitude":0.5,"rewards":[{"type":3,"info":{"amount":200}}]}`),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	return ps, v
}

// Control: with the platform enabled, dispatch reaches the validation stage.
func TestDispatchQuestSummary_EnabledPlatformReachesDelivery(t *testing.T) {
	ps, v := fireTimeFixture(t, platformCfg(true, true))
	ps.DispatchQuestSummary("u1", AlertTypeQuest)
	if v.callCount() != 1 {
		t.Fatalf("validator calls = %d, want 1 (dispatch should reach delivery stages)", v.callCount())
	}
}

// #311: entries buffered before the platform was disabled are not sent.
func TestDispatchQuestSummary_DisabledPlatformDropsBuffered(t *testing.T) {
	ps, v := fireTimeFixture(t, platformCfg(false, true))
	ps.DispatchQuestSummary("u1", AlertTypeQuest)
	if v.callCount() != 0 {
		t.Errorf("validator calls = %d, want 0 (platform disabled must stop dispatch)", v.callCount())
	}
	if got := ps.summaryBuffer.List("u1", AlertTypeQuest); len(got) != 0 {
		t.Errorf("buffer not cleared: %d entries left", len(got))
	}
}

// #311: a mute active at fire time suppresses the digest.
func TestDispatchQuestSummary_MutedEntriesNotSent(t *testing.T) {
	ps, v := fireTimeFixture(t, platformCfg(true, true))
	ps.muteStore = mute.NewStore()
	ps.muteStore.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopePokestop, ScopeValue: "stop-A", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	ps.DispatchQuestSummary("u1", AlertTypeQuest)
	if v.callCount() != 0 {
		t.Errorf("validator calls = %d, want 0 (all entries muted)", v.callCount())
	}
}

// A raid-table mute with uid N must not drop a quest row whose rule uid is N.
func TestUnmutedSummaryRows_OtherTableMuteIgnored(t *testing.T) {
	ms := mute.NewStore()
	exp := time.Now().Add(time.Hour).Unix()
	ms.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeTracking, RuleTable: "raid", ScopeValue: "7", ExpiresAt: exp})
	ps := &ProcessorService{muteStore: ms}
	got := ps.unmutedSummaryRows("u1", "discord:user", []tracker.BufferedQuest{{PokestopID: "s", RuleUIDs: []int64{7}}})
	if len(got) != 1 {
		t.Errorf("raid mute dropped a quest row: %v", got)
	}
}

// A typed tracking mute must drop a MatchedUser carrying the same
// (RuleTable, RuleUID) through filterMuted, and only that table.
func TestFilterMutedTypedTrackingMute(t *testing.T) {
	ms := mute.NewStore()
	ms.Add(mute.Entry{HumanID: "u1", ScopeType: mute.ScopeTracking, RuleTable: "raid", ScopeValue: "7", ExpiresAt: time.Now().Unix() + 600})
	ps := &ProcessorService{muteStore: ms}

	got := ps.filterMuted([]webhook.MatchedUser{{ID: "u1", Type: "discord:user", RuleUID: 7, RuleTable: "raid"}}, nil, mute.Event{})
	if len(got) != 0 {
		t.Fatalf("raid/7 should be dropped, got %+v", got)
	}
	got = ps.filterMuted([]webhook.MatchedUser{{ID: "u1", Type: "discord:user", RuleUID: 7, RuleTable: "pokemon"}}, nil, mute.Event{})
	if len(got) != 1 {
		t.Fatalf("pokemon/7 must not be dropped by a raid mute, got %+v", got)
	}
}
