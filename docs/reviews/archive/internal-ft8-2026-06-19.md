# internal/ft8 code review - 2026-06-19

Scope: fresh review of `internal/ft8` as a new codebase, plus the adjacent API,
bridge, logging, config, frontend state, and FT8 documentation contracts that
can change FT8 runtime behavior. Reviewed at `d384fb3a`.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is a review artifact only; no production code was changed.

## Summary

The FT8 subsystem has good coverage around decode wiring, occupancy generation,
sequencer state transitions, TX single-flight behavior, QSO logging assembly,
static-build stubs, and SSE fanout. The current PSK Reporter sink is also
non-blocking from the decode loop (`internal/pskreporter/service.go:91-118`).

The main remaining risks are daemon-boundary validation issues. FT8 endpoints can
drive real RF, so the daemon should not rely on the SPA to provide safe offsets or
loggable dial frequencies.

## Findings

### M1 - TX endpoints accept out-of-passband or nonsensical audio offsets

`POST /v1/ft8/tx/send` passes `offset_hz` straight through to
`Service.TransmitNext` (`internal/api/handler_ft8_tx.go:18-23`,
`internal/api/handler_ft8_tx.go:51-56`). `TransmitNext` checks only that the
message encodes, then starts a transmission (`internal/ft8/servicetx.go:213-232`).
`EncodeToSlot` documents that errors are only for unencodable messages
(`internal/ft8/modulate.go:126-134`), and `Modulate` uses the supplied offset as
the base angular frequency with no passband or sanity check
(`internal/ft8/modulate.go:67-70`, `internal/ft8/modulate.go:89-113`).

The sequenced QSO endpoints have only a lower-bound check. `qso/start`,
`cq/start`, and `qso/work` forward the JSON offset directly
(`internal/api/handler_ft8_qso.go:73-74`,
`internal/api/handler_ft8_qso.go:118`, `internal/api/handler_ft8_qso.go:174-175`),
and the sequencers reject only `offsetHz <= 0`
(`internal/ft8/sequencer.go:199-202`,
`internal/ft8/caller_sequencer.go:23-26`,
`internal/ft8/work_sequencer.go:30-33`). A positive value outside the configured
picker passband, or above the 12 kHz sample rate's usable audio range, is still
accepted.

The frontend is not a sufficient guard. A restored localStorage value is parsed
without range validation (`frontend/logging/src/lib/states/ft8.svelte.ts:154-160`)
and any selected value is persisted (`frontend/logging/src/lib/states/ft8.svelte.ts:236-240`).
The docs say the picker passband defaults to 200-3000 Hz and that the strip is
guidance rather than an overlap admission gate (`docs/ft8.md:371-388`,
`docs/ft8.md:494-501`), but this is separate from refusing offsets that cannot
represent a valid transmit placement at all.

Impact: a direct API client, stale/corrupt browser localStorage, or future UI
regression can key the rig with an audio tone outside the intended FT8 passband.
For a hardware-facing internal API this is both a correctness and safety/security
boundary issue.

Recommendation: add one daemon-owned `ValidateTxOffset` path used by
`TransmitNext`, `StartQso`, `StartCallCq`, and `StartWorkCaller`. It should reject
non-finite values, values below the resolved passband low edge, and values where
`offset + signalWidthHz` exceeds the resolved passband high edge or sample-rate
Nyquist constraints. Map failures to a distinct 400 error. Add API and FT8 service
tests for zero, negative, very large, below-passband, and high-edge offsets. The
SPA should also drop/clamp restored localStorage offsets when a new passband
arrives.

### M2 - Sequenced QSOs can be accepted with an unloggable dial frequency

The QSO start requests carry `operating_freq_mhz`, but the handlers never validate
it (`internal/api/handler_ft8_qso.go:28-38`,
`internal/api/handler_ft8_qso.go:86-91`,
`internal/api/handler_ft8_qso.go:130-138`). The value is passed into the FT8
service unchanged (`internal/api/handler_ft8_qso.go:73-74`,
`internal/api/handler_ft8_qso.go:118`, `internal/api/handler_ft8_qso.go:174-175`).
The bridge TX readiness gate does not include a known dial frequency; it checks
only connected client, identity, and keyed-state flags
(`internal/bridge/ft8tx.go:289-301`).

At completion, `cmd/smd` prefers the bridge's current dial, but falls back to the
SPA-supplied `CompletedQso.DialFreqMHz` when the bridge has not decoded a frequency
yet (`cmd/smd/main.go:483-492`). `BuildQso` formats that value into ADIF `FREQ`
and derives `BAND` from it (`internal/ft8/qsolog.go:27-40`). The submit path then
rejects missing/invalid band or frequency values (`internal/qsoservice/submit.go:121-124`,
`internal/qsoservice/submit.go:175-182`).

