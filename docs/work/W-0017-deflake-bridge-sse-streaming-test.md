# W-0017 — De-flake CI-observed environment-sensitive tests

**Status:** Deferred — adjacent-change work; also fix on next recurrence
**Selected:** Not selected
**Outcome:** Two tests that flake red only on a loaded/instrumented CI runner become deterministic,
each proving its original guarantee without leaning on a wall-clock margin.

## Flake class (shared), not a shared root cause

Both items below are the same *class* of failure — a test that passes locally but goes red on a
heavily loaded or race-instrumented CI runner because it depends on wall-clock timing margin rather
than a deterministic condition. Their **root causes differ** (a cross-goroutine startup barrier vs.
concurrent SQLite lock contention), so each has its own acceptance criteria and must be fixed on its
own terms; neither fix is to raise a timeout.

Evidence (CI run `32734396428`, 2026-08-24, three attempts, while landing an unrelated `internal/api`
change): attempt 1 reddened on the bridge test in the full no-race step (the `-race` step passed);
attempts 2 and 3 both reddened on the evidence test in the `-race, -short` step. That step runs
first, so its failure skipped the later full step on attempts 2–3. Neither package is touched by the
change under test, and both pass locally.

**Runner/infrastructure pattern:** rerunning did not clear the gate. The `-race` step was green on
attempt 1 but failed on attempts 2 and 3 with the same evidence `SQLITE_BUSY`, i.e. the flake became
*more* likely as the runner stayed under load through the afternoon — a CI-environment amplifier, not
a defect in the change under test. Because the evidence failure reproduced 2/2 rather than varying,
reruns were stopped after attempt 3: the gate will not go green by retrying, only by the fixes below.

## Sub-item A — `internal/bridge` streaming-startup barrier

`internal/bridge/handler_test.go`'s `TestHTTPHandler_StreamsPipelineEvents` gates on
`waitForWriteCount(fake, 3, 3*time.Second)` — a wall-clock wait for three cross-goroutine writes
(INIT → post-INIT READ → bootstrap READ, the last firing only after `Subscribe`). On a loaded runner
the bootstrap write misses the deadline (`fake did not reach 3 writes within 3s (have 2)`). Already
widened once (run `31574196491`, 2026-08-12, 1s → 3s) and flaked again at 3s.

**Acceptance:** replace the timeout-based write-count barrier with a deterministic signal that the
bootstrap READ has been published to the subscriber (synchronize on `Subscribe` + bootstrap
completion via an observable barrier), rather than raising the deadline again. Do not weaken what the
test proves: a late subscriber still receives the INIT frame, the post-`Subscribe` bootstrap
snapshot, and the `event: rig-state` / `data:` frame. Stay inside the isolated bridge test harness;
add no synchronization framework.

## Sub-item B — `internal/evidence` concurrent-SQLite lock

`internal/evidence`'s `TestReceipt_DialContextRecordedAndSeparated` failed at `retention_test.go:430`
with `database is locked (5) (SQLITE_BUSY)` under `-race`. The race detector's slowdown widens the
window in which one connection holds the write lock while another attempts access, so a connection
that lacks busy handling fails immediately instead of waiting. This now reproduces on the CI runner
(attempts 2 and 3 of run `32734396428`) while passing locally, so it blocks a green gate until fixed
rather than clearing on rerun — the more urgent of the two sub-items.

**Acceptance:** make the concurrent SQLite access deterministic under `-race` using appropriate busy
handling/retry (e.g. a `busy_timeout` / serialized writer / bounded retry on `SQLITE_BUSY`), not by
simply increasing test timeouts. Preserve what the test proves about dial-context recording and
separation, and keep the fix within the test/store setup without loosening production locking
guarantees.

## References

- `internal/bridge/handler_test.go` (barrier + its inline flake history at the wait site)
- [`internal/bridge/AGENTS.md`](../../internal/bridge/AGENTS.md) — pipeline supervisor, INIT/READ/
  bootstrap write ordering the barrier depends on
- `internal/evidence/retention_test.go` (the `SQLITE_BUSY` site)
