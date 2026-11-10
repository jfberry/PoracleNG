package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An unknown move must be rejected rather than becoming "any move" (#277).
func TestMaxbattle_RejectsUnknownMove(t *testing.T) {
	ctx := maxbattleCtx(t)
	replies := runMaxbattle(t, ctx, "level3 move:thundrbolt")
	require.NotEmpty(t, replies)
	assert.Equal(t, "🙅", replies[0].React, "reply: %s", replies[0].Text)
	rows, _ := ctx.Tracking.Maxbattles.SelectByIDProfile("user1", 1)
	assert.Empty(t, rows)
}