The SPA correctly gates on `freqKnown` to avoid the old placeholder-frequency
failure (`frontend/logging/src/lib/ui/panels/Ft8Panel.svelte:197-203`,
`frontend/logging/src/lib/ui/panels/Ft8MsgPanel.svelte:48-61`), but the daemon
does not enforce the same invariant. A direct client, stale UI state, or malformed
request can receive 202 Accepted, let the on-air exchange complete, and only then
fail automatic logging.

Impact: the operator can make a real FT8 QSO that the daemon cannot log. Because
the failure happens after the final rung, the contact is already on air and the
data loss is hard to recover automatically.

Recommendation: before accepting `qso/start`, `cq/start`, or `qso/work`, require a
loggable frequency. Prefer a daemon-owned source: either expose bridge
`CurrentDialMHz` to the API handler and fail before starting if it is unknown, or
validate the supplied `operating_freq_mhz > 0` and that it maps to a recognized
band before committing the sequencer. Add handler tests for missing, zero,
out-of-band, and malformed frequencies on all three start paths.

### M3 - FT8 occupancy/passband config is not validated before it shapes TX picks

`config.Validate` validates many adjacent blocks but currently only covers FT8
display settings, not `ft8.tx.occupancy` (`internal/config/validate.go:56-78`).
The occupancy override resolver applies any non-zero passband, threshold, and
weight values without range checks (`internal/ft8/occupancy.go:121-150`). Those
values are then returned to the SPA and used to score clear-offset suggestions
(`internal/ft8/occupancy.go:157-168`, `internal/ft8/occupancy.go:416-470`).

Bad values do not necessarily panic, but they can produce invalid guidance:
negative passband lows can yield negative suggested TX offsets, high edges above
Nyquist can yield offsets the modulator will alias, `high <= low` yields no useful
suggestions, and negative or non-finite scoring weights/thresholds make ranking
unpredictable. The public docs list the knobs but not the accepted range
(`docs/ft8.md:514-524`).

Impact: configuration can make the clear-offset picker misleading, and today M1
means those invalid suggestions can also be transmitted if selected or restored.

Recommendation: add config validation for `ft8.tx.occupancy`: `0 < low < high`,
`high <= sampleRate/2`, passband width wide enough for at least one FT8 signal plus
the configured guard, finite positive threshold factor, finite non-negative
weights, and non-negative guard. Document those bounds and add config plus
occupancy tests for rejected values and edge cases.

### L1 - The selected-channel status reports "unknown" for a valid clear slot with no occupied bands

The daemon explicitly treats a silent slot as a valid occupancy report with no
occupied bands and still provides clear-offset suggestions
(`internal/ft8/occupancy_test.go:54-63`). The frontend handler records the incoming
slot and normalizes `occupied` to an empty array
(`frontend/logging/src/lib/states/ft8.svelte.ts:300-315`).

However, `channelOccupied` returns `null` whenever `occupied.length === 0`
(`frontend/logging/src/lib/states/ft8.svelte.ts:253-260`). That conflates "no
occupancy report has arrived" with "a valid report arrived and no bands are busy".
The docs promise grey unknown only when no offset is picked or no occupancy report
has arrived (`docs/ft8.md:294-301`). The current frontend test suite covers the
"no occupancy has arrived" case but not the "slot arrived with empty occupied"
case (`frontend/logging/src/lib/states/ft8.test.ts:180-217`).

Impact: on a quiet/clear slot, the active-contact banner can show "channel
unknown" instead of "channel clear", reducing the value of the
pick-time-to-TX-time safety indicator.

Recommendation: make `channelOccupied` use `slot === null` (or a dedicated
`hasOccupancyReport` flag) for the unknown state, and return `false` for a valid
report with an empty occupied list. Add a frontend state test for
`slot != null`, `selectedOffset != null`, and `occupied = []`.

### L2 - FT8 documentation and source comments disagree on shipped operator-pick and offset behavior

The state-store comment still says a restored occupied offset is harmless because
"the daemon TX gate ... refuses/snaps an overlapping offset at send time"
(`frontend/logging/src/lib/states/ft8.svelte.ts:146-151`). Current docs say the
opposite: overlap is guidance only and the daemon deliberately does not refuse or
snap overlapping picks (`docs/ft8.md:381-387`, `docs/ft8.md:497-501`). If overlap
admission is intentionally absent, the comment should not tell future maintainers
that the daemon enforces it.

`operator_pick` is also described inconsistently. The config type and early FT8
docs describe it as the pile-up stack / future settings behavior
(`internal/types/ft8.go:214-220`, `docs/ft8.md:77-83`), while the shipped pile-up
docs say the SPA stack supersedes daemon `operator_pick` (`docs/ft8.md:228-236`).
The implementation still rejects `operator_pick` with 501
(`internal/ft8/servicetx.go:367-373`), and the roadmap table still says the
`operator_pick` stack is pending (`docs/ft8.md:600-606`).

