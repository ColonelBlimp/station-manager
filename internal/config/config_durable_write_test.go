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

// stepFS wraps the real osFS, records the ordered op sequence, and fails at ONE named
// step — the deterministic seam PT-6's crash-durability barriers are tested through.
// Injectors live in _test.go; production is always osFS.
type stepFS struct {
	real    fsOps
	failAt  string
	failErr error
	mu      sync.Mutex
	calls   []string
	temps   []string
}

func (s *stepFS) note(step string) error {
	s.mu.Lock()
	s.calls = append(s.calls, step)
	s.mu.Unlock()
	if step == s.failAt {
		return s.failErr
	}
	return nil
}

func (s *stepFS) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *stepFS) Stat(name string) (os.FileInfo, error) { return s.real.Stat(name) }

func (s *stepFS) CreateTemp(dir, pattern string) (fsFile, error) {
	if err := s.note("create"); err != nil {
		return nil, err
	}
	f, err := s.real.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.temps = append(s.temps, f.Name())
	s.mu.Unlock()
	return &stepFile{real: f, fs: s}, nil
}

func (s *stepFS) Rename(oldpath, newpath string) error {
	if err := s.note("rename"); err != nil {
		return err
	}
	return s.real.Rename(oldpath, newpath)
}

func (s *stepFS) SyncDir(dir string) error {
	if err := s.note("syncdir"); err != nil {
		return err
	}
	return s.real.SyncDir(dir)
}

func (s *stepFS) Remove(name string) error {
	s.mu.Lock()
	s.calls = append(s.calls, "remove")
	s.mu.Unlock()
	return s.real.Remove(name) // a fault injector still cleans up honestly
}

type stepFile struct {
	real fsFile
	fs   *stepFS
}

func (f *stepFile) Write(p []byte) (int, error) {
	if err := f.fs.note("write"); err != nil {
		return 0, err
	}
	return f.real.Write(p)
}
func (f *stepFile) Chmod(m os.FileMode) error {
	if err := f.fs.note("chmod"); err != nil {
		return err
	}
	return f.real.Chmod(m)
}
func (f *stepFile) Sync() error {
	if err := f.fs.note("sync"); err != nil {
		return err
	}
	return f.real.Sync()
}
func (f *stepFile) Close() error {
	if err := f.fs.note("close"); err != nil {
		return err
	}
	return f.real.Close()
}
func (f *stepFile) Name() string { return f.real.Name() }

func seedConfig(t *testing.T, path, marker string) {
	t.Helper()
	cfg := Config{}
	cfg.UserAgent = marker
	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func leftoverTemps(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "config.json-*.tmp"))
	require.NoError(t, err)
	return m
}

// (5)/(1) On a clean write: unique temp name (config.json-*.tmp, NOT a fixed
// "config.json.tmp"), the file is 0600, and no temp is left behind.
func TestWriteJSONDurable_UniqueTempAndCleanSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	fs := &stepFS{real: osFS{}}
	cfg := Config{}
	cfg.UserAgent = "one"
	dur, err := writeJSONDurable(path, cfg, fs)
	require.NoError(t, err)
	require.Equal(t, Durable, dur)

	require.Len(t, fs.temps, 1)
	require.Regexp(t, `config\.json-\d+\.tmp$`, filepath.Base(fs.temps[0]),
		"temp name must be unique (CC-5), not a fixed .tmp two writers would collide on")
	require.Empty(t, leftoverTemps(t, dir), "no temp file may remain after a clean write")

	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "the secret-bearing file stays owner-only")
}

// (7) Order barrier: temp data sync precedes the rename, which precedes the directory
// sync — the two crash-durability barriers in the documented order.
func TestWriteJSONDurable_SyncBeforeRenameBeforeDirSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	fs := &stepFS{real: osFS{}}
	_, err := writeJSONDurable(path, Config{}, fs)
	require.NoError(t, err)

	calls := fs.log()
	idx := func(step string) int {
		for i, c := range calls {
			if c == step {
				return i
			}
		}
		return -1
	}
	require.Greater(t, idx("rename"), idx("sync"), "the temp file must be fsync'd BEFORE the rename")
	require.Greater(t, idx("syncdir"), idx("rename"), "the directory must be fsync'd AFTER the rename")
}

// (2) Temp-file SYNC failure (before rename): hard error, the old file is byte-identical,
// the temp is cleaned, and the rename never ran.
func TestWriteJSONDurable_TempSyncFailure_OldFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	fs := &stepFS{real: osFS{}, failAt: "sync", failErr: os.ErrInvalid}
	cfg := Config{}
	cfg.UserAgent = "NEW"
	_, err = writeJSONDurable(path, cfg, fs)
	require.Error(t, err, "a temp-sync failure before the rename must be a hard error")

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "the old config must be byte-identical — the rename never happened")
	require.Empty(t, leftoverTemps(t, dir), "the temp must be removed on a pre-rename failure")
	require.NotContains(t, fs.log(), "rename", "the rename must not be attempted after a sync failure")
}

