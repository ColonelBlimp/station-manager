package evidence

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The evidence DSN uses _txlock=immediate so a read-then-write transaction (purgeAckedChunk,
// compaction, sync markOffered/applyOutcomes, profile activation) takes the write lock at BEGIN
// rather than upgrading from a read snapshot mid-transaction. Under a concurrent writer a DEFERRED
// begin loses its snapshot when it upgrades and fails with SQLITE_BUSY_SNAPSHOT (517) — the
// intermittent CI flake in TestReceipt_DialContextRecordedAndSeparated (retention_test.go), which
// busy_timeout does NOT retry (it retries lock-waits, not snapshot conflicts). IMMEDIATE takes the
// lock upfront so the concurrent writer waits instead.
//
// This pins the fix at the DSN it lives on: many goroutines each run BEGIN → read a row → increment
// it, on the service's own connection pool. The value-changing UPDATE (v = v + 1) makes each tx a
// genuine writer, so a DEFERRED begin races the snapshot; IMMEDIATE serializes cleanly. Reversion:
// drop _txlock=immediate from the DSN in acquire → this reports 517s.
func TestDB_ReadThenWrite_NoBusySnapshotUnderContention(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	// A dedicated probe table isolates the contention from evidence's real tables — this test is about
	// the DSN's transaction-locking mode, not any particular query.
	if _, err := s.db.Exec(`CREATE TABLE txprobe (id INTEGER PRIMARY KEY, v INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	for i := 0; i < 50; i++ {
		if _, err := s.db.Exec(`INSERT INTO txprobe (id, v) VALUES (?, 0)`, i); err != nil {
			t.Fatalf("seed probe row %d: %v", i, err)
		}
	}

	var busy517 atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for r := 0; r < 60; r++ {
				id := (base + r) % 50
				tx, err := s.db.Begin()
				if err != nil {
					continue
				}
				var v int
				_ = tx.QueryRow(`SELECT v FROM txprobe WHERE id = ?`, id).Scan(&v) // read: takes a snapshot
				_, err = tx.Exec(`UPDATE txprobe SET v = v + 1 WHERE id = ?`, id)  // write: upgrades the tx
				if err != nil {
					_ = tx.Rollback()
					if strings.Contains(err.Error(), "(517)") { // SQLITE_BUSY_SNAPSHOT
						busy517.Add(1)
					}
					continue
				}
				_ = tx.Commit()
			}
		}(g * 7)
	}
	wg.Wait()

	if n := busy517.Load(); n != 0 {
		t.Errorf("SQLITE_BUSY_SNAPSHOT (517) x%d — read-then-write transactions must take the write lock upfront (_txlock=immediate)", n)
	}
}
