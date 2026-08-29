package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// CC-4. The service update boundary now enforces normalize→validate before commit,
// so a callback that produces an invalid config is rejected with a typed
// *ValidationError and NEITHER memory nor disk changes — for all three primitives.
// Before CC-4 the candidate was committed to disk and memory unchecked, failing only
// at the next Load. The self-heal keeps its guarantee: once the candidate VALIDATES,
// memory commits even if the disk write then fails.

// newValidService returns a Service whose in-memory and on-disk config are a valid
// DefaultConfig, with the durable-write path set. DefaultConfig passes Validate, so a
// single-field mutation isolates whichever rule the test is exercising.
func newValidService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	base := DefaultConfig(dir)
	data, err := json.MarshalIndent(base, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	svc := New(base)
	svc.SetPath(path)
	return svc, path
}

func TestUpdateBoundary_InvalidCandidate_NoStateChange(t *testing.T) {
	// A valid mutation would pass; this one drives the candidate invalid (CC-3's
	// datastore rule) so validate must reject it at the boundary.
	invalidate := func(c *Config) error { c.Datastore.Driver = "postgres"; return nil }

	methods := []struct {
		name string
		call func(s *Service) error
	}{
		{"Update", func(s *Service) error { _, err := s.Update(invalidate); return err }},
		{"UpdateIfChanged", func(s *Service) error { _, _, err := s.UpdateIfChanged(false, invalidate); return err }},
		{"UpdateInMemoryThenPersist", func(s *Service) error { _, err := s.UpdateInMemoryThenPersist(invalidate); return err }},
	}
	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			svc, path := newValidService(t)
			diskBefore, err := os.ReadFile(path)
			require.NoError(t, err)

			callErr := m.call(svc)

			var ve *ValidationError
			require.ErrorAs(t, callErr, &ve, "an invalid candidate must be rejected with a typed ValidationError")
			require.Equal(t, "invalid_datastore", ve.Finding.Code)

			require.Equal(t, types.SqliteDriverName, svc.Cfg.Datastore.Driver,
				"in-memory config must be UNCHANGED — the invalid candidate must not be published")

			diskAfter, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, diskBefore, diskAfter, "on-disk config.json must be byte-identical")
		})
	}
}

// The self-heal guarantee (B1): once the candidate validates, UpdateInMemoryThenPersist
// commits memory even when persistence fails — a wrong logbook id this session is worse
// than an unpersisted fix that the next startup re-runs.
func TestUpdateInMemoryThenPersist_ValidCandidate_MemoryWinsOnPersistFailure(t *testing.T) {
	svc, _ := newValidService(t)
	// Hard pre-rename write failure — nothing reaches disk.
	svc.fs = &stepFS{real: osFS{}, failAt: "write", failErr: os.ErrInvalid}

	_, err := svc.UpdateInMemoryThenPersist(func(c *Config) error {
		c.DefaultLogbookID = 42 // a valid mutation — mirrors the boot default-logbook heal
		return nil
	})

	require.Error(t, err, "a hard persist failure is reported")
	require.False(t, errors.As(err, new(*ValidationError)), "the error is a persist failure, not a validation rejection")
	require.Equal(t, int64(42), svc.Cfg.DefaultLogbookID,
		"memory MUST win once the candidate validates, even when the disk write fails")
}
