package logbuffer

import (
	"fmt"

	log "github.com/sirupsen/logrus"
)

// Hook is a logrus.Hook that feeds WARN, ERROR, FATAL, and PANIC
// entries into a Buffer.
type Hook struct {
	buf *Buffer
}

// NewHook returns a Hook backed by buf.
func NewHook(buf *Buffer) *Hook {
	return &Hook{buf: buf}
}

// Levels implements logrus.Hook.
func (h *Hook) Levels() []log.Level {
	return []log.Level{
		log.WarnLevel,
		log.ErrorLevel,
		log.FatalLevel,
		log.PanicLevel,
	}
}

// Fire implements logrus.Hook. It is called synchronously by logrus on
// every log event at the listed levels. It must never return an error
// that bubbles up to the caller — failures are silently swallowed to
// avoid circular logging.
func (h *Hook) Fire(entry *log.Entry) error {
	level := mapLevel(entry.Level)
	source := ""
	if entry.HasCaller() && entry.Caller != nil {
		source = fmt.Sprintf("%s:%d", entry.Caller.File, entry.Caller.Line)
	}
	h.buf.Capture(level, entry.Message, source)
	return nil
}

func mapLevel(l log.Level) string {
	switch l {
	case log.WarnLevel:
		return "WARN"
	case log.ErrorLevel:
		return "ERROR"
	case log.FatalLevel, log.PanicLevel:
		return "ERROR" // treat fatal/panic as ERROR in the buffer
	default:
		return "WARN"
	}
}
