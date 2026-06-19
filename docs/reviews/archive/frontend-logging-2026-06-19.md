# frontend/logging code review - 2026-06-19

Scope: `frontend/logging` Svelte/Vite logging SPA, plus adjacent daemon contracts where
the frontend depends on them (`/v1/qso`, logbook/config, band validation, QSO edit).

Review posture: approached as a fresh package review. I read the package structure,
API wrappers, state modules, high-risk panels, tests, and the daemon contracts that
turn frontend state into persisted QSOs.

Focus areas: correctness, performance, security/privacy, test coverage, and
documentation.

## Findings

### M1 - QSO submit ignores the configured default logbook and always posts to logbook 1

`QsoPanel.svelte` still defines `DEFAULT_LOGBOOK_ID = 1` and submits every Phone/CW
QSO with that value (`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:41-47`,
`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:435`). That conflicts with the
state and API shape already present in the frontend: `/v1/config` exposes
`default_logbook` (`frontend/logging/src/lib/api/config.ts:28-32`),
`configState.applyResponse` hydrates `defaultLogbook.id`
(`frontend/logging/src/lib/states/config.svelte.ts:359-362`), and the header count
refresh already uses that configured ID
(`frontend/logging/src/lib/states/config.svelte.ts:494-501`).

The daemon contract makes this a data-routing bug, not just a future switcher
placeholder. `POST /v1/qso` requires an explicit `logbook` query parameter
(`internal/api/handler_qso.go:197-214`; `docs/v2-design/api-endpoints.md:49-55`),
and `qsoservice.Submit` rejects missing/nonexistent/mismatched logbooks after checking
that the target logbook callsign matches `STATION_CALLSIGN`
(`internal/qsoservice/submit.go:121-142`). If the configured default logbook is not
ID 1, the SPA can display/count one logbook while submitting into ID 1, or fail with
`logbook_not_found` / `callsign_mismatch`.

The existing tests do not cover the component-level logbook choice. `api/qso.test.ts`
only proves the helper uses the `logbookID` it is passed
(`frontend/logging/src/lib/api/qso.test.ts:24-48`), while `QsoPanel.test.ts` verifies
submit de-duping/re-enable behavior but never asserts the second argument passed to
`submitQso` (`frontend/logging/src/lib/ui/panels/QsoPanel.test.ts:75-147`).

Recommendation: submit with `configState.defaultLogbook.id` when it is nonzero, disable
or toast when it is missing, and add a `QsoPanel` regression test that sets
`configState.defaultLogbook.id = 7` and expects `submitQso(adif, 7, ...)`.

### M2 - Frontend band mapping can emit bands the daemon rejects

The frontend's `frequencyToBand` includes VHF/UHF bands through 23cm
(`frontend/logging/src/lib/utils/frequency.ts:16-45`), and tests explicitly bless
`2m` and `70cm` (`frontend/logging/src/lib/utils/frequency.test.ts:30-36`). The QSO
submit path writes that value into ADIF `BAND`
(`frontend/logging/src/lib/ui/panels/QsoPanel.svelte:361-376`), and the edit overlay
uses the same frontend mapping when updating the visible session row
(`frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte:250-259`).

The daemon accepts only HF plus 6m in `internal/enums/bands`
(`internal/enums/bands/bands.go:6-27`), and its own tests mark `2m` invalid
(`internal/enums/bands/bands_test.go:5-31`). `qsoservice.Submit` rejects an unknown
band with `invalid_field_value` (`internal/qsoservice/submit.go:140-143`), and
`qsoservice.Update` validates the merged band the same way
(`internal/qsoservice/update.go:111-116`). This means a user can enter a valid-looking
146 MHz or 435 MHz frequency in the logging SPA, see a correct-looking `2m`/`70cm`
label, and then have the daemon reject the QSO or edit.

Recommendation: make the supported band set a shared contract. The low-churn fix is
to restrict the logging SPA mapping to the daemon-accepted set until the daemon grows
the additional ADIF bands. If VHF/UHF support is intended, expand
`internal/enums/bands` and add an end-to-end submit/edit test for at least `2m`.

