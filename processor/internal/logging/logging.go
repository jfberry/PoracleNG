package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// Setup initialises the logger matching Golbat's logging pattern.
func Setup(cfg Config) {
	logLevel, err := log.ParseLevel(cfg.Level)
	if err != nil {
		logLevel = log.InfoLevel
	}

	filename := cfg.Filename
	if filename == "" {
		filename = filepath.ToSlash("logs/processor.log")
	}
	maxSize := cfg.MaxSize
	if maxSize == 0 {
		maxSize = 50
	}
	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = 30
	}
	maxBackups := cfg.MaxBackups
	if maxBackups == 0 {
		maxBackups = 5
	}

	lumberjackLogger = &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
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
	return []byte(fmt.Sprintf("%s %s %s\n", f.LevelDesc[entry.Level], timestamp, entry.Message)), nil
}
