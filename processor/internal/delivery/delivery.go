package delivery

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// TTH represents time-to-hide (how long until the message should be deleted).
type TTH struct {
	Days    int `json:"days"`
	Hours   int `json:"hours"`
	Minutes int `json:"minutes"`
	Seconds int `json:"seconds"`
}

func (t TTH) Duration() time.Duration {
	return time.Duration(t.Days)*24*time.Hour +
		time.Duration(t.Hours)*time.Hour +
		time.Duration(t.Minutes)*time.Minute +
		time.Duration(t.Seconds)*time.Second
}

// Job is a platform-agnostic delivery job.
type Job struct {
	Target       string          `json:"target"`       // user/channel/thread/webhook ID or URL
	Type         string          `json:"type"`         // "discord:user", "discord:channel", "discord:thread", "webhook",
	                                                   // "telegram:user", "telegram:group", "telegram:channel"
	Message      json.RawMessage `json:"message"`      // pre-rendered message JSON
	TTH          TTH             `json:"tth"`
	Clean        bool            `json:"clean"`        // track for deletion on TTH expiry
	EditKey      string          `json:"editKey"`      // non-empty = track for future edits
	Name         string          `json:"name"`         // human-readable destination name
	LogReference string          `json:"logReference"` // encounter/gym ID for tracing
	Lat          float64         `json:"lat"`
	Lon          float64         `json:"lon"`
}

// SentMessage is returned after successful delivery.
type SentMessage struct {
	ID string // opaque platform-specific ID (sender knows how to parse for delete/edit)
}

// Sender delivers messages to a specific platform.
type Sender interface {
	Send(ctx context.Context, job *Job) (*SentMessage, error)
	Delete(ctx context.Context, sentID string) error
	Edit(ctx context.Context, sentID string, message json.RawMessage) error
	Platform() string // "discord" or "telegram"
	// WaitForRateLimit blocks until the target is not rate-limited.
	// Called BEFORE acquiring the platform semaphore so that rate-limited
	// goroutines don't hold concurrency slots.
	WaitForRateLimit(target string)
}

// PermanentError wraps an error that should not be retried (e.g. user blocked bot).
type PermanentError struct {
	Err    error
	Reason string // human-readable reason
}

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// PlatformFromType extracts "discord" or "telegram" from a job type string.
func PlatformFromType(typ string) string {
	if typ == "webhook" {
		return "discord"
	}
	parts := strings.SplitN(typ, ":", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
