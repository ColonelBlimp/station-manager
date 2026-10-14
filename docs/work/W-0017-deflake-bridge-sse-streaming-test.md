# W-0017 — De-flake CI-observed environment-sensitive tests

**Status:** Sub-item A closed (`544fb62a`, CI green 2026-10-13); sub-item B fixed in `e7237b3a` (busy handling on the raw test connections)
**Selected:** Sub-item A, 2026-10-12 (operator), as the change after W-0021 5F.4 commit 4
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

**Recurrence (2026-10-12).** CI run `38048543752` (W-0021 5F.4 commit 2, which touched nothing in
`internal/bridge`) failed in this test alone, with the same text: `fake did not reach 3 writes
within 3s (have 2)`. That signature meets the recurrence trigger. The operator selected sub-item A
as the separate change after W-0021 5F.4 commit 4 (ordering in `docs/backlog.md`). The matching
signature is not a proven root cause; the fix must still establish the cause.

**Acceptance:** replace the timeout-based write-count barrier with a deterministic signal that the
bootstrap READ has been published to the subscriber (synchronize on `Subscribe` + bootstrap
completion via an observable barrier), rather than raising the deadline again. Do not weaken what the
test proves: a late subscriber still receives the INIT frame, the post-`Subscribe` bootstrap
snapshot, and the `event: rig-state` / `data:` frame. Stay inside the isolated bridge test harness;
add no synchronization framework.

### Sub-item A: cause, plan and build (2026-10-13)

**Cause (a publication-order race in the tests; the CI interleaving is inferred).** The pipeline
publishes `activeClient` (with the bootstrap bytes) only after its INIT and post-INIT READ writes
(`pipeline.go:363–417`). `TriggerBootstrap` writes nothing while `activeClient` is nil; it logs
that skip at Debug (`service.go:1037–1043`). The handler subscribes and then triggers the bootstrap
at once (`handler.go:82–112`). A subscriber that arrives before the publication, whether before the
port opens or between the startup writes and the assignment, therefore gets no bootstrap write.
After the startup writes, the count is 2, and the test's barrier was waiting for a third write that
this ordering does not produce. Later INIT and READ writes come only from liveness recovery
(`pipeline.go:790`). The deterministic reproduction below reaches CI's exact state; that the CI run
took this ordering is inferred. A finite observation cannot rule out slower scheduling as well.

**Not a product defect.** A subscriber that misses its bootstrap is already subscribed when the
answers to the pipeline's own startup READ are published, so it still receives the snapshot. EB1
proves this.

**Ruled 2026-10-13 (operator).** Proceed test-only; keep the early-subscriber regression. Make the
reproduction deterministic: no sleep stands for "the bootstrap ran". Release every gate before
`Stop`. Force the window after the second write for the proxy tests, with an explicit release
protocol, requiring failure at the relevant assertion. The regression proves startup delivery from
the fake's own answer to the startup READ. Narrow these claims.

