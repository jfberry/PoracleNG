package discordbot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A failed template command (🙅 react) must surface through the reporter —
// the bulk path has no Discord channel, and the interactive path's
// synthetic reply message can't carry reacts, so without this the failure
// is completely invisible to the operator.
func TestReportCommandFailures(t *testing.T) {
	rep := &collectingReporter{}
	reportCommandFailures(rep, "lure normal glacial mossy", []bot.Reply{
		{React: "🙅", Text: "Unrecognized: glacial, mossy"},
	})
	require.Len(t, rep.warns, 1)
	assert.Contains(t, rep.warns[0], "lure normal glacial mossy")
	assert.Contains(t, rep.warns[0], "Unrecognized: glacial, mossy")
}

func TestReportCommandFailures_SuccessStaysQuiet(t *testing.T) {
	rep := &collectingReporter{}
	reportCommandFailures(rep, "lure everything clean", []bot.Reply{
		{React: "✅", Text: "Tracking: any lure"},
		{React: "👌"},
	})
	assert.Empty(t, rep.warns)
}

// Some commands fail with a bare react and no text (e.g. store errors);
// the reporter line still has to appear.
func TestReportCommandFailures_NoText(t *testing.T) {
	rep := &collectingReporter{}
	reportCommandFailures(rep, "lure everything", []bot.Reply{{React: "🙅"}})
	require.Len(t, rep.warns, 1)
	assert.Contains(t, rep.warns[0], "lure everything")
}

// The escaped spelling "\U0001f645" is the same rune as "🙅" — both must
// be caught.
func TestReportCommandFailures_EscapedReact(t *testing.T) {
	rep := &collectingReporter{}
	reportCommandFailures(rep, "lure everything", []bot.Reply{
		{React: "\U0001f645", Text: "This alert type is disabled"},
	})
	require.Len(t, rep.warns, 1)
	assert.Contains(t, rep.warns[0], "disabled")
}
