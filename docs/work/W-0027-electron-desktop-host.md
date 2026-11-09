# W-0027 — Electron desktop host: building ADR 0102

**Status:** Selected 2026-11-09; ranked in [`docs/backlog.md`](../backlog.md). A1 and B1 started
2026-11-09 (operator). Slice 0 done: annotated tag `pre-w0027`
on `a9457725` (CI-green, run 42613353737), pushed by the operator. Each further slice starts on the
operator's direction.
**Selected:** 2026-11-09 (operator: "yes, accept ADR 0102, commit and open W-0027").
**Outcome:** Station Manager runs as the Electron desktop app decided in
[ADR 0102](../decisions/0102-electron-desktop-host-for-the-ui.md), built in small slices that
are each reviewed before the next. The evaluation, the proof of concept (POC) and the rulings
are in [W-0026](W-0026-ui-host-evaluation.md).

`W-0027` is an immutable identity. Its status may change, while priority and ranked position live
only in [`docs/backlog.md`](../backlog.md).

**Not in scope:** ADR 0100 (daemon-owned operating sessions) and ADR 0014's upstream forwarding
(deferred). `smd` keeps every operating decision (ADR 0102, Decision).

## Working rules (operator, 2026-11-09)

- **Small parts, each reviewed before the next.** The operator: "As we build each part (small
  changes), these should be reviewed by me and Codex before moving on to the next part."
  Per slice:
  1. state its operator-visible acceptance criteria;
  2. build it test-first, with reversion proofs;
  3. run the gates;
  4. hand it to the operator (a desktop check where it shows) and to Codex through `.scratch/`,
     with the uncommitted diff;
  5. fix every finding;
  6. commit on the operator's direction.

  The next slice starts only after that. The automatic post-commit Codex review still applies to
  non-Markdown commits.
- **A way back.** The operator: "we should tag SM before we start the changes so we can go back".
  Slice 0 is an annotated tag on the last commit before any W-0027 code, following `pre-w0021`.
- **The browser keeps working.** Until the operating-client check is switched on (track C), the
  desktop app is one more client of `smd`, and the browser still operates the station, so every
  slice before then is reversible by not using the app.

## Tracks and proposed slices (Claude's proposal, 2026-11-09; order and contents not ruled)

**Slice 0 — baseline tag** `pre-w0027` on the commit that accepts ADR 0102 and opens this
dossier (documentation only, so its code is today's). The operator pushes tags.

**A — the shell in the Station Manager repository.** The POC host is rebuilt, not copied. It is
for development only and is not released. **It executes no real daemon-served UI until C1's
authentication is implemented and validated** (ADR 0102; design review alone is not enough,
codex-036); until then its checks run against an isolated fixture harness that never touches
the station.

- A1: skeleton against the fixture harness. One main window; sandboxed, context-isolated renderer
  with no Node; navigation limited to the served origin; Electron permissions denied by default;
  single instance; the local "waiting for the daemon" page, independent of the daemon; the main
  window's minimum size 1024x640.
- A2: the window set (Map, Manual, detached Rig Control and Session), each at most once.
  **Prerequisite:** a reviewed Session authority and transfer design. Today the Session list is
  renderer-local per tab and archive (`session.svelte.ts`, `sessionStorage`; `main.ts` files
  submit results into it; `SessionPanel` reads it), so another renderer cannot show or maintain
  the same list; the POC's shared Session was fixture-daemon state, not product evidence. The
  design either names a reviewed ADR 0100 prerequisite or a constrained adapter, covering
  archive isolation, edits and email marks, and panel-local state. Acceptance: detach with
  existing rows, log another contact in main, see it in the detached Session, re-dock without
  loss or a second active copy.
- A3: closing (stop the FT8 run, disarm TX, and the confirmation) and no user reload. **Depends
  on C4** (daemon stop and disarm on loss of authenticated presence, including app failure).
- A4: placement and the XWayland default; restore as left.
- A5: the native Save dialog; shortcuts (ADR 0102 ruling 5).
- A6: floating Rig Control and Session panels (move, resize, reopen in place, remembered across
  restarts).

**B — the UI's own chrome for a windowed application.** The operator: "There are going to be
quite a lot of changes - chrome included as some of the way things look don't quite suit a
windows situation". The SPA stays browser-compatible.

- B1: the operator's list of what does not suit a windowed app (screenshots welcome), recorded
  here as the brief.
- B2: app-drawn form controls (ruled; dropdown lists stay native).
- B3 onward: one change per slice, each a design exchange recorded here.

**C — the daemon's side and security** (ADR 0102, Requirements). Each needs its own reviewed
design before code.

- C1: the authenticated channel, a design reviewed by Codex before code. The shell authenticates
  `smd`'s content and binds that authentication to every later request, stream and reconnect.
- C2: the operating-client check.
- C3: the view-only route classification, leaving room for a daemon submitter whose credential
  grants QSO submission only.
- C4: presence on the authenticated operating subscription, stopping and disarming through the
  guaranteed-stop controller.
- C5: idle-only reload with asset compatibility.

**D — packaging and release.**

- D1: the signed shell package, depending on the daemon package.
- D2: our own signed dnf repository.
- D3: the security-update routine and the daily support check.
- D4: Chromium's third-party licence notices, due before the first public release.
- D5: production hardening and the CSP (default-deny permissions start in A1).
- D6: package acceptance: an in-place upgrade while the app runs (editing, a detached window open)
  keeps every window working and applies the new runtime only after a restart; daemon scriptlets
  do not interrupt `smd` during operation.

## Rulings

- **2026-11-09 (operator): "yes, commit and start A1 and B1 in desktop/".** A1 (the fixture-only
  skeleton) and B1 (the operator's chrome brief) start together; the shell lives in `desktop/`.

## Open questions for the operator

- **C1's design in parallel with A1 and B1** (proposed; not yet directed).
- **Before A2:** the Session authority and transfer design as a reviewed ADR 0100 prerequisite, or
  a constrained adapter under its own small design.

**Review recommendations (codex-036, 2026-11-09, first given in chat; not ruled):**

- **Start B1 alongside A1, with separate, separately reviewable results.**
- **A1 depends on C1.** ADR 0102 requires authentication before the shell executes daemon-served
  UI. So A1 builds the window, the independent waiting page and fixture-backed checks, and loading
  the real daemon's UI waits until C1's authentication is implemented and validated (codex-036
  corrected "reviewed mechanism": a design review authenticates nothing).
- **Default-deny permissions belong in the initial shell.**
- **`desktop/` fits,** with its own package manifest, lockfile, and focused Task and CI checks. The
  SPA stays in `frontend/app`; the shell consumes the daemon-served UI and keeps no copy of it.

## Record
