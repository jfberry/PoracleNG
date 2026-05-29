// Package logref provides small wrappers around logrus that thread a
// per-event correlation reference (encounter_id, gym_id, station_id,
// pokestop_id, etc.) through the logger.
//
// The reference flows into a logrus structured field named "ref" so the
// PlainFormatter in internal/logging prints it as "[ref] message". This
// matches the convention established by webhook handlers that do
// `l := log.WithField("ref", evt.ID)` at the top of each handler.
//
// Use these helpers from code paths where:
//
//   - the per-event reference is available only as a string (e.g. from
//     a job, render request, or struct field) and you don't want to
//     hand-roll WithField calls everywhere, OR
//   - you want a one-shot Errorf/Warnf/etc. without keeping a
//     FieldLogger around.
//
// Code paths that already build a `l := log.WithField("ref", ...)` at
// the top of a handler should continue to use that pattern — it's both
// cheaper (single allocation) and idiomatic for that style.
package logref

import (
	log "github.com/sirupsen/logrus"
)

// Errorf logs at error level with "ref" set to the supplied reference.
// PlainFormatter renders this as "ERRO timestamp [ref] message".
func Errorf(ref, format string, args ...any) {
	log.WithField("ref", ref).Errorf(format, args...)
}

// Warnf logs at warn level with "ref" set to the supplied reference.
func Warnf(ref, format string, args ...any) {
	log.WithField("ref", ref).Warnf(format, args...)
}

// Infof logs at info level with "ref" set to the supplied reference.
func Infof(ref, format string, args ...any) {
	log.WithField("ref", ref).Infof(format, args...)
}

// Debugf logs at debug level with "ref" set to the supplied reference.
func Debugf(ref, format string, args ...any) {
	log.WithField("ref", ref).Debugf(format, args...)
}

// With returns a logrus.FieldLogger pre-bound to "ref". Use when you
// want to make multiple log calls under the same reference without
// re-paying the WithField cost.
func With(ref string) *log.Entry {
	return log.WithField("ref", ref)
}

// WithOptional returns a logger bound to "ref", or the unprefixed standard
// logger when ref is empty. Use on shared code paths that sometimes run
// with a per-event reference (webhook enrichment) and sometimes without
// (synchronous tile-API callers), so the latter don't emit a stray
// "[] message" prefix.
func WithOptional(ref string) log.FieldLogger {
	if ref == "" {
		return log.StandardLogger()
	}
	return log.WithField("ref", ref)
}
