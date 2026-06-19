package logging

import (
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// initializeRollingFileLogger configures a lumberjack logger for file rotation
// using the configured size/age/backup limits. The filename is derived from
// the executable name plus .log, written under RelLogFileDir relative to WorkingDir.
func (s *Service) initializeRollingFileLogger(exeName string) *lumberjack.Logger {
	if exeName == emptyString {
		exeName = "app"
	}

	path := filepath.Join(s.WorkingDir, s.LoggingConfig.RelLogFileDir, exeName+".log")

	return &lumberjack.Logger{
		Filename:   path,
		MaxBackups: s.LoggingConfig.LogFileMaxBackups,
		MaxAge:     s.LoggingConfig.LogFileMaxAgeDays,
		MaxSize:    s.LoggingConfig.LogFileMaxSizeMB,
		Compress:   s.LoggingConfig.LogFileCompress,
	}
}

// probeLogFileWritable proves the log file path can actually be opened for
// writing, using the same final filename + flags + owner-only mode lumberjack
// will use (it opens lazily on first Write, so this surfaces a bad dir/perms at
// Initialize time — review 2026-06-19 M1). It creates the file if absent (as
// lumberjack would) and closes it immediately; the logger then appends.
func probeLogFileWritable(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}

// initializeWriters creates the set of io.Writer targets for the logger based on configuration.
// If both console and file logging are disabled, file logging is enabled by default for safety.
// The method also stores the file writer on the Service for later Close().
func (s *Service) initializeWriters(logfile string) []io.Writer {
	var writers []io.Writer

	// Create a local copy to avoid mutating shared config
	fileLogging := s.LoggingConfig.FileLogging
	consoleLogging := s.LoggingConfig.ConsoleLogging

	// If both writers are disabled, enable the file writer
	if !consoleLogging && !fileLogging {
		fileLogging = true
	}
	if fileLogging {
		s.fileWriter = s.initializeRollingFileLogger(logfile)
		writers = append(writers, s.fileWriter)
	}
	if consoleLogging {
		cw := zerolog.ConsoleWriter{Out: os.Stderr}
		if s.LoggingConfig.ConsoleNoColor {
			cw.NoColor = true
		}
		if s.LoggingConfig.ConsoleTimeFormat != "" {
			cw.TimeFormat = s.LoggingConfig.ConsoleTimeFormat
		}
		writers = append(writers, cw)
	}

	return writers
}
