---
number: 0042
title: Guaranteed stop covers pipeline teardown + reconnect (the daemon's own exit)
status: Accepted
date: 2026-07-02
---

# 0042 — Guaranteed stop on pipeline teardown + reconnect

## Context

The tune-carrier (ADR 0027) and FT8-TX (ADR 0030) machinery is built around one
promise: *the daemon owns the guaranteed stop* — `tx_on`/`tx_off` are never
`exposed`, only the controllers key TX, and every keyed path has a hard auto-off
backstop + release-on-disconnect. A fresh-eyes review (2026-07-02) found the one
lifecycle path that never enters that machinery: **pipeline teardown**.

`runPipeline`'s teardown defer clears state, cancels the auto-off timer, closes
the port, and calls `clearTuneOnDisconnect`/`clearFt8TxOnDisconnect` — which
deliberately write no `tx_off` on the grounds "the carrier physically dropped
with the rig." That holds only when the *rig* died. Two cases break it:

- **Healthy-port exit (daemon shutdown):** `systemctl restart smd` /
  `task deploy:local:dev` cancels the context. The rig is healthy, the port is
  still open and writable, PTT is CAT-keyed up — and the auto-off timer dies with
  the process. The rig keeps transmitting (a dead carrier at full power in the
  FT8 case) until its own TOT or the operator intervenes.
- **Dead-port exit with a live rig:** a write-watchdog port closure or an EIO
  mid-tune tears the pipeline down while the rig is still CAT-keyed. The teardown
  can't unkey (the link is dead), and the supervisor (ADR 0020) reopens the port
  with the rig still transmitting — nothing sends a defensive `tx_off`.

## Decision

Two layers.

- **F1(a) — teardown unkey (SHIPPED).** In the teardown defer, when
  `tuneActive || ft8TxActive`, best-effort write the encoded `tx_off` on
  `context.Background()` *before* `client.Close()`. This fully closes the
  healthy-port case (the port is still writable). `unkeyOnTeardown` in
  `pipeline.go`; test `TestUnkeyOnTeardown`. **Bench-validated 2026-07-02:**
  `systemctl restart smd` while keyed drops PTT on the FTdx10.

- **F1(b) — flag-based reconnect unkey (SHIPPED 2026-07-02).** When F1(a)'s
  teardown unkey *write fails* (the dead-port case — the rig may still be keyed),
  `markStrandedKeyed` sets an in-memory `strandedKeyed` flag. The next pipeline
  instance's `readLoop`, once identity confirms (the H2 write gate),
  `defensiveUnkeyIfStranded` sends one defensive `tx_off` and clears the flag
  (one-shot). Surgical: the daemon unkeys only when it *knows* it stranded the rig
  this session. Test `TestDefensiveUnkeyIfStranded`.

## Alternatives considered

### F1(b): unconditional defensive `tx_off` on every identity-confirm

Simpler (no flag) and it also covers the daemon-restart-after-dead-port case
(fresh process, no in-memory flag). Rejected: an unconditional unkey at *every*
connect risks clobbering a legitimate operator transmission if the operator
happens to be manually (mic-PTT) transmitting the moment the daemon connects —
and whether a CAT `TX0;` even overrides a physical PTT on the FTdx10 is an
unverified rig behaviour. The flag-based path never touches a transmission the
daemon didn't itself strand, so it sidesteps the question entirely.

### F1(b): persist the `strandedKeyed` flag to disk

Would cover the dead-port-*exactly-at-daemon-shutdown* → restart case (where the
in-memory flag is lost with the process). Rejected as not worth the persistence
machinery: that specific combination is rare, SM is attended (the operator is
present), and the rig's own TOT is the backstop of last resort. In-memory only.

## Consequences

- The healthy-port exit (the common, routine `systemctl restart` /
  `task deploy:local:dev`) is fully covered — F1(a) bench-validated on the FTdx10
  2026-07-02 (`systemctl restart smd` while keyed drops PTT).
- F1(b) covers the realistic dead-port case: a transient port blip mid-tune
  within the same process, where the supervisor reconnects and clears the
  stranding on the next identity-confirm.
- **Residual (accepted):** a dead port *exactly at daemon shutdown* followed by a
  restart is not covered — the in-memory flag is gone with the old process. The
  attended operator + rig TOT are the fallback.
- F1(a) and F1(b) dovetail: F1(a) tries to unkey; only its *failure* arms F1(b).
  No double-unkey, and F1(b) fires only when it's genuinely needed.
- F1(b) is logic-tested (`TestDefensiveUnkeyIfStranded`: teardown-write-fail arms
  the flag; a confirmed reconnect fires one `tx_off` and clears it, one-shot; no
  unkey while unconfirmed). It reuses F1(a)'s already-bench-validated `tx_off`
  write, so the wire behaviour is proven; a full dead-port→reconnect on-air
  rehearsal is hard to stage deliberately and not required for the flag logic.

## Triggers to revisit

- **A dead-port-at-shutdown stranding is observed in practice** → persist the flag
  (or reconsider the unconditional approach).
- **The unconditional approach becomes attractive** (e.g. F1(b)'s coverage feels
  too narrow) → first bench-confirm whether `TX0;` overrides mic-PTT on the target
  rigs; only then is unconditional safe.
- **A rig without a usable `tx_off` command is added** → the whole guaranteed-stop
  model already assumes one (the pre-key gate proves it encodable); revisit there.

## References

- ADR 0027 (tune-carrier guaranteed stop), ADR 0030 (FT8-TX PTT seam), ADR 0020
  (pipeline supervisor / reconnect), ADR 0019 (read-only bridge).
- `internal/bridge/pipeline.go` (`unkeyOnTeardown`), `tune.go`/`ft8tx.go`
  (`clearTuneOnDisconnect`/`clearFt8TxOnDisconnect`), `tune_test.go`
  (`TestUnkeyOnTeardown`).
- Memory `project_sm_serial_bridge`.
