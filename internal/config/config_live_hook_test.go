package config

// The live-change hook (ADR 0085; operator ruling 2026-10-02): the daemon tells
// clients that configuration changed — after the new config is LIVE, whatever
// becomes of the client's response — so a page invalidates what it derived
// from it (a saved draft's attribution). A rejected write is not a change.
//
//   L1  Update calls the hook once the new config is live (Snapshot inside the
//       hook sees it, and the hook runs outside the lock).
//   L2  A durability-uncertain write took effect: the hook is called.
//   L3  A rejected write — fn error, validation failure, hard write failure —
//       does not call it.
//   L4  UpdateIfChanged calls it only when it actually changed something.
//   L5  UpdateInMemoryThenPersist calls it even when the disk write fails: the
//       memory change stands.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func hookedService(t *testing.T) (*Service, *[]string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")
	svc := New(DefaultConfig(dir))
	svc.SetPath(path)
	seen := []string{}
	svc.SetOnChanged(func() { seen = append(seen, svc.Snapshot().UserAgent) })
	return svc, &seen
}

func TestOnChanged_UpdateCallsItOnceLive(t *testing.T) {
	svc, seen := hookedService(t)
	_, err := svc.Update(func(c *Config) error { c.UserAgent = "NEW"; return nil })
	require.NoError(t, err)
	require.Equal(t, []string{"NEW"}, *seen, "called once, after the new config is live")
}

func TestOnChanged_DurabilityUncertainCounts(t *testing.T) {
	svc, seen := hookedService(t)
	svc.fs = &stepFS{real: osFS{}, failAt: "syncdir", failErr: os.ErrInvalid}
	dur, err := svc.Update(func(c *Config) error { c.UserAgent = "NEW"; return nil })
	require.NoError(t, err)
	require.Equal(t, DurabilityUncertain, dur)
	require.Equal(t, []string{"NEW"}, *seen)
}

func TestOnChanged_RejectedWritesDoNotCount(t *testing.T) {
	svc, seen := hookedService(t)
	_, err := svc.Update(func(c *Config) error { return errors.New("refused") })
	require.Error(t, err)
	_, err = svc.Update(func(c *Config) error { c.DefaultRigID = 99; return nil }) // no such rig
	require.Error(t, err)
	svc.fs = &stepFS{real: osFS{}, failAt: "write", failErr: os.ErrInvalid}
	_, err = svc.Update(func(c *Config) error { c.UserAgent = "NEW"; return nil })
	require.Error(t, err)
	require.Empty(t, *seen)
}

func TestOnChanged_UpdateIfChangedOnlyOnChange(t *testing.T) {
	svc, seen := hookedService(t)
	// Bring the fixture to its canonical (normalized) shape first: that first
	// pass IS a change; the no-op below is then a true one.
	_, err := svc.Update(func(c *Config) error { return nil })
	require.NoError(t, err)
	*seen = nil
	_, _, err = svc.UpdateIfChanged(false, func(c *Config) error { return nil })
	require.NoError(t, err)
	require.Empty(t, *seen, "a semantic no-op is not a change")
	_, _, err = svc.UpdateIfChanged(false, func(c *Config) error { c.UserAgent = "NEW"; return nil })
	require.NoError(t, err)
	require.Equal(t, []string{"NEW"}, *seen)
}

func TestOnChanged_InMemoryThenPersistCountsEvenWhenDiskFails(t *testing.T) {
	svc, seen := hookedService(t)
	svc.fs = &stepFS{real: osFS{}, failAt: "write", failErr: os.ErrInvalid}
	_, err := svc.UpdateInMemoryThenPersist(func(c *Config) error { c.UserAgent = "NEW"; return nil })
	require.Error(t, err, "the disk write failed")
	require.Equal(t, []string{"NEW"}, *seen, "the memory change stands, so clients must hear of it")
}
