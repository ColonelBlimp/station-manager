# Code Review: frontend/logging SPA

Date: 2026-06-04

Scope reviewed:

- `frontend/logging/src/lib/api`
- `frontend/logging/src/lib/states`
- `frontend/logging/src/lib/ui`
- `frontend/logging/src/lib/actions`
- Related backend contracts where needed for edit/QSO mode behavior

Verification:

- `npm run check` from `frontend/logging`: pass, `svelte-check` reported 0 errors and 0 warnings.
- `npm run lint` from `frontend/logging`: pass.
- `npm test` from `frontend/logging`: fail, 679 passed / 2 failed. Both failures are in `src/lib/ui/components/Vfos.test.ts` and assert that actionable VFO boxes should have `title="Select VFO"` while `VfoBox.svelte` now renders `Shift+Ctrl+[ / ]: Select VFO`.

## Findings

### High: Lookup/enrichment state is not scoped to the current callsign, so stale data can be logged

`QsoPanel.svelte` starts contact-history and enrichment requests in parallel after setting `qsoDraft.lookupCallsign` (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:166`). The response handlers unconditionally write global UI state and draft fields when the promises resolve:

- `fetchContactHistory(call).then(...)` updates `contactHistoryState` without checking that `call` is still the active callsign (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:196`).
- `enrichCallsign(call).then(...)` writes `enrichmentState` and auto-fills `qsoDraft.name` / `qsoDraft.qth` without a request token, abort signal, or callsign equality check (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:200`, `frontend/logging/src/lib/ui/panels/QsoPanel.svelte:211`, `frontend/logging/src/lib/ui/panels/QsoPanel.svelte:229`).
- On a non-OK enrichment outcome, the handler returns without clearing any previous enrichment result (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:205`).

Submit then reads the singleton enrichment state directly for ADIF and session-row data:

- `ANT_AZ` from `enrichmentState.activeBearing` (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:353`).
- `COUNTRY`, `CQZ`, `ITUZ`, `DXCC`, and `GRIDSQUARE` from `enrichmentState.result` (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:361`).
- Session-row country and distance from the same state (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:393`).

Failure mode:

1. Operator looks up `K1AAA`; enrichment succeeds and populates country/grid/bearing.
2. Operator types `N0BBB` and starts a new lookup.
3. The `N0BBB` lookup fails, or a slow `K1AAA` response resolves after the `N0BBB` request starts.
4. `N0BBB` can be submitted with `K1AAA` enrichment and possibly `K1AAA` name/QTH.

This can corrupt logged ADIF fields, not just display stale UI.

Recommended fix:

- Add a monotonic lookup request id or `AbortController` per lookup.
- Clear or mark enrichment/contact-history state as loading for the current callsign when a new lookup starts.
- Ignore every response unless its normalized callsign still matches the current draft/lookup callsign.
- At submit time, only consume enrichment when `enrichmentState.result?.callsign` matches `submittedCall`; otherwise omit enrichment fields.

Recommended tests:

- Slow lookup A resolves after fast lookup B; only B appears in draft/enrichment/contact history.
- Lookup B fails after lookup A succeeded; B submit does not inherit A's enrichment.
- Submit with mismatched `enrichmentState.result.callsign` omits country/grid/bearing.

### High: The QSO edit overlay can reopen with stale data after the user closes it

`SessionPanel.openEdit` guards against double-clicks only while `qsoEditState.open` is true, then awaits `fetchQso(uuid)` and always calls `qsoEditState.populate(outcome.qso)` on success (`frontend/logging/src/lib/ui/panels/SessionPanel.svelte:71`, `frontend/logging/src/lib/ui/panels/SessionPanel.svelte:74`, `frontend/logging/src/lib/ui/panels/SessionPanel.svelte:77`).

`qsoEditState.close()` resets `open`, `loading`, `uuid`, and all fields (`frontend/logging/src/lib/states/qsoEdit.svelte.ts:165`), but `populate()` later sets fields and flips `open = true` again (`frontend/logging/src/lib/states/qsoEdit.svelte.ts:117`, `frontend/logging/src/lib/states/qsoEdit.svelte.ts:141`). The comment above `beginOpen()` says the close path before `populate()` resolves is covered, but the implementation does not enforce that (`frontend/logging/src/lib/states/qsoEdit.svelte.ts:145`).

Failure mode:

1. Operator clicks a session row; the overlay opens in loading state.
2. Operator presses Escape/cancel before `GET /v1/qso/{uuid}` returns.
3. The GET resolves and `populate()` reopens the overlay with stale data.

A related race exists if a user closes the first row and opens another row before the first request resolves: the older response can overwrite the newer edit state.

Recommended fix:

- Pass an `AbortController` into `fetchQso` and abort it on close/new open, or
- Store an open token/current UUID and only call `populate()` when `qsoEditState.open && qsoEditState.loading && qsoEditState.uuid === uuid`.
- Make `populate()` itself defensive by accepting the expected token/uuid or refusing to reopen a closed overlay.

Recommended tests:

- Begin open, close before the mocked GET resolves, then resolve; overlay remains closed.
- Open row A, close/open row B, resolve A after B; row B remains loaded.

### High: Edit and session-list mode handling lose ADIF `SUBMODE`

The QSO submit path correctly resolves both ADIF `MODE` and `SUBMODE` (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:300`) and emits both (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:316`). The local session row, however, stores `displayedState.mode` (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:390`) even though `SessionQso.mode` is documented as the operator-facing submode form such as `USB` / `FT8` (`frontend/logging/src/lib/states/sessionQsos.svelte.ts:41`). For an SSB QSO, that means the session list can show `SSB` instead of `USB` or `LSB`.

