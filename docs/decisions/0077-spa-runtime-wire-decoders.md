---
number: 0077
title: SPA runtime wire decoders — validate success and safety-state boundaries per endpoint/event
status: Accepted (operator-ratified 2026-08-31)
date: 2026-08-31
---

# 0077 — SPA runtime wire decoders at the success + safety-state boundaries

## Context

W-0008 frontend-wire finding **F-03** (`docs/reviews/frontend-app-review.md`): the consolidated
SPA's API layer is documented as the last runtime trust boundary, but several endpoints and SSE
events cast a `JSON.parse` result straight to its TypeScript type with no shape check. Malformed-
but-valid JSON (version skew, a proxy, a daemon regression) then produces four harms:

- **False success.** `qso.ts:105` classifies any 2xx `status` that is not literally `"duplicate"`
  as `"stored"`, so `{status:"unexpected", uuid}` clears the operator's draft and adds a phantom
  session row; the same shape gap lets a malformed `ft8-logged` mirror inject a phantom row.
- **Silently corrupted safety state.** `rig-sse.ts` / `ft8-sse.ts` cast blindly, and the payloads
  feed *safety-relevant* UI: `tx-alarm.active` (stuck-TX alarm, ADR 0051), `drive-alarm.active`,
  `tune-state.active` (ADR 0027), `ft8-tx.transmitting`, `ft8-audio-level` (TX stand-down), and
  `rig-state` freq/mode (which feeds the *logged* QSO). A wrong-typed scalar hides the true state
  and never throws, so nothing surfaces the corruption.
- **Listener/render throws.** Array fields spread or `#each`'d by consumers — `ft8-decode.decodes`,
  `ft8-occupancy.occupied/suggested`, `ft8-qso.answerers/queue` — throw inside the EventSource
  listener or mid-render when handed a non-array (`?? []` does not defend a present non-array).
- **Store corruption.** `fetchLogbooks`/`fetchQsoPage` cast elements unchecked; `patchQso`/`fetchQso`
  accept any non-null object (an array included) as a QSO, which is written into the long-lived
  `rows` store — and a uuid-less or duplicate-uuid row breaks the uuid-keyed rendering (AW-1).

The existing tests cover invalid-JSON-dropped but never valid-JSON/wrong-shape.

## Decision

Introduce **small, concrete per-endpoint / per-event decoders** — not a single permissive cast and
not a generic schema library. They delegate to the existing `_helpers.ts` primitives
(`isPlainObject`, which excludes arrays; `isShape`) and generalize the one existing runtime guard
(`txDrive.svelte.ts:68`). A decode failure **drops the frame/record and leaves the last known good
state intact**; it never defaults a value into place.

**Submit + success boundaries (`qso.ts`, `ft8-logged`).** A 2xx `status` must be exactly `"stored"`
or `"duplicate"`; anything else (or a missing/empty `uuid`) becomes the existing `malformed_response`
outcome (`kind:'server'`), which the caller already routes to draft-preserving refusal — **no session
row**. `body.status` is authoritative: an HTTP-code↔status mismatch (e.g. 200 with `"stored"`) is
classified by `status`, with a diagnostic, rather than hard-failed — a false-malformed on a genuinely
stored QSO would drive a retry into the duplicate dialog and a forced double-write. `ft8-logged` is a
success boundary too: require a non-empty `uuid` and a usable callsign, or drop it (no phantom row).

**RF/FT8 SSE safety events.** Each event gets a decoder validating its **complete load-bearing
structure**, not merely array-ness: the safety scalars (`tx-alarm`/`drive-alarm`/`tune-state.active`,
`ft8-tx.armed/transmitting`, `ft8-qso.active`, `ft8-audio-level` numbers, `rig-state` freq/mode,
`vfoB`/`selectedVfo`, rig meters), the array fields **and their element shapes**
(`decodes[]`, `answerers[]`/`queue[].{call,snr}`, `occupied[]`/`suggested[]`), and the FT8
slot/passband/dial fields. A frame failing any load-bearing check is dropped; the previous state
stands. Defaulting is rejected specifically because defaulting `alarm.active` to `false` would clear
a real alarm.

Three strictness rules sharpen "load-bearing", each tied to the daemon's wire contract:

