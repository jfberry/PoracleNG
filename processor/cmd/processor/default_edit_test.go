package main

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestApplyDefaultEdit(t *testing.T) {
	tracking := config.TrackingConfig{DefaultEdit: []string{"raid"}}

	matched := []webhook.MatchedUser{{ID: "a", Clean: 0}, {ID: "b", Clean: 1}, {ID: "c", Clean: 4}}
	applyDefaultEdit(matched, tracking, "raid")
	for _, u := range matched {
		if !db.IsEdit(u.Clean) {
			t.Errorf("user %s: edit bit not set (clean=%d)", u.ID, u.Clean)
		}
	}
	if !db.IsClean(matched[1].Clean) || !db.IsSummary(matched[2].Clean) {
		t.Errorf("other clean bits not preserved: %+v", matched)
	}

	eggs := []webhook.MatchedUser{{ID: "a", Clean: 1}}
	applyDefaultEdit(eggs, tracking, "egg")
	if eggs[0].Clean != 1 {
		t.Errorf("egg not in default_edit: clean = %d, want 1", eggs[0].Clean)
	}
}

// A raid rule without the edit bit, defaulted to edit, takes the full
// template like an explicit edit rule rather than the rsvpChanges one.
func TestPartitionRaidUsers_DefaultEdit(t *testing.T) {
	ts := newRsvpTemplateStore(t, "discord", "")
	matched := []webhook.MatchedUser{{ID: "u1", Type: "discord:user", Clean: 0}}
	applyDefaultEdit(matched, config.TrackingConfig{DefaultEdit: []string{"raid"}}, "raid")

	full, rsvp := partitionRaidUsers(matched, testReplyKey, "raid", hasPriorMessage, ts, "")
	if len(full) != 1 || len(rsvp) != 0 {
		t.Errorf("got full=%d rsvp=%d, want full=1 rsvp=0", len(full), len(rsvp))
	}
}