Impact: future changes can easily be made against the wrong contract: either
expecting daemon overlap enforcement that is not present, or assuming
`caller_answer_mode=operator_pick` activates the already-shipped SPA pile-up stack.

Recommendation: update the comments and `docs/ft8.md` to one current story:
overlap is guidance-only unless M1 adds a passband sanity gate, and
`caller_answer_mode=operator_pick` is unsupported/superseded by the SPA pile-up
stack until deliberately removed or redefined.

## Test coverage notes

Existing Go tests cover much of the FT8 critical path: decode WAV handling,
occupancy reports, HTTP/SSE fanout, sequencer ladders, caller-side sequencing,
work-a-caller sequencing, TX arming and single-flight behavior, static-build
stubs, and QSO assembly/logging. The gaps are specifically around boundary
validation: passband-safe transmit offsets, loggable operating frequencies, and
frontend clear-vs-unknown state for empty occupancy reports.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go vet ./internal/ft8 ./internal/api ./internal/bridge ./internal/qsoservice ./internal/config`
- `GOCACHE=/tmp/go-build go test ./internal/ft8 ./internal/api ./internal/bridge ./internal/qsoservice ./internal/config`
- `GOCACHE=/tmp/go-build go test -race ./internal/ft8 ./internal/api ./internal/bridge`
- `CGO_ENABLED=0 GOCACHE=/tmp/go-build go test ./internal/ft8`
- `npm test -- src/lib/states/ft8.test.ts` from `frontend/logging`

The first sandboxed Go test attempt failed because `httptest.NewServer` could not
bind localhost (`socket: operation not permitted`). The same focused Go suites
passed when rerun with localhost binding allowed.

## Resolution (2026-06-19)

All five findings fixed.

- **M1 (fixed — safety).** New `ErrTxBadOffset` + a daemon-owned
  `Service.validateTxOffset` called first by `TransmitNext`, `StartQso`,
  `StartCallCq`, `StartWorkCaller`: rejects non-finite offsets and any whose
  signal (`offset..offset+signalWidthHz`) falls outside the resolved occupancy
  passband (config-validated ≤ Nyquist by M3); offset 0 stays `ErrNoOffset`.
  Mapped to 400 `ft8_bad_offset` in both `writeFt8TxError` and
  `writeFt8QsoError`. The SPA's localStorage comment was corrected (it claimed
  the daemon snaps overlap; it doesn't — it now rejects out-of-passband). Tests:
  `TestValidateTxOffset_RejectsOutOfPassband`.
- **M2 (fixed).** `validFt8OperatingFreq` (positive + `utils.FrequencyToBand`
  resolvable) gates `qso/start`, `cq/start`, `qso/work` before committing the
  sequencer → 400 `ft8_no_frequency`, so an on-air exchange can't complete and
  then fail logging. (Chose handler-level validation of the SPA-supplied dial —
  the value used when the bridge hasn't decoded a freq — over coupling the
  handler to `bridge.CurrentDialMHz`.) Test:
  `TestHandleFt8Qso_RejectsUnloggableFrequency`.
- **M3 (fixed).** `validateFt8Occupancy` wired into `config.Validate`: bounds the
  passband (each edge independently; `low<high` + width ≥ one signal when both
  set; high ≤ 6 kHz Nyquist), threshold (finite positive), weights (finite
  non-negative), guard (≥ 0). Kept `internal/ft8` out of `config` (single-source:
  single-edge overrides resolve against the known-good default, so no duplicated
  constants). Test: `TestValidateFt8Occupancy_RejectsBadValues`.
- **L1 (fixed).** `channelOccupied` now returns `null` only when no offset is
  picked OR `slot === null` (no report yet); a report with an empty `occupied`
  list is `false` (clear), not unknown. Frontend test added for the
  slot-present + empty-occupied case (40 vitest pass).
- **L2 (fixed).** One current story: the SPA localStorage comment, `docs/ft8.md`
  (overlap is guidance-only **but** the daemon rejects out-of-passband offsets;
  the roadmap now says the operator-pick experience shipped as the SPA pile-up
  stack and the daemon `operator_pick` mode is superseded/501, not pending), and
  the `types.Ft8TXConfig.CallerAnswerMode` comment. `api-endpoints.md` updated
  with the new `ft8_bad_offset`/`ft8_no_frequency` codes (Tier-1, same commit).

The `writeSSEEvent` event-name note needs no action — production event names are
trusted constants.

Verified: `gofmt`/`go vet` clean; CGO-free `go build ./...`; `internal/ft8`,
`internal/api`, `internal/config`, `internal/qsoservice` pass; `go test -race
./internal/ft8 ./internal/api` clean; frontend `svelte-check` 0 + 40 vitest +
prettier clean.