### M3 - Opening and saving an imported QSO silently drops HHMMSS time precision

`qsoEditState.populate` converts daemon `HHMMSS` values to `HH:MM`
(`frontend/logging/src/lib/states/qsoEdit.svelte.ts:49-60`) and tests explicitly pin
that seconds are dropped (`frontend/logging/src/lib/states/qsoEdit.test.ts:106-116`).
`toPatchBody` then always sends `time_on` and `time_off`
(`frontend/logging/src/lib/states/qsoEdit.svelte.ts:213-241`), so a no-op edit/save of
an imported or externally-created QSO with seconds rewrites `143215` as `14:32`.

The inline documentation says the opposite: "The PATCH round-trip preserves any
seconds the operator didn't touch" (`frontend/logging/src/lib/states/qsoEdit.svelte.ts:49-54`).
That makes the behavior easy to miss and turns an edit of unrelated fields, such as a
name or comment, into silent time precision loss.

Recommendation: either preserve the original raw `HHMMSS` value when the time field is
untouched, or document minute-only edit semantics and make the truncation visible in
the UI/tests as an intentional limitation.

### L1 - VFO swap mutates CAT mirror state before the rig command succeeds

The rig-control module documents confirm-by-push semantics for live CAT writes
(`frontend/logging/src/lib/actions/rigControl.ts:1-12`), but `swapVfoLive` updates
`catState.vfoB = catState.vfoA` before awaiting `sendRigCommand('swap_vfo')`
(`frontend/logging/src/lib/actions/rigControl.ts:54-70`). `driveRig` only toasts on a
non-ok outcome and does not roll that optimistic mirror back
(`frontend/logging/src/lib/actions/rigControl.ts:242-247`).

If the daemon rejects the command (`rig_not_connected`, identity failure, serial error,
etc.), the UI can retain a false VFO-B until a later rig-state refresh. The tests cover
the optimistic success/no-op branches but not the failed-command branch
(`frontend/logging/src/lib/actions/rigControl.test.ts:103-128`).

Recommendation: move the single-RX mirror behind an accepted command result, or keep a
rollback path on non-ok outcomes. Add a regression test where `sendRigCommand` resolves
to a validation/server outcome and `catState.vfoB` remains unchanged.

### L2 - Session storage retains full ADIF snapshots even though email no longer uses them

`SessionQso.adif` stores the full submitted ADIF record, including station identity and
operator-entered comments, in `sessionStorage`
(`frontend/logging/src/lib/states/sessionQsos.svelte.ts:51-58`,
`frontend/logging/src/lib/states/sessionQsos.svelte.ts:88-96`). The current email flow
does not need that snapshot: it posts UUIDs and the daemon rebuilds ADIF from the live
DB rows (`frontend/logging/src/lib/ui/panels/SessionEmailControls.svelte:45-59`,
`frontend/logging/src/lib/api/session-email.ts:15-21`).

This is not an XSS finding by itself, but it is unnecessary browser-side retention of
QSO and station PII. The code comments still describe the field as retained for
"potential offline/export use" even though no caller consumes it.

Recommendation: remove `adif` from the persisted session row, or split the persisted
shape from the in-memory shape so full ADIF is retained only when a concrete feature
needs it.

## Security review

No high-severity frontend security issue was found in this pass.

Positive observations:

- No `{@html}`, `innerHTML`, `eval`, `new Function`, or cookie access was found under
  `frontend/logging/src`.
- API calls use relative same-origin `/v1/...` paths, with `URLSearchParams` or
  `encodeURIComponent` for user-controlled query/path components
  (`frontend/logging/src/lib/api/qso.ts:64-71`,
  `frontend/logging/src/lib/api/qso-update.ts:109-154`,
  `frontend/logging/src/lib/api/enrichment.ts:109`).
