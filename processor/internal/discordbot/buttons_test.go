package discordbot

import (
	"context"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// TestLookupSnapshotForClick_KeyMatchesDeliveryTarget pins the click
// handler to the same key format the render path writes (bare user/
// channel ID, NOT the typed "discord:user:..." prefix). Regression
// test for the "This alert has expired" bug — a mismatch here makes
// every click report expired because the lookup misses.
func TestLookupSnapshotForClick_KeyMatchesDeliveryTarget(t *testing.T) {
	t.Run("DM lookup uses bare user id", func(t *testing.T) {
		store := newFakeStore()
		// Render path keys snapshots as <target>:<messageID> with target
		// = bare user id (webhook.DeliveryJob.Target).
		const userID = "344179542874914817"
		const msgID = "1234567890"
		_ = store.Put(snapshots.MakeKey(userID, msgID), &snapshots.Snapshot{
			MessageID:  msgID,
			Target:     userID,
			TargetType: "dm",
		})

		ic := &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				ChannelID: "dm_channel_id_different_from_user_id",
				User:      &discordgo.User{ID: userID},
				Message:   &discordgo.Message{ID: msgID},
			},
		}
		snap, _, err := lookupSnapshotForClick(store, ic)
		if err != nil {
			t.Fatalf("lookupSnapshotForClick: %v", err)
		}
		if snap == nil || snap.MessageID != msgID {
			t.Errorf("got %+v, want snapshot for messageID=%s", snap, msgID)
		}
	})

	t.Run("Channel lookup uses bare channel id", func(t *testing.T) {
		store := newFakeStore()
		const channelID = "987654321"
		const userID = "11111"
		const msgID = "msg_in_channel"
		_ = store.Put(snapshots.MakeKey(channelID, msgID), &snapshots.Snapshot{
			MessageID:  msgID,
			Target:     channelID,
			TargetType: "channel",
		})

		ic := &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				ChannelID: channelID,
				Member:    &discordgo.Member{User: &discordgo.User{ID: userID}},
				Message:   &discordgo.Message{ID: msgID},
			},
		}
		snap, _, err := lookupSnapshotForClick(store, ic)
		if err != nil {
			t.Fatalf("lookupSnapshotForClick: %v", err)
		}
		if snap == nil || snap.MessageID != msgID {
			t.Errorf("got %+v, want snapshot for messageID=%s", snap, msgID)
		}
	})
}

// fakeStore is an in-memory snapshots.Store stand-in for tests. Avoids
// dragging pogreb into the unit test path.
type fakeStore struct {
	data map[string]*snapshots.Snapshot
}

func newFakeStore() *fakeStore                              { return &fakeStore{data: map[string]*snapshots.Snapshot{}} }
func (s *fakeStore) Put(key string, v *snapshots.Snapshot) error { s.data[key] = v; return nil }
func (s *fakeStore) Write(_ context.Context, v *snapshots.Snapshot) error {
	s.data[v.Key()] = v
	return nil
}
func (s *fakeStore) Read(_ context.Context, key string) (*snapshots.Snapshot, error) {
	if v, ok := s.data[key]; ok {
		return v, nil
	}
	return nil, snapshots.ErrNotFound
}
func (s *fakeStore) Delete(_ context.Context, key string) error          { delete(s.data, key); return nil }
func (s *fakeStore) Sweep(_ context.Context, _ int64) (int, error)       { return 0, nil }
func (s *fakeStore) Close() error                                        { return nil }
