# internal/ft8 scoped instructions

These tool-neutral rules apply under `internal/ft8/` in addition to the root instructions. The
canonical subsystem guide is [`docs/ft8.md`](../../docs/ft8.md); load it only for the FT8 topic at
hand. Git history retains the review archaeology removed from this kernel.

## TX and attribution invariants

Do not weaken these without explicit discussion. Test their observable effects—QSO rows, RF,
published events, and spots—not temporary implementation fields.

1. **Log every rogered contact exactly once, on the session-pinned frequency, regardless of the
   closing rung.** Group A completion runs before session retirement even if the courtesy RR73/73
   is refused, the rig disappears, or TX is disarmed. Retiring first invalidates the
   generation-guarded completion callback. Never read the live dial at completion: a refused QSY
   must not file the QSO on the new band. Observable: one QSO row and one `ft8-logged` event at the
   session frequency.

2. **Never key without positive frequency confirmation.** For a session rung, the confirmed dial
   must still equal the session dial. Unknown is refusal, not permission; `bridge.TxReady` alone
   does not prove the selected VFO is known. This includes manual `TransmitNext` sends. The
   decisive check is `Service.preKeyDialCheck`, adjacent to PTT through
   `TxController.SetPreKeyCheck`, because request acceptance may precede keying by a slot. A
   request-time check is only an early courtesy and follows armed/active/in-flight/readiness error
   precedence. With no CAT, the check is inert because that deployment cannot key. Observable: no
   PTT; a start reports `ErrTxDialUnknown` / 503 `rig_dial_unknown`.

3. **The final `ft8-qso` event must describe the live session.** Publish terminal state while
   `Sequencer.mu` still excludes a replacement `Start*`; never reintroduce `Unlock` followed by
   `publish`. Publishing under the lock is safe because the hub is non-blocking and never re-enters
   the sequencer. Preserve both guards: runtime probes in `publishatomicity_test.go` /
   `publishguard_test.go`, and `TestSource_NoStatusPublishedAfterUnlock` for unexecuted paths,
   aliases, and transitively publishing methods. A self-locking publishing method is exempt only
   when `s.mu.Lock()` is its first statement. Do not broaden this positive allowlist.

4. **A capture window must be attributable to one known frequency before any decode is displayed,
   spotted, or acted on.** Suppress a slot spanning a dial change as for a TX slot, including all
   sequencer and PSK Reporter consumers. Still publish the empty `ft8-decode` event so the slot
   clock advances. Observable: no decode row, spot, or sequencer advance from the mixed slot.

5. **A session ends only through operator abandon, disarm, its completion policy, or failed
   frequency confirmation, including an asynchronous pre-key failure.** `dialguard_test.go` is the
   executable specification; read it before changing this path. Zero tolerance is deliberate, and
   any dial change also disarms TX even without a session. The async refusal path uses
   `onDialRefusal` after any completion callback, then a generation-scoped `AbandonIfCurrent`, so a
   stale slot cannot kill a replacement session. Non-operator endings publish `end_reason`
   (`dial_moved` or `dial_unknown`); an idle `publishCurrent` must not overwrite that terminal
   event.

6. **Every path that ends a session uses `retireSessionLocked`.** The primitive advances the
   generation, consumes a staged teardown reason, clears ladder/contact state, and publishes the
   terminal status while the lock excludes a new start. Call-CQ completion is different: it resumes
   CQ rather than ending the run. Preserve `TestSource_SessionsEndOnlyThroughThePrimitive`: outside
   the retirement primitives, `seqIdle` is comparison/case-only, `.mode` assignments name an
   enumerated active mode, arithmetic/indirect idle writes are refused, and taking `&x.mode` is
   forbidden everywhere. These are positive structural rules; when changing a source guard, audit
   every protection in the previous version rather than replacing one axis and losing another.

7. **Offer a stop-RF control only on a rung where it can stop RF.** Skip-if-silent is a property of
   a pre-final rung, not of the session mode. `rungSkippableLocked` positively enumerates eligible
   rungs and defaults false, so a new mode must opt in deliberately. Refuse an ineligible active
   rung as `ft8_rung_not_skippable`, distinct from `ft8_no_active_qso`; never report a refused arm as
   armed. Disarm remains accepted.

## Adding a sequencer mode

Modes differ materially; use this coordinated-edit checklist rather than inventing a false common
abstraction. Add a mode to:

- `OnSlot` dispatch;
- `ActiveCallsign`;
- `rungSkippableLocked` (omission safely means not skippable);
- `abandonLocked` and `statusLocked`;
- `fireOpening`, or record the deliberate no-opening decision at the `Start*` function;
- the appropriate `completed*QsoLocked` snapshot; and
- Service-side staging in `servicetx.go`.

Maintain these structural rules:

- Put per-contact state inside `contactFlags`, whose zeroing at every start, retirement, and
  abandon resets it consistently. Keep longer-lived state outside: run state (`autoWork*`),
  retirement state (`confirmHold`), exclusion memory (`stalledCalls`/`stallCooloff`), and rig-slot
  state (`lastTxSlot`). Decide the lifetime before adding a field.
- Every active mode has exactly its matching exchange pointer (`seqAnswering` with `ex`,
  `seqWorking` with `caller`, etc.). Defensive nil checks do not make a missing pointer valid.
- Choose final-rung policy by protocol family. Group A means the partner already has a complete QSO,
  so log even if our courtesy closer fails: standard answer, FD work, type-4 answer. Group B means
  we still owe the roger, so retry and log only after true on-air success: standard work, Call-CQ,
  FD answer, type-4 work.

If a behavioral test needs a fact the system does not carry, add the fact. Do not substitute a
threshold, age check, or heuristic for attribution the system cannot actually establish.