The edit flow has the more serious version of the same bug:

- `DaemonQsoForEdit` includes `mode` but not `submode` (`frontend/logging/src/lib/api/qso-update.ts:68`).
- `QsoEditState.populate()` reads only `qso.mode` (`frontend/logging/src/lib/states/qsoEdit.svelte.ts:117`, `frontend/logging/src/lib/states/qsoEdit.svelte.ts:125`).
- `QsoEditState.toPatchBody()` sends only `mode`, not `submode` (`frontend/logging/src/lib/states/qsoEdit.svelte.ts:203`, `frontend/logging/src/lib/states/qsoEdit.svelte.ts:212`).
- The edit overlay mode dropdown lists operator-facing values such as `USB` and `LSB`, not ADIF parent mode `SSB` (`frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte:57`, `frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte:365`). The shared `Mode` component is a plain select over that list (`frontend/logging/src/lib/ui/components/Mode.svelte:24`).

The backend QSO details type has a real `Submode` field (`internal/types/qso_details.go:22`), and `qsoservice.Update` explicitly preserves JSON keys that are not present in the PATCH body (`internal/qsoservice/update.go:27`). Because the SPA omits `submode`, editing an existing `MODE=SSB, SUBMODE=USB` row can preserve stale submode data when the visible mode changes. For example, changing a row to `CW` can leave the old `SUBMODE=USB` in the merged backend object.

Recommended fix:

- Add `submode?: string` to `DaemonQsoForEdit`.
- Add `subMode` to `QsoEditState`.
- Populate the edit control from `submode || mode` for display.
- On save, convert the operator-facing selection with `resolveModeAndSubmode()` and PATCH both `mode` and `submode`.
- Store session rows with `displayedState.subMode || displayedState.mode`.

Recommended tests:

- GET fixture `{ mode: "SSB", submode: "USB" }` displays `USB`.
- Saving `USB` sends `{ mode: "SSB", submode: "USB" }`.
- Changing `USB` to `CW` sends `{ mode: "CW", submode: "" }` or otherwise clears submode daemon-side.
- Newly stored SSB QSOs show `USB`/`LSB` in the session list.

### Medium: QSO submit has no in-flight guard, allowing duplicate POSTs

`submitQso()` only checks `qsoDraft.canSubmit` before building ADIF and awaiting the daemon (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:275`, `frontend/logging/src/lib/ui/panels/QsoPanel.svelte:374`). The submit button calls `onSubmit` directly and is disabled only from the parent-provided `submitDisabled` prop (`frontend/logging/src/lib/ui/components/FormControls.svelte:39`). The global `Ctrl+Enter` shortcut can also invoke `submitQso()` repeatedly while the first request is still pending (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:641`).

