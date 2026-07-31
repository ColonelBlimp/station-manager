---
number: 0060
title: Alert surfaces sort by event-vs-state, never shift the working surface, and only tx_still_keyed takes the screen
status: Proposed
date: 2026-07-31
---

# 0060 — Operator alert surfaces: event-vs-state, no layout shift, and a single emergency overlay

## Context

An audit of every operator-facing warning surface in `frontend/app` (2026-07-31)
found the tiering is already sound but the *placement* is not. The app sorts
alerts on a good axis — a toast reports **an event that happened**, a banner
reports **a state that is still true** — and `DriveMonitorNotice` correctly has
no Dismiss button because that state ends observably and hiding it would hide
something still true.

The placement is the problem. `Toasts.svelte` is `fixed inset-0 z-50` and says
so in a comment: *"never reflows the working surface."* But the three shell
alerts — `TxAlarmBanner`, `DriveAlarmBanner`, `DriveMonitorNotice` — render **in
document flow** in `App.svelte`, between `<Header/>` and `<main>`, each a
`border-b px-4 py-2` strip. All three can be active at once, so the working
surface can be pushed down by up to three rows. The timing is the worst case:
the drive alarm raises **mid-slot, with ~9 s of a 12.6 s FT8 slot still to
run** — content jumps down while the operator is reading it, and jumps back up
on dismiss. The surfaces that matter most are the only ones that move the page.

Operator constraint (2026-07-31): **an alert may overlay, but must never shift
content up or down.** The `Header` is a candidate host — `sticky top-0 z-40 flex
h-16 shrink-0`, fixed height, with a large contiguous empty centre (the
session timer leads; the station-identity block's `ml-auto` absorbs all free
space to its left, and the rig chip's `ml-auto` is overridden to `ml-4` at
`sm`+). It is already permanently reserved chrome, so hosting an alert there
costs zero shift by construction — the pattern the `rigGate()` dot already
validates.

Separately, this is **not yet decided**. Some of these messages were seen in
live operating on 2026-07-31, and the operator wants several more runs to
observe how the current surfaces actually behave before committing. This ADR
records the reasoning while it is fresh; it is not a mandate to build.

## Decision

**Proposed, not accepted.** The direction under consideration:

1. No alert surface participates in document flow — nothing may shift the
   working area in either direction.
