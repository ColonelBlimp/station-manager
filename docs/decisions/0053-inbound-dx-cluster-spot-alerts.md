---
number: 0053
title: Inbound DX-cluster client with watch-list spot alerts
status: Accepted
date: 2026-07-20
---

# 0053 — Inbound DX-cluster client with watch-list spot alerts

## Context

Station Manager can *submit* spots (`internal/pskreporter`, outbound FT8 to PSK
Reporter) but has no way to *consume* them. Operators hunting on phone/CW work
from DX clusters — nodes that broadcast human spots (`DX de W1ABC: 14205.0
DL9UW cq ...`) over telnet — and the value is not the raw firehose but an
**alert when something the operator wants becomes active**: a needed DXCC, a
needed band/mode slot, or a specific callsign.

The pieces that make "do I want this spot?" answerable already ship: **callsign
enrichment** (`/v1/enrich/callsign` → country/DXCC/zones) and the
**contest-dupe** check (`/v1/contest-dupe` → worked-on-this-band-and-mode) —
the same machinery behind FT8 Band Activity's flag + worked-before tint. So a
spot pipeline is largely composition, not new logic. Three standing constraints
shape the design: the **narrow-daemon-scope** invariant (log/forward and
rig/CAT subsystems must not couple; new inputs are siblings, enforced by the
package-import graph), the **attended-only** posture (SM never operates
unattended), and the existing **subsystem precedent** — `internal/bridge` and
`internal/ft8` are long-lived input subsystems that emit state over SSE and
survive disruption via a supervisor/reconnect loop.

## Decision

Build an inbound DX-cluster client as a **new daemon subsystem**
(`internal/dxcluster`), a sibling to `internal/bridge` and `internal/ft8`: it
holds a persistent telnet connection to a classic DX-cluster node (callsign
login + keepalive), parses spot lines, and — for each spot — resolves DXCC via
the existing enrichment path and worked-status via the existing contest-dupe
path, then matches against an operator **watch-list**. Matches raise an alert;
all spots feed a live SSE spot stream the SPA renders (a filtered table like FT8
Band Activity). Config lives in a `dxcluster` block in `config.json`; the
watch-list (needed-DXCC, band/mode slots, specific calls) lives there too. A
spot carries frequency + mode, so **click-a-spot → QSY** drives the *existing*
bridge rig-control write seam (`set_freq`/`set_mode`) — reused via the same
injected-closure seam the FT8 TX path uses, never a package import, so
narrow-scope holds.

## Alternatives considered

### Reverse Beacon Network (RBN) as the first source

RBN is telnet skimmer spots for CW/RTTY/FT8 (and uniquely spots *your own*
signal, a propagation check). Rejected as the *first* source because the stated
use is phone/CW hunting, where human cluster spots are the fit; RBN is
firehose-volume and digital-leaning. Kept as an explicit later source — the
telnet-client + parser + filter engine are the same, so adding RBN is a parser
variant, not a re-architecture.

### Web / HTTP spot APIs (DX Summit, HamAlert, cluster web feeds)

A REST/poll model instead of a persistent telnet socket. Rejected: telnet is the
native, real-time push protocol for clusters (an alert is only useful *now*);
polling adds latency and rate-limit friction; and the telnet supervisor/reconnect
pattern is already proven in `internal/bridge`. (HamAlert-style external alerting
also moves the "wants" logic off-box, duplicating enrichment/dupe we already run.)

### Build want-detection from scratch

A self-contained DXCC/worked-before engine inside the new subsystem. Rejected:
enrichment and contest-dupe already answer both questions and are the same
answers the log uses, so a parallel engine would drift from the logbook's truth.
The subsystem calls those existing paths.

### A separate `cmd/dxcluster` process (split-host, like the parked `cmd/bridge`)

Rejected for the default deployment: a hunting aid belongs next to the rig and
the operator's screen, and click-to-QSY wants the same daemon that owns the
bridge. In-process subsystem, consistent with the single-binary default.

### External tooling (a standalone cluster client + manual cross-reference)

Rejected: the whole value is *automatic* correlation of a spot against the
operator's own log and needs — which SM already has the data for. An external
client can't see the logbook's worked-before state.

## Consequences

- A second long-lived outbound network client joins `pskreporter` + the
  forwarders; it needs its own supervisor/reconnect, a callsign login, and
  keepalive. First-boot ordering and mid-session drops must self-heal without a
  restart (the ADR 0020 pipeline-supervisor pattern applies).
- New surfaces: a `dxcluster` config block (+ watch-list model), a spot-feed SSE
  event set, and SPA work — a live spot table, a watch-list config panel in
  Settings, and alert delivery.
- The **watch-list needs a "needed entities" derivation** (which DXCC/band/mode
  slots are still unworked) from the logbook; this is new aggregation, adjacent
  to the awards-tracking gap already in the backlog.
- **Spot-volume management**: clusters are firehoses. Server-side cluster
  filters (set at login) plus local watch-list filtering keep the feed and the
  alert rate sane; the design must not alert on every spot.
- **Alert delivery** is a real UX choice with a permission cost: a browser
  desktop notification needs an explicit grant; in-app banner/sound do not. v1
  can ship in-app first and add desktop notifications behind a toggle.
- **click-to-QSY couples the feature to the rig**, but only through the existing
  injected rig-control seam (not an `internal/bridge` import), so the
  narrow-scope boundary tests still hold. An audio-only / bridge-disabled setup
  simply shows spots without the QSY action.
- Attended-only is preserved trivially: the operator reads alerts and acts;
  nothing keys TX or logs automatically.

## Triggers to revisit

- If CW/digital hunting or self-spot propagation checks become wanted, add
  **RBN** as a second source (parser variant).
- If real cluster traffic overwhelms the local pipeline despite server-side
  filters, reconsider a **coarser pre-filter** or a bounded spot ring.
- If a web/HTTP spot API ever offers materially better data (e.g. richer
  metadata than a spot line), reconsider the telnet-first choice for that source.
- If click-to-QSY grows toward *auto*-tuning on an alert, stop: that edges toward
  unattended operation and needs an explicit attended-only review.
- If awards-tracking lands first, the "needed entities" derivation should be
  shared with it rather than duplicated.

## References

- Backlog: "inbound DX cluster client (spot consumption for phone/CW hunting)"
  (2026-07-20 never-captured-gaps survey).
- Precedent subsystems: `internal/bridge` (ADR 0013/0019/0020 supervisor),
  `internal/ft8` (ADR 0024), `internal/pskreporter` (outbound network client).
- Reused paths: `/v1/enrich/callsign`, `/v1/contest-dupe`; bridge rig-control
  write seam (ADR 0026 `set_freq`/`set_mode`).
- Invariants: narrow daemon scope (`docs/v1-analysis/invariants.md`),
  attended-only (CLAUDE.md, ADR 0031).
