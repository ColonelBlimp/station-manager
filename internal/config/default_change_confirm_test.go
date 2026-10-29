package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

/*
   W-0024 slice 3a (ADR 0097 §1, "Removing the selector"): a default-change
   tombstone is confirmed — dropped from memory — only by a config write that
   (a) actually wrote a document holding that tombstone and (b) reported
   durable. The capture below records each document a write renamed into
   place, so a test sees what was written and how the write ended.

     DT1  Update: durable drops it; uncertain and failed keep it.
     DT2  UpdateIfChanged: a no-op writes nothing and reports Durable, and
          confirms nothing; a forced write or a real change, durable, drops it.
     DT3  UpdateInMemoryThenPersist: durable drops it; uncertain and failed
          keep it.
     DT4  the removal write itself: replacing a pending selector with its
          tombstone, durably, confirms it at once; uncertain keeps it.
     DT5  a pending selector is never dropped by a durable write.
     DT6  the drop is from memory only: the confirming document holds the
          tombstone, and the next write is the first without it.
     DT7  a config loaded from disk holds its tombstone: confirmation is per
          process.
*/

// captureFS records every document a write renamed into place, over the
// injector that can fail one named step.
type captureFS struct {
	*stepFS
	mu      sync.Mutex
	pending map[string]*strings.Builder // temp file → its bytes
	written []string                    // documents renamed into place, in order
}

func newCaptureFS(failAt string) *captureFS {
	return &captureFS{stepFS: &stepFS{real: osFS{}, failAt: failAt, failErr: os.ErrInvalid}, pending: map[string]*strings.Builder{}}
}

func (c *captureFS) CreateTemp(dir, pattern string) (fsFile, error) {
	f, err := c.stepFS.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	b := &strings.Builder{}
	c.mu.Lock()
	c.pending[f.Name()] = b
	c.mu.Unlock()
	return &captureFile{fsFile: f, buf: b}, nil
}

func (c *captureFS) Rename(oldpath, newpath string) error {
	if err := c.stepFS.Rename(oldpath, newpath); err != nil {
		return err
	}
	c.mu.Lock()
	if b, ok := c.pending[oldpath]; ok {
		c.written = append(c.written, b.String())
	}
	c.mu.Unlock()
	return nil
}

func (c *captureFS) docs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.written...)
}

type captureFile struct {
	fsFile
	buf *strings.Builder
}

func (f *captureFile) Write(p []byte) (int, error) {
	n, err := f.fsFile.Write(p)
	f.buf.Write(p[:n])
	return n, err
}

const tombID = "019fd5c5-0000-7000-8000-00000000c0de"

func tombstone(removed string) *DefaultLogbookChange {
	return &DefaultLogbookChange{RequestID: tombID, ArchiveID: "019fd5c5-efcc-7193-be4f-1fee532ee300",
		LogbookUUID: "019fd5c5-0000-7000-8000-0000000000b0", Removed: removed}
}

// tombService is a service on disk holding rec in memory, as a start that
// found it, writing through fs.
func tombService(t *testing.T, rec *DefaultLogbookChange, fs *captureFS) *Service {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := DefaultConfig(dir)
	cfg.DefaultLogbookChange = rec
	_, err := WriteJSON(path, cfg)
	require.NoError(t, err)
	// Loaded, as a start loads it: memory then holds the canonical shape, so a
	// write that changes nothing is a no-op.
	loaded, err := Load(path)
	require.NoError(t, err)
	svc := New(loaded)
	svc.SetPath(path)
	svc.fs = fs
	return svc
}

// holds reports whether a written document holds the record with that removal.
func holds(t *testing.T, doc, removed string) bool {
	t.Helper()
	var c struct {
		Change *DefaultLogbookChange `json:"default_logbook_change"`
	}
	require.NoError(t, json.Unmarshal([]byte(doc), &c))
	return c.Change != nil && c.Change.RequestID == tombID && c.Change.Removed == removed
}

func wantMemory(t *testing.T, svc *Service, removed string, kept bool) {
	t.Helper()
	got := svc.Snapshot().DefaultLogbookChange
	if !kept {
		require.Nil(t, got, "the tombstone should have been confirmed and dropped from memory")
		return
	}
	require.NotNil(t, got, "the record should still be held in memory")
	require.Equal(t, removed, got.Removed)
}

func touch(c *Config) error { c.UserAgent += "x"; return nil }

func TestDefaultChange_DT1_Update(t *testing.T) {
	for _, tc := range []struct {
		name, failAt string
		docs         int
		dur          Durability
		kept         bool
	}{
		{"durable", "", 1, Durable, false},
		{"uncertain", "syncdir", 1, DurabilityUncertain, true},
		{"failed", "write", 0, Durable, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newCaptureFS(tc.failAt)
			svc := tombService(t, tombstone(RemovedWithdrawn), fs)
			dur, err := svc.Update(touch)
			if tc.failAt == "write" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.dur, dur)
			}
			docs := fs.docs()
			require.Len(t, docs, tc.docs)
			if tc.docs == 1 {
				require.True(t, holds(t, docs[0], RemovedWithdrawn), "the write's document must hold the tombstone")
			}
			wantMemory(t, svc, RemovedWithdrawn, tc.kept)
		})
	}
}

