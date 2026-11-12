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
  the same list; the POC's shared Session was fixture-daemon state, not product evidence. Ruled
  2026-11-10: the design is ADR 0100 (revised); building it is separate prerequisite work outside
  this dossier's shell scope, covering archive isolation, edits and email marks, and panel-local
  state. Acceptance: detach with
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

- **No up-front brief (operator, 2026-11-10):** "Let's not concern ourselves too much with B1 as
  we implement, I will ask for a change". Chrome changes come as the operator asks for them during
  implementation, each one a small slice with the usual review, recorded here when made.
- B2: app-drawn form controls (ruled; dropdown lists stay native).

**C — the daemon's side and security** (ADR 0102, Requirements). Each needs its own reviewed
design before code.

- C1: the authenticated channel, a design reviewed by Codex before code. The shell authenticates
  `smd`'s content and binds that authentication to every later request, stream and reconnect.
- C2: the operating-client check. **Depends on D1** (operator ruling 2026-11-11): the browser is
  not made view-only or removed until a user can launch the desktop app.
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
- **2026-11-11 (operator): "Before we retire the browser readonly or remove, the must be able to
  launch the desktop app".** The browser stays a full operating client until a user can launch
  the desktop app (D1: the shell package with its menu launcher, not `npm start` from a checkout).
  So C2's switch of the browser to view-only, and any removal of the browser path, depend on D1.
  Today the app runs only from a repository checkout (`cd desktop && npm start`).

## Open questions for the operator

- **C1 design started 2026-11-10** (operator: "keep them Proposed, start C1 design"):
  [ADR 0103](../decisions/0103-pinned-tls-channel-between-the-desktop-app-and-smd.md), Proposed,
  for review. Ports ruled 2026-11-10: HTTP 8080 (unchanged), app HTTPS 8443; bind failure
  reported until C2, fatal after.
- **Before A2:** the Session authority design. Ruled 2026-11-10 (operator, "1-7 yes"): it is
  ADR 0100, revised 2026-11-10 so a session follows the desktop app's lifetime (rulings in its
  Decision; ADR 0101 revised to match). Building it depends on C1/C2/C4.

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

### A1 — shell skeleton against the fixture harness (2026-11-09; done)

**Acceptance criteria** (operator-visible where marked; each is a check in
`desktop/test/scenario-a1.mjs`):

1. *(visible)* With the "daemon" down at start, the window shows the shell's own waiting page,
   which requests nothing from the origin; when the origin answers, the window loads it by
   itself.
2. *(visible)* After daemon loss, a failed load returns to the waiting page, and the window
   recovers when the daemon returns.
3. Renderer: sandbox on, context isolation on, Node integration off, no `<webview>`; the page
   sees no `require`, `process`, `module` or `Buffer`.
4. A request to another origin is cancelled and never arrives (a real listening server stands in
   for "elsewhere"); a request to the served origin is allowed.
5. A page cannot navigate the window to another origin, and `window.open` creates no window;
   only an `https:` external address goes to the default browser (`http:` and `file:` do not).
6. Notifications, geolocation and the microphone are refused, by the shell's request and check
   handlers.
7. *(visible)* The main window cannot be made smaller than 1024x640.
8. *(visible)* No application menu.
8b. *(visible)* The window carries Station Manager's icon in the title bar (operator, 2026-11-09:
    "yes, add the SVG icon to A1"): `desktop/assets/icon.png`, the SPA's `logo.svg` rendered to
    256x256. The automated check proves the window is configured with it; the title bar itself is
    the desktop check. Launcher and native-Wayland icons come with the package (D1).
9. *(visible)* A second launch exits at once and the running instance is told (and raised).
10. The shell's availability probe refuses redirects: a replacement listener at the daemon's
    address that redirects elsewhere reaches nothing and the window keeps waiting (codex-038).
    The probe says only that something answers; it is never an identity check (that is C1).
11. A replacement that answers the probe but redirects the window's own load elsewhere: nothing
    arrives there, the window never shows the other address or an error page, it returns to the
    waiting page, and it retries at the waiting pace (operator, 2026-11-09: "yes, fix it now",
    on claude-072's residual).

**Built:** `desktop/` with its own manifest and lockfile (Electron 44.9.3; dev tools ESLint,
Prettier; all dependency licences permissive and GPL-3.0-compatible). `src/main.mjs` (entry;
single instance; harness only), `src/hardening.mjs` (permissions, request filter),
`src/window.mjs` (window, navigation, external links), `src/waiting.mjs` (waiting page, retry,
failed-load fallback), `harness/server.mjs`. No preload: A1 exposes no host interface. Gates:
`task desktop:check`, the same steps in `scripts/ci-local.sh`, and CI steps after the SPA gate
(the CI step lifts Ubuntu's AppArmor user-namespace restriction on the runner so the sandbox
stays on).

**Evidence:** A1 PASS, 26 checks (the icon added after the operator's first desktop look showed
the generic X11 icon; the redirect check and a per-permission media check added after codex-038,
with a fake capture device in automated runs so a refused microphone is the policy's
NotAllowedError, never a missing device). Reversion proofs, each restored and verified by hash: sandbox
off; Node in the renderer; request filter not cancelling (first attempt proved nothing because
"elsewhere" was not listening; fixed with a real server, then proven); navigation unguarded;
permission request granted; permission check granted; no minimum size; no waiting page; no
failed-load fallback; single instance off; any scheme handed to the browser; default menu;
`window.open` allowed; no icon; the probe following redirects; the media permission alone
granted; no main-frame redirect guard; no retry pause after a stopped redirect (1,157 window
loads in about 4 s). All 18 failed for their stated reason. Lint and format clean.

**Desktop check (operator, 2026-11-09, XWayland):** the icon "shows in the title bar now"; then
"all good, a to f pass": the waiting page then the fixture page by itself, no menu bar, no
shrinking below 1024x640, a second launch raising the first window, a minimised window restored
by a second launch, and closing the window exiting the application.

**Review:** codex-038 (two findings: the probe followed redirects; the media check could not tell
refusal from a missing device) and claude-072's own residual (a redirected window load left an
error page), all fixed with checks and reversion proofs; codex-040 closed the review with no
remaining objection. Committed on the operator's direction ("yes, commit A1"). The hosted CI
step (the AppArmor setting) is confirmed only once CI runs it.

**Not covered by A1:** no user reload (A3), window roles (A2), placement and XWayland (A4), the
host interface (later slices), and loading `smd` (after C1).

### C1 proof — pinned TLS against fixture listeners (2026-11-10; done)

