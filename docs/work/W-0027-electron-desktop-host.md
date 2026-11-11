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