Because the draft is cleared only after a `stored` response (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:405`), a double-click or repeated `Ctrl+Enter` can send multiple identical POSTs with the same draft values. Backend dedupe may reject some duplicates, but the SPA should not create concurrent submissions from one operator action.

Recommended fix:

- Add a local `submitting` `$state(false)` around the `await submitQsoToDaemon(...)` call.
- Return early when `submitting` is true.
- Disable the Log Contact button and keyboard branch while submitting.
- Reset `submitting` in `finally`.

Recommended tests:

- Double-clicking Log Contact sends one POST.
- Repeated `Ctrl+Enter` while the first promise is pending sends one POST.
- The button re-enables after stored, duplicate, validation, and network outcomes.

### Medium: CAT disconnect falls back to stale manual defaults instead of the last rig state

`manual.svelte.ts` documents a snapshot-on-disconnect rule: when the bridge transitions from connected to disconnected, `manualState` should adopt the most recent `catState` values for continuity (`frontend/logging/src/lib/states/manual.svelte.ts:30`). The bridge event handlers do not implement that snapshot:

- SSE `error` sets `bridgeState.connected = false` and `bridgeState.rigResponding = false` (`frontend/logging/src/lib/states/bridge.svelte.ts:166`).
- `rig-disconnected` eventually sets `bridgeState.rigResponding = false` (`frontend/logging/src/lib/states/bridge.svelte.ts:243`).

`displayedState` then switches from CAT fields to `manualState` fields whenever `isLive` becomes false (`frontend/logging/src/lib/states/displayed.svelte.ts:65`, `frontend/logging/src/lib/states/displayed.svelte.ts:87`, `frontend/logging/src/lib/states/displayed.svelte.ts:104`, `frontend/logging/src/lib/states/displayed.svelte.ts:113`). If `manualState` still contains localStorage/default values from an older session, the visible frequency/mode can jump when the transport drops. Any QSO logged during that outage can carry the wrong frequency/mode.

Recommended fix:

- Before flipping from live to not-live on error/disconnect, snapshot `catState.vfoA`, `catState.vfoB`, `catState.mode`, `catState.subMode`, `catState.selectedVfo`, split/power where applicable into `manualState`.
- Alternatively introduce a dedicated last-known displayed state used as the fallback while CAT is unavailable.

Recommended tests:

- With live CAT at a non-default frequency/mode, dispatch `error`; displayed state remains on the last CAT values.
- With live CAT at VFO B/split settings, dispatch `rig-disconnected`; selected VFO and split-derived values remain stable.

### Medium: `LoggingStationView` uses plain fields for values that are bound and derived

`LoggingStationView` says most ADIF station identity fields are plain properties because "Nothing reactively derives from these" and "`bind:value` works fine on plain fields" (`frontend/logging/src/lib/states/config.svelte.ts:100`). That is no longer true:

- `MyStationPanel.svelte` binds station callsign, owner callsign, operator name, grid square, and other station fields directly to `configState.loggingStation` (`frontend/logging/src/lib/ui/panels/MyStationPanel.svelte:334`, `frontend/logging/src/lib/ui/panels/MyStationPanel.svelte:360`, `frontend/logging/src/lib/ui/panels/MyStationPanel.svelte:378`).
- `enrichmentState.paths` derives from `configState.loggingStation.myGridsquare` (`frontend/logging/src/lib/states/enrichment.svelte.ts:46`).
- `configState.applyResponse()` mutates those plain fields after config load/save (`frontend/logging/src/lib/states/config.svelte.ts:274`, `frontend/logging/src/lib/states/config.svelte.ts:285`, `frontend/logging/src/lib/states/config.svelte.ts:290`).

In Svelte 5, plain class fields are not tracked like `$state` fields. The current code can therefore rely on incidental rerenders to update mounted inputs or derived path calculations after the daemon normalizes a config response. The most visible risk is `myGridsquare`: a daemon-normalized grid update may not recompute path distance/bearing until another reactive dependency changes.

Recommended fix:

- Make every form-bound/rendered `LoggingStationView` field `$state`, or store the whole logging-station object in a `$state` proxy and update it structurally.
- Remove the stale comment and add tests that `applyResponse()` updates rendered My Station fields and recomputes enrichment paths when only `myGridsquare` changes.

### Medium: Successful API responses are often only checked for "object", then cast to contract types

Several API wrappers reject non-object 200 responses but accept arbitrary objects as successful contracts:

- `/v1/config` returns `kind: 'ok'` for any plain object and casts it to `ConfigResponse` (`frontend/logging/src/lib/api/config.ts:219`, `frontend/logging/src/lib/api/config.ts:230`). `applyResponse()` then dereferences required nested fields such as `resp.logging_station.operator` (`frontend/logging/src/lib/states/config.svelte.ts:281`).
- `/v1/enrich/callsign` casts any plain object to `EnrichmentResult` (`frontend/logging/src/lib/api/enrichment.ts:117`, `frontend/logging/src/lib/api/enrichment.ts:125`).
- `/v1/qso/{uuid}` casts any plain object to `DaemonQsoForEdit` (`frontend/logging/src/lib/api/qso-update.ts:116`, `frontend/logging/src/lib/api/qso-update.ts:128`).
- Contact history treats malformed 200 responses as an OK empty history (`frontend/logging/src/lib/api/contact-history.ts:64`).

This means a daemon/proxy regression can become either a SPA crash or a false empty state instead of a controlled `server/malformed_response` outcome.

Recommended fix:

- Use endpoint-specific shape guards for required top-level fields.
- Treat malformed 200 responses as `kind: 'server'` with a clear code/message.
- Keep permissive optional-field parsing only after required container objects are validated.

Recommended tests:

- `/v1/config` 200 `{}` returns `server/malformed_response`, not `ok`.
- `/v1/enrich/callsign` 200 missing `callsign` returns `server/malformed_response`.
- Contact history 200 `{}` reports malformed response instead of silently clearing history.

### Low: Enrichment source types/comments are stale

`enrichment.ts` documents `country_source` as `"hamnut" | "cache" | "none"` (`frontend/logging/src/lib/api/enrichment.ts:4`) and the TypeScript interface repeats the same union (`frontend/logging/src/lib/api/enrichment.ts:81`). The current daemon lookup contract uses source names such as `country_table` and `contacted_station`; `station_source` is already typed as a generic string, but the country source is not.

This is not currently breaking the UI because the SPA mostly displays resolved country/station data rather than branching on the literal source. It is still contract drift and will mislead future code that tries to use the typed source.

Recommended fix:

- Update comments and the union to the daemon's real source values, or type the field as `string` plus known constants if the backend intentionally keeps source names extensible.
- Add a fixture covering the daemon's actual enrichment response.

### Low: VFO tooltip text disagrees with tests and the exposed keyboard hint

`VfoBox.svelte` renders `title={interactive ? 'Shift+Ctrl+[ / ]: Select VFO' : null}` (`frontend/logging/src/lib/ui/components/VfoBox.svelte:51`). The component tests still expect `Select VFO` for actionable boxes (`frontend/logging/src/lib/ui/components/Vfos.test.ts:339`, `frontend/logging/src/lib/ui/components/Vfos.test.ts:447`), which is why `npm test` currently fails.

There is also a product/a11y mismatch around the hint:

- VFO boxes are `role="button"` but intentionally `tabindex={-1}` (`frontend/logging/src/lib/ui/components/VfoBox.svelte:45`).
- The tests document that Tab should skip the boxes and mention a planned `Ctrl+\` shortcut (`frontend/logging/src/lib/ui/components/Vfos.test.ts:416`).
- The tooltip points to `Shift+Ctrl+[ / ]`, which is not the direct select shortcut represented by the test name.

Recommended fix:

- Decide the intended operator shortcut and make `VfoBox`, `Vfos.test.ts`, and `QsoPanel` keyboard handling agree.
- If boxes remain intentionally skipped in tab order, avoid advertising focus-only key behavior on the box itself.

## Review Notes

The SPA has good local separation between API wrappers, state objects, and Svelte UI components, and the existing test suite is broad enough to catch regressions like the VFO tooltip mismatch. The highest-risk gaps are not static type errors; they are cross-request state races and contract mismatches where valid operator workflows can produce incorrect logged QSO data.

## Resolution — L2 + Batch E (2026-06-04)

All 9 findings were independently re-verified against the source (three
read-only passes). Two calibration notes: **L1 was already fixed** in the prior
lookup-review remediation (`country_source` is now `'country_table'`), and
**L2 was a live regression** — the committed VfoBox tooltip change had broken 2
`Vfos.test.ts` assertions, so `npm test` was red on main.

Fixed this pass (`npm run check` 0 errors, `lint` clean, **687 tests pass**,
`build` OK):

- **L2 — fixed (the build was red).** VfoBox's tooltip was `'Shift+Ctrl+[ / ]:
  Select VFO'` — wrong twice over (those are band-step keys, not VFO-select;
  the box's shortcut is Shift+Ctrl+\\) and breaking 2 tests. Reverted to the
  accurate `'Select VFO'`; suite green.
- **H3 — fixed (data integrity + a broken feature).** Added `submode` to
  `DaemonQsoForEdit`; `QsoEditState.mode` now holds the operator-facing form
  (populate = `submode || mode`, so an SSB QSO shows "USB" rather than a blank
  dropdown), and `toPatchBody` resolves it back through `resolveModeAndSubmode`
  to PATCH **both** `mode` and `submode` — `submode: ''` explicitly clears a
  stale submode on a mode change (SSB→CW), which the daemon's PATCH-merge
  honours (present-empty key; verified against `update.go`). Session rows now
  store `displayedState.subMode || displayedState.mode` (QsoPanel +
  QsoEditOverlay, via the now-friendly `qsoEditState.mode`). Tests in
  `qsoEdit.test.ts` cover all three: submode shows + resolves, submode-less
  mode shows the parent, mode-change clears submode.
- **H1 — fixed (silent ADIF corruption).** `runLookup` scopes each lookup with
  a monotonic token (stale / out-of-order responses are ignored) and clears the
  prior call's enrichment + contact-history immediately. The definitive guard
  is at submit: `enrichmentState.resultForCallsign(submittedCall)` returns the
  result only when it belongs to the call being logged, else null — so
  country/CQZ/ITUZ/DXCC/grid/bearing and the session row's country/distance are
  consumed only for the matching callsign. `resultForCallsign` is a pure method
  unit-tested in `enrichment.test.ts` (match / mismatch / empty). A full
  QsoPanel component test of the lookup-token race is a possible follow-up; the
  data-integrity guard itself is unit-tested.

**Remaining (as of this pass):** Batch F (M1 submit in-flight guard, H2
edit-overlay AbortController) — done in the next section — then Batch G (M2
disconnect snapshot, M3 `LoggingStationView` `$state`, M4 API shape guards).
None data-corrupting; M3/M4 are latent. L1 is done.

## Resolution — Batch F (2026-06-04)

The two open-backlog items targeting concurrent operator actions. Fixed this
pass (`npm run check` 0 errors / 0 warnings, `lint` clean, **692 tests pass**
across 44 files, `build` OK):

- **M1 — fixed (concurrent duplicate POSTs).** `QsoPanel` gained a local
  `submitting = $state(false)`. `submitQso` early-returns when it is set, flips
  it true before building/awaiting, and resets it in a `finally` so every
  outcome (stored / duplicate / validation / server / network) re-enables. The
  Log Contact button now disables on `!qsoDraft.canSubmit || submitting`; the
  `Ctrl+Enter` branch needs no extra check — it calls `submitQso`, which
  early-returns while a submit is outstanding (the internal guard is the
  authoritative one, mirroring H1's submit-time guard). New `QsoPanel.test.ts`:
  double-click → 1 POST, repeated `Ctrl+Enter` while in flight → 1 POST, button
  re-enables after the round-trip resolves.
- **H2 — fixed (stale edit-overlay reopen + cross-row clobber).**
  `SessionPanel.openEdit` now threads an `AbortController` (aborting the prior
  open's GET when a new open starts) and — the load-bearing defence — gates
  `populate()` on `qsoEditState.open && qsoEditState.uuid === uuid`, so a late
  GET can neither re-open a dismissed overlay nor overwrite a newer open. The
  `'aborted'` outcome (already on `FetchQsoOutcome`/`safeFetch`) is a no-op; the
  error paths' unconditional `close()` is safe because a stale error can only
  resolve while the overlay is already closed (a newer open aborts the prior GET
  first; a GET that completed pre-abort does so before the next open's click
  runs). The misleading `beginOpen` doc comment in `qsoEdit.svelte.ts` — which
  claimed `close()` covered the race — was corrected to describe the actual
  guard. New `SessionPanel.test.ts`: close-before-resolve stays closed; open A /
  close / open B / A resolves late → B stays loaded.

**Remaining (after Batch F):** Batch G — M2 (CAT-disconnect snapshot), M3
(`LoggingStationView` `$state`), M4 (API shape guards) — done in the next
section, which closes the review.

## Resolution — Batch G (2026-06-04)

The final three items, all Medium. Fixed this pass (`npm run check` 0 errors /
0 warnings, `lint` clean, **701 tests pass** across 44 files, `build` OK). This
**closes the frontend-logging SPA review** — all 9 findings resolved (L1 was
already fixed pre-review; L2/H1/H3 in Batch E; M1/H2 in Batch F; M2/M3/M4 here).

- **M2 — fixed (stale CAT-off fallback on disconnect).** `bridge.svelte.ts` now
  snapshots the rig's last-known state into `manualState` on the two
  involuntary-disconnect paths (the transport `error` handler + the genuine-
  outage disconnect timer), BEFORE the `rigResponding` flip so displayedState
  recomputes once against the snapshot (no flash). This implements the rule
  `manual.svelte.ts` already documented. Mode is stored in the operator-friendly
  form (`subMode||mode` of the mapped ADIF pair — the value QsoPanel's mode
  mirror writes), so `resolveModeAndSubmode` round-trips it on read. Guarded on
  `rigResponding` so a disconnect before any `rig-state` can't clobber the
  operator's manual edits with default catState. **Scope notes (validated
  against the state shape, not oversights):** split has no manualState slot — it
  is carried implicitly via the VFO pair (a same-frequency split stays
  inexpressible, the documented ADR-0009 limitation); power has no manualState
  slot either — CAT-off power falls to `configState.station.defaultPower` by the
  session-36 design. New `bridge.test.ts` cases: snapshot on error, mode-mapping
  friendly form, snapshot on genuine outage, NO snapshot on in-window recovery,
  NO clobber before any rig-state.
- **M3 — fixed (plain fields not reactively tracked).** Every
  `LoggingStationView` field is now `$state` (was: only `operator`). The
  highest-risk was `myGridsquare`, which feeds `enrichmentState.paths`
  (`$derived`) — a plain field meant a daemon-normalised grid (re-hydrated by
  `applyResponse`) never recomputed path distance/bearing, and mounted My
  Station inputs wouldn't reflect the canonical value. Rewrote the stale
  "nothing derives from these / plain is fine" comment. New `enrichment.test.ts`
  case verifies paths recomputes when only `myGridsquare` changes after the
  first read — **empirically confirmed a real regression test** (it fails with
  the field reverted to plain).
- **M4 — fixed (object-only cast → crash / false-empty).** Endpoint-specific
  `isShape` guards so a malformed 200 becomes a controlled
  `server/malformed_response` instead of a crash or a misleading empty:
  - `/v1/config` now requires `logging_station` / `default_logbook` /
    `default_rig` (the containers `applyResponse` dereferences unconditionally —
    a `{}` would have crashed hydration). station / bridge / mailer stay optional
    (applyResponse already guards each).
  - `/v1/enrich/callsign` now requires `callsign` (the identity the H1
    `resultForCallsign` guard keys off); country / station stay omitempty.
  - `/v1/contact-history` now distinguishes a genuine empty (`{items: []}` → ok)
    from a malformed 200 (non-object / no items array → server). **This reverses
    my Batch-F note that the "false empty" was by-design:** on review, the
    fail-soft that actually matters (logging never blocked) is preserved —
    `runLookup` still ignores any non-ok contact-history outcome, so the panel
    stays empty — while the wrapper no longer conflates a daemon regression with
    "never worked them". The three former "falls through to items=[]" tests were
    updated to assert the new malformed outcome.
  - `/v1/qso/{uuid}` (qso-update) was **validated, no change**: it already
    guards non-object → malformed, and its `populate` is fully `?? ''`-tolerant,
    so a plain-but-incomplete object yields a safe empty edit form rather than a
    crash or corruption. The review listed it as least-problematic; that holds.

Review complete — all 9 findings resolved across Batches E / F / G (+ the
pre-review L1).
