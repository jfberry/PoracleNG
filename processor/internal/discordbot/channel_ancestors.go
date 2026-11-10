package discordbot

import (
	"github.com/bwmarrin/discordgo"
)

// channelLookup is the slice of *discordgo.Session channelAncestors needs:
// the state cache first, the REST API as a fallback.
type channelLookup interface {
	stateChannel(id string) (*discordgo.Channel, error)
	apiChannel(id string) (*discordgo.Channel, error)
}

type sessionChannels struct{ s *discordgo.Session }

func (c sessionChannels) stateChannel(id string) (*discordgo.Channel, error) {
	if c.s.State == nil {
		return nil, discordgo.ErrStateNotFound
	}
	return c.s.State.Channel(id)
}

func (c sessionChannels) apiChannel(id string) (*discordgo.Channel, error) {
	return c.s.Channel(id)
}

// channelAncestors returns the IDs above a guild channel that delegated
// administration can be keyed on: a text channel's category, or a thread's
// parent channel and that channel's category. PoracleJS matched the
// category (channel.parentId); NG passed nothing, so category-keyed
// delegation never granted access (#286).
func channelAncestors(s *discordgo.Session, channelID string) []string {
	if s == nil {
		return nil
	}
	return ancestorsVia(sessionChannels{s}, channelID)
}

func ancestorsVia(l channelLookup, channelID string) []string {
	var out []string
	id := channelID
	// A thread's parent is a channel whose parent is a category: two hops
	// cover every case.
	for range 2 {
		ch := lookupChannel(l, id)
		if ch == nil || ch.ParentID == "" {
			break
		}
		out = append(out, ch.ParentID)
		id = ch.ParentID
	}
	return out
}

func lookupChannel(l channelLookup, id string) *discordgo.Channel {
	if ch, err := l.stateChannel(id); err == nil && ch != nil {
		return ch
	}
	if ch, err := l.apiChannel(id); err == nil {
		return ch
	}
	return nil
}
