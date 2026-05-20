// Package snapshots is an opt-in on-disk store of fully-resolved enrichment
// views keyed by delivered message ID. It is the substrate for any feature
// that needs to re-render an alert in a different shape, or take action
// against it, after the message has already been sent — interactive buttons,
// late edits, action-triggered mutations, and future drill-down UIs.
//
// Design rationale, alternatives considered, and the per-field reasoning live
// in GitHub issue #108 and docs/buttons-and-snapshots/DESIGN.md. This file
// keeps the surface intentionally narrow: a Snapshot value type, a Store
// interface for persistence, and a few error sentinels.
package snapshots

import (
	"context"
	"errors"
)

// SchemaVersion is the on-disk schema version embedded in every serialised
// Snapshot. Bumped when the persisted shape changes. A snapshot with a
// version higher than this binary supports is treated as a read miss (the
// consumer responds with "alert has expired") so older binaries reading
// newer snapshots fail gracefully rather than misinterpreting them.
const SchemaVersion = 1

// Snapshot is the per-delivery record stored by the snapshot store. One
// snapshot per delivered message; edits overwrite the previous record under
// the same key.
//
// Field semantics are documented in issue #108. The two-template-field
// pattern (TemplateRequested + TemplateSelected) captures both the tracking
// rule's intent and the chain's resolved outcome, so consumers can either
// re-render the exact entry the user saw or re-resolve against intent if
// the entry has since been removed.
type Snapshot struct {
	// Schema version of this record. Set to SchemaVersion on Write; checked
	// against SchemaVersion on Read. Mismatches surface as a read miss.
	Version int `json:"version"`

	// Identity.
	MessageID  string `json:"messageId"`  // platform-assigned message id (Discord snowflake / Telegram message id)
	Target     string `json:"target"`     // human_id of the destination (DM user, channel, webhook)
	TargetType string `json:"targetType"` // "dm" / "channel" / "webhook"
	CreatedAt  int64  `json:"createdAt"`  // unix seconds
	ExpiresAt  int64  `json:"expiresAt"`  // unix seconds; TTH-derived

	// Alert metadata — the "out-of-band" fields actions need that aren't in
	// the rendered view.
	AlertType    string `json:"alertType"`    // source webhook type: "monster" / "raid" / "incident" / ...
	TemplateType string `json:"templateType"` // resolved template type: "raid" / "rsvpChanges" / "incident" / ...

	// Both forms of template identity. When they differ, the selection chain
	// fell back from the requested id. Consumers (e.g. the redeliver action)
	// choose which to use — TemplateSelected for exact re-render, or
	// TemplateRequested to re-run the selection chain against original intent.
	TemplateRequested string `json:"templateRequested"`
	TemplateSelected  string `json:"templateSelected"`

	Language string `json:"language"`
	Platform string `json:"platform"` // "discord" / "telegram"

	// Every tracking rule UID that fired for this delivery. A pokemon delivery
	// can match multiple rules at once (basic IV + great PVP + ultra PVP); the
	// full list lets buttons offer per-rule mute / unsubscribe granularity.
	TrackingUIDs []int64 `json:"trackingUids,omitempty"`

	// Geographic context for area-scoped actions ("mute this area for an hour").
	MatchedAreas []string `json:"matchedAreas,omitempty"`

	// The fully-resolved LayeredView this user actually saw. Re-rendering a
	// template against this view reproduces the original message (subject to
	// the operator changing the template since).
	View map[string]any `json:"view,omitempty"`
}

// Key returns the canonical pogreb key for a snapshot: target:messageID.
// Same convention as MessageTracker. Both fields must be non-empty.
func (s *Snapshot) Key() string {
	return s.Target + ":" + s.MessageID
}

// MakeKey returns the canonical store key for a (target, messageID) pair
// without requiring a fully-populated Snapshot value. Use for reads and
// deletes where only the identity is known.
func MakeKey(target, messageID string) string {
	return target + ":" + messageID
}

// Store is the abstract persistence interface for snapshots. The pogreb
// implementation lives in store.go; tests can substitute an in-memory fake.
//
// All methods must be safe for concurrent use from multiple sender
// goroutines (the snapshot write happens on the delivery hot path, one call
// per delivered message).
type Store interface {
	// Write persists s under s.Key(). Overwrites any existing entry for the
	// same key — edits to a message replace the previous snapshot. Returns
	// an error on persistence failure; callers log and continue (snapshot
	// writes must never block delivery).
	Write(ctx context.Context, s *Snapshot) error

	// Read returns the snapshot for the given key, or ErrNotFound if no
	// snapshot exists. Snapshots with a Version newer than SchemaVersion
	// surface as ErrNotFound to consumers (the on-disk record is left intact
	// so a newer binary can still read it).
	Read(ctx context.Context, key string) (*Snapshot, error)

	// Delete removes the snapshot under key. A missing key is not an error.
	Delete(ctx context.Context, key string) error

	// Sweep walks the store and deletes any snapshot whose ExpiresAt is older
	// than maxAge in the past (i.e. ExpiresAt + maxAge < now). Returns the
	// number of records deleted. This is the safety net for snapshots that
	// outlived their natural delete callback (clean-deletion via
	// MessageTracker) — restarts, missed callbacks, etc.
	Sweep(ctx context.Context, now int64) (deleted int, err error)

	// Close releases the underlying storage. After Close, all other methods
	// return ErrClosed.
	Close() error
}

// ErrNotFound is returned by Read when no snapshot exists for the requested
// key, or when the stored snapshot's Version is newer than SchemaVersion.
var ErrNotFound = errors.New("snapshots: not found")

// ErrClosed is returned by Store methods after Close.
var ErrClosed = errors.New("snapshots: store closed")
