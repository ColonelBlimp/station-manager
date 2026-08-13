package pskreporter

import (
	"math/rand/v2"
	"os"
	"path/filepath"
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
