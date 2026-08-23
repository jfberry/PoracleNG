package logging

import (
	"fmt"
	"io"
	stdlog "log"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var lumberjackLogger *lumberjack.Logger

// Config holds logging configuration.
type Config struct {
	Level              string `toml:"level"`
	FileLoggingEnabled bool   `toml:"file_logging_enabled"`
	Filename           string `toml:"filename"`
	MaxSize            int    `toml:"max_size"`
	MaxAge             int    `toml:"max_age"`
	MaxBackups         int    `toml:"max_backups"`
	Compress           bool   `toml:"compress"`
}

// resolveLevel maps a configured level string to a logrus level. It accepts
// this project's legacy Winston-style names (inherited from PoracleJS) as
// aliases: logrus has no level between Debug and Info, so "verbose" collapses
// to Info and "silly" to Trace. Any name logrus already understands passes
// through. Empty or unrecognised input falls back to Info.
func resolveLevel(s string) log.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "verbose":
		return log.InfoLevel
	case "silly":
		return log.TraceLevel
	}
	if lvl, err := log.ParseLevel(s); err == nil {
		return lvl
	}
	return log.InfoLevel
}

// Setup initialises the logger matching Golbat's logging pattern.
func Setup(cfg Config) {
	logLevel := resolveLevel(cfg.Level)

	lumberjackLogger = &lumberjack.Logger{
		Filename:   cfg.Filename,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   cfg.Compress,
	}

	var output io.Writer
	if cfg.FileLoggingEnabled {
		output = io.MultiWriter(os.Stdout, lumberjackLogger)
	} else {
		output = os.Stdout
	}

	logFormatter := &PlainFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		LevelDesc:       []string{"PANC", "FATL", "ERRO", "WARN", "INFO", "DEBG"},
	}

	log.SetFormatter(logFormatter)
	log.SetLevel(logLevel)
	log.SetOutput(output)

	// Redirect Go's standard log package (used by third-party libraries like
	// go-telegram-bot-api) through logrus so their output goes to our log file
	// and uses our formatter.
	stdlog.SetOutput(log.StandardLogger().Writer())
	stdlog.SetFlags(0) // logrus handles timestamps
}

// RotateLogs triggers a log file rotation.
func RotateLogs() {
	if lumberjackLogger != nil {
		_ = lumberjackLogger.Rotate()
	}
}

// PlainFormatter matches Golbat's log format: LEVL 2006-01-02 15:04:05 message
type PlainFormatter struct {
	TimestampFormat string
	LevelDesc       []string
}

// Format implements logrus.Formatter.
func (f *PlainFormatter) Format(entry *log.Entry) ([]byte, error) {
	timestamp := entry.Time.Format(f.TimestampFormat)
	if ref, ok := entry.Data["ref"]; ok {
		return fmt.Appendf(nil, "%s %s [%v] %s\n", f.LevelDesc[entry.Level], timestamp, ref, entry.Message), nil
	}
	return fmt.Appendf(nil, "%s %s %s\n", f.LevelDesc[entry.Level], timestamp, entry.Message), nil
}