func TestDefaultChange_DT2_UpdateIfChanged(t *testing.T) {
	t.Run("a no-op writes nothing, reports Durable, and confirms nothing", func(t *testing.T) {
		fs := newCaptureFS("")
		svc := tombService(t, tombstone(RemovedWithdrawn), fs)
		_, dur, err := svc.UpdateIfChanged(false, func(*Config) error { return nil })
		require.NoError(t, err)
		require.Equal(t, Durable, dur)
		require.Empty(t, fs.docs(), "a no-op must not write")
		wantMemory(t, svc, RemovedWithdrawn, true)
	})
	for name, fn := range map[string]struct {
		force bool
		fn    func(*Config) error
	}{
		"a forced write": {true, func(*Config) error { return nil }},
		"a real change":  {false, touch},
	} {
		t.Run(name+", durable, confirms", func(t *testing.T) {
			fs := newCaptureFS("")
			svc := tombService(t, tombstone(RemovedWithdrawn), fs)
			_, dur, err := svc.UpdateIfChanged(fn.force, fn.fn)
			require.NoError(t, err)
			require.Equal(t, Durable, dur)
			docs := fs.docs()
			require.Len(t, docs, 1)
			require.True(t, holds(t, docs[0], RemovedWithdrawn))
			wantMemory(t, svc, RemovedWithdrawn, false)
		})
		t.Run(name+", uncertain, keeps it", func(t *testing.T) {
			fs := newCaptureFS("syncdir")
			svc := tombService(t, tombstone(RemovedWithdrawn), fs)
			_, dur, err := svc.UpdateIfChanged(fn.force, fn.fn)
			require.NoError(t, err)
			require.Equal(t, DurabilityUncertain, dur)
			wantMemory(t, svc, RemovedWithdrawn, true)
		})
	}
}

func TestDefaultChange_DT3_UpdateInMemoryThenPersist(t *testing.T) {
	for _, tc := range []struct {
		name, failAt string
		docs         int
		dur          Durability
		kept         bool
	}{
		{"durable", "", 1, Durable, false},
		{"uncertain", "syncdir", 1, DurabilityUncertain, true},
		{"failed", "write", 0, Durable, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newCaptureFS(tc.failAt)
			svc := tombService(t, tombstone(RemovedAbandoned), fs)
			dur, err := svc.UpdateInMemoryThenPersist(touch)
			if tc.failAt == "write" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.dur, dur)
			}
			docs := fs.docs()
			require.Len(t, docs, tc.docs)
			if tc.docs == 1 {
				require.True(t, holds(t, docs[0], RemovedAbandoned), "the write's document must hold the tombstone")
			}
			wantMemory(t, svc, RemovedAbandoned, tc.kept)
		})
	}
}

func TestDefaultChange_DT4_TheRemovalWriteItself(t *testing.T) {
	remove := func(c *Config) error { c.DefaultLogbookChange = tombstone(RemovedCancelled); return nil }
	t.Run("durable: confirmed at once", func(t *testing.T) {
		fs := newCaptureFS("")
		svc := tombService(t, tombstone(""), fs)
		_, err := svc.Update(remove)
		require.NoError(t, err)
		docs := fs.docs()
		require.Len(t, docs, 1)
		require.True(t, holds(t, docs[0], RemovedCancelled))
		wantMemory(t, svc, "", false)
	})
	t.Run("uncertain: the tombstone is kept, unconfirmed", func(t *testing.T) {
		fs := newCaptureFS("syncdir")
		svc := tombService(t, tombstone(""), fs)
		_, err := svc.Update(remove)
		require.NoError(t, err)
		wantMemory(t, svc, RemovedCancelled, true)
	})
}

func TestDefaultChange_DT5_APendingSelectorIsNeverDropped(t *testing.T) {
	fs := newCaptureFS("")
	svc := tombService(t, tombstone(""), fs)
	_, err := svc.Update(touch)
	require.NoError(t, err)
	wantMemory(t, svc, "", true)
	_, _, err = svc.UpdateIfChanged(true, func(*Config) error { return nil })
	require.NoError(t, err)
	wantMemory(t, svc, "", true)
	dur, err := svc.UpdateInMemoryThenPersist(touch)
	require.NoError(t, err)
	require.Equal(t, Durable, dur)
	wantMemory(t, svc, "", true)
	docs := fs.docs()
	require.Len(t, docs, 3)
	for _, d := range docs {
		require.True(t, holds(t, d, ""), "each durable write held the pending selector")
	}
}

func TestDefaultChange_DT6_TheDropIsFromMemoryOnly(t *testing.T) {
	fs := newCaptureFS("")
	svc := tombService(t, tombstone(RemovedWithdrawn), fs)
	_, err := svc.Update(touch)
	require.NoError(t, err)
	_, err = svc.Update(touch)
	require.NoError(t, err)
	docs := fs.docs()
	require.Len(t, docs, 2)
	require.True(t, holds(t, docs[0], RemovedWithdrawn), "the confirming write holds the tombstone")
	require.False(t, holds(t, docs[1], RemovedWithdrawn), "the next write is the first without it")
}

func TestDefaultChange_DT7_ALoadedConfigHoldsItsTombstone(t *testing.T) {
	fs := newCaptureFS("")
	svc := tombService(t, tombstone(RemovedApplied), fs)
	loaded, err := Load(svc.Path)
	require.NoError(t, err)
	require.NotNil(t, loaded.DefaultLogbookChange)
	require.Equal(t, RemovedApplied, loaded.DefaultLogbookChange.Removed)
}
