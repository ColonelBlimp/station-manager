package pskreporter

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Acceptance criterion for the persisted sender identifier (PSK Reporter incident,
// 2026-08-13). pskdev.html: the header's "random identifier ... should be constant
// for any particular sender". We re-minted it every Start, so a heavy-restart
// reporter presented a new sender per restart — the confusable state this breaks:
// "one station that restarted" vs "many distinct new senders". These pin the
// helper's contract; TestService_IdentifierStableAcrossRestart pins the same rule
// on the observable datagram.

func TestLoadOrCreateIdentifier_StableAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pskreporter.id")
	id1 := loadOrCreateIdentifier(path, rand.Uint32, nil) // first boot: mint + persist
	id2 := loadOrCreateIdentifier(path, rand.Uint32, nil) // restart: read the persisted id
	if id1 != id2 {
		t.Fatalf("identifier changed across restart: %d then %d (must be constant — pskdev.html)", id1, id2)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("identifier not persisted to disk: %v", err)
	}
}

func TestLoadOrCreateIdentifier_ReturnsPersistedValueVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pskreporter.id")
	if err := os.WriteFile(path, []byte("3735928559\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// gen must NOT be called when a valid id is already on disk (no re-mint).
	got := loadOrCreateIdentifier(path, func() uint32 {
		t.Fatal("gen called despite a valid persisted id — id must be read verbatim")
		return 0
	}, nil)
	if got != 3735928559 {
		t.Fatalf("got %d, want the persisted 3735928559", got)
	}
}

func TestLoadOrCreateIdentifier_CorruptFileRegeneratesAndSelfHeals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pskreporter.id")
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadOrCreateIdentifier(path, func() uint32 { return 99 }, nil)
	if got != 99 {
		t.Fatalf("corrupt file must regenerate via gen; got %d", got)
	}
	// Self-heal: the regenerated id is now persisted, so the next boot reads it
	// rather than re-minting again.
	got2 := loadOrCreateIdentifier(path, func() uint32 {
		t.Fatal("regenerated id was not persisted — second boot re-minted")
		return 0
	}, nil)
	if got2 != 99 {
		t.Fatalf("regenerated id not persisted; got %d", got2)
	}
}

// A corrupt id whose FILE is writable but whose DIRECTORY is not must still heal
// by overwriting in place: os.CreateTemp needs directory-write, overwriting an
// existing file does not, so a temp+Link-only path would fail here and re-mint
// every restart (codex 85b55262 P2).
func TestLoadOrCreateIdentifier_CorruptFileHealsInReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions; the read-only-dir path can't be exercised as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pskreporter.id")
	if err := os.WriteFile(path, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // r-x: no new entries; the existing file stays writable
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // restore so TempDir cleanup can remove it

	got := loadOrCreateIdentifier(path, func() uint32 { return 4242 }, nil)
	if got != 4242 {
		t.Fatalf("corrupt id in a read-only dir must heal via in-place overwrite; got %d", got)
	}
	if v, ok := readIdentifier(path); !ok || v != 4242 {
		t.Fatalf("heal not persisted (would re-mint every restart): v=%d ok=%v", v, ok)
	}
}

// An existing id that is WRITABLE but UNREADABLE (mode 0200) in a read-only dir
// must also heal in place: the read fails, but the file exists and can be
// overwritten. Routing every read error through the atomic-create path fails here
// and re-mints every restart (codex 35f7b774 P2).
func TestLoadOrCreateIdentifier_UnreadableFileHealsInReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file/dir permissions; the unreadable path can't be exercised as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pskreporter.id")
	if err := os.WriteFile(path, []byte("777\n"), 0o200); err != nil { // exists, writable, UNREADABLE
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700); _ = os.Chmod(path, 0o600) })

	got := loadOrCreateIdentifier(path, func() uint32 { return 5150 }, nil)
	if got != 5150 {
		t.Fatalf("unreadable+writable id in a read-only dir must heal in place; got %d", got)
	}
	if err := os.Chmod(path, 0o600); err != nil { // make it readable to verify persistence
		t.Fatal(err)
	}
	if v, ok := readIdentifier(path); !ok || v != 5150 {
		t.Fatalf("heal not persisted (would re-mint every restart): v=%d ok=%v", v, ok)
	}
}

func TestLoadOrCreateIdentifier_EmptyPathIsInMemory(t *testing.T) {
	// The probe CLI and tests pass "" — always an in-memory random, never a file.
	dir := t.TempDir()
	got := loadOrCreateIdentifier("", func() uint32 { return 42 }, nil)
	if got != 42 {
		t.Fatalf("empty path must use the injected gen; got %d", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("empty path must not write any file, found %d entries", len(entries))
	}
}

func TestLoadOrCreateIdentifier_UnwritablePathFailsOpen(t *testing.T) {
	// A path inside a non-existent directory: the read fails (regenerate) and the
	// write fails (no dir) — the helper must still return the minted id, not panic
	// or block. Reporting is best-effort; a state-file problem never blocks Start.
	path := filepath.Join(t.TempDir(), "no-such-dir", "pskreporter.id")
	got := loadOrCreateIdentifier(path, func() uint32 { return 7 }, nil)
	if got != 7 {
		t.Fatalf("unwritable path must fail open to the in-memory id; got %d", got)
	}
}

// Two daemons started against one working directory must converge on a SINGLE
// persisted id — the non-atomic ReadFile→WriteFile it replaced let each concurrent
// caller return its own minted value (codex 63c94cfa P2; a second smd is not
// prevented — a unix ListenAndServe removes + rebinds the socket). The write-temp
// + os.Link publish makes exactly one caller win and the losers read the winner's
// fully-written file. The afterReadHook barrier drives every caller into the
// create window at once — without it the scheduler serialises them enough that
// even the non-atomic impl converges, so the assertion would prove nothing.
func TestLoadOrCreateIdentifier_ConcurrentCreateConverges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pskreporter.id")
	const n = 64

	var arrived sync.WaitGroup
	arrived.Add(n)
	release := make(chan struct{})
	afterReadHook = func() {
		arrived.Done()
		<-release // hold every caller here until all n have read-missed
	}
	defer func() { afterReadHook = nil }()
	go func() { arrived.Wait(); close(release) }()

	var wg sync.WaitGroup
	ids := make([]uint32, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i] = loadOrCreateIdentifier(path, rand.Uint32, nil)
		}(i)
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("concurrent callers diverged: ids[0]=%d ids[%d]=%d (each returned its own id)", ids[0], i, ids[i])
		}
	}
	persisted, ok := readIdentifier(path)
	if !ok || persisted != ids[0] {
		t.Fatalf("persisted id=%d ok=%v does not match the returned id %d", persisted, ok, ids[0])
	}
}
