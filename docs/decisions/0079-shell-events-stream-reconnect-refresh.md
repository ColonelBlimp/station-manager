---
number: 0079
title: Open the general events stream from the shell so reconnection is CAT-independent
status: Accepted
date: 2026-09-07
---

# 0079 — Open the general events stream from the shell so reconnection is CAT-independent

## Context

The header's "Logbook (n)" count is seeded at boot and re-fetched after each QSO logged from the
app. Nothing refreshed it when the daemon restarted underneath the SPA — `smctl import` stops and
restarts the daemon with changed logbook contents and count — so the alpha.2 fresh deployment showed "(1)" over a
logbook of four until a reload (dogfood Finding #16). The SPA already detects a reconnection
transition (a transport error followed by a reopen) for the build-identity re-fetch (W-0004 AC3),
but only on the rig stream, which opens solely when CAT is enabled. With no rig configured — the
fresh-install case that produced the finding — the SPA held no stream at all: the FT8 stream opens
only on the FT8 view and `/v1/events` only while the map is open. There was nothing to reconnect.

The architecture already requires a reconnecting client to fetch a fresh SQLite-backed baseline:
the daemon's streams keep no backlog (`lib/api/log-events.ts`), so "reopened after a drop" is the
defined moment to re-query. The daemon caps event subscribers at a shared 16 per instance
(`defaultMaxEventSubscribers`, `internal/config/defaults.go`; `max_event_subscribers` in
`docs/v2-design/config.md`; enforced in `internal/api/limits.go`).

## Decision

`main.ts` opens `GET /v1/events` at boot unconditionally as the shell's always-on transport, and
`openLogEvents` gains a transport-specific `onReconnect` callback fired once per error → reopen
transition, never on the boot open. The header count re-fetch subscribes to it. The stream's QSO
events are not consumed by the shell, and build identity stays on the rig stream.

## Alternatives considered

### Rig-stream signal only, excluding no-rig operation from the criterion

Wire the count refresh to the existing rig-stream transition. Rejected because it leaves the
originating case unfixed: a station without CAT never opens that stream, so an import or restart
still leaves the count stale until a reload.

### A generic reconnection listener registry shared with build identity

One module owning "the" transition with a subscriber list. Rejected in favour of a
transport-specific callback on the stream that actually carries the signal: the two consumers sit
on different streams by ratified decision (build identity on the rig stream, W-0004 AC3), and a
registry would invite them to drift into one signal without a decision to do so.

### QSO-event-driven live updates

The same stream carries `qso.stored` / `qso.deleted`, which could keep the count live for QSOs
logged elsewhere. Deferred, not rejected: it is a separate behaviour with its own rate questions
(bulk deletes), outside the item that fixes the finding.

### Polling the count on an interval

Rejected: a schedule fetches when nothing changed and still misses the restart moment by up to one
interval; the reconnection transition is exact and free.

## Consequences

- One more long-lived SSE subscriber per open tab, against the daemon's shared cap of 16; the map
  tab already opens this stream, so a station with the map open holds two.
- A daemon restart now yields one count request per open tab when the stream reopens, and none
  otherwise.
- The production-boundary pin (`src/main.boot.test.ts`) imports the real `main.ts` with only
  `fetch` and `EventSource` stubbed and asserts at the request level: one seed at boot, one request
  per transition, none on a reopen without a new error, with the bridge disabled.

## Triggers to revisit

- If the subscriber cap is approached in practice (several tabs plus the map), multiplex the shell's
  streams (W-0013 lists this as trigger-bound) rather than dropping the always-on stream.
- If build identity should also recover without CAT, move its transition to this stream by a
  dated update to W-0004 AC3, not by widening this decision.
- If the count needs to be live across tabs, take up the deferred QSO-event alternative as its own
  item.

## References

- `frontend/app/src/lib/api/log-events.ts`, `frontend/app/src/main.ts`, `frontend/app/src/main.boot.test.ts`
- `docs/reports/dogfood-acceptance-v2.0.0-alpha.2.md` — Finding #16
- ADR 0045 (consumers never import api wrappers), W-0004 AC3 (build identity on the rig stream), ADR 0077 (SSE decode warnings)