- The QRZ external lookup encodes the callsign and opens with `noopener,noreferrer`
  (`frontend/logging/src/lib/ui/panels/DetailsPanel.svelte:34-46`).
- Error parsing is centralized and fail-soft through `safeFetch`, `readJsonBody`, and
  object guards (`frontend/logging/src/lib/api/_helpers.ts:58-125`).

Residual security/privacy risk:

- See L2 for unnecessary local retention of full ADIF snapshots in `sessionStorage`.
- Dependency vulnerability status was not checked with `npm audit` because that would
  require registry/network access in this environment.

## Performance review

No correctness-impacting performance issue was found.

Positive observations:

- Long-lived SSE state is scoped and cleaned up: bridge/FT8 EventSources are idempotent
  and close/reset state on teardown (`frontend/logging/src/lib/states/bridge.svelte.ts`,
  `frontend/logging/src/lib/states/ft8.svelte.ts:471-499`).
- FT8 decode history is bounded by `configState.ft8Display.historyMax`
  (`frontend/logging/src/lib/states/ft8.svelte.ts:348-353`), so the hot decode list
  does not grow unbounded.
- The QSO submit path has an in-flight guard, with tests for double-click and repeated
  Ctrl+Enter (`frontend/logging/src/lib/ui/panels/QsoPanel.test.ts:75-147`).

Notes:

- The production build succeeded but emitted Rolldown/Vite plugin timing warnings.
  The generated bundle sizes from the `/tmp` build were about `index.js` 216.60 kB
  (67.73 kB gzip) and `index.css` 460.60 kB (94.14 kB gzip). The CSS size appears
  dominated by current styling/assets rather than a broken code path.

## Test coverage

Coverage is broad for the current package shape: API wrappers, validators, utilities,
Svelte state modules, rig-control actions, FT8 SSE state, QSO edit state, session rows,
and several panel/component behaviors all have focused Vitest coverage.

Important gaps tied to the findings:

- No component test proves `QsoPanel` submits to `configState.defaultLogbook.id` instead
  of hardcoded `1` (M1).
- No cross-boundary test keeps the frontend band table aligned with daemon band
  validation (M2).
- The edit-state tests pin seconds truncation, but there is no no-op-save test that
  protects imported `HHMMSS` precision (M3).
- `rigControl.test.ts` lacks a failed `swap_vfo` branch that asserts optimistic CAT
  state is not left stale (L1).

## Documentation review

The codebase is heavily commented, often with useful ownership and contract notes. The
main documentation problems found are cases where the comments now conflict with code:

- `QsoPanel.svelte` and `docs/v2-design/frontend-spa.md:607` still describe logbook ID
  `1` as a deliberate placeholder, while the frontend already hydrates
  `configState.defaultLogbook.id` and the API endpoint requires an explicit logbook
  parameter.
- `internal/config/config.go:100-108` says the default logbook is used when the SPA
  does not supply `?logbook=N`, but the current handler rejects missing `?logbook`.
- `qsoEdit.svelte.ts:49-54` says seconds are preserved; the implementation and tests
  drop seconds.

## Verification

- `npm run check` - passed, 0 errors / 0 warnings.
- `npm run lint` - passed.
- `npm test` - passed, 51 test files / 836 tests.
- `npm run build -- --outDir /tmp/station-manager-logging-build --emptyOutDir` -
  passed; build output was written outside the repo.
- `npm run format:check` - failed. Prettier reports formatting issues in:
  `src/lib/ui/demo/Ft8PileupDemo.svelte`,
  `src/lib/ui/panels/Ft8MsgPanel.svelte`,
  `src/lib/ui/panels/Ft8Panel.svelte`.
- `GOCACHE=/tmp/go-build go test ./internal/api ./internal/qsoservice ./internal/enums/bands ./internal/utils`
  - first sandboxed run failed because `httptest` could not bind localhost
  (`socket: operation not permitted`); rerun outside the sandbox passed.

## Resolution (2026-06-19)