**Built (uncommitted).**
- Barriers (`handler_bootstrap_race_test.go`):
  - `waitForPipelineActive` polls `activeClient` under `s.mu`, the condition `TriggerBootstrap`
    reads.
  - `passMarker` publishes a marker event and reads the stream until its frame. The handler writes
    events only from its loop, entered after `TriggerBootstrap` returns, so the marker proves the
    bootstrap attempt finished.
  - `heldService` holds the pipeline from the start, at the port's open or after its second write
    (`fakeSerial.afterWrite`, a hook run outside the fake's lock). Every gate releases at cleanup,
    registered after the service's own, so it runs before `Stop`.
- EB1 (permanent): an early subscriber, before the port opens and between the startup writes and
  the publication. The fake answers the startup READ with the FT-710's identity; the subscriber
  receives one complete `event: rig-state` / `data: …FT-710…` frame. The writes show INIT, the
  startup READ and no bootstrap READ (the pipeline's defensive unkey may follow).
- Late subscriber (the bootstrap guarantees, kept):
  - `StreamsPipelineEvents` waits for the publication before the GET and for the subscriber after
    it. It then requires INIT first, the bootstrap READ third with the rigdef READ bytes, and the
    FT-710 frame.
  - `BootstrapFiresOnSubscribe` and `TriggerBootstrap_WritesReadCommand` wait for the publication
    instead of two writes.
  - `ShutdownChClosesStream` replaces its silent 1 s loop with the publication, subscriber and
    bootstrap barriers.
- Reversions (each test with its old barrier, on a service held after its second write; released
  once the marker shows the bootstrap was attempted; file restored and hash-verified):
  - B1 `StreamsPipelineEvents`: fails "fake did not reach 3 writes within 3s (have 2)", CI's text.
  - B2 `BootstrapFiresOnSubscribe`: fails "fake did not reach 3 writes within 1s (have 2)".
  - B3 `TriggerBootstrap_WritesReadCommand`: fails "expected 3 writes … got 2".
  - B4 `ShutdownChClosesStream`'s old loop **passes**. It falls through after 1 s, and the early
    subscriber still gets the fed push once the pipeline is released. As predicted in the ruling,
    no reversion distinguishes it: the change removes a silent fall-through rather than fixing a
    reproduced failure.
  - B5 (EB1, before the port opens) with the pipeline's startup READ removed: fails EB1's write-order
    assertion. The frame arrived only after the 5 s liveness recovery wrote INIT and READ again.
  - Harness slips, re-run before counting: the first B1/B2 runs installed the hold after the
    pipeline had started, so it never held. EB1's between-writes case had the same ordering, now
    fixed (hold first, then open). B5's between-writes case deadlocks without a startup READ, so B5
    runs the before-open case. The first B4 anchor matched twice.
- Gates, all exit 0: gofmt, vet, `go test ./internal/bridge/ -race -count=3` (the handler and
  bootstrap tests also `-count=20`), `go test ./...`, maintainability, `task ci:local` (dist
  restored) and the context gate. No production file changed.
- Operator review (2026-10-13), two P2 findings, fixed:
  - EB1's between-writes case did not confirm its hold was reached before connecting, so both
    cases could have tested subscription before startup. The hold now closes a `reached` channel,
    and `heldService` waits for it before returning.
  - EB1's final READ tally treated every later READ as a bootstrap. Identity or liveness recovery
    may add some. The no-bootstrap check now runs right after the marker, while the pipeline is
    still held: no writes before the open, or exactly INIT and the startup READ between them.
    After release only the startup order and the complete FT-710 frame are checked.
  - Re-proved: B1–B3 fail as before. B5 now fails EB1's startup-order check (a second INIT ahead
    of the READ, from liveness recovery). B6, a hold after INIT alone, fails the held-writes check
    (`["AI1;"]`), so that check pins the claimed ordering in every run. `-race -count=3` for the
    package and `-count=20` for the handler and bootstrap tests pass, as does `git diff --check`.
- Approved by the operator and committed 2026-10-13 as `544fb62a` (the four bridge test files;
  test-only). The clean-room review of `544fb62a` found no actionable findings; closed and the
  review file deleted. CI run 38356688161 at `3968d269` (which includes `544fb62a`): the bridge
  package passed in attempt 1. The run's one failure was the unrelated `cmd/smd` shutdown
  double-close (W-0022), and attempt 2 is green. Sub-item A is closed.

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

## Sub-item C — `cmd/smd` DR3 sees the adopter's version probe (2026-10-14)

**Observed:** CI run `38416339715` (docs-only commit `ec81682b`) failed
`TestReconcileSet_DR3_TheSetIsTheStartSnapshot` with `a request carried ""; want the start's
token`; it passed locally.

**Cause (proven):** DR3 saves a new station token before the aggregate. The token keys the SM Cloud
account fingerprint (`smcloud.AccountFingerprint`, HMAC on the token), so Home's default binding is
no longer confirmed for the current account. If the adopter's first check (asynchronous at start)
runs after the save, it re-attempts adoption and sends `GET /v1/version`, which carries no token by
design (`adoption_client.go`). When that lands between the test's mark and its read, the assertion
counts it as the aggregate's. The adopter is correct; the test's assertion was too broad.

**Reproducer (temporary instrumentation, removed, hashes verified):** the adopter's `Run` waited for
the saved `t2` token before its first check, and each reconciler GET slept 100 ms. Before the fix:
3/3 failures with CI's exact text. After it: 3/3 passes under the same instrumentation.

**Fix (test only):** DR3 skips `GET /v1/version` and requires every other request in the window to
carry the start's token, and at least one such request. Production is unchanged. Not yet committed.

## References

- `internal/bridge/handler_test.go` (barrier + its inline flake history at the wait site)
- [`internal/bridge/AGENTS.md`](../../internal/bridge/AGENTS.md) — pipeline supervisor, INIT/READ/
  bootstrap write ordering the barrier depends on
- `internal/evidence/retention_test.go` (the `SQLITE_BUSY` site)
