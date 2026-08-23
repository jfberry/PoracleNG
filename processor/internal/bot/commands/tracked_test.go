package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fillAllTrackingStores populates every store the tracked command queries;
// a nil store interface would panic on SelectByIDProfile.
func fillAllTrackingStores(ts *store.TrackingStores) {
	ts.Monsters = store.NewMockTrackingStore[db.MonsterTrackingAPI](store.MonsterGetUID, store.MonsterSetUID)
	ts.Raids = store.NewMockTrackingStore[db.RaidTrackingAPI](store.RaidGetUID, store.RaidSetUID)
	ts.Eggs = store.NewMockTrackingStore[db.EggTrackingAPI](store.EggGetUID, store.EggSetUID)
	ts.Quests = store.NewMockTrackingStore[db.QuestTrackingAPI](store.QuestGetUID, store.QuestSetUID)
	ts.Invasions = store.NewMockTrackingStore[db.InvasionTrackingAPI](store.InvasionGetUID, store.InvasionSetUID)
	ts.Gyms = store.NewMockTrackingStore[db.GymTrackingAPI](store.GymGetUID, store.GymSetUID)
	ts.Nests = store.NewMockTrackingStore[db.NestTrackingAPI](store.NestGetUID, store.NestSetUID)
	ts.Forts = store.NewMockTrackingStore[db.FortTrackingAPI](store.FortGetUID, store.FortSetUID)
	ts.Maxbattles = store.NewMockTrackingStore[db.MaxbattleTrackingAPI](store.MaxbattleGetUID, store.MaxbattleSetUID)
}

// A lure rule created via !lure must show up in the !tracked listing.
func TestTracked_ShowsLures(t *testing.T) {
	ctx := lureCtx(t)
	fillAllTrackingStores(ctx.Tracking)

	replies := runLure(t, ctx, "everything clean")
	require.NotEmpty(t, replies)
	require.Equal(t, "✅", replies[0].React, "lure command failed: %s", replies[0].Text)

	tracked := &TrackedCommand{}
	trackedReplies := tracked.Run(ctx, nil)
	require.NotEmpty(t, trackedReplies)
	var all strings.Builder
	for _, r := range trackedReplies {
		all.WriteString(r.Text)
		all.WriteByte('\n')
	}
	assert.Contains(t, all.String(), "Lures", "lure section missing from !tracked")
	assert.NotContains(t, all.String(), "not tracking any lures")
}