2. **Only `tx_still_keyed` earns a blocking emergency overlay** ("forget
   everything else, deal with this now").
3. Every other alert state — the remaining four TX alarm codes, the drive
   alarm, the drive-monitor notice — is hosted in the header's empty centre.

## Alternatives considered

### Keep the in-flow banners (status quo)

Rejected on the operator constraint. The mid-slot jump is the specific harm:
an alert that moves the thing the operator is looking at, at the moment they
most need to look at it.

### Overlay all three over the working surface

Rejected by the operator. These are dismiss-until-resolved states, not
transients, so an overlay would sit over the working area indefinitely.

### Permanently reserve an alert strip in the shell

Never shifts in either direction, but pays the vertical space at all times —
including the overwhelming majority of the time nothing is wrong. Rejected by
the operator.

### A sticky corner toast (`ttl: 0`)

ADR 0008 already supports non-expiring toasts, so no new mechanism would be
needed, and a corner costs far less working surface than a full top edge. Still
overlays the working area, and conflates a standing state with the transient
event queue — the exact distinction the axis exists to preserve. Weakly
considered; available as a fallback if the header centre proves too small.

### Header centre for everything, including stuck-TX

Rejected: the stuck-TX alarm is deliberately the loudest thing in the app
because the intended response is *get up and look at the radio*. A chip in
shared chrome cannot carry that, and it would sit beside calmer conditions that
call for no action at all.

### Emergency overlay for all five TX alarm codes

Rejected on confidence, not severity. `tx_still_keyed` is the rig
**affirmatively answering** the status query with "CAT TX on" — near-certain,
and the case `txconfirm.go:330` logs as `CHECK YOUR RADIO`. The other four
(`tx_unconfirmed`, `tx_liveness_lost`, `tx_teardown_unconfirmed`,
`tx_key_write_failed`) all mean *we could not confirm*, which is a different
claim; a CAT read timeout raises them. A full-screen takeover on a transient CAT
hiccup mid-contest is expensive, and false alarms here are not hypothetical —
`DriveMonitorNotice` exists precisely because two false NO RF OUTPUT alarms
fired while RF was leaving the rig normally (2026-07-31).

## Consequences

**A daemon change is required, and it is the load-bearing one.**
`raiseTxAlarm` publishes only on the false→true edge (`if !already`). So once an
alarm is latched, a later raise with a *different* code changes nothing the SPA
can see. That is exactly the sequence this split cares about: an unkey times out
→ `raiseTxAlarm(TxAlarmUnconfirmed)` → header chip; `startAlarmProbes()` keeps
querying; a probe returns TXSTATUS `"1"` → `raiseTxAlarm(TxAlarmStillKeyed)` is
**suppressed**. The daemon logs `CHECK YOUR RADIO`, calls
`retryUnkeyStillKeyed()`, and the operator's screen still shows the quiet chip.
The escalation from *"I can't confirm"* to *"the rig says it is transmitting"*
is the moment the overlay exists for, and it is currently the one moment that
cannot be reported. Promotion must publish; **demotion must not** —
`tx_still_keyed` may never quietly fall back to a chip on a later inconclusive
probe.

Note this is not a defect today: with all five codes rendering one identical
banner, a suppressed code change is invisible *and* harmless. This decision is
what turns the code into a tier selector and makes the gap load-bearing.

**The overlay is safer than a blocking modal usually is.** The stuck-TX alarm
has a real daemon clear — `txconfirm.go:230` publishes `active=false` once the
transmitter is confirmed idle — so the overlay can remove itself when the
emergency is genuinely over. The operator is never trapped behind a takeover for
a resolved problem.

**Precedence dissolves rather than needing design.** With the emergency out of
the header entirely, the calmer conditions can share the centre with no risk of
masking the one that matters.

**Costs accepted.** Banner text must compress: `TxAlarmBanner` currently carries
a code, a recheck note, and two buttons (Recheck + Dismiss), which will not fit
64 px of shared chrome — so header-hosted alerts become chips with detail on
click, following the rig chip's existing "chip in header, panel below" idiom.
The header centre is also narrower below `sm`, where the station-identity block
is `hidden`.

**The drive alarm still cannot clear honestly.** `publishDriveRecovery` sends
`active=false`, but the SPA deliberately does not clear — `drivealarm.go` states
the reason: *"the operator asked to be told the rig is fine now without losing
the record that it was not."* The banner is standing in for a **log entry**.
Nothing in this ADR fixes that; consolidated logging would, by giving the record
somewhere to live so the banner can drop when the rig is confirmed fine.

## Open questions — operator's call, deliberately not filled in

- **Dismiss semantics for the overlay.** If the operator dismisses while
  `tx_still_keyed` still holds, vanishing is wrong. Demoting to the header chip
  is one option (the emergency reduces rather than disappears, and the daemon
  clear still removes it properly). Undecided.
- **Precedence among header-hosted states** when several hold at once.
- **Narrow-width behaviour** where the header centre shrinks.
- **Whether the header can carry amber loudly enough** for the drive alarm, or
  whether the drive alarm needs its own weight below the emergency tier.

## Draft acceptance criteria (to be checked by the operator before any build)

Stated in operator-observable terms, each with the nearest confusable state —
the clause that is usually missing:

1. When the rig affirmatively reports CAT TX still keyed after an unkey, the
   screen is taken over and I cannot miss it, **and I can tell it apart from**
   the daemon merely being unable to confirm — which must not take the screen.
2. When the daemon later confirms the transmitter idle, the takeover clears
   itself without my acknowledgement, **and I can tell that apart from** my
   having dismissed it while it was still keyed.
3. When an alarm escalates from unconfirmed to affirmatively-keyed, the surface
   changes tier at that moment, **and I can tell it apart from** a fresh alarm
   raised from a clean state.
4. When any alert appears or is dismissed, nothing I was looking at moves, **and
   I can tell that apart from** the alert simply not having fired.

## References

- ADR 0008 — notifications / toast system (levels, TTL, stack depth, placement).
  Note the app's toast container resolves to bottom-centre; 0008 specifies
  `top-4 right-4`. One of the two is out of date.
- ADR 0051 — TX state uncertainty and the stuck-TX alarm (the five codes).
- ADR 0057 — TX safety scope: CAT confirmation is detection, not guarantee.
- ADR 0044 — the consolidated app shell that hosts these surfaces.
- `frontend/app/src/App.svelte` — the in-flow banner stack.
- `frontend/app/src/lib/ui/Header.svelte` — the proposed host.
- `frontend/app/src/lib/ui/{TxAlarmBanner,DriveAlarmBanner,DriveMonitorNotice,Toasts}.svelte`
- `internal/bridge/txconfirm.go` — `raiseTxAlarm` edge-gated publish; the clear.
- `internal/bridge/drivealarm.go` — `publishDriveRecovery`, deliberately not a clear.
