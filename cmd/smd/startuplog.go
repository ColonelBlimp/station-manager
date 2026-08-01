package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// logStartupFailure best-effort mirrors a startup error into smd.log.
//
// Startup failures that happen BEFORE the logging service is wired — a missing,
// malformed, or invalid config.json is by far the most common — otherwise reach
// only stderr, which under the systemd user service lands in the journal
// (journalctl --user -u smd), NOT smd.log where the operator looks. The symptom
// is "the daemon won't start and smd.log is empty / unchanged", a dead end on a
// fresh deploy. This writes the same error into smd.log so it's discoverable
// there too (the error still also goes to stderr via main()).
//
// Best-effort and side-effect-only: every failure here is swallowed. The log
// location is resolved via utils.WorkingDir() — the same resolver that found
// config.json — so in the normal install (no DataDir redirect) it lands next to
// the real smd.log. The line matches the zerolog JSON shape the rest of smd.log
// uses, so `grep '"level":"error"'` and log tooling keep working.
func logStartupFailure(startupErr error) {
	if startupErr == nil {
		return
	}
	wd, err := utils.WorkingDir()
	if err != nil {
		return
	}
	logPath := filepath.Join(wd, "log", "smd.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return
	}
	// 0600 to match internal/logging's own open mode. This path can CREATE smd.log
	// (a startup failure on a fresh install is exactly when it runs), and lumberjack
	// copies the mode off an existing logfile on every rotation — so a 0644 here made
	// the daemon log world-readable for the life of the install. It is also the one
	// writer whose payload is a config-parse error, the class of message most likely
	// to quote something sensitive.
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	// version from the SAME carrier the logging service uses (buildinfo.Version),
	// not a local copy: this record is written when config.json is missing or
	// malformed, i.e. on a fresh deploy, where "which build is this?" is the first
	// question and there is no other stamped line in the file to answer it.
	line, err := json.Marshal(map[string]string{
		"level":   "error",
		"time":    time.Now().UTC().Format(time.RFC3339),
		"version": buildinfo.Version,
		"message": "smd startup failed (fatal) before logging was initialised — check config.json",
		"error":   startupErr.Error(),
	})
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}
