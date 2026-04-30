package discordbot

import (
	"strings"
)

// Discord limits custom_id to 100 chars; this prefix scheme leaves
// ~80 chars of headroom which is more than enough for two snowflake IDs.
const threadJoinIDPrefix = "poracle:thread:"
const threadJoinIDSuffix = ":join"

// encodeThreadJoinID builds the button custom_id for a "join thread" button.
// The encoded form is stateless: the click handler can act on it directly
// after a bot restart with no warm state.
func encodeThreadJoinID(masterChannelID, threadID string) string {
	return threadJoinIDPrefix + masterChannelID + ":" + threadID + threadJoinIDSuffix
}

// decodeThreadJoinID reverses encodeThreadJoinID. Returns ok=false for any
// input that doesn't match the expected shape — callers must reject those
// rather than treating empty IDs as "all threads".
func decodeThreadJoinID(id string) (masterID, threadID string, ok bool) {
	if !strings.HasPrefix(id, threadJoinIDPrefix) || !strings.HasSuffix(id, threadJoinIDSuffix) {
		return "", "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(id, threadJoinIDPrefix), threadJoinIDSuffix)
	parts := strings.Split(body, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
