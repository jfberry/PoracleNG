package bot

import "strings"

// SplitMessage splits text into chunks that fit within maxLen, splitting at
// newlines. If a single line exceeds maxLen, it is broken into maxLen-sized
// chunks to avoid dropping content.
func SplitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var messages []string
	lines := strings.Split(text, "\n")
	var current strings.Builder

	for _, line := range lines {
		// If a single line exceeds maxLen, break it into chunks
		for len(line) > maxLen {
			if current.Len() > 0 {
				messages = append(messages, current.String())
				current.Reset()
			}
			messages = append(messages, line[:maxLen])
			line = line[maxLen:]
		}

		if current.Len()+len(line)+1 > maxLen && current.Len() > 0 {
			messages = append(messages, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		messages = append(messages, current.String())
	}

	return messages
}
