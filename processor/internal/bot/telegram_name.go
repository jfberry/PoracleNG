package bot

import "strings"

// DisplayName composes a human-readable label for a Telegram chat or
// user. Works for any *models.ChatFullInfo or *models.User by passing
// the relevant subset of fields:
//
//   - Private chats / users: pass FirstName, LastName, Username, "".
//   - Groups / channels: pass "", "", Username, Title.
//
// Output shape:
//
//   - "First Last [@username]" for a user with both names and a handle.
//   - "First Last" for a user with no public handle.
//   - "GroupTitle [@grouphandle]" for a public group/channel.
//   - "GroupTitle" for a private group with no handle.
//   - "[@username]" if no name fields are set but a handle is.
//   - "" if nothing identifies the chat.
//
// Non-ASCII characters are stripped to avoid MySQL utf8 charset
// issues with emoji and other multi-byte glyphs in stored names.
func DisplayName(firstName, lastName, username, title string) string {
	name := strings.TrimSpace(firstName + " " + lastName)
	if name == "" {
		name = title
	}
	if username != "" {
		if name != "" {
			name += " [" + username + "]"
		} else {
			name = "[" + username + "]"
		}
	}
	return stripNonASCII(name)
}

// stripNonASCII removes characters above the 0xFF Latin-1 range.
// Matches the legacy alerter's emojiStrip(/[^\x00-\xFF]/g) used to
// prevent MySQL utf8 charset issues with emoji and other multi-byte
// characters in stored names.
func stripNonASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r <= 0xFF {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