All five findings fixed; the `format:check` drift was also cleared. Operator
decision: M2 → expand the daemon's accepted band set to full VHF/UHF (make all
three layers coherent at the broad set), not restrict the SPA.

- **M1 (fixed — wrong logbook routing).** `QsoPanel` no longer carries a
  hardcoded `DEFAULT_LOGBOOK_ID = 1`; it submits with
  `configState.defaultLogbook.id` (already hydrated from `/v1/config`). A
  non-positive id (config not hydrated / setup incomplete) blocks the submit with
  a toast rather than posting an invalid `?logbook`. Tests: `QsoPanel.test.ts`
  asserts the configured id (7) is passed and that a 0 id blocks the POST; the
  existing in-flight-guard tests now seed `defaultLogbook.id = 1`. The stale
  "placeholder id=1" comment is gone, and the daemon's `DefaultLogbookID` doc
  (`config.go`) was corrected to say the SPA supplies the required `?logbook`
  (the daemon does NOT default a missing one).
- **M2 (fixed — band set divergence).** `internal/enums/bands` now accepts the
  VHF/UHF bands (`4m`, `2m`, `1.25m`, `70cm`, `33cm`, `23cm`) on top of HF+6m, so
  `bands.IsValidBand` matches what the SPA's `frequencyToBand` and
  `utils.FrequencyToBand` (expanded in the serial/utils review) emit — a 144 MHz
  dial labelled `2m` now stores instead of being rejected. (This also resolves the
  latent inconsistency that review left: the daemon was deriving VHF bands it then
  rejected.) Tests: `bands_test.go` (VHF/UHF valid), `qsoservice` end-to-end
  `TestSubmit_AcceptsVHFBand` (a 2m QSO stores). The SPA band table was already
  broad, so no frontend change was needed.
- **M3 (fixed — misleading time-precision docs).** Storage is minute precision
  by the sqlite `length(time_on)=4` CHECK and the service truncation added in the
  cmd/smd/utils review, so the daemon never emits `HHMMSS` and there are no
  seconds to lose. The `qsoEdit.svelte.ts` comments that claimed "the PATCH
  round-trip preserves any seconds" were corrected to state minute-precision
  semantics (the `HHMMSS` input branch is now documented as defensive tolerance).
  No behaviour change — the premise of silent precision loss was already moot.
- **L1 (fixed — optimistic CAT state not rolled back).** `driveRig` returns the
  command outcome; `swapVfoLive` captures the prior `catState.vfoB`, applies the
  optimistic mirror, and rolls it back when the daemon returns non-ok
  (`rig_not_connected`, identity failure, serial error) — so a rejected swap no
  longer leaves a false VFO-B until the next rig-state refresh. Test:
  `rigControl.test.ts` rejected-`swap_vfo` branch asserts the rollback.
- **L2 (fixed — unnecessary PII in sessionStorage).** Dropped the full-ADIF
  `adif` snapshot from `SessionQso` — it had no consumer (email-out posts UUIDs
  and the daemon rebuilds ADIF), so it was just QSO/station PII persisted in
  `sessionStorage`. Removed from the interface, both construction sites
  (`QsoPanel`, the FT8 `ft8-logged` path), and the test fixtures; the module doc
  was updated to record the deliberate omission.

Also cleared the **`format:check`** failure the review flagged (`prettier`
reformatted `Ft8PileupDemo.svelte`, `Ft8MsgPanel.svelte`, `Ft8Panel.svelte`).
The Tier-2 historical brief `docs/v2-design/frontend-spa.md` (the other
logbook-id=1 mention) was deliberately left frozen per the doc-map rule.

Verified: SPA `npm run format:check` / `check` / `lint` clean and `npm test`
green (51 files / 839 tests); `gofmt`/`go vet` clean; `internal/enums/bands`,
`internal/qsoservice`, `internal/api`, `internal/config`, `internal/utils` build
+ pass; `-race` clean on `internal/qsoservice` and `internal/enums/bands`;
`CGO_ENABLED=0 go build ./...` succeeds.
