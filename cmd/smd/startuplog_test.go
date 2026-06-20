package main

import (
	"encoding/json"
	stderrors "errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A pre-logger startup error must land in smd.log (not just stderr/journal), so a
// misconfigured config.json isn't a silent exit on a fresh deploy.
func TestLogStartupFailure_WritesToSmdLog(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SM_WORKING_DIR", tmp) // utils.WorkingDir() resolves here

	logStartupFailure(stderrors.New("invalid config (bad_field): boom"))

	data, err := os.ReadFile(filepath.Join(tmp, "log", "smd.log"))
	require.NoError(t, err, "smd.log should exist with the startup error")

	var rec map[string]string
	require.NoError(t, json.Unmarshal(data, &rec), "line is valid zerolog-shaped JSON")
	require.Equal(t, "error", rec["level"])
	require.Contains(t, rec["error"], "invalid config (bad_field): boom")
	require.Contains(t, rec["message"], "config.json")
}

func TestLogStartupFailure_NilIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SM_WORKING_DIR", tmp)
	logStartupFailure(nil)
	_, err := os.Stat(filepath.Join(tmp, "log", "smd.log"))
	require.True(t, os.IsNotExist(err), "a nil error writes nothing")
}

// Two failures append (don't truncate) — a restart loop leaves a trail.
func TestLogStartupFailure_Appends(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SM_WORKING_DIR", tmp)
	logStartupFailure(stderrors.New("first"))
	logStartupFailure(stderrors.New("second"))
	data, err := os.ReadFile(filepath.Join(tmp, "log", "smd.log"))
	require.NoError(t, err)
	require.Contains(t, string(data), "first")
	require.Contains(t, string(data), "second")
}