Started on the operator's direction ("yes, start the C1 proof slice"), on the brief in claude-106
(codex-049's five points). Confined to `desktop/`: synthetic identities made per run with `openssl`,
an ephemeral loopback port, a temporary profile; no `smd`, no operator identity. It does **not**
lift A1's restriction: the daemon listener, the protected identity directory and the rest of
ADR 0103's acceptance are still to be built.

**Acceptance criteria** (each a check in `desktop/test/scenario-c1.mjs`):

1. With an impostor (another key) at the app address at cold start, the probe tries it, the pin
   refuses it, nothing reaches it, and the window stays on the waiting page.
2. The genuine listener then loads by itself, and its page script runs.
3. **Success before rejection:** `fetch`, `wss:`, EventSource and a download all succeed against the
   genuine listener.
4. With the genuine listener replaced by the impostor at the same address (its connections closed),
   fresh, uniquely marked attempts on every transport are refused, **each with its own evidence of
   having been attempted** (a handshake the impostor saw refused; a skipped download, or a pending
   one without observed rejection evidence, fails, codex-050/051); no handshake with the impostor
   completes; nothing arrives there; the loaded page keeps its draft and panel marker and is not
   sent to the waiting page.
5. The genuine listener returns: `fetch`, `wss:`, EventSource and a download recover; the draft is
   still there.
6. The pin accepted only the genuine certificate, rejected the impostor's, and never changed.
7. No downgrade (`http:`, `ws:`) and no other port; no extra window; the page requested only its own
   origin.
8. **The certificate verifier was asked only about 127.0.0.1.** This is a bounded observation
   (verifications are cached and the hook sees only verifications the network service requests),
   not a network inventory; it is what caught the spellchecker.
9. The copied-certificate case (the pinned certificate with another key) cannot be served: Node's
   TLS refuses the pair at set-up (`ERR_OSSL_X509_KEY_VALUES_MISMATCH`). Recorded as a
   **fixture-set-up rejection, not a handshake result**; the guarantee relied on is TLS key
   possession (RFC 8446 §4.4.3).

**Built:** `src/pin.mjs` (DER SHA-256 pin, `setCertificateVerifyProc`: `0` only for 127.0.0.1 with
the pinned fingerprint, `-2` otherwise, never `-3`; installed before the first request); the request
filter admits `wss:` beside an `https:` origin and nothing else (`src/hardening.mjs`); the probe
uses the window's session (`ses.fetch`), so it passes the filter and the pin (`src/waiting.mjs`); a
TLS mode, WebSocket, EventSource and download routes and handshake counting in the harness;
`test/identity.mjs`; a `c1` mode in `src/main.mjs`; `check:c1` in `task desktop:check`,
`scripts/ci-local.sh` and CI.

**Finding: the spellchecker reached Google.** The first run's pin log showed a verification for
`redirector.gvt1.com`; Chromium's network log identified Electron's spellchecker downloading
`en-gb-10-1.bdic`. The pin refused it, but a connection was attempted off the machine. The shell now
disables the spellchecker (`setSpellCheckerEnabled(false)`, `setSpellCheckerLanguages([])`) first
thing at start, and in every hardened session, with `spellcheck: false` on the window; disabling the
component updater made no difference. A network log of the fixed run shows no host other than
127.0.0.1 (a bounded observation of that run, not a guarantee). Check 8 watches for it. Consequence:
no spellchecking in text fields (operator, 2026-11-10: "spellchecking off is fine").

**Evidence:** C1 PASS (17 checks), repeated; A1 PASS (26 checks); lint and format clean;
`task desktop:check` passes. C1 reversions, each restored and verified by hash: **only the
fingerprint comparison removed** (acceptance path kept): the impostor's page loads and `fetch`,
`wss:` and EventSource receive the impostor's answers (`impostor:i1`, `i2`, `i3`); `wss:` partner
removed: genuine `wss:` refused; downgrade allowed: `http:`/`ws:` requests leave; probe on Node's
global `fetch`: the pinned listener never counts as up; spellchecker left on: a connection to
`redirector.gvt1.com`. All 5 fail for their stated reason. A1's 18 reversions rerun: 17 proven;
R18's fixture had to tell the probe from the window by `Sec-Fetch-Dest: document` (the probe now
shares Chromium's user agent), after which it fails as before (893 window loads in about 4 s). **R15
alone (probe following redirects) no longer fails**, because the session filter now also cancels the
off-origin redirect; with both the manual-redirect setting and the filter's cancellation removed,
the redirect reaches the other address (three times), so the protection is layered, and neither
layer alone is proven by itself.

**Answers ADR 0103's open question:** in this fixture, Electron's verifier covers page loads,
`fetch`, `wss:`, EventSource, downloads and the probe, including reconnects after a prior
successful verification.

**Review (codex-050):** the pin proof stands, including WebSocket and EventSource, independently
re-run by Codex outside its sandbox (its first attempt, inside its execution sandbox, aborted at
Chromium's sandbox start-up with SIGTRAP at 13:29, the crash notice the operator saw; no evidence
either way). Fixed after review: each refused transport needs its own evidence of having been
attempted (Codex's counter-test, a skipped download, passed 16/16 before and fails now, as does a
"pending" download never attempted); a download recovers in step 5; criterion 8 and the spellchecker
note are worded as bounded observations. The fixture checks show page retention and fresh
connections, not the product's "daemon unavailable" overlay or its retry policy.

### C1b — smd's protected TLS identity (2026-11-11; done)

Started on the operator's direction ("yes, start C1b"). A new Go package, `internal/desktopid`,
implementing ADR 0103 point 1. It is **not wired into `smd` yet**: C1c (the app listener) is the
first user. Nothing listens on the network.

**Acceptance criteria** (each a test in `internal/desktopid/desktopid_test.go`):

1. With no identity, `Ensure` creates `desktop-identity/` (0700) holding `key.pem` (0600, PKCS#8)
   and `cert.pem` (not group/other writable): ECDSA P-256, self-signed, for exactly 127.0.0.1,
   valid for 10 years; it returns the DER, the fingerprint and a usable TLS certificate.
2. A second `Ensure` reuses the identity unchanged (same fingerprint, files byte-identical).
3. The fingerprint is the SHA-256 of the DER in the shell's format (upper-case hex pairs joined by
   colons, as Node's `X509Certificate.fingerprint256`).
4. Refused, naming the path, and **nothing changed** (never repaired): a key without its
   certificate, the reverse, an empty identity directory; a certificate from another identity;
   a malformed certificate or key; an unexpected file; the directory not 0700, the key readable by
   group, the certificate writable by others; an ancestor writable by group without the sticky
   bit; an ancestor owned by another user; an identity file owned by another user; a symlinked
   identity directory or certificate. A sticky, world-writable ancestor (as `/tmp`) is accepted.
5. Creation runs its barriers in order: write and sync the key, write and sync the certificate,
   sync the prepared directory, one rename, sync the parent.
6. A failure at any barrier up to and including the rename publishes nothing; the next start
   creates a complete identity and leaves no remnant.
7. A failed final parent sync publishes a valid identity with its durability **unconfirmed**
   (reported, not an error), and the next start reuses it rather than replacing it.
8. An unpublished remnant from an earlier crash is removed at the next start; other files and
   look-alike directories are left alone.
9. **Concurrent creators** (codex-053): creation holds an exclusive lock
   (`.desktop-identity.lock`, owner-only, in the working directory) and re-checks under it, so
   eight concurrent `Ensure` calls share one identity, a creation in progress is never removed as a
   remnant (a second creator waits for the lock), and publication never replaces an identity that
   is already there (it is refused, and the existing one survives unchanged).
10. A non-regular identity file (a directory named `cert.pem`) is refused.
11. **The validated path is the path used** (codex-054 P1): the working directory is resolved once
    and checked, and every later access (load, creation, lock, clean-up) goes through the resolved
    path, so swapping a symlink in the caller's path after validation cannot redirect a read; an
    identity reached through an alias is created at the resolved location.
12. **The creation lock is checked before use** (codex-054 P2): opened without following a symlink,
    and refused unchanged unless it is a regular file owned by the user with mode 0600 (a symlink,
    a 0666 lock, a foreign-owned lock, a named pipe and a directory are all refused).
13. The key must be exactly 0600 (0700 is refused).

**Built:** `internal/desktopid/desktopid.go` (`Ensure`, `Load`, `Fingerprint`, `Durability`),
`internal/desktopid/fs.go` (a package-private filesystem seam for the barriers, after the config
writer's precedent but not shared with it, as ADR 0103 says). Errors use `internal/errors`.

**Evidence:** 49 tests and subtests pass, also under `-race`; `go vet` and `go build` across the
module; `golangci-lint` 0 issues; `scripts/maintainability-metrics.sh` 0 regressions (it first
refused a package-level function literal used as a test seam, so `ownerOf` now points at a declared
`statOwner`). Reversions, each restored and verified by hash: no prepared-directory sync; a failed
parent sync reported as an error; a group-writable ancestor accepted; an unexpected file accepted;
key, certificate and directory modes unchecked; symlinks accepted; the pair not matched; a refusal
that recreates; remnants not removed; the files written with a plain rename instead of through the
seam; the ancestor-owner check removed; the identity-file owner check removed; no creation lock; no
re-check under the lock; a non-regular file accepted; publication clearing what is there first;
`Load` reading through the original path (the swap test then returns the substitute identity);
`Ensure` keeping the original path; the lock following symlinks; the lock's mode, owner and type
unchecked; the key's mode not exact. **All 25 fail for their stated reason.** Two proofs needed
fixing on the way: one mutation did not compile (a build failure is not a proof), and a directory
used as the lock cannot be opened for writing at all, so the regular-file check is now exercised
with a named pipe. Two tests were sharpened during the proofs: the symlink tests now require "`… is
a symlink`" (the temporary path contains the test's name, so "symlink" alone matched any message),
and two owner tests make one path appear foreign through `ownerOf` (changing real ownership needs
root).

**Ruling for C1c (operator, 2026-11-11):** an identity that cannot be created or validated is
treated like the busy app port: before C2, `smd` starts without the app listener and reports it (log
and Station Event, naming the file); after C2, a start-up error. Unconfirmed durability is only
reported. Recorded in ADR 0103 point 2.

**Limits, stated:** publication uses `os.Rename`, which refuses a non-empty destination (so a
published identity is never replaced) but would replace an empty directory; the creation lock and
the re-check under it make that unreachable between cooperating creators (`RENAME_NOREPLACE` would
need `golang.org/x/sys` as a direct dependency). Power-loss
durability is designed for, not demonstrated: the tests prove the order and the failure handling of
the barriers, not survival of a real power cut. The fingerprint format is tested against its
definition, not cross-checked with the shell at runtime (C1c's acceptance will).

### C1c brief — smd's app listener (2026-11-11; built, see the C1c record below)

Started on the operator's direction ("yes, start the C1c brief"). Implements ADR 0103 points 2 and
5 on `smd`'s side, using C1b's identity. The shell's real-daemon mode is the next slice (C1d).

**Outcome the operator sees:** `smd` keeps serving the browser on 8080 exactly as today, and also
serves the same UI and API to the desktop app over HTTPS on 127.0.0.1:8443 with its own identity. If
the app's port or identity is not available, `smd` still starts and the browser still operates the
station; the problem is reported. Nearest confusable outcome: a second, separate "station" (its own
services or state) behind 8443.

**What exists today (read for this brief):**
- `api.Server` owns **one** `http.Server`, one listener and one close owner
  (`internal/api/server.go:34,123-129`, `ListenAndServe` 622-655); `StopAccepting` (759) and
  `Shutdown` (811) act on that one; hijacked streams are tracked by `s.streams` and waited for.
- The lifecycle's `http` node runs it in a goroutine whose error goes to the daemon's fatal error
  channel (`cmd/smd/lifecycle_adapters.go` `startHTTP`), so any serve error stops `smd`.
- Guards: `requireSameOrigin` (Host for every method, Origin for unsafe ones; any loopback Origin on
  any port is accepted, `internal/api/csrf.go` `originAllowed`); WebSocket upgrades need an exact
  Origin whose scheme follows `r.TLS` (`internal/api/stream_ws.go` `wsOriginExact`).
- **Station Event kinds are fixed by a schema `CHECK`** (`internal/stationevents/stationevents.go`
  `KindsByCategory`; `internal/database/sqlite/migrations/log/0013_operator_event_archive.up.sql`):
  a new kind is a log-schema migration (schema 20).

**Proposed design:**
1. **Config:** `server.app_port`, default 8443, always bound to 127.0.0.1, validated (1-65535, not
   the main listener's port), served resolved and documented in `docs/v2-design/config.md` in the
   same change. No setting to turn it off (the desktop app is the ruled primary client).
2. **Identity:** the `http` node calls `desktopid.Ensure(working directory)` before binding the app
   listener. Outcomes per the operator's 2026-11-11 ruling: failure -> no app listener, reported,
   `smd` runs; `DurabilityUnconfirmed` -> reported, listener runs. (After C2 an identity failure
   becomes a start-up error; `DurabilityUnconfirmed` stays report-only in both phases. C2 does not
   exist yet, so only the pre-C2 behaviour is built.)
3. **One station, two listeners:** `api.Server` gains a second `http.Server` for the app listener
   with the **same handler chain** (same services, admission, rate limits, drain gate, stream
   tracking), serving over `tls.NewListener` with the identity's certificate (TLS 1.3 minimum).
   `StopAccepting` closes both listeners; `Shutdown` shuts both down and waits for streams once;
   each listener keeps its own close-once owner so a drain reports both close results. One stalled
   drain never prevents the other server from beginning its shutdown (codex-056).
4. **Errors:** the main listener stays fatal as today. The app listener's bind failure, or a serve
   error, is reported (operator ruling 2026-11-10) and never reaches the fatal channel before C2.
5. **Origin per listener (corrected after codex-056):** on the app listener, Host must be
   `127.0.0.1:<app port>` and an unsafe method requires the exact Origin
   `https://127.0.0.1:<app port>`, bound to the configured app port, not taken from the request's
   Host; an **absent** Origin stays allowed for ordinary non-browser unsafe requests, as today, but
   a **WebSocket upgrade without an Origin is refused** before admission, as `wsOriginExact` already
   does. The HTTP listener's rules are **unchanged for now**, which means it still accepts any
   loopback Origin, including the app's (`internal/api/csrf.go` `originAllowed`); tightening it is a
   separate, reviewed change, not part of C1c.
6. **Reporting:** a warning in `smd.log` naming the port or the identity file (log only, operator
   ruling 2026-11-11).

**Acceptance (tests in `internal/api` and `cmd/smd`; never the live station):**
- a request, an SSE stream and a WebSocket each work on both listeners against the same state (a
  change made through one is seen through the other);
- a TLS client pinning the identity's fingerprint connects to the app listener; a client expecting a
  different certificate fails the handshake; plain HTTP to the app port is refused;
- Origin on the app listener: refuses `http://127.0.0.1:8080`, another port and a wrong scheme for
  unsafe methods and upgrades, and accepts its own; an absent Origin is accepted for an unsafe
  request and refused for an upgrade; the HTTP listener behaves exactly as today;
- app port already in use, or the identity refused: `smd` starts, the main listener serves, the
  report is written, nothing is fatal; `DurabilityUnconfirmed`: reported, listener serves;
- partial start-up, a busy main port, cancellation, and a deadline or failure on either drain are
  each covered; a stalled drain on one server does not hold the other's shutdown;
- drain and shutdown close both listeners, end streams on both, and report both close results; a
  failed main bind leaves no app listener running;
- ephemeral ports in tests; any manual run uses its own working directory (`SM_WORKING_DIR`) and
  ports other than 8080/8443, because the live station uses them.

**Rulings (operator, 2026-11-11): "b, and no disable setting".** C1c reports in `smd.log` only;
the Station Event comes once event kinds no longer need a migration (the operator's direction,
recorded in W-0020, 2026-11-11). No setting disables the app listener.

**Questions as asked:**
- **Q1. The Station Event needs a schema migration.** Options: (a) add the kind now, a log-schema
  migration to 20 in C1c, with the rollback note that implies; (b) C1c reports in `smd.log` only,
  and the Station Event kind is added with the next log-schema change (sessions or contests, which
  need migrations anyway). Claude recommends (b): it keeps C1c free of a schema change, and the
  report the operator sees first is still written.
- **Q2.** Confirm no setting to disable the app listener (Claude's recommendation), or add one.

**Not in C1c:** the shell reading the real identity and connecting to `smd` (C1d); C2's
operating-client check; any change to the browser path.

### C1c — smd's app listener (2026-11-11; done: committed a27adce7, accepted by the operator)

Built on the operator's direction ("yes, defer it and carry on with C1c"), to the brief above.

**What changed:**
- `internal/config`: `server.app_port` (`omitempty`) with `EffectiveAppPort()`, default 8443
  (`defaultAppPort`). It is a **read/serve resolver**, not a load-time fill: under ADR 0074 an older
  `smd` refuses a key it does not know, so writing the default into `config.json` would stop a
  downgrade from starting. Validation: a value outside 0-65535 is fatal; a port equal to the
  browser listener's TCP port is a **warning** (`app_port_conflict`), not the refusal the brief
  proposed. Before C2 nothing about the app listener may stop the station (ruling 2026-11-10), and
  the listener's bind then fails and is reported.
- `internal/api`: `BindApp(port, cert)`, `ServeApp()`, `AppPort()`. A second `http.Server` with
  the main server's handler (the same chain, so the same services, admission, drain gate and stream
  tracking), the same timeouts and error log, over `tls.NewListener` (TLS 1.3 minimum) on its own
  close-once owner. A `ConnContext` marks its requests, and `requireSameOrigin` sends them to
  `requireAppOrigin`. That function enforces Host `127.0.0.1:<bound port>` for every method and,
  for unsafe methods, a present Origin of exactly `https://127.0.0.1:<bound port>`; both come from
  the bound port, never from the request. `StopAccepting` closes both listeners; `Shutdown` drains
  the app server concurrently with the main one and joins both results.
- `cmd/smd`: `startHTTP` (the `http` node) starts the browser listener as before, then calls
  `startDesktopListener`. That function runs `desktopid.Ensure(working directory)`, binds and
  serves; a refused identity, a busy port or a serve error goes to `smd.log` as a warning and
  never reaches the fatal channel, and `DurabilityUnconfirmed` is reported while the listener serves.
- Canonical references updated in the same change: `docs/v2-design/config.md` §3.1, §4 and §5,
  and `docs/v2-design/api-endpoints.md` Conventions.

**Acceptance as tested** (all ephemeral ports and temporary working directories; the live station
was not touched):
- `internal/api` `TestApp_*` (9):
  - pinned TLS 1.3 served, with the handler chain shared;
  - another certificate, TLS 1.2 and plain HTTP refused;
  - the Host and Origin matrix;
  - the browser listener unchanged (it still accepts the app's Origin);
  - WebSocket upgrades: own Origin 101, absent or browser Origin 403;
  - stop and shutdown close both listeners;
  - a bind failure is returned, not fatal;
  - one station: a logbook created through the app is read through the browser listener, and the
    SSE stream opens on the app listener;
  - a stalled drain on either server does not delay the other's (both directions).
- `internal/config` `TestAppPort_*` (4): the default, explicit value, default not written, and the
  validation table.
- `cmd/smd` `TestDesktopListener_*` (8):
  - five on the start function with fakes: clean start, refused identity, unconfirmed durability,
    busy port, serve error;
  - three on an orchestrated daemon:
    - the `http` node publishes the identity and serves `/v1/version` to a client pinning it, and
      the port closes with the generation;
    - a busy app port: the station starts and serves the browser, with nothing on the fatal channel;
    - a failed browser bind stays fatal, and its stop takes the bound app listener with it.
- The orchestrated test daemons now get a free loopback app port (`freeLoopbackPort`), so the test
  suite never binds 8443.

**Reversion proofs: 20 of 20 proven**, each failing at its intended assertion; the tree was
verified unchanged after each run. Two proofs first came out vacuous and were fixed:
- The drain-order test passed under a sequential drain, because `StopAccepting`'s
  `SetKeepAlivesEnabled(false)` already closes idle connections (net/http `closeIdleConns`). The
  test now calls `Shutdown` alone.
- A close mutation passed because the harness counted an empty expected-failure list as proven;
  the harness now refuses one. The listener is closed both by `StopAccepting` and by `Shutdown`,
  so the proof removes both together.

**Gates:** gofmt (whole tree) clean, `go vet ./...`, golangci-lint `./cmd/... ./internal/...`
0 issues, maintainability 0 regressions, whole-tree `go test ./...` pass, `-race` for `cmd/smd`,
`internal/api`, `internal/config` and `internal/desktopid` pass.

**Not covered, and open (none decided here):**
- **Rollback drill.** From C1c, the new binary in `scripts/rollback-drill.sh` also tries
  127.0.0.1:8443 and writes `desktop-identity/` under the drill's scratch working directory. If the
  live station holds 8443, only a warning results. If not, the drill daemon briefly serves its
  scratch copy there. Writing `app_port` into the drill's config would make the older binary refuse
  it. This needs an operator ruling: accept, or a drill-side fix.
- A WebSocket carrying events on the app listener is tested only up to the 101 upgrade; the SSE
  stream and shared state are tested end to end.
- A cancelled start between the browser bind and the app bind is not separately tested: the start
  is synchronous, and every stop path runs `StopAccepting` and `Shutdown`, which close both.

**Corrections after codex-058 (2026-11-11):**
- **P1, start-up order.** `startHTTP` started the browser listener in a goroutine and bound the
  app listener immediately. With a shared port (an accepted warning) the app listener could win
  it, and the browser bind then failed on the fatal channel. Codex reproduced this
  deterministically. Now `api.Server.BindAttempted()` and `Listening()` report the browser bind,
  and the `http` node starts the app listener only after the browser listener holds its port
  (`afterBrowserBind`). A failed browser bind starts no app listener; a cancelled start-up is
  reported and starts none. `BindApp` also refuses once a stop has begun (`appClosed`), so a late
  bind can never leave an unclosed listener.
- **P2, rollback drill.** `scripts/rollback-drill.sh` now:
  - removes any source `app_port` from the scratch config;
  - gives the new-binary phase a free scratch port, and removes it before `config-downgrade` and
    the old-binary phase;
  - gives an old build with the listener a scratch port only with `--old-knows-app-port`
    (superseded after codex-059, below);
  - after each daemon start, fails if its daemon listens on any TCP port but that phase's.

  Codex verified that `config-downgrade` refuses a same-version target and keeps an explicit
  `app_port`. The explicit-port rollback step is documented in `docs/install.md` §7 and
  `config.md` §3.1.
- **Added tests:**
  - `TestApp_AWebSocketDeliversTheStationsEvents` (a real frame over the app listener);
  - `TestApp_BindAttemptedSignalsTheBrowserBindEitherWay`;
  - `TestApp_NoAppListenerOnceTheStopHasBegun`;
  - `TestApp_ShutdownAloneClosesABoundListenerNeverServed` (Codex's single-layer case);
  - `TestAfterBrowserBind` (bound, failed, cancelled);
  - `TestDesktopListener_NeverTakesTheBrowserListenersPort`. Its seam holds the browser bind until
    the app listener binds, or for 500 ms. It failed on the old ordering with Codex's "address
    already in use" and passes on the new.

  `TestDesktopListener_AFailedBrowserBindLeavesNoAppListener` now asserts that no app listener
  ever starts. That is stronger than before, when the stop only closed it.
- **Reversions:** A11-A16 proven (26 of 26 in all). Drill guard reversions on scratch copies of
  the script:
  - D1, the port kept for the old phase: the pre-C1c build refuses `server.app_port`, and the
    drill fails;
  - D2, a stray listener in the old phase: the listener check fails.
- **Drill exercised on synthetic data only:** an empty SQLite file and a minimal config in Claude's
  scratchpad; the source's `app_port` was a stand-in, 18443. The C1c build ran as new; the
  `2.0.0~alpha.5.110.gb2a16f5a` RPM's `smd`, extracted read-only, ran as old. ROWS IDENTICAL; the
  new daemon held only its scratch port, the old none. Port 8443 was free and untouched. The live
  station's files were not read. All drill scratch was removed.

**Corrections after codex-059 (2026-11-11):**
- **Cancellation priority.** `afterBrowserBind` chose between a completed browser bind and a
  cancelled context with one `select`. When both are ready, Go picks either, so a cancelled
  start-up could still start the app listener. Codex measured this in 125 of 256 calls; the new
  test failed on the third call. Now the wait ends on either signal and cancellation is then
  checked first. The decision sits in `startAppAfterBrowser`, which the `http` node calls.
  `TestStartAppAfterBrowser_ACancelledStartNeverBindsTheApp` drives it 256 times with both
  ready, and once cancelled mid-wait, asserting that `BindApp` is never called.
- **Drill capability checked before launch.** The post-start listener check found exposure only
  after it happened. Now the drill first runs each binary's `config-check` on a probe copy of the
  config with a scratch `app_port`. That subcommand is read-only by contract: nothing started,
  bound or written; present since 2026-08-22, so in every build that knows `app_port`. Outcomes:
  - exit 0: the phase gets a free scratch port;
  - a refusal of exactly `server.app_port`: the phase runs without the key;
  - any other answer, or a binary without `config-check`: the drill refuses before launch.

  The listener check stays as a second assertion. `--old-knows-app-port` is removed: the
  preflight establishes the capability, so a flag could only disagree with it.
- **Synthetic drill evidence** (scratchpad inputs only, stand-in source port 18443):
  - A: C1c build as old, no flag; it was given scratch port 46471 before launch, and its log shows
    it bound only that.
  - B: the pre-C1c alpha.5.110 build as old; the preflight said no, the daemon held no TCP port,
    and the rows came out identical.
  - C: as A, with a migrated source; the rows came out identical.
  - D3: an unrecognised preflight answer.
  - D4: an old binary with no `config-check`.
  - D3 and D4 both refused before the old binary was launched, with no "old version" line.
  - All drill scratch was removed; the repo script hash was unchanged after the scratch-copy runs.
- **Reversions:** 27 of 27 proven, including A17: restoring the bound-case early return fails the
  256-call test. W1, A11, A16 and W7 were re-anchored after the refactor. W7 now targets the node
  test, because a failed browser bind no longer starts an app listener.
- **A race in Claude's tests, found during the gates.** One of eight four-package `-race` runs
  failed. Stressing `TestApp_*` (30 runs at `-cpu 1,2`) reproduced it once:
  `TestApp_BindAttemptedSignalsTheBrowserBindEitherWay` built a second test server while the first
  server's goroutine was still writing its "listening" log line. Building a server initialises a
  logging service, which writes zerolog's process-global `TimeFieldFormat`
  (`internal/logging/service.go:146`). Production builds one logging service per process, so this
  is a test-only hazard. Fix, in the tests only:
  - that test builds both servers before starting either, and waits for its goroutines;
  - `startTwo`'s cleanup waits until both serve goroutines have returned.

  Afterwards: 80 `TestApp_*` race runs and 20 runs of the desktop-listener tests at `-cpu 1,2`,
  all clean; the four-package race gate passes. The original failing run's output was cut off, so
  it is not certain that it was this race.

**Correction after codex-060 (2026-11-11): a bounded probe.** Grepping the binary for
`config-check` does not prove it dispatches that subcommand. Codex showed this with a stand-in
that contains the string and hangs, and claude-130's no-bind reasoning was withdrawn in
claude-131. Changes:
- The probe now runs as `setsid timeout --kill-after=5 30 <bin> config-check …` (operator ruling
  2026-11-11: 30 s, then a 5 s grace before SIGKILL), in its own session.
- Its output goes to a 0600 file, so a surviving child cannot hold a command substitution open.
- After the wait, anything left in the probe's session is killed, and the drill refuses if
  anything survives.
- Only exit 0 (knows the key) or exit 1 with the exact single-key refusal (does not) is accepted.
  Expiry (124 or 137) and every other status refuse before any data phase, and a timed-out
  probe's output is never classified.
- The grep stays as a quick negative filter only.

Evidence:
- **S1, a stand-in that ignores SIGTERM, and S2, which adds a child that ignores SIGTERM and holds
  the output.** Both contain the string with no such subcommand, and open no socket. Each ran as
  the drill's old binary and was refused after 36-37 s with exit 137. Neither printed an
  "old version" line, and no stand-in process survived (checked with `ps`).
- **The kill path, measured in the drill's exact form.** `setsid` did not fork: the job's PID is
  the session and group ID. On expiry GNU `timeout`'s SIGKILL reached the whole group, the S2
  child included, before the `pkill -s` backstop ran.
- **Limit:** a process that starts its own session escapes both and would not be detected.
- **B** (pre-C1c alpha.5.110 as old) and **C** (C1c as old): rows identical, exit 0, as before.
- **D3** (an unexpected diagnostic) and **D5** (the exact refusal text with exit 2): both refused
  with exit 3.
- All drill scratch was removed, and the repo script hash was unchanged after the scratch-copy runs.
- The zerolog process-global question stays outside C1c (Codex agrees). If it is tracked, it
  belongs with the existing maintenance work, not in this slice.

**Accepted (operator, 2026-11-11): "C1c accepted".** Committed as a27adce7, after review through
codex-061. The post-commit clean-room review (`.codex-reviews/a27adce74e16.md`) raised one P2:
with `server.app_port` 443, browsers omit the default port from Host and Origin, so the exact
comparison would refuse every request. It is triaged as valid. The operator ruled (2026-11-11):
"yes, accept the portless form for 443". `appHostAllowed` and `appOriginAllowed` in
`internal/api/csrf.go` now accept `127.0.0.1` and `https://127.0.0.1` when the bound port is 443,
and only then. `TestApp_AppAddressForms` covers both ports, other hosts, an empty port and the
wrong scheme. Its RED failed on the 443 portless rows. Reversions: removing the equivalence,
extending it to every port, and ignoring the Origin's scheme each fail. Tests cannot bind 443, so
the rule is a pure function that the guard calls. At codex-062's suggestion,
`TestApp_Port443PortlessFormsThroughTheGuard` also drives the real handler chain with the bound
port set to 443, without a socket. It covers portless and explicit GET and POST, another port, an
`http` Origin, and WebSocket upgrades: the portless pair passes, and no Origin or an `http` Origin
is refused. Reconnecting the guard to the literal Host or Origin comparison fails it.

### C1d brief — the shell pins smd's real identity (2026-11-11; done: c9e9f2f3, accepted by the operator)

Started on the operator's direction ("yes, start the C1d brief"). Implements ADR 0103 point 3 (the
pin, with discovery), point 5's shell side and point 6's waiting page against `smd`'s own listener
(C1c). Once its acceptance passes, A1's restriction lifts: the shell may load the real `smd`'s UI.
The A1 and C1 fixture scenarios remain regression gates, including their download and redirect
coverage.

**Outcome the operator sees:**
- Starting the desktop app opens Station Manager from the local `smd`, over the pinned HTTPS
  listener.
- With `smd` down, or before it has ever made its identity, the app shows its own waiting page and
  opens by itself when `smd` answers.
- With an unsafe identity, the waiting page says what is wrong and names the path (Q2); nothing
  from the listener runs.

Nearest confusable outcome: an app that connects to whatever answers on 8443, or one that trusts
the identity because a file exists, without checking where it is and who could have written it.

**What exists today (read for this brief, verified after codex-064):**
- The shell runs fixtures only (`desktop/src/main.mjs`). `src/pin.mjs` pins a given fingerprint;
  `src/waiting.mjs` probes through the pinned session with redirects refused; `src/hardening.mjs`
  confines requests to the origin, with `wss:` beside `https:`.
- **Where `smd` keeps the identity:** `desktopid.Ensure(d.cfgSvc.WorkingDir())`, which is
  `utils.WorkingDir(cfg.DataDir)` (`internal/config/config.go:2605-2614`).
  - An absent `data_dir` defaults to the directory of `config.json` (`applyDefaults(&cfg,
    filepath.Dir(path))`, `config.go:680`).
  - A relative `data_dir` is made absolute against **the daemon's** working directory
    (`utils.WorkingDir` → `filepath.Abs`), which the shell may not share.
  - `config.json` itself is found by `defaultConfigPath` (`SM_WORKING_DIR`, then XDG, then the
    executable's directory; `cmd/smd/main.go:910`) unless `--config` is given.
  - The packaged unit sets `SM_WORKING_DIR=%h/.local/share/station-manager`
    (`packaging/smd.service:22`).
- **The port** is `server.app_port` in `config.json` (8443 when absent or 0). `config.json` holds
  credentials and is `0600`.
- **The real SPA is served only on a `tcp` browser listener with `serve_spa` on**
  (`internal/api/server.go:551`, a shared handler, so the app listener has it then too). Codex
  measured `GET /` at 404 with a Unix browser listener and 200 with TCP. The UI's canonical route
  is `/`; `frontend/embed.go`'s `/app/` comment is stale (outside C1d).
- CI's `releasability` job has Go (`ci.yml:88`), but runs the desktop scenarios **before** the SPA
  build (`ci.yml:155-166`).
- The loaded workspace's "daemon unavailable" state is SPA work (ADR 0102 ruling 6), not the
  shell's.

**Proposed design:**
1. **Discovery, read-only, in the main process (Q1).** `config.json` is found by `smd`'s default
   rule: an explicit `--sm-config <path>` (portable or development layouts), else
   `SM_WORKING_DIR/config.json`, else `$XDG_DATA_HOME/station-manager/config.json`. The
   executable-directory fallback is never used. Two fields are extracted, nothing else:
   - **`data_dir`:** absent or empty means the directory of `config.json`, as `smd` does. An
     absolute path is used as given. A **relative path is refused** unless an explicit
     `--sm-data-dir` names the absolute root, because it depends on the daemon's working
     directory; it is never reinterpreted relative to `config.json`.
   - **`server.app_port`:** absent or 0 means 8443; a non-integer or out-of-range value is
     refused, never replaced by a default.
   - **Handling:** no copy of the document is kept and nothing is written back. No document
     content, credential, parse-error snippet or raw error object reaches the renderer, IPC or any
     log. A missing or malformed `config.json` is a local "waiting" state, re-read on each probe
     before the first pin, so an app started before `smd` has written its config still finds a
     later non-default root or port. (Dropping a JavaScript reference does not erase memory; no
     such promise is made.)
2. **Validation, the shell's role (ADR 0103 point 1), a small Linux-scoped Node module:**
   - the data root is resolved once (`realpath`), its ancestors are checked (owned by root or the
     user; not group- or other-writable unless sticky), and only the resolved path is used
     afterwards, as `trustedRoot` does in `smd`;
   - `desktop-identity` is checked with `lstat` (no symlink): owned by the process's effective
     UID, mode `0700`;
   - `cert.pem` is checked with `lstat`: a regular file, not a symlink, owned by the effective UID,
     not group- or other-writable, parseable;
   - the fingerprint is SHA-256 over the certificate's DER encoding;
   - **`key.pem` is never opened**, and no key-pair check is made: TLS proves possession;
   - dates are not checked (the ruled date-independent pin);
   - adversarial fixtures and expected outcomes may be shared with C1b's tests, with no shared
     runtime framework.
3. **The pin and the origin are fixed together**, once, at the first valid read. The origin is
   normalised as a URL (port 443 written as `https://127.0.0.1`). The verifier is installed on
   the session before any request, so there is no unpinned network phase (Electron caches
   verification results). Before a pin exists, verification rejects everything. The shell never
   re-pins in response to a TLS failure.
4. **Identity states and sequence boundaries:**
   - **No identity yet** (cold start): the waiting page. No request goes to the app origin until a
     valid identity is read, which is re-read on each probe.
   - **A local refusal** (unsafe mode, foreign owner, symlink, non-regular or malformed
     certificate, unsafe ancestor): the waiting page names the path and the
     reason (Q2), nothing is pinned, and the shell keeps re-checking.
   - **A substituted but locally valid certificate read before the first pin:** the shell
     validly pins it, and the running `smd`'s original certificate is then refused at TLS. That is
     "unavailable", not a local refusal; the file alone cannot show a mismatch.
   - **The certificate changes on disk after the pin:** the app keeps its pin. The original
     daemon still matches; a daemon with a new identity is refused until the next launch.
5. **Modes:** interactive `npm start` targets the real daemon by this discovery. `check:a1` and
   `check:c1` are unchanged. A1's "no way to point it at smd" comment and restriction go only in
   the change that adds C1d's passing acceptance.
6. **No fallback:** a TLS or pin failure is "unavailable", never a reason to try `http:`, 8080 or
   the system's PKI.

**Acceptance** (`check:c1d`, against a **real `smd`** built from the tree with the SPA embedded,
never the live station). Setup:
- a temporary working directory as `SM_WORKING_DIR`;
- a synthetic seeded `config.json`: setup complete, a synthetic station, every external
  integration off, bridge and FT8 off;
- an ephemeral loopback **TCP** browser listener with `serve_spa` on, and a separate free app
  port.

In CI it runs **after** the SPA build and the `smd` build. Cases:
- **cold start, no identity:** the waiting page, and no request to the app origin. Once `smd`
  starts and creates its identity, the shell pins it and the **real SPA** runs: its own UI is
  rendered, not a status code or a placeholder;
- **cold start with an existing valid identity and an impostor** (another key) on the app port:
  the pin rejects it, nothing reaches it, and the shell stays on the waiting page;
- **transports:** `fetch`, `wss:` (`/v1/events/ws`) and EventSource (`/v1/events`) work against
  `smd`; the pinned `/v1/version` probe refuses redirects;
- **a loaded workspace with a real draft** typed into one of the SPA's own fields: `smd` is
  stopped and an impostor put in its place, and every fresh attempt is refused with its own
  evidence, with nothing arriving there. Then `smd` is restarted, the transports recover, and the
  draft is still in the field;
- **local refusals, no pin and the reason shown:**
  - the identity directory at `0755`;
  - `cert.pem` group-writable;
  - a foreign owner (with a test seam for the owner lookup, as in C1b);
  - a symlinked identity directory;
  - a symlinked `cert.pem`;
  - a non-regular `cert.pem`;
  - a malformed `cert.pem`;
  - an unsafe ancestor;
  - a valid sticky ancestor is accepted;
- **a root alias swapped after resolution** (corrected after codex-065; not a refusal): the genuine
  root is resolved and validated, then its original alias is pointed at another identity before the
  certificate is read. The shell still pins the genuine certificate from the resolved path, and the
  substitute is never read, as C1b's `TestLoad_UsesTheResolvedPathNotASwappableAlias` does for
  `smd`. A mutation that re-opens through the original alias must fail this. Symlinked identity
  directories and certificates remain refusals;
- **substitution before the first read:** the shell pins the substitute, and `smd` is then
  unavailable (refused at TLS);
- **change after the pin:** the original `smd` still loads; a restarted `smd` with a new identity
  is refused until relaunch;
- **discovery:** `data_dir` absent (the `config.json` directory), absolute elsewhere, relative
  (refused without `--sm-data-dir`); an explicit `server.app_port`; an invalid port type or range
  (refused); `config.json` missing at launch and written later (found);
- **the key is never opened by the shell:** the shell's file-system boundary records every
  attempted open and read. An intentional mutation that opens `key.pem` and swallows the error
  must fail this. Access times and a mode-`000` copy are not evidence (relatime; a caught error
  would pass). `smd`'s own reads of the key are attributed separately;
- **the pin is asked only about 127.0.0.1.**

Reversion proofs, among them:
- only the fingerprint comparison removed: the impostor's page loads;
- discovery ignoring `data_dir`: that case fails;
- each validation rule removed: its case passes wrongly;
- the key-open mutation, as above;
- re-pinning on a TLS failure: the after-pin case loads a new identity.

**Rulings (operator, 2026-11-11): "Q1 - a, Q2 - yes".**
- **Q1 (a):** the shell reads `config.json` read-only for `data_dir` and `server.app_port`, as in
  design point 1. ADR 0103 point 3 has a dated note.
- **Q2 (yes):** the waiting page shows the path and a bounded reason as escaped local text.

Implementation is not yet directed.

**Questions as asked:**
- **Q1. Finding the identity when `data_dir` is set.** Options:
  - (a) the shell reads `config.json` read-only for `data_dir` and `server.app_port` only, as in
    design point 1;
  - (b) the operator passes the identity directory and port to the shell explicitly;
  - (c) change `smd` so the identity always lives in the canonical working directory, ignoring
    `data_dir`, which changes committed C1b/C1c behaviour.

  Claude and Codex (codex-064) recommend (a). ADR 0103 point 3 would get a dated note naming
  `data_dir`.
- **Q2. A refused identity on the waiting page:** show the path and a bounded, actionable reason
  as escaped local text, with no config contents and no file access from the page (Claude and
  Codex recommend this); or only "waiting"?

**Not in C1d:**
- the SPA's "daemon unavailable" state (ADR 0102 ruling 6);
- the operating credential (C2);
- presence and disarm (C4);
- the window set (A2);
- live deployment.

Using the app against the **live station** needs a C1c-or-later build deployed there. That
deployment, and any live use, are the operator's call. Before C2 the app is one more operating
client, like a browser tab: it adds no new TX path, and an FT8 run still starts only on the
operator's action.

### C1d — progress (2026-11-11)

- **Part 1, committed `6b1c1f94`** on direction ("yes, commit it and start part 2"):
  `desktop/src/discovery.mjs`, 16 unit tests and 22 reversion proofs. Reviewed through codex-067;
  the post-commit clean-room review found nothing actionable.
- **Part 2, the shell's wiring (built, uncommitted, for review):**
  - `src/pin.mjs`: `oncePin()`, set exactly once; the verifier reads it on each verification, and
    an empty pin rejects everything.
  - `src/hardening.mjs` and `src/window.mjs` take the origin as a value read on use; no origin
    means nothing is allowed on the network.
  - `src/waiting.mjs` escapes all text and shows the discovery's state, path and reason (Q2).
  - `src/daemon.mjs`: `--sm-config` and `--sm-data-dir`; discovery until the first valid
    identity, then the pin and origin fixed together in one synchronous step, never read again;
    then the existing pinned probe.
  - `src/main.mjs`: the start-up promise exits an automated run non-zero on any error. A set-up
    error once hung the check: the test directory name `sm <i>x</i>` contained a `/`.
  - The interactive start still loads only fixtures (A1's restriction stands until part 3).
- **Evidence:** `check:c1d-shell` (synthetic install, TLS fixtures, never `smd`), 14 checks:
  - no identity: the waiting page says so, and nothing is sent to an impostor listening at the
    address;
  - an unsafe identity: the path and reason are shown, a path containing markup is shown as text,
    and still nothing is sent;
  - a valid identity: pinned; the impostor is refused at TLS; the genuine listener loads;
  - after the pin: a new identity on disk and at the address is refused, with a refused handshake
    as evidence; nothing is re-read (12 opens before and after); the original recovers;
  - a second pin is refused; the verifier is asked only about 127.0.0.1; nothing is blocked.

  Wired into `task desktop:check`, `ci-local` and CI. **Six Electron reversions proven:**
  - early probing: caught by the request filter, the guarding layer while no origin is fixed;
  - the path unescaped;
  - no reason on the waiting page;
  - re-reading after the pin (12 to 26 opens);
  - a second pin allowed;
  - the fingerprint comparison removed: the impostor is accepted. This one needed a fix to the
    wait, which had been a timeout before.
- **Corrections after codex-068:**
  - At the transition to a valid identity, the ordinary pinned-daemon waiting page is shown once,
    so a repaired refusal ("must be mode 0700") no longer stays on screen while the probe runs.
  - `parseArgs` refuses a named option with a missing or empty value, or one followed by another
    option. The error is shown locally, with no discovery and no network, rather than becoming "no
    override" and selecting the default install.
  - New `test/daemon.test.mjs`, without Electron (a fake window records the pages loaded), covers
    no identity to valid and refused to valid while the listener is down, both parser cases, and
    the local error. `test:unit` now runs 21 tests.
  - `check:c1d-shell` gains the transition check (15 checks).
  - Reversions F1 (no transition page; unit and Electron), F2 (a missing value accepted) and F3
    (the parser error ignored) are proven.
- **Part 3, next:** `check:c1d` against a real `smd` (TCP browser listener with `serve_spa`, a
  free app port, the SPA embedded), the CI ordering after the SPA and `smd` builds, and only then
  the interactive start against the real daemon. The pinned probe moves to `/v1/version` (ADR 0103
  point 6; `waiting.mjs` probes `/` today, codex-068).
- **Part 2 committed `8f3847a4`** on direction ("yes, commit it and start part 3"); its post-commit
  clean-room review found nothing actionable.
- **Part 3, the real-`smd` acceptance and the interactive switch (built, uncommitted, for
  review):**
  - The pinned probe asks `GET /v1/version` (ADR 0103 point 6); the fixture harness answers it.
  - `src/launch.mjs`, `launchTrusted`, is the one launch path: verifier and hardening first,
    then the window, then discovery. The interactive app, `check:c1d` and `check:c1d-shell`
    all use it.
  - **Interactive `npm start` now opens the local `smd`** by discovery (`--sm-config`,
    `--sm-data-dir`). The fixture harness remains only for the A1 and C1 checks, and
    `SM_DESKTOP_HARNESS_DELAY_MS` is gone. **This lifts A1's restriction**, in the same change as
    the passing acceptance, as the brief requires.
  - `test/scenario-c1d.mjs`, `check:c1d`: a real `smd` (`SM_DESKTOP_SMD`) in a temporary
    `SM_WORKING_DIR`, with `data_dir` a separate absolute directory (Q1 (a) end to end), a TCP
    browser listener with the SPA on a free port, a free app port, and bridge, FT8 and evidence
    off. 14 checks:
    - cold start with no identity and an impostor on the port: nothing is sent;
    - `smd` creates the identity under `data_dir`: it is pinned, and the real SPA runs (its
      script set the title);
    - `fetch`, `wss:` and EventSource work;
    - a draft in the SPA's own field: with `smd` replaced by an impostor, every transport is
      refused with refused handshakes as evidence, nothing arrives, and the draft and URL are
      kept; after a restart the transports recover and the draft is still there;
    - a freshly started window (its own session, verifier and pin) with the identity present and
      an impostor: rejected, then the real SPA loads when `smd` returns;
    - a new `smd` identity is refused by the already-pinned window;
    - `key.pem` is never opened; the verifier is asked only about 127.0.0.1; nothing is blocked.
  - **CI** runs it after the static daemon build (`SM_DESKTOP_SMD=/tmp/smd`), with the same
    AppArmor handling. `ci-local` runs it after its daemon build, and `task desktop:check:c1d`
    builds and runs it.
  - **Reversions:** G1 (fingerprint comparison removed: the impostor's answer reaches the page)
    and G2 (discovery ignores `data_dir`: never pinned) are proven. Installing the verifier
    later is **not observable**: no network request exists before the pin, so no proof was
    attempted.
  - The README now describes the real-daemon start.
  - **Live use:** the interactive app reads the live station's `config.json` read-only, and needs
    a C1c-or-later `smd` on the station for its app listener. Neither the deployment nor a live
    run has been done; both are the operator's call.
- **Corrections after codex-070 (2026-11-11; the work was interrupted by a power outage and resumed
  afterwards, with `git fsck` clean):**
  - `check:c1d` now seeds a configured workspace (`setup_complete`, a placeholder
    `station_callsign` `N0CALL`). It asserts the contact form (`#lc-call`, no setup form) and keeps
    the draft in `#lc-call`.
  - `smd` is owned by `test/smd-process.mjs` (`managedProcess`) from spawn until it has exited:
    exit tracked from spawn, a spawn failure or early exit reported, SIGTERM then SIGKILL, and
    `stop()` returns only after the exit. Every resource is released in `finally` from
    acquisition. 6 unit tests with synthetic children; `test:unit` is now 27 tests.
  - Phase 3 no longer claims per-transport attribution, because the SPA's reconnects also reach the
    impostor; the C1 fixtures attribute each transport.
  - Phase 5 waits for any handshake.
  - Phase 6 adds a fresh session that pins the new identity, while the old windows keep the old
    pin.
  - A fresh window records `verifier < filter < window`.
  - `check:c1d-shell` proves the `/v1/version` probe: with `/` answering 200 and `/v1/version`
    503, the shell keeps waiting. Its wait is for any request, so a root probe fails the check
    itself.
  - The README's stale fixture-only text is replaced. An earlier edit had written a stale copy
    over its own replacements.
  - Results: C1d 16 checks, C1d-shell 16, `task desktop:check` passes.
  - Reversions proven: P1 (root probe), P2 (no wait after SIGKILL), P3 (no spawn error handler),
    P4 (verifier after the window: `filter < window < verifier`), and G1 and G2 again on the
    rewritten scenario. P1 first failed at its wait and was fixed, the same trap as E6.
  - "Fresh window" means a fresh, non-persistent session and window, not a restarted process.
- **Correction after codex-071:**
  - `managedProcess` treats an `error` event as a spawn failure only before the `spawn` event.
    Afterwards it is a failed operation on a live child, such as a signal that could not be
    delivered: it is recorded in `errors`, the handle is kept, and `stop()` fails rather than
    reporting the child reaped.
  - New `releaseAfter(proc, dir)` removes the scratch directory only once the exit is confirmed;
    otherwise it keeps the directory and returns it with the PID. `check:c1d`'s `finally` uses it
    and fails, naming both, when termination cannot be established.
  - Three new unit tests (30 in all), each reaping its own child in its own `finally`: a post-spawn
    EPERM keeps the child owned and alive; the scratch directory is kept when termination is
    unconfirmed; and removed once it is confirmed.
  - Reversions Q1 (any error taken as an exit) and Q2 (the directory removed regardless) proven.
  - `check:c1d` still passes, 16 of 16.
- **Part 3 committed `c9e9f2f3`** on direction, and pushed. CI run 43026953181 **passed**
  (28m47s), including `check:c1d`'s first run on CI. The post-commit clean-room review found
  nothing actionable.
- **Live desktop check (operator, 2026-11-11):** the operator deployed
  `2.0.0-alpha.5-212-gc9e9f2f3-dirty` with `task deploy:local:dev` and ran `npm start`. The app
  opened the station's Operate workspace over the pinned listener: archive Home, logbook Default
  (8,663), the rig on 14.255.000 USB, 20 m. Known and expected: Map and Manual do not open from
  the app, because the shell refuses same-origin pop-out windows until the window set (A2).
- **C1d accepted (operator, 2026-11-11): "yes, C1d accepted, commit it".** A1's fixture-only
  restriction is lifted: the desktop app loads the local smd's UI over the pinned listener.

### D1 brief — the desktop app as an installable package (2026-11-11; revised after codex-074 to codex-076; rulings Q1-Q5 2026-11-12; committed 14db8c23; being built, see the D1 progress below)

Started on the operator's direction ("yes, commit it and start the D1 brief"), after the ruling that
the browser is not retired until a user can launch the desktop app (Rulings, 2026-11-11). D1
delivers the shell package (ADR 0102 rulings 7-9); D2 (the repository and signing) follows. D1
stays development-only until the later release gates (D2-D6).

**Outcome the operator sees:** installing the desktop package puts **Station Manager** in the
desktop's application menu with its icon. Choosing it opens the app on the local `smd`, or on the
app's waiting page while `smd` is not running. There is no repository checkout, Node or `npm`.
Removing the desktop package as documented leaves `smd` and the browser working. Nearest
confusable outcome: a package that installs files but cannot be launched from the menu, one that
starts but runs the test fixtures, or one whose removal takes `smd` with it.

**What exists today (read and measured for this brief):**
- The daemon RPM is built by `nfpm` from `nfpm.yaml` (`scripts/release-rpm.sh`, the public path;
  `scripts/dev-rpm.sh`, the private keyed path). It has no desktop file and no scriptlets; `smd`
  is a systemd user service.
- The shell runs from a checkout. `desktop/node_modules/electron/dist` is Electron 44.9.3 (284 MB
  unpacked), pinned by `desktop/package-lock.json`. Its `install.js` passes the bundled
  `checksums.json` to `@electron/get`, which verifies the downloaded archive by SHA-256, unless
  `electron_use_remote_checksums` is set. An existing `dist` is not re-verified. The app is
  `desktop/src`, `assets` and `package.json`, with no runtime npm dependencies.
- **`src/main.mjs` statically imports `../harness/server.mjs`** (the test fixture harness) and
  switches to test modes on `SM_DESKTOP_SCENARIO`. Before anything else it takes
  `SM_DESKTOP_USERDATA` as the profile and a fake-media switch. A package without the harness
  would fail to start.
- **`app.isPackaged` depends on the executable's name** (codex-074; reproduced by Claude). The same
  runtime with an app in `resources/app` prints `isPackaged=false` when the binary is named
  `electron`, and `true` when it is renamed `station-manager-desktop`. A wrapper script does not
  change this.
- **Measured on this host (Fedora 44, this machine's configuration):**
  - The ELF files shipped in `dist` are `electron`, `chrome_crashpad_handler`, `chrome-sandbox`,
    `libffmpeg.so`, `libvk_swiftshader.so` and `libvulkan.so.1`.
  - Their libraries resolve to 76 system packages, with none missing: alsa-lib atk at-spi2-atk
    at-spi2-core avahi-libs bzip2-libs cairo cairo-gobject cups-libs dbus-libs expat fontconfig
    freetype fribidi gdk-pixbuf2 glib2 glibc glycin-libs gmp gnutls graphite2 gtk3 harfbuzz
    json-glib keyutils-libs krb5-libs lcms2 libblkid libbrotli libcloudproviders libcom_err
    libdatrie libdrm libepoxy libffi libgcc libidn2 libmount libpng libseccomp libselinux libtasn1
    libthai libtinysparql libunistring libwayland-client libwayland-cursor libwayland-egl libX11
    libXau libxcb libXcomposite libXcursor libXdamage libXext libXfixes libXi libXinerama
    libxkbcommon libxml2 libXrandr libXrender mesa-libgbm nettle nspr nss nss-util openssl-libs
    p11-kit pango pcre2 pixman sqlite-libs systemd-libs xz-libs zlib-ng-compat. This is
    supporting evidence from a populated workstation, not acceptance: runtime-loaded libraries
    are not visible to `ldd`.
  - `chrome-sandbox` is not setuid (`-rwxr-xr-x`); `user.max_user_namespaces` is 123510; the
    automated checks run with sandboxed renderers. This holds for this host's configuration only,
    not for Fedora in general.

**Proposed design:**
1. **A second package, `station-manager-desktop`** (amd64), built by `nfpm` from a new
   `nfpm-desktop.yaml` by `scripts/desktop-rpm.sh` (Task `rpm:desktop`). There is no new
   packaging tool. The shell holds no secret, so there is one public build path and no private
   marker. The build uses a clean `npm ci`, refuses checksum or source overrides
   (`electron_use_remote_checksums`, mirrors), and records the Electron version and the archive's
   SHA-256.
2. **Layout** (root-owned, modes preserved, absolute paths throughout):
   - `/usr/lib/station-manager-desktop/`: Electron's `dist`, **with the runtime binary itself
     named `station-manager-desktop`** (so `app.isPackaged` is true), and the app in
     `resources/app/` (`src/`, `assets/`, a production `package.json`). **No `test/`, no
     `harness/`, no `node_modules`.**
   - `/usr/bin/<command>` (Q1): a symlink to, or `exec` of, that binary by absolute path.
   - `/usr/share/applications/<id>.desktop`: `Name=Station Manager`, the icon, `Categories`, no
     `Terminal`, and a `StartupWMClass` matching the window's actual class, checked in the guest.
   - `/usr/share/icons/hicolor/{256x256/apps/*.png, scalable/apps/*.svg}`, from the SPA's
     `logo.svg`.
   - `/usr/share/licenses/station-manager-desktop/`: Electron's `LICENSE` and
     `LICENSES.chromium.html`.
3. **Production mode:** the first thing `main.mjs` decides is whether it is packaged. If so,
   `SM_DESKTOP_SCENARIO`, `SM_DESKTOP_USERDATA` and the fake-media switch are ignored before the
   profile is set, any switch is applied or the instance lock is taken. Every harness and test
   import is lazy, reached only on the development path. A packaged app given `a1`, `c1`, `c1d`,
   `c1d-shell` or `second` takes the ordinary path. The interactive path (`launchTrusted`) is
   unchanged.
4. **Dependencies:** `Requires: station-manager` with the version contract of Q4. The system
   packages are declared from the measured list above, then validated on a clean Fedora guest by
   launching the installed app (RPM headers inspected).
5. **Removal (codex-074):** `rpm -e station-manager-desktop` removes only the shell. With
   `dnf remove`, DNF may also remove `station-manager` if it was pulled in only as a dependency
   (its default `clean_requirements_on_remove`, an inference from DNF's documentation, to be
   tested in the guest). The documented install is therefore both packages by name
   (`dnf install station-manager station-manager-desktop`), so both are user-installed. The
   documented removal of the desktop alone is `dnf remove station-manager-desktop`, which keeps a
   user-installed `smd`. No scriptlet touches a user service, and no global DNF setting is
   changed. `rpm -e station-manager` with the desktop installed is refused by the dependency;
   `dnf remove station-manager` would offer to remove the desktop too, which is DNF's documented
   behaviour and is stated in the install guide.
6. **The sandbox:** shipped as measured, with `chrome-sandbox` not setuid and renderers sandboxed
   through user namespaces. The app is **never** run with `--no-sandbox`. Where namespaces are
   denied, it fails to start. **The failure must be visible for a menu launch with no terminal:**
   the guest test denies namespace creation, records where the error appears (for example the
   user journal), and the operator rules whether that is enough.
7. **A start-up trace:** the shell writes one line to stderr when its discovery state changes
   ("waiting: smd has not created its identity yet <path>", "pinned <origin>"), with paths and
   reasons only and never config contents. It also writes one line from the main window's actual
   `did-finish-load`, classifying its current URL as the local waiting page (a bounded label,
   never the `data:` URL or HTML) with the renderer's process ID from that same `webContents`,
   and one when a load fails or the renderer is gone. That proves the page loaded, not its
   visual or textual correctness, which the page tests and the guest check cover (codex-076).
   The trace is a diagnostic, not proof on its own (codex-075). Whether a menu launch's stderr
   reaches the user journal is a guest observation, not assumed: the guest test records where it
   lands and the command that retrieves it.
8. **The version:** SM's own version, computed once for both packages through the existing
   `scripts/version.sh` conversion, with the RPM version and release contract stated (Q4). Both
   packages are built from the same commit.
9. **CI:**
   - builds the RPM and checks its contents: no `test/` or `harness/`; the binary named
     `station-manager-desktop`; the licences, desktop file and icons; `desktop-file-validate`
     clean;
   - runs a **production smoke check** of the staged artifact. It runs from an unrelated working
     directory, with no checkout path and no Node on `PATH`, with synthetic `config.json` and data
     and no daemon. It runs once for each development selector, `SM_DESKTOP_SCENARIO` set to
     `a1`, `c1`, `c1d`, `c1d-shell` and `second`, each with `SM_DESKTOP_USERDATA=<dir>`
     (codex-075; `second` matters because its development branch exits early).
   - The smoke check asserts, for each run:
     - the trace reports waiting, and that the waiting page finished loading, with no load
       failure or lost renderer in the settle window;
     - the renderer the trace names is, by external `/proc` inspection, a live `--type=renderer`
       descendant of this launch (intermediate processes allowed); a missing, vanished,
       unrelated or uninspectable process fails, never skips;
     - that renderer shows, by external `/proc` inspection, seccomp filter mode (`Seccomp: 2`)
       and user and PID namespaces different from the main process's: observed namespace
       isolation and filter mode for that renderer only, not the filter's rules or Chromium's
       full sandbox policy (codex-076);
     - the process keeps running (not a test exit); the synthetic config is unchanged; and
       nothing is created in the hostile profile directory.
   - Each run has an overall deadline, and the harness owns and reaps every process it starts;
     the settle window is bounded.
   - **Reversion proofs,** in an isolated review copy of the staged artifact, never the shipped
     one: a broken waiting-page load fails the page check; `sandbox: false` and `--no-sandbox`
     each fail the sandbox check **on the CI runner itself** (seccomp can be inherited from the
     environment, so a host-only proof is not enough). Measured on this host in a scratch probe (2026-11-12): a
     sandboxed renderer shows `Seccomp: 2` and its own user and PID namespaces; with
     `--no-sandbox`, or `sandbox: false`, it shows `Seccomp: 0` and the host's namespaces.
   - The A1 checks of the production window options (`sandbox: true`,
     `contextIsolation: true`, `nodeIntegration: false`) stay.
   - `ELECTRON_FORCE_IS_PACKAGED` is never set. The A1, C1 and C1d checks stay on the development
     path.

**Acceptance** (CI's contents and smoke checks, then a clean Fedora guest, as in earlier acceptance
lanes; never the station until the operator chooses):
- **Install:** `dnf install station-manager station-manager-desktop` succeeds with no manual step.
  The menu shows **Station Manager** with its icon, and the window matches its desktop entry.
- **Launch:** with `smd` stopped, the waiting page (Q3); starting `smd` opens the workspace. A
  second launch raises the first. The launcher works from an unrelated directory with no Node
  installed.
- **Hostile environment:** `SM_DESKTOP_SCENARIO` and `SM_DESKTOP_USERDATA` set in the session do
  not change the installed app.
- **Removal:**
  - `dnf remove station-manager-desktop` with `smd` installed by name: `smd` and the browser keep
    working;
  - the same with `smd` pulled in only as a dependency: the observed outcome is recorded against
    the install guide's wording;
  - `rpm -e station-manager` with the desktop installed: refused.
- **Sandbox:** the renderers are sandboxed with no setuid helper and no `--no-sandbox`. With
  namespace creation denied, the app does not run unsandboxed, and where its error appears is
  recorded.
- **Dependencies:** the RPM's `Requires` are inspected, and the app runs on a clean guest that has
  only what they pull in.
- **Upgrade:** upgrading both packages while the app runs keeps the window running, and the new
  code applies at the app's next start. This proves only D1's part; D6 owns editing,
  detached-window and lazy-asset compatibility.
- **Package:** the file list matches the layout exactly. The size is measured and recorded.

**Questions for the operator (not decided here); recommendations from Claude and Codex
(codex-074) agree:**
- **Q1. The command.** **Ruled 2026-11-12: `station-manager`** (not `sm-desktop`); the menu
  name is "Station Manager".
- **Q2. Autostart of the desktop window at login** (`smd`'s own start-up, its systemd user
  service, is unchanged). **Ruled 2026-11-12: no**; the window opens only from the menu or the
  command ("launcher only", ADR 0102 ruling 7). The package installs no autostart entry.
- **Q3. When `smd` is not running.** **Ruled 2026-11-12: (a).**
  - (a) Show `smctl start` guidance on the waiting page when the daemon does not answer, keeping
    C1d's identity-refusal explanations, since an unavailable endpoint does not prove `smd` is
    stopped (recommended);
  - (b) the app starts the user service;
  - (c) only "waiting".
- **Q4. The version contract.** **Ruled 2026-11-12: exact matching versions during alpha**,
  computed once for both packages from the same commit (not a minimum version).
- **Q5. Where Electron comes from.** **Ruled 2026-11-12: the lockfile-pinned npm install**, with
  its SHA-256 artifact check documented and overrides refused (not a separate download).

**Not in D1:**
- the dnf repository and signing (D2);
- the security-update routine and support check (D3);
- the Chromium notices review (D4);
- the CSP and further hardening (D5);
- the full in-place upgrade acceptance (D6);
- window placement (A4).

No package scriptlet restarts or otherwise touches the operating daemon.

### D1 — progress (2026-11-12)

Started on the operator's direction ("yes, commit it and start D1"), after the brief was committed
(14db8c23). **Parts (Claude's proposal; not ruled):**
1. **Production mode, the start-up trace and the start guidance** in the shell's own code, with
   unit tests and a runtime proof against a scratch staging of the renamed runtime.
2. **The package:** `nfpm-desktop.yaml`, `scripts/desktop-rpm.sh` (Task `rpm:desktop`), the
   launcher, the desktop entry, the icons, the licences, `Requires` and the shared version; CI's
   content checks.
3. **The production smoke check** in CI against the staged artifact, with its reversion proofs on
   the CI runner (brief item 9).
Then the clean Fedora guest acceptance, which needs the operator.

**Part 1 (committed 152f7b4b on direction; the clean-room review found nothing):**
- `src/mode.mjs` decides first, from `app.isPackaged`: an installed app ignores
  `SM_DESKTOP_SCENARIO`, `SM_DESKTOP_USERDATA` and fake media; only a strict `false` is
  development. `src/main.mjs` applies it before the profile, any switch or the instance lock, and
  imports the fixture harness lazily.
- ESLint's `no-restricted-imports` is an allowlist on `src/`: only `electron`, `node:` built-ins
  and `./*.mjs` siblings (static `import` and `export … from`). `test/imports.test.mjs` proves it
  refuses ten static forms, allows dynamic `import()`, applies to `src/` only, and that `src/`
  passes. A regex scan was tried first and dropped: it misread `export const` and
  `export function` lines.
- `src/trace.mjs`: one prefixed stderr line per state change, control characters escaped. The
  window's lines come from `did-finish-load` itself, with a bounded label (`waiting page`, `app`,
  `other`) and the pid of that same `webContents`; a main-frame load failure other than `-3`
  (superseded) and `render-process-gone` are traced too. `src/daemon.mjs` traces each discovery
  state change once, `refused:` for a malformed option, and `pinned <origin>`.
- The waiting page (Q3 (a)): "If the daemon is not running, start it with `smctl start`" while
  waiting (pinned and not answering, or before the identity exists); never on a refusal.
- **Unit tests:** `test:unit` now covers 7 files, 54 tests (55 after codex-077), all passing; lint and format are clean.
- **The development path is unchanged:** `check:a1`, `check:c1` and `check:c1d-shell` (16 of 16)
  and `task desktop:check:c1d` against a real temporary `smd` (16 of 16) pass.
- **Runtime proof (scratch staging, this host):** Electron's `dist` copied to the scratchpad, the
  binary renamed `station-manager-desktop`, the app (`src/`, `assets/`, a production
  `package.json`) in `resources/app`, with no harness or tests. It was run under `xvfb-run`, from an
  unrelated directory, with a minimal environment, an isolated `HOME`, and a synthetic
  `config.json` on a free port, once for each of `a1`, `c1`, `c1d`, `c1d-shell` and `second`,
  each with a hostile `SM_DESKTOP_USERDATA`. Every run gave:
  - the trace `waiting: smd has not created its identity yet …` and
    `loaded waiting page renderer <pid>`;
  - the named pid is a `--type=renderer` descendant, with `Seccomp: 2` and user and PID
    namespaces different from the main process's;
  - the main process still running after the settle; no hostile profile; the config unchanged;
  - the profile under the isolated `HOME`.
  The D-Bus errors in its log come from the stripped environment.
- **Reversion proofs, each restored and verified byte for byte:**
  - R1, packaged honours the selectors: 7 mode tests fail. At runtime, on the staged copy only,
    the `a1` run creates the hostile profile, tries to load the absent harness and exits.
  - R2, a static harness import in `main.mjs`: the import test fails.
  - R3, the trace on `did-start-loading`: two trace tests fail.
  - R4, `-3` traced as a failure: the failure test fails.
  - R5, control characters not escaped: the line test fails.
  - R6 and R7, start advice on a refusal or none at all: their page tests fail.
  - R8 and R9, no discovery or pin trace: the trace test fails.
  - R10, the window trace removed: no unit test reaches it (it needs Electron). At runtime, on the
    staged copy, only the discovery line appears and no `loaded waiting page`. Part 3's smoke
    check is its automated proof.
- **codex-077 (P2), fixed:** `pageLabel` had named every `data:` document the waiting page, so
  an unrelated or blank local document that loaded would have passed for it. `src/waiting.mjs` now
  remembers the exact waiting documents it generated (the latest 32, in memory only), and the
  trace names a page the waiting page only when it is one of them; any other `data:` document is
  `other`. The new tests use unrelated, blank, JSON and altered waiting documents, and check the
  bound. Reversions: R11 (the scheme rule restored) fails two trace tests; R12 (no eviction)
  fails the bound test. At runtime Chromium reports the exact URL: `check:c1d-shell` (16 of 16)
  traced 3 waiting-page loads and 1 app load, and the staged `a1` run traced the waiting page.
  R13, on the staged copy only: the shell loads a wrong document, and the trace says
  `loaded other`, never `waiting page`. `test:unit` is now 55 tests.
- **Not yet in part 1:** a hardened probe. The scratch probe hung when R10 removed the renderer
  line, until `timeout` reaped it (no process was left). Part 3's harness needs the overall
  deadline and the owned-process cleanup that codex-076 asks for.

**Part 2 (committed 39b015fe on direction; the clean-room review found nothing): the package.**
Started on direction ("yes, commit it and start part 2").
- **Measured first, on the scratch staging (this host):**
  - the shipped ELF files' direct `NEEDED` libraries: 34 sonames, one of them (`libffmpeg.so`)
    shipped by Electron. All 33 others resolve to Fedora 44 packages (`rpm -q --whatprovides`):
    glibc, glib2, gtk3, nss, nspr, mesa-libgbm, the X11 and AT-SPI libraries and others.
  - a `/usr/bin` **symlink** launcher keeps the app installed: a hostile `a1` is ignored and no
    hostile profile is made.
  - under Xvfb, the visible main window's `WM_CLASS` is `station-manager-desktop` whether it
    is launched through the symlink or directly. The launcher's name appears only on an
    unmapped 10x10 leader window. Wayland's app id is left to the guest.
  - Fedora refreshes the desktop-entry and icon caches itself (`rpm -q --filetriggers`:
    `desktop-file-utils` and the icon theme), so the package needs no scriptlet for them.
  - Electron 44 has **no install script**: `install.js` downloads on first use. The build runs it
    explicitly.
- **`scripts/desktop-rpm.sh`** (Task `rpm:desktop`):
  - It refuses Electron source, checksum, platform and cache overrides by pattern
    (codex-079): any environment variable whose name contains `electron`, case-insensitively
    (the downloader reads `ELECTRON_*`, `electron_*`, `npm_config_electron_*`,
    `NPM_CONFIG_ELECTRON_*` and `npm_package_config_electron_*` in several spellings), plus
    `npm_config_platform`, `npm_config_arch` and `force_no_cache`; and any npm configuration
    key naming `electron`, `platform` or `arch`.
  - It runs a clean `npm ci --ignore-scripts` of the lockfile (which has no install scripts) in
    a temporary directory, then Electron's `install.js` with only `PATH`, an empty `HOME` and a
    fresh private cache in its environment, so an input the refusal does not name still cannot
    reach the downloader, and the archive is always downloaded and verified.
  - It asserts the archive's SHA-256 against `checksums.json` and records it in
    `build/desktop/electron-source.txt`. That is the installer's own trust source, so it is an
    explicit assertion on the archive used, not independent provenance (codex-079).
  - It stages the runtime renamed, with Electron's default app removed (a broken app must fail,
    not open Electron's demo), and the app with a production `package.json` carrying SM's
    version.
  - It packages with `nfpm-desktop.yaml`.
- **`nfpm-desktop.yaml`:**
  - the layout of the brief;
  - the desktop entry `station-manager-desktop.desktop`
    (`packaging/desktop/`, `Exec=station-manager`, `StartupWMClass=station-manager-desktop`,
    `Categories=Network;HamRadio;`);
  - the icons, the SPA's `logo.svg` and the shell's 256x256 `icon.png`;
  - the licences: SM's `COPYING`, `LICENSE.electron` and `LICENSES.chromium.html`;
  - no scriptlets;
  - `Requires: station-manager = <version>-1` (Q4).
- **Library requirements as sonames, a refinement of the brief** (it named packages): the 33
  `NEEDED` sonames as `soname()(64bit)` requirements, which is what rpmbuild would generate and
  is not tied to Fedora's package names. Libraries Chromium loads at run time are still the
  guest's to find.
- **`scripts/desktop-rpm-check.py`** inspects the built RPM itself: the header, the file list
  with modes and owners, and the extracted package.json, desktop entry, icon and each ELF
  file's `NEEDED`. It requires:
  - name, arch and version, and exactly one `station-manager = V-R` requirement;
  - library requirements equal to the shipped binaries' `NEEDED`, minus what it ships;
  - no scriptlet;
  - the renamed binary, no `electron` and no default app; the launcher symlink;
  - under `resources/app` only an allowlist (`package.json`, `src/*.mjs` equal to
    `desktop/src`, `assets/*`), and a production `package.json` with only six keys;
  - root ownership; nothing setuid, setgid or group/world-writable; the helpers executable;
  - the desktop entry's keys, and `desktop-file-validate` clean;
  - the icons (the PNG 256x256) and the licences.
- **It caught a real defect:** nfpm flattens a plain directory source, so `locales/`, `src/`
  and `assets/` had all landed in one directory. `type: tree` fixed it, and the allowlist then
  admits only the `src` and `assets` directory entries.
- **Result:** `task rpm:desktop` passes, `station-manager-desktop-<v>-1.x86_64`: 94 files,
  119 MiB, 33 library requirements; 84 s with the download. The RPM unpacked into the
  scratchpad and launched through its own symlink ran installed: the hostile `second` selector
  was ignored, the waiting page loaded in a sandboxed renderer, the app stayed alive, no hostile
  profile was made and the config was unchanged. It was not installed on this machine.
- **Reversion proofs:**
  - The checker's 16 unit tests: C1-C10 each remove one rule (the exact daemon requirement,
    the library comparison, the bundled exclusion, scriptlets, the unrenamed binary, the
    allowlist, setuid, the desktop keys, the directory entries, the package.json keys). Each
    fails its test. Restored and verified.
  - The artifact, repacked by nfpm from a scratch copy of the stage: unchanged passes. Each of
    these fails the check, naming the fault:
    - A1, the harness shipped;
    - A2, the binary not renamed;
    - A3, `station-manager >= V`;
    - A4, a library dropped;
    - A5, `chrome-sandbox` setuid;
    - A6, the default app kept;
    - A7, the development `package.json`.
  - The build guard, offline (`scripts/desktop_rpm_guard_test.py`, 5 tests; `npm ci` is a stub
    that writes a fake Electron package, so nothing is downloaded and the stage is untouched):
    35 override spellings and 7 npm configuration keys are each refused before `npm ci`; the
    downloader sees only `PATH`, `HOME` and the cache, even with `NODE_OPTIONS`, `HTTPS_PROXY`
    and an unnamed variable set; an archive without the pinned SHA-256 is refused before
    staging. Before the fix, 24 cases failed, including codex-079's
    `npm_package_config_electron_mirror` and `npm_config_electron_custom_filename`. Reversions,
    each failing and restored: R1 the inherited environment; R2 a prefix-only pattern; R3 no
    npm configuration scan; R4 `npm_config_platform` dropped; R5 a case-sensitive match; R6 the
    SHA-256 assertion disabled.
- **Wiring:** Task `rpm:desktop` (the checker's and the guard's tests, the build, the check); `ci-local.sh`
  runs it after the desktop scenarios; CI runs it after the desktop scenarios, installing
  `rpm`, `cpio`, `binutils` and `desktop-file-utils` and the pinned nfpm v2.47.0.
- **Not in part 2:** `docs/install.md` is unchanged, because nothing is published before D2; the
  documented `dnf install station-manager station-manager-desktop` goes there when there is a
  repository. The production smoke check is part 3. The guest acceptance is the operator's.

**Part 3 (committed 8c667809 on direction; its clean-room review's two P2s are fixed in a
follow-up, below): the production smoke check.** Started on direction ("yes, commit
it and start part 3").
- **`scripts/desktop-smoke.py <rpm>`** unpacks the built RPM into a temporary directory and runs
  its renamed runtime directly, once for each of `a1`, `c1`, `c1d`, `c1d-shell` and `second`.
  Each launch has a hostile `SM_DESKTOP_USERDATA` and runs:
  - from an unrelated directory, with an empty `PATH` (no Node, no checkout) and an isolated
    `HOME`;
  - with a synthetic `config.json` on a free port, and no daemon;
  - on the script's own Xvfb.
- **The verdict** (`judge`, pure; `scripts/desktop_smoke_test.py`, 27 tests with the harness's
  start and cleanup tests) requires, from the
  trace and from `/proc` read while the launch runs:
  - a `waiting:` line, a `loaded waiting page renderer <pid>` line that is also the last load,
    and no `load failed` or `renderer gone` line;
  - the named pid inspectable, a `--type=renderer` process of this binary, and an inspectable
    descendant of the launch (intermediate processes allowed, a cycle or an unreadable step
    fails);
  - `Seccomp: 2`, and user and PID namespaces different from the main process's;
  - the main process this binary, still running after the settle window, the config
    unchanged, no hostile profile, and the profile under the isolated `HOME`'s XDG config (the
    installed app's own);
  - no process of the launch left after the reap.

  Anything missing or unreadable is a failure; nothing skips.
- **Bounds and ownership:** each launch has a 45 s deadline and a 3 s settle window. It runs in
  its own session. The reap sends SIGTERM, then SIGKILL, to that session and to every process
  whose `argv[0]` is under that unpacked root, then checks that none is left. The Xvfb is the
  script's own and is killed at the end.
- **codex-081, fixed:**
  - Xvfb's start is bounded: `select()` against a 15 s deadline, not a blocking read. Every
    unsuccessful start closes both descriptors and kills and waits for the server.
  - The temporary directory is removed only after a passing run whose processes are confirmed
    gone. A failed check, or any exception (unpack, Xvfb, a launch's observation or reap),
    keeps it and names it on stderr, and the run still fails.
  - Tests:
    - a live but silent server (it fails within 5 s, without hanging the suite);
    - a server that exits without reporting a display;
    - a failure to start Xvfb at all (no descriptor leaks);
    - unpack, observation and reap failures (the reap case launches a synthetic sleeping
      child, and the real reap stops it);
    - a passing run with a process still listed;
    - success removes the directory.
  - Reversions, each failing and restored:
    - X1, the blocking read;
    - X2, no cleanup on a failed start;
    - X3, the write end leaking on a failed start;
    - K1, removal on an exception;
    - K2, the kept path not reported;
    - K3, unconfirmed termination ignored.
- **codex-082, fixed:** the exit status is decided after the cleanup check. A process of the
  run still listed turns a pass into a failure (non-zero), names the process and keeps the
  directory; the test asserts all three. Reversions: K4, the status unchanged by survivors;
  K5, the survivors not named; K1', removal on an exception. Each fails its test.
- **Measured on this host:** Chromium rewrites a child process's title as one space-joined
  string with no NULs, so the first smoke run named every renderer "not a renderer". The check
  failed closed, as intended. The cmdline is now split on spaces when it has one element, and
  the launch refuses a root path containing a space.
- **Reversions, on isolated hard-linked review copies** (`--reversions`; the reverted file is
  unlinked and rewritten, so the shared original is untouched). The control, an unchanged
  copy, passes. Each of these fails for its own reason:
  1. a wrong waiting document (`loaded other`): "never reported 'loaded waiting page'";
  2. a failing waiting-page load: `load failed waiting page -20`;
  3. `sandbox: false`: `Seccomp is 0`, and the host's user and PID namespaces;
  4. the window trace removed (part 1's R10): "never reported 'loaded waiting page'", within
     the deadline;
  5. `--no-sandbox` on the launch's command line: `Seccomp is 0`, and the host's namespaces.

  `--no-sandbox` appended from `main.mjs` (`app.commandLine.appendSwitch`) was tried first.
  The renderer crashed on `/dev/shm` instead, a different failure from the one claimed, so the
  reversion passes the real switch to the launch.
- **The verdict's own reversions,** each failing its test, then restored and verified:
  - V1, ancestry not required;
  - V2, Seccomp not checked;
  - V3, matching namespaces allowed;
  - V4, the last load ignored;
  - V5, failure lines ignored;
  - V6, a rewritten title not split;
  - V7, leftovers ignored;
  - V8, the renderer type not checked;
  - V9, an uninspectable intermediate assumed;
  - V10, any line containing the prefix counted. V10 first passed by accident (the slice
    garbled the line), so a direct trace-parsing test was added; it now fails.
- **Wiring:**
  - Task `rpm:desktop` runs the three test suites, the build, the content check, the smoke
    check and its reversions. On this host it passes in 2 min 40 s; all five launches waited in
    a sandboxed renderer; no process or temporary directory was left.
  - CI's RPM step also does this, with unprivileged user namespaces allowed for the step only
    (as the scenario steps do), so the reversions run on the runner itself.
  - `ci-local.sh` runs the Task.
- **Not in part 3:** the clean Fedora guest acceptance, which is the operator's.
- **The clean-room review of 8c667809 (two P2s), fixed:**
  - A reversion counted as caught even when its launch's processes survived the reap, and the
    final survivor check looked only at the original root, not the review copies. Now a launch
    with survivors never counts as caught, and the final check covers the whole work
    directory (`owned_under(work)`).
  - The sandbox reversions required a `Seccomp` diagnostic. Where the launching environment
    already applies a seccomp filter, every process shows `Seccomp: 2`, and only the
    namespaces tell. Now any of the Seccomp, user-namespace or PID-namespace diagnostics is
    the evidence.
  - Tests (33 now), for the reversion runner:
    - all caught passes;
    - a leftover fails a caught reversion, and fails the control;
    - namespace evidence alone is accepted for both sandbox reversions;
    - another reason is not caught;
    - a real process left under a review copy fails `main`, is named, and the directory is
      kept.
  - Reversions, each failing its test: C1, leftovers counted as caught; C2, Seccomp-only
    evidence; C3, the final check on the original root only.
  - Re-run on the built RPM: 5 of 5 launches pass, and the control passes while all 5
    reversions fail.
- **On the hosted runner (CI run 43194862069, on `319ba18a`, completed: success):**
  - the smoke check passed for `a1`, `c1`, `c1d`, `c1d-shell` and `second`, each with
    `loaded waiting page renderer <pid>`;
  - `--reversions`: the control passed, and all 5 failed for their own reason;
  - both sandbox reversions showed `Seccomp is 0` and the runner's own user and PID namespaces.

  Part 3's CI evidence is complete. The clean Fedora guest acceptance remains, and is the
  operator's.

### D1 addition: the Amateur Radio menu group (2026-11-12)

**Operator (chat, after installing the desktop app on the station):** "Station Manager desktop
should be added to the Amateur Radio Menu group - there is one alread defined in Fedora"; "It
is defaulting to Internet". On the proposal below: "yes, build it — only Station Manager".

- **What was measured first (Fedora 44, KDE Plasma, passive):**
  - The menu (`/etc/xdg/menus/plasma-applications.menu`) has no `HamRadio` category and no
    ham-radio menu. There is no such `.directory` file, and
    `/etc/xdg/menus/applications-merged/` was empty.
  - KDE's own builder (`kbuildsycoca6 --menutest`, temp cache and config) placed the entry
    under `Internet/`: `Network` is its main category, and `HamRadio` only an additional one.
  - Fedora's `wsjtx-3.0.1-1.fc44` (read from the package, not installed) has
    `Categories=AudioVideo;Audio;HamRadio;` and no menu file; the same test placed it under
    `Multimedia/`.
  - The Fedora packages that ship a merged menu are astronomy-menus, electronics-menu,
    games-menus, islamic-menus, multimedia-menus, security-menus, waydroid and wine-desktop.
    None is for amateur radio.
  - So Fedora defines no Amateur Radio group to join; the package has to bring it.
- **What the package now ships,** following `astronomy-menus-1.0-34.fc44`:
  - `/etc/xdg/menus/applications-merged/station-manager-desktop.menu`, a config file as
    Fedora's is: an "Amateur Radio" menu that includes the `HamRadio` category, and the removal
    of `station-manager-desktop.desktop`, by filename, from Internet.
  - `/usr/share/desktop-directories/station-manager-amateur-radio.directory`: the group's name
    and icon.
  - No scriptlet: the desktop reads `/etc/xdg/menus` itself.
- **Scope (operator ruling):** only Station Manager's own entry leaves Internet. Other
  `HamRadio` applications appear in the group as well and keep their existing places; this
  package does not rearrange another package's entry.
- **`Categories=Network;HamRadio;` is unchanged.** `desktop-file-validate` prints a hint for
  `HamRadio` without `Network` or `Audio`, and the content check fails on any validator output.
- **The content check** (`scripts/desktop-rpm-check.py`, 24 tests now) requires:
  - both files, and the menu file as the package's only config file;
  - the directory entry's `Type`, `Name` and `Icon`;
  - `HamRadio` among the desktop entry's categories, as a whole item;
  - the menu file to hold exactly the group (its `Directory`, and an `Include` of exactly
    `HamRadio`) and an Internet menu whose only rule excludes Station Manager's entry by
    filename; no other menu, no other element, well-formed XML.
- **Evidence:**
  - RED: the eight new tests failed before the rules existed; the sixteen existing ones passed.
  - Reversions M1 to M11 (and M6b), each one exact replacement in the check, each caught by
    its test and restored byte for byte. M1, an Internet exclusion by category or of other
    applications, is the operator's ruling.
  - The built RPM passes the check: 96 files, the menu file flagged `c`, no scriptlets.
  - KDE's menu test on the **extracted package** in temp dirs, with Fedora's `wsjtx.desktop`
    and a `Network;HamRadio;` probe entry beside it:
    `Amateur Radio/ station-manager-desktop.desktop` and nothing under Internet for it;
    `wsjtx` under both Multimedia and Amateur Radio; the probe under both Internet and Amateur
    Radio.
- **Not tested:** desktops other than KDE Plasma. GNOME's overview has no category menu. The
  file is the standard merged-menu mechanism, so a desktop that builds its menu from
  `/etc/xdg/menus` should show the group; that is an inference, not a measurement.
- **Cost of installing it:** the desktop RPM requires the exactly matching daemon version
  (ruling Q4), so a rebuilt desktop RPM needs the daemon redeployed from the same tree.