- **Always-sent scalars are required, not merely typed-if-present.** `ft8-tx.armed`/`transmitting`,
  `ft8-qso.active`, and `rig-meters.{meter,value}` carry no `omitempty` — the daemon marshals them
  on every frame — so a frame that omits one is malformed and dropped. Accepting it would let the
  consumer's `?? false` clear a live arm/transmit/session to idle, or dispatch a half meter reading.
- **Enumerated fields are checked against their value set, not just their type.**
  `rig-state.selectedVfo` must be `A`/`B` (the consumer's field is typed `'A' | 'B'`); an FT8 slot
  `period` must be `even`/`odd`. `rig-state` is a partial merge, so it must also carry at least one
  field this build models — an empty or unknown-only frame is dropped, not merged as a silent no-op
  (the SPA ships embedded with its daemon, so a real frame always names a known field).
- **Consumed strings are string-checked, and `ft8-logged` rejects whitespace-only identity.** Every
  `ft8-qso` string the state module reads is validated (`their_call` and `end_reason` reach `.trim()`,
  which throws on a non-string); `ft8-logged` requires a non-whitespace `uuid` and callsign, since a
  blank uuid cannot dedup a session row and a blank callsign cannot key one.

**List/page/edit (`logbooks.ts`, `qso-patch.ts`, `fetchQso`).** Per-record decode: a page
`LogbookQso` needs a non-empty string `uuid`; a `Logbook` needs numeric `id` + string `name` +
string `callsign`. Invalid elements are dropped (valid ones kept); **page uuids and logbook ids are
deduplicated** so keyed rendering cannot throw on a duplicate key; `next_cursor` is accepted only as
exactly `string` or `null` — any other type stops pagination (the safe side: never page past a
cursor we cannot parse) and warns, rather than discarding the page's valid rows. A single-object
response (`patchQso`/`fetchQso`) must be a plain object
whose `uuid` **equals the requested uuid**, else it is an error — never a QSO written to the store.

**Diagnostics (deterministic, no time interval).** SSE: at most one warning per `(event label,
rejection reason)` per logical stream subscription, reset when the stream closes and reopens. HTTP
list filtering: one aggregated warning per response summarizing how many records were dropped.

## Consequences

- A malformed frame can no longer silently corrupt or clear a safety state, throw in a listener or
  mid-render, create a phantom logged/session QSO, or inject an unusable row into a long-lived store.
- On a dropped SSE frame the state is briefly stale — on the safe side for alarms (a real alarm keeps
  showing; only a well-formed clear retires it).
- Cost: a decoder + a valid-JSON/wrong-shape test per boundary (many of these paths have no tests
  today). Decoders stay endpoint-specific, so a wire-shape change touches exactly one decoder.
- Diagnostics are bounded and reproducible: a broken stream logs each distinct failure once per
  subscription, not once per frame.

## Alternatives considered

- **A generic schema/validation library (e.g. zod).** Rejected: a new dependency and a less explicit
  contract for a bounded, safety-specific set of boundaries; the project prefers concrete
  implementations over speculative frameworks. The per-boundary decoders read as the wire contract.
- **Per-field defaulting on SSE instead of dropping the frame.** Rejected on safety grounds:
  defaulting `tx-alarm.active`/`drive-alarm.active`/`tune-state.active` to `false` on a malformed
  frame would actively *clear* a real safety alarm. Dropping preserves last-known-good.
- **Strict HTTP-code↔status pairing on submit.** Rejected: a benign proxy code-swap on a genuinely
  stored QSO would false-report `malformed`, steering the operator into a retry → duplicate dialog →
  forced double-write. `body.status` (the daemon's explicit classification) is authoritative.
- **Reject the whole list/page response on one bad element.** Rejected: it hides every valid record
  behind a single malformed one; drop-the-element keeps the good rows.
- **A time-interval log throttle.** Rejected as non-deterministic; one warning per `(event, reason)`
  per subscription (reset on reopen) and one aggregated warning per HTTP response are deterministic
  and testable.

## Relationship to other work

- **W-0008 Slice 4 (frontend-wire), finding F-03.** Implemented in that slice: F-03a submit
  first, then F-03b RF/FT8 SSE (including the `ft8-logged` success boundary), then F-03c
  list/page/edit. F-04 (ambiguous-write outcome-unknown policy) is the sibling finding and is out
  of scope here.
- **AW-1 (ADR 0016).** `LogbookQso.uuid` is required and rendering/selection key on it, which is why
  the per-record decoder requires a non-empty uuid and deduplicates page uuids.