// (3) RENAME failure: hard error, old file intact, temp cleaned.
func TestWriteJSONDurable_RenameFailure_OldFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	fs := &stepFS{real: osFS{}, failAt: "rename", failErr: os.ErrPermission}
	_, err = writeJSONDurable(path, Config{}, fs)
	require.Error(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "a failed rename leaves the old config untouched")
	require.Empty(t, leftoverTemps(t, dir), "the temp must be removed on a rename failure")
}

// (4) DIRECTORY-SYNC failure — the confusable to exclude. The rename SUCCEEDED, so the
// new config is live on disk; the outcome MUST be DurabilityUncertain with a nil error,
// never an ordinary durable-success and never a generic failure.
func TestWriteJSONDurable_DirSyncFailure_AppliedButUncertain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")

	fs := &stepFS{real: osFS{}, failAt: "syncdir", failErr: os.ErrInvalid}
	cfg := Config{}
	cfg.UserAgent = "NEW"
	dur, err := writeJSONDurable(path, cfg, fs)

	require.NoError(t, err, "a directory-sync failure is NOT a failure — the change is applied")
	require.Equal(t, DurabilityUncertain, dur,
		"rename succeeded but the dir fsync failed → durability unconfirmed, NOT ordinary durable success")

	// The new config is the live file on disk.
	got := readUserAgent(t, path)
	require.Equal(t, "NEW", got, "the renamed new config is live even though its durability is unconfirmed")
	require.Empty(t, leftoverTemps(t, dir), "the renamed temp is now config.json; nothing is left over")
}

// Preserved-tighter mode survives the durable rewrite (a 0400 file stays 0400).
func TestWriteJSONDurable_PreservesTighterMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")
	require.NoError(t, os.Chmod(path, 0o400))

	_, err := writeJSONDurable(path, Config{}, osFS{})
	require.NoError(t, err)
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o400), fi.Mode().Perm(), "an operator-tightened 0400 must be preserved")
}

// (6) Concurrent writers to the same path: the unique temp (CC-5) means no interleaved
// corruption — the file ends as exactly ONE writer's valid payload with no leftover temps.
func TestWriteJSON_ConcurrentWriters_NoCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cfg := Config{}
			cfg.UserAgent = "writer-" + string(rune('a'+i))
			_, _ = WriteJSON(path, cfg)
		}(i)
	}
	wg.Wait()

	// The final file must be a COMPLETE, valid config that is exactly one writer's
	// payload — never a half-written interleave of two.
	got := readUserAgent(t, path)
	require.True(t, strings.HasPrefix(got, "writer-"), "final config must be one writer's valid payload, got %q", got)
	require.Empty(t, leftoverTemps(t, dir), "unique temps must all be renamed/cleaned — none left behind")
}

// Service.Update on an uncertain durability PUBLISHES in-memory (coherent with the
// now-live on-disk file) and returns DurabilityUncertain — the caveat the PUT surfaces.
func TestServiceUpdate_DirSyncUncertain_PublishesAndReportsCaveat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")

	svc := New(DefaultConfig(dir))
	svc.SetPath(path)
	svc.fs = &stepFS{real: osFS{}, failAt: "syncdir", failErr: os.ErrInvalid}

	dur, err := svc.Update(func(c *Config) error {
		c.UserAgent = "NEW"
		return nil
	})
	require.NoError(t, err, "an uncertain durability is not a failure — the change is applied")
	require.Equal(t, DurabilityUncertain, dur)
	require.Equal(t, "NEW", svc.Cfg.UserAgent, "in-memory MUST be published to match the live on-disk file")
	require.Equal(t, "NEW", readUserAgent(t, path), "on-disk is the new config")
}

// Service.Update on a hard (pre-rename) write failure must NOT publish in-memory — the
// old file and the in-memory value both stay put (coherent).
func TestServiceUpdate_HardWriteFailure_DoesNotPublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")

	svc := New(DefaultConfig(dir))
	svc.SetPath(path)
	svc.fs = &stepFS{real: osFS{}, failAt: "write", failErr: os.ErrInvalid}

	_, err := svc.Update(func(c *Config) error {
		c.UserAgent = "NEW"
		return nil
	})
	require.Error(t, err, "a pre-rename write failure is a hard error")
	require.Equal(t, "", svc.Cfg.UserAgent, "in-memory must NOT be published on a hard write failure")
	require.Equal(t, "OLD", readUserAgent(t, path), "the old config is untouched")
}

func readUserAgent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg Config
	require.NoError(t, json.Unmarshal(data, &cfg), "config.json must be complete, valid JSON (no corruption)")
	return cfg.UserAgent
}
