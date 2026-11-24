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
  not made view-only or removed until a user can launch the desktop app. **Ruled 2026-11-13:
  C2 builds no view-only class** (ADR 0102, "Update 2026-11-13"); C3 below is not built.
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
- **2026-11-18 (operator): "D1 - accepted. Electron in the right way forward, just from a Session
  stand point it's the right choice."** D1 is accepted (record at the end of this dossier). The
  second sentence is the operator's judgement of the direction after using the installed app
  through C2 and C4: ADR 0102's choice stands, and the Session is the reason they name. ADR 0100
  (the Session that follows the app's lifetime) is the design that sentence points at; its build
  is not started.
- **2026-11-18 (operator): "W-0029 is fine, open the dossier".** ADR 0100's build has its own
  dossier, [W-0029](W-0029-operating-sessions.md). A2 and the whole of A3 wait on it.
- **2026-11-18 (operator): "If we are now planning to code-around a temporary feature, then
  deal with the tmp feature first! You need to check we are not wasting more time on coding
  and testing for features that we remove the next cycle - we are wasting time, effort and
  money!"** The temporary feature is the operating-client switch,
  `server.require_desktop_app`, ruled "a - temporary switch, later removed" on 2026-11-13
  (C2 brief, question 1). Its removal is now the next work, ahead of W-0029's build. That
  ruling placed the removal "when others can install the desktop app (D2 to D6)"; doing it
  first means the next release serves only the desktop app. That consequence is put to the
  operator with the removal's brief (C2e, at the end of this dossier) and is not assumed here.
- **2026-11-18 (operator), on the check's permanence: "Yes. We need to work on permanence. We
  don't care about backward compatibility with any other install. Assume that my install is
  the ONLY install. If it breaks someone elses install, they will let us know and we'll deal
  with it then. But there is only one other install I know of and we'll provide a migration
  path for them when needed."** C2e is directed. Its question 3 (whether anyone else installs
  a release before D2 to D6) is answered: no backward compatibility is owed to any existing
  install other than the operator's, and the one other known install gets a migration path
  when it is needed, not now.
  - **Corrected by the operator the same day,** after I restated it as "your install is the
    only one it is designed for": "not quite. All installs going forward, mine just happens
    to be the only install." So the permanent check is the design for **every install from
    here on**. What is dropped is compatibility with installs that already exist, of which
    the operator's is the only one, bar the one other known.
- **2026-11-18 (operator): "No, I don't use Vite"; "No, don't use the loadgenerator"; "Why
  would I rollback to an older build? We have never done this so far?"; and then "Delete the
  Cabrillo plain-HTTP mode and remove the downgrade tooling".** Four things leave with C2e:
  the Vite dev server, `cmd/loadgen`, `ukeicc-cabrillo.py`'s plain-HTTP mode, and the
  downgrade tooling. The last supersedes W-0021's ruling (e) of 2026-09-22, under which the
  tooling and its drill were built.
- **2026-11-18 (operator): "We'll verify all of this with Codex before we implement".**
  Nothing of C2e is implemented until Codex has verified the brief, part 1's measurement and
  the inventories. The rulings above settle what goes; they do not start the work.
- **2026-11-18 (operator): "commit and push part 2, delete both tests in part 3".** The two
  daemon tests that exist only for a station carrying on without its app listener
  (`TestDesktopListener_ABusyAppPortNeverStopsTheStation`,
  `TestDesktopListener_NeverTakesTheBrowserListenersPort`) are deleted in C2e part 3.
- **2026-11-18 (operator): "commit and push the follow-up, remove the field".** The field is
  `required` in the reply of `GET` and `POST /v1/app/lifetime`, which after C2e part 3 could
  only say `true`: it is removed in part 3, with `api-endpoints.md`, and the shell stops
  reading it. The follow-up is the redirect fix to the test clients (C2e part 2's section).
- **2026-11-18 (operator): "yes, start part 3".** C2e part 3 is built from that direction.
- **2026-11-18 (operator): "a, and record the Fox lines in the appropriate entry".** Asked
  how to deal with the finding that the one-way version 7 step breaks the config half of
  the downgrade tooling (C2e part 3's section), with three options: (a) take that half out
  in part 3, (b) keep version 6 and leave the key as a dead, ignored setting, (c) remove
  all the downgrade tooling first. **(a) is ruled.** The database half (`smd db-downgrade`,
  the SQLite down files) stays for the later, separate removal. The Fox lines are an FT8
  matter recorded in `docs/dogfood-inbox.md` (2026-11-18), not part of this dossier.
- **2026-11-19 (operator), on the account-name clash found in the review of part 3: "This
  cannot happen. As discussed, assume THERE ARE NO INSTALLS BUT MINE, and the one we know
  about will be handled manually".** It applies the ruling of 2026-11-18 above, which I had
  not applied: the clash needs the first seeding of an older install's Home archive, the
  operator's is seeded, and the one other known install is handled by hand. **Closed: no
  fix, no inbox entry.**
- **2026-11-19 (operator): "README approved, remove preview, commit and push part 3".** The
  reworded first paragraph of `README.md` is approved. `frontend/app`'s `preview` script
  (`vite preview`) is removed with part 3, as its dev server was. Part 3 is committed and
  pushed on this direction; the Fox notes (the inbox entry and the discussion record's
  pointer) are not in that commit.
- **2026-11-19 (operator): "refusal, item 2 agreed".** Asked two things about the follow-up
  to part 3's review (its section, below). One: a `config.json` whose main listener already
  holds the desktop app's endpoint (`127.0.0.1` and the app's port) is **refused at load**,
  not warned about. Two: a host name in `socket_path` (`localhost:8443`) is **not judged by
  the validation** and is left to the bind, which stops the daemon with its own message if
  the two collide.
- **2026-11-19 (operator): "commit and push the follow-up and the Fox notes after CI".** The
  follow-up to part 3's review is committed and pushed once the hosted run on 8cccbd09 has
  given its verdict (a newer push cancels a running one), and the Fox notes go in their own
  documents commit.
- **2026-11-19 (operator), the listeners' ports: "approved, leave explicit 0 as the
  default"**, then **"commit and push the port rules".** Under `server.protocol` `tcp`,
  `socket_path` must be host:port with a plain decimal port from 1024 to 65535; a given
  `server.app_port` must be in the same range, and left out or 0 it is the default 8443.
  The operator's words that led to it, and the build, are in the part 3 section's review
  record. Committed 8e957293.
- **2026-11-19 (operator): "accepted, commit and push the dossier".** C2e is accepted at
  the station after the deploy of `2.0.0~alpha.5.275.g8e957293` (its section, at the end of
  this dossier).
- **2026-11-19 (operator): "align them. I want to finish the security work and then move
  on, not leave items dangling..."**, then, on the three questions that followed (its
  section, at the end of this dossier): **"commit and push 1, fix the redirect now, keep
  the stronger guarantee".** One: the readers' port range and the scenario's race fix are
  committed and pushed. Two: the redirect path is fixed now. Three: the guarantee stays
  that an address other than the daemon's gets **no contact at all** from the app, not
  only no request and no credential.
- **2026-11-19 (operator): "commit and push the redirect guard".** After Codex's review
  closed with no blocking finding (codex-173, codex-174).
- **2026-11-19 (operator): "commit, but no push".** For the scenario's strengthened
  downgrade check (the clean-room finding on 99c9c840): committed locally and not pushed,
  so that the hosted run on 99c9c840 is not cancelled by a newer push.
- **2026-11-19 (operator): "commit"**, then **"commit and push".** The two fixes to that
  check that its clean-room reviews asked for: 92002312 committed locally, then 7adb5d48
  committed and all three test commits pushed together.
- **2026-11-19 (operator): "accepted, the rig was off, commit and push the dossier".** The
  station's deploy of `2.0.0~alpha.5.281.g7adb5d48` is accepted (its record, at the end of
  this dossier). The "rig went quiet" and "rig unreachable" warnings after the day's
  deploys are the rig being switched off, on the operator's word.
- **2026-11-19 (operator): "Accepted".** Said of nine items listed for
  [W-0029](W-0029-operating-sessions.md) (its Rulings hold the list). Two are this
  dossier's: the database half of the downgrade tooling is removed **before** W-0029's
  part 1, and the up migrations are **not folded** into one file; only the way down goes.
  The plan is at the end of this dossier, for Codex's verification before anything is
  deleted.
- **2026-11-19 (operator): "build the removal".** After Codex verified the plan
  (codex-181). The build starts with the sorting table of the affected tests, which goes
  to Codex before any test is changed.
- **2026-11-23 (operator): "Elsewhere. commit and push, then A3".** A3 starts, with its
  acceptance criteria first.
- **2026-11-23 (operator): "go with recommendations, commit and push".** Said of A3's six
  questions, after Codex's review of the criteria closed with no finding open (codex-237).
  Each is ruled as recommended; they are set out under "A3: acceptance criteria" in the
  Record, "Ruled".

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

### D1 addition: upgrading the pair, and a deploy that never leaves `smd` stopped (2026-11-12)

**The incident.** With the desktop package installed, the operator ran `task deploy:local:dev`
for the menu-group build. The script stopped `smd`, then ran `rpm -Uvh` on the daemon RPM
alone. rpm refused: "station-manager = 2.0.0~alpha.5.229.g79c4a285-1 is needed by (installed)
station-manager-desktop-2.0.0~alpha.5.229.g79c4a285-1.x86_64". The script exited there, and the
station was left without its daemon. The steps the operator had been given (deploy, then
install the desktop RPM) could not work: with an exact requirement (ruling Q4), the two
packages can only change in one transaction. The operator installed both RPMs in one
`rpm -Uvh` and started `smd`; the pair is now `2.0.0~alpha.5.230.gecdbceb3-1`.

**Operator (chat):** "station is back up, fix the deploy script".

- **`scripts/deploy-local-dev.sh` now:**
  - **refuses early when the pair is needed and is not at hand.** If `station-manager-desktop`
    is installed and the deploy changes the daemon's version (from `scripts/version.sh`, as
    `dev-rpm.sh` takes it), `build/release/station-manager-desktop.x86_64.rpm` must have that
    version. Otherwise it says to run `task rpm:desktop`, before anything is built or stopped.
    Rebuilding the installed version needs no desktop RPM.
  - **tries the install before the stop:** `rpm -U --test --replacepkgs` on the transaction it
    is about to run. Whatever rpm would refuse is refused with `smd` still running.
  - **installs the pair in one transaction** when the desktop package is installed and the
    desktop RPM has the version just built.
  - **never leaves a running daemon stopped:** if the install fails after the stop, a daemon
    that was running is started again; one that was not running is not started.
  - It still does not build the desktop RPM itself: that downloads Electron and runs the
    package's own checks.
- **Tests** (`scripts/deploy_local_dev_test.py`, 9, also in `task ci:local` and CI): a copy of
  the script in a temporary directory, with stand-ins for `systemctl`, `sudo`, `rpm`, `ss` and
  the build. The stand-in `rpm` models the dependency rule. Six failed on the old script; one
  reproduced the incident.
- **Reversions D1 to D7,** each caught and restored: no dry run; the desktop RPM left out;
  no early refusal; a failed install leaving the daemon stopped; a failed install starting a
  daemon that was not running; the installed version demanding a desktop RPM; the old order.
- **Against the real system (passive):**
  - `rpm -U --test --replacepkgs`, unprivileged, accepted the pair and refused the daemon alone
    with the incident's message.
  - The real script, with `sudo` and every `systemctl` call except `is-active` blocked by
    guards, refused at the preflight on a tree whose version differed from the desktop RPM's.
    It built nothing, and `smd` stayed active.
- **Not exercised for real:** the stop, the install of the pair and the restart on failure
  (they need the operator's deploy).
- **A cost that follows from ruling Q4:** with the desktop app installed, every deploy of a new
  daemon version needs `task rpm:desktop` first (about five minutes, and an Electron
  download). **D2 must carry the same rule to a public install:** the repository and
  `docs/install.md` upgrade the two packages together.

### D1 addition: review of the menu group and the deploy fix, and two rulings (2026-11-13)

**Operator rulings (chat, 2026-11-13),** on the two questions left by the deploy fix:
"Whether the deploy should build the desktop RPM itself, and whether the exact-version rule
should hold for development builds - yes, yes".
1. The deploy builds the desktop RPM itself when the pair is needed.
2. Ruling Q4 (the exact version) holds for development builds too.

**Codex's review (codex-100) found two P2 defects in the committed work; both are fixed.**

- **The menu check read only the last section of each name.** `_menu_problems` kept
  `menus[name] = e`, so an earlier Internet section excluding `wsjtx.desktop`, followed by the
  expected one, passed. A desktop combines sections of one name, so that file would have moved
  another application: the "only Station Manager" guard did not hold. The shipped menu file was
  correct; the defect was in the guard.
  - Now a repeated section name is reported, and the whole file is compared with the one
    shape the package may ship (`MENU_SHAPE`): tags, attributes, text and the text after each
    element, comments and whitespace aside, in any order.
  - The same class, found while fixing it: a second `<Name>` in a section, an attribute, stray
    text and another root name all passed before.
  - Tests: 26. Reversions N1 to N5, and N1+N2 together (the old behaviour), each caught.
- **An interruption at the install step left the daemon stopped.** The restart added on
  2026-11-12 ran only when `sudo rpm` returned a failure. Ctrl+C at the sudo prompt, or a
  termination, ended the script by the signal with `smd` stopped.
  - Now recovery has one owner, set before the stop: an `EXIT` trap, with `INT`, `TERM` and
    `HUP` turned into exits. From the stop until the daemon has been started again, however
    the script ends, it tries to start a daemon that was running; one that was not is left
    alone.
  - Bash runs a trap only after the foreground command has ended, so the package command is
    never still replacing files when the daemon is started. A test holds the install open
    after the termination and requires the start to come after its end.
  - It is an attempt to get the station back, not a package rollback, and the script says so.
    `kill -9` cannot be caught.
- **Wording corrected, as Codex advised:** `rpm -U --test` is rpm's simulation. It finds a
  missing dependency, a conflict or too little disk space before the stop; passing it does not
  guarantee the real install succeeds.

**Ruling 1, built: the deploy makes the pair itself.** With the desktop package installed and a
deploy that changes the version, `scripts/deploy-local-dev.sh`:
- builds the desktop RPM with `scripts/desktop-rpm.sh` (the Electron download), unless the one
  on disk already has the deploy's version, and confirms the built RPM has that version;
- runs the content check and the production smoke check on the RPM it will install, **whether
  it was just built or found on disk** (codex-101, below).

All of that happens before the daemon is built or stopped. If any step fails (no Internet, a
failed check), the deploy ends there and a running `smd` keeps running. It runs the steps of
`task rpm:desktop` that examine the artifact; the tests of those checks stay in CI.

- **Tests** (`scripts/deploy_local_dev_test.py`, 17 at this point; 20 after codex-101, below): the interruption cases with the
  daemon running and not running (Ctrl+C at the prompt; a termination during the install); a
  failed final start; the desktop RPM built and checked before the stop; a failed build, check
  or smoke run; a build of the wrong version; no build without a desktop package or a version
  change, or when the RPM already matches. RED first: 10 failed on the committed script, with
  the same signal exits Codex reported.
- **Reversions,** each caught and restored: D1, D2, D6, D7 (kept from 2026-11-12); I1 to I5
  (signals not handled; nothing owns the recovery; recovery for a daemon that was not running;
  recovery still armed after a good start; recovery not waiting for the package command); B1
  to B6 (no desktop build; no content check; no smoke check; a failed check ignored; a
  wrong-version build accepted; a matching RPM rebuilt).
- **The real script, guarded** (`sudo` and every `systemctl` call except `is-active` blocked by
  stand-ins on PATH), on the station's own tree: it built the desktop RPM, passed the real
  content check (96 files) and the real smoke check (5 launches), built the daemon RPM, and
  rpm's simulation accepted the pair. It was stopped at the guard on `systemctl stop`, and the
  recovery path then tried the start. `smd` stayed active throughout. The run's by-products
  were put back: the operator's two RPMs (SHA-256 verified) and `frontend/app/dist/index.html`.
- **Not exercised for real:** the stop, the paired install, and a recovery that actually starts
  the daemon. The operator's next deploy is their first real run.

**Codex's review of that revision (codex-101) found one more P2, fixed before commit.**
- **A retry trusted an RPM that had failed its check.** The checks ran only when the deploy
  built the desktop RPM. The build writes the file before it is checked, so a failed check left
  a file of the right version behind. The next deploy found the version matching, skipped the
  checks, and would have installed the rejected RPM.
- **Now only an RPM this run has checked goes into the transaction** (`WITH_DESKTOP`). A
  matching RPM on disk is not rebuilt, but it is checked like a new one.
- **A second gap of the same kind, closed with it:** a deploy rebuilding the installed version
  added any desktop RPM of that version on disk to the transaction, unchecked. That deploy does
  not need the pair, so it now installs the daemon alone.
- **Tests** (20 test methods): a failed content or smoke check, then a retry with the artifact and the
  failure still there (nothing stopped, nothing installed), then a third run once the check
  passes (installed without a rebuild); a matching RPM on disk checked before the stop, and
  refused if it fails; the same-version case; HUP alongside TERM.
- **Reversions** V1 to V3 and I6, with the earlier ones rerun: 19, each caught. Each changed
  script is now also parse-checked (`bash -n`) before its tests run. My first V1 was a syntax
  error, which fails every test and proves nothing; I replaced it with a valid restoration of
  the reviewed behaviour, which fails exactly the two tests that describe it.
- **Not test-first, said plainly:** the same-version test was written after the selection rule
  changed. V2 restores the old rule and that test fails.

### C2 scope question: keep a view-only browser, or no browser client at all (2026-11-13; ruled the same day, see "Ruling" below the inventory)

**How it arose.** Working through the session design (ADR 0100 §4), it turned out that any
other client of `/v1/ft8/events` keeps TX armed after the desktop app leaves FT8, a plain
browser tab being the likely one: the route has no operating-client check before C2. The
operator: "These situations is what partly prompted the switch from browser to desktop"; then
"you have just made the case to remove the ability to view via the browser? We need to
simplify?"; and, more widely, "My current view of the codebase is that it is too complicated
and this maintenance is more costly. The desktop should reduce complexity and with it reduce
the maintaiability burden?"

**What is accepted today.** ADR 0102 ruling 1 keeps the browser as a view-only client, off
unless enabled in `config.json`; ADR 0103 makes 8080 "the view-only listener" once C2 is on.
ADR 0102 lists the daemon work that costs: the operating-client check, **the view-only route
classification** ("every log, config and operating mutation, not only RF commands or hidden
buttons") and **the presence/subscription rule**.

**The position after review** (Claude's proposal in claude-208 and claude-209, corrected by
Codex in codex-103 to codex-105 and verified against the source; a recommendation, not a
ruling).

- **A desktop-only C2 is worth considering.** The extra-subscriber case alone does not require
  removing the browser: C2 closes it either way. What a view-only class costs is the
  classification: each route (some eighty are declared; the inventory below has the count) and each event
  stream sorted into view or operate, and kept sorted as routes are added. Dropping the class
  means that work is not built.
- **It does not make presence automatic.** A per-install credential says who is calling. C2
  still has to name the one operating subscription that is the operator's presence, and its
  loss and reconnect rules. An authenticated API call, a Map stream or a detached window must
  not keep FT8 running merely by being authenticated. The linger, the disarm through the
  guaranteed-stop path and the capture lifecycle stay.
- **Given up:** viewing in a local browser (little use once the app is installed), and viewing
  from another machine (ruling 1 has it off by default, and it is not built). ADR 0102 already
  names "Remote operation becomes a requirement" as a trigger to revisit view-only access.
  Nothing operating is lost: under the accepted design the browser cannot operate after C2.
- **Still to be provided for:**
  - the SPA development loop (`frontend/app/vite.config.ts` proxies `/v1` to 8080);
  - the tools that read the daemon over HTTP (`scripts/ukeicc-cabrillo.py --from-daemon`,
    `cmd/loadgen`, `scripts/tune-duration-probe.sh`), which remain API clients;
  - the deploy script's port check;
  - what becomes of the 8080 listener itself.
- **The earlier ruling still gates it:** the browser path is not retired until the desktop app
  can be installed and launched (D1's guest acceptance, and D2 to D6 for anyone but the
  operator).

**Does the desktop reduce complexity? Measured so far, it has added.**
- From the `pre-w0027` tag to `420fef3f` (`git diff --numstat`): 14,798 lines added and 1,264
  removed across 93 files. Leaving out docs, tests, harnesses and the lockfile: about 3,800
  lines of code added and about 40 removed, by Claude's count. That includes the W-0028 fix, a
  few hundred lines. Line counts show scale; the saving that matters is which behaviours,
  failure paths and dependencies disappear.
- It also added an operational coupling: the exact-version pair, and the deploy logic the
  2026-11-12 incident showed was needed.
- **What it adds for good:** an Electron runtime to keep current (ADR 0102's update targets),
  the package pair, and the packaging checks and smoke harness.
- **What it will not reduce:** the parts that are complicated for their own reasons (the FT8
  sequencer and its invariants, archives and activation, forwarding). For scale, the tree holds
  about 98,000 lines of Go and 38,500 of SPA code outside tests, about 205,000 lines of tests,
  and 105 ADRs.

**What retiring the browser does and does not let us remove.** The saving is mainly work not
built (the view-only classification), plus the 8080 surface once its tools are moved. It is
not a large deletion of existing guards:

| Candidate | Verdict | Why |
|---|---|---|
| Archive and default request guards (`archive_guard.go`, `default_guard.go`) | **Stay** | They stop an action reaching an archive or default the window does not show. One window can still hold an old draft or send a delayed request across a restart; imported QSO UUIDs can repeat across archives. The app's credential says who is calling, not which archive, default or session generation the action belongs to. ADR 0100 §5 keeps them. |
| Per-tab Session list (`session.svelte.ts`, sessionStorage) | **Replace, then remove** | It owns the list today. It goes only when ADR 0100's daemon-owned session replaces it. |
| Stream revival (`sse-reviving.ts`) | **No deletion shown** | The desktop runs the same SPA over the same transports; who may connect does not replace reconnecting after a network change. To be assessed in Electron. |
| Subscriber-count presence | **Replace the admission rule, keep the rest** | C2 names which subscription is the operator's presence. The linger, the disarm and the capture lifecycle remain. |
| Plain TCP on 8080 for the browser, and browser compatibility | **Plausibly removable** | After its consumers move: the Vite proxy, the Cabrillo reader, loadgen and the tune probe. No broad unauthenticated loopback exemption in its place. The API listener itself stays (see the inventory). |
| The `beforeunload` hook (`lib/config/unsaved.ts`), the anchor download (`lib/api/session-export.ts`), the new-tab links (`lib/ui/Sidebar.svelte`) | **Conditional** | Each needs its planned host replacement first (close confirmation, native Save, the finite window roles). Dirty-state tracking, export generation and external links keep their purposes. |

**C2's boundary, if it is desktop-only.**
- A default-deny check with a short explicit exception list, covering the non-`/v1` surfaces
  too (the SPA assets, the manual, the optional `/debug/pprof`).
- The same-user limit, server authentication and the reload rules are kept.
- ADR 0102's future QSO-only daemon submitter is not given the full operating credential or
  presence.
- No general authorization framework.

**Acceptance must rule out the nearest confusable outcome:** the browser UI stops working while
an unauthenticated API or stream client can still operate, or keep FT8 running. So: a denied
subscription adds no presence and no capture demand; an authorized operating stream still
recovers safely; old-context actions are refused; the supported local tools and the desktop's
install and launch path work. A structural check that every route is behind the gate helps, but
does not prove those behaviours by itself.

**Recommendation (Claude and Codex), for the operator's ruling:**
1. Do a bounded inventory in this dossier before any saving is promised: for each candidate,
   the behaviour it owns, what must replace it, and the evidence that allows its removal.
2. Then rule on dropping view-only access. That changes ADR 0102 ruling 1 and ADR 0103's line
   about the view-only listener; the ADRs are amended only on that ruling.

Nothing here authorizes building C2 or removing anything.

### Inventory: what a desktop-only client lets us remove or leave unbuilt (2026-11-13)

**Operator (chat):** "commit the docs and do the inventory". Read-only: measured in the tree at
`50c04880` with `grep`, `wc` and `awk`; nothing was changed, built or run. Line counts include
comments. Each row names the behaviour the code owns, what would have to replace it, and the
evidence that would allow its removal.

**Corrected 2026-11-13 after Codex's review (codex-106), each point verified in the source:**
the route counts and the list of event streams; `POST /v1/contest-serial` (it allocates a
serial, so it is a mutation); the listener rows (the generic listener is not browser code); and
the `'storage'` listener (theme only). The conclusion is narrower as a result, not different.

**1. Work that would not be built: the view-only classification.**
- **Counting method:** literal `Handle`/`HandleFunc` registrations with a method and a path in
  the non-test files of `internal/api`. That finds 77: 35 `GET`, 36 `POST`, 2 `PUT`, 2 `PATCH`,
  2 `DELETE`. Two more `GET` routes are built from constants, the WebSocket twins
  `/v1/events/ws` and `/v1/rig/events/ws` (`stream_ws.go`). These are declared routes; fewer
  are enabled in any one configuration (`/debug/pprof` is optional, and the rig and FT8 routes
  depend on those subsystems).
- **Event streams:** three by SSE (`/v1/events`, `/v1/ft8/events`, `/v1/rig/events`) and the
  two WebSocket twins. `/v1/station-events` is not one: it returns a JSON list.
- A view-only class needs each route classified, with coverage that the classification holds,
  and a judgment where the method does not settle it. Some of the latter:
  - reads with side effects or demand: `GET /v1/ft8/events` (capture demand and presence),
    `GET /v1/rig/events`, `GET /v1/enrich/callsign` (upstream lookups on the station's
    accounts, and cache writes);
  - reads of sensitive or operational state: `GET /v1/config`, `GET /v1/hardware`,
    `GET /v1/evidence/status`, `GET /debug/pprof/*`;
  - a post that only generates: `POST /v1/session/export`.
  - Not on that list: `POST /v1/contest-serial` allocates a serial
    (`AllocateContestSerialWithContext`), and the number is consumed even if the contact is
    never logged. It is a mutation.
- With a desktop-only C2 this is one default-deny gate and a short exception list. The
  operating-subscription rule is still needed either way. How many tests the classification
  would have taken is not something this inventory measures.

**2. Existing code, candidate by candidate.**

| Candidate | Size | Behaviour it owns | Replacement needed | Evidence that allows removal | Verdict |
|---|---|---|---|---|---|
| The API listener (`Server.ListenAndServe`; `server.protocol` `tcp` or `unix`, `socket_path`) | about 40 lines | The daemon's configured API transport, for any client. It is **not** browser code: the Unix-socket mode and the daemon-only deployment of ADR 0102 use it | n/a | n/a | **Stays.** The ruling retires the browser as a client, not this transport |
| Plain TCP on 127.0.0.1:8080 as the default the browser reaches | a default and its validation in `internal/config`; not measured | Lets a browser, the Vite proxy and the tools reach the API with no credential | C2's transport decision, accounting for the local tools and headless use | C2's brief settles it, and the tools below work | **For C2 to decide; not counted** |
| The same-origin guard for that listener (`requireSameOrigin`, `hostAllowed`, `tcpBindHost`, `originAllowed` in `internal/api/csrf.go`) | about 95 of 246 lines; tests 169 lines, shared with the app guard | DNS-rebinding and cross-origin defence wherever a browser can reach the listener; it already passes a Unix socket through | `requireAppOrigin` stays for the app listener | No browser-reachable TCP listener remains | **Stays until C2's transport decision** |
| Consumers of 8080: the Vite proxy (`frontend/app/vite.config.ts`), `scripts/ukeicc-cabrillo.py` (331 lines), `cmd/loadgen` (484), `scripts/tune-duration-probe.sh` (134), the deploy script's port check | config and call sites only | Development and operator tools over HTTP | Each tool presents the credential or uses a named local path; no broad loopback exemption | Each tool shown working without 8080 | **To migrate, not delete** |
| `beforeunload` warning (`lib/config/unsaved.ts`) | about 30 of 220 lines | Warns before a tab with unsaved settings is closed | The shell's close confirmation (ADR 0102 ruling 6) | The app asks before closing with unsaved settings | **Conditional** |
| Anchor download (`lib/api/session-export.ts`) | about 10 of 111 lines | Saves an exported file through the browser | A native Save dialog from the shell | Export saved through the shell in the installed app | **Conditional** |
| New-tab links (`target="_blank"`, 4 uses in 3 files) | a few lines | Opens the Manual and external links in another tab | The Manual window and the default-browser handler (the shell already routes `window.open`) | A2's Manual window exists | **Conditional** |
| Cross-tab theme sync (`'storage'` listener in `lib/ui/state.svelte.ts`) | one listener | Keeps the theme in step in the Map, which opens in its own tab. Theme only | Whatever shares the theme with A2's Map and detached windows | A2's replacement keeps the theme in step | **Conditional; keep until then** |
| Per-tab Session list (`lib/operate/session.svelte.ts`, sessionStorage) | 154 lines | The Session list and its persistence | ADR 0100's daemon-owned session | ADR 0100 built and accepted | **Replace, then remove** |
| Subscriber presence and linger (`internal/ft8/service.go`, `onSubscriberAdded` to `onLingerExpired`) | about 100 lines | Capture demand, the grace, the unattended disarm | C2's rule for which subscription is the operator's | n/a | **Stays; the admission rule changes** |
| Request guards (`internal/api/archive_guard.go`, 72 lines; `default_guard.go`, 150) | 222 lines | Refuse an action aimed at an archive or default the window does not show | n/a | n/a | **Stays** |
| Stream revival (`lib/api/sse-reviving.ts`, 119 lines; `socket-reviving.ts`, 162) | 281 lines | Reopens a dead stream after a network or visibility change | n/a | An Electron assessment showing a case cannot occur | **Stays** |
| Other window-local state (sessionStorage in `archiveStores`, `callsignStack`, `ft8`, `SessionTimer`; localStorage in 9 files) | 41 uses | Drafts, picks, panel and display preferences | n/a | n/a | **Stays** |

- **Not found:** no service worker, no `BroadcastChannel` outside a test fixture, no
  `window.open`. The tab-coordination machinery of ADR 0085 is already gone (ADR 0087).
- **Documentation to revise if the browser path is retired:** `docs/install.md` (4 mentions of
  the 8080 address), `docs/architecture.md`, `docs/ft8.md`, `docs/smcloud-deploy.md`, and seven
  manual chapters that mention the browser or the port.

**3. The answer.**
- **Direct removal in the candidates examined is small:** about 50 lines of SPA code, each
  conditional on its host replacement. No daemon code is counted: the listener and its origin
  guard serve the generic API transport, and what C2 does with plain TCP on 8080 is its own
  decision.
- Replaced by other planned work, not saved: 154 lines (the Session list).
- Staying: the request guards, the revival rules, the presence code and the listener.
- **What a desktop-only C2 buys is on the other side:** the view-only classification of some
  eighty declared routes is never written, with the judgments it needs, and there is one kind
  of UI client to reason about.
- Line counts show scale. They do not measure maintenance value, which lies in the behaviours,
  failure paths and supported states that go.

**Limits.** Counts are from text searches, so a route or a browser dependency registered in a
way the searches do not match is missed. Sizes are of whole functions or files, not of what a
careful removal would leave. Nothing was assessed inside Electron.

### Ruling: C2 drops the view-only browser (2026-11-13)

**Operator (chat):** "yes, drop view-only for C2", after the inventory above.

- Recorded as a dated update in ADR 0102 (amending ruling 1) and a note in ADR 0103 (the HTTP
  listener is not a view-only listener after C2). The original rulings are left as made.
- **C2 is now:** one default-deny check for the desktop app's credential with a short explicit
  exception list, covering the non-`/v1` surfaces too.
- **C3, the view-only route classification, is not built.** The daemon submitter's separate,
  narrower credential (ADR 0014) keeps its place.
- **C4 stays as planned:** the rule naming the one operating subscription that is the
  operator's presence. The credential alone does not provide it.
- **Unchanged:** the 2026-11-11 timing ruling (no retirement of the browser path before the
  desktop app can be installed and launched), the request guards, the stream revival rules and
  the presence code.
- **Not ruled, for C2's brief:** the fate of the 8080 listener; how the Vite proxy, the
  Cabrillo reader, loadgen and the tune probe present the credential; the exception list; the
  acceptance evidence, which must rule out "the browser UI stops working while an
  unauthenticated client can still operate or keep FT8 running".
- **Next:** C2's brief, for review by the operator and Codex before any code. Not started; it
  waits for the operator's direction.

### C2 brief — the operating-client check (2026-11-13; reviewed, the operator's questions ruled)

Started on the operator's direction ("commit the docs and start the C2 brief"). It is a design
for review by the operator and Codex before any code. Where it proposes a default, a name or a
policy, that is a proposal: the questions at the end are the operator's to rule.

**Outcome the operator sees.** With the check switched on, the desktop app works exactly as it
does today, and a client without the app's credential is refused on every route and every
stream, bar the one named exception: a browser at the old address shows a short notice, and
nothing else. A process of the operator's own user that reads the credential is still inside
the accepted limit, and which connection counts as the operator's presence is C4's to define.
With the check off, nothing changes from today.

**Nearest confusable outcome:** the browser UI stops working, while an unauthenticated client
can still call the API, or hold an FT8 stream open and keep TX armed.

**What is already ruled.**
- Operating requires the desktop app; `smd` checks a per-install secret the app presents. Its
  limit is accepted: it keeps out other users and machines, not other processes of the same
  user (ADR 0102 ruling 1).
- C2 builds no view-only class: one default-deny check with a short explicit exception list
  (ADR 0102, "Update 2026-11-13").
- The credential is added by the shell's main process, as a request header, only to requests
  for the pinned origin. It is sent only inside an authenticated connection, never reaches page
  script and never leaves the machine (ADR 0103 point 7 and its requirements).
- The browser path is not retired until a user can install and launch the desktop app
  (2026-11-11).
- Once C2 is on, an app port that cannot be bound, or an unusable identity, is a start-up
  error; before that it is only reported (ADR 0103 point 2). Unconfirmed durability stays
  report-only in both states.
- Presence is its own rule and its own slice (C4). The request guards for archive, default and
  session generation stay (ADR 0100 §5). The daemon submitter keeps its own, narrower
  credential (ADR 0014).

**Ruled on this brief (operator, chat, 2026-11-13).**
- "I don't want to have code in the base to cater for a browser - too much complexity. Direct
  access by any client without the secret, or a second+ client is refused."
- On the four questions below: "2 - agreed" (the tools); "3 - a notice can be good" (what a
  browser shows); "4 - yes" (the health check is open).
- On the switch (question 1): "a - temporary switch, later removed". Off by default now, on at
  the operator's station once C2 is accepted there, and deleted with its off state when others
  can install the desktop app.
- On a Unix-socket main listener with the check on: "1 - yes". It is refused as TCP is: the
  health check and the notice only.
- On what "a second client" means (question 5): "1 - A". Reading A: no second user interface;
  a process of the operator's user that holds the credential, such as the ruled tools, is not
  what is refused.

**What exists today (read for this brief).**
- **One wrapping point.** The main listener's handler is built once
  (`internal/api/server.go:252`): security headers, request log, drain gate, concurrency limit,
  panic recovery, `requireSameOrigin`, then the router for `/v1` and the non-API surfaces (the
  SPA, the manual, optional pprof). The app listener serves that same handler (`BindApp`,
  `server.go:710`). A mark on the connection tells the origin guard which listener a request
  arrived on (`csrf.go:40`, `requireAppOrigin`); the check can use the same mark.
- **Routes:** 77 declared by literal method and path, plus two WebSocket twins; three SSE
  streams and the two WebSockets hold a connection (the inventory above).
- **The identity** (`internal/desktopid`): the directory `desktop-identity` under the working
  directory, mode `0700`, holding exactly `key.pem` and `cert.pem`. It is validated on every
  use, including each path component above it. An unexpected file is refused. It is created as
  one unit and never repaired or replaced silently.
- **The shell** (`desktop/src`): discovers the identity, pins the certificate
  (`pin.mjs`, `setCertificateVerifyProc`), and reaches `smd` only through the window's session.
  Its availability probe is `GET /v1/version` through that session (`waiting.mjs:86`).
  `/v1/version` carries archive identifiers, so it is not public data.
- **`GET /v1/healthz`** reports three words: an overall status, whether the database answers,
  and whether logging is degraded (`handler_health.go`). No station data.
- **Other clients of the API today:** the Vite development proxy (`/v1` to 8080);
  `scripts/ukeicc-cabrillo.py --from-daemon`; `scripts/tune-duration-probe.sh`; `cmd/loadgen`.
  `scripts/rollback-drill.sh` drives scratch daemons over a Unix socket.

**Proposed design.**

1. **The credential.**
   - 32 random bytes from the system's generator, base64url-encoded, in one file.
   - It lives in its own directory beside the identity, `desktop-credential/` (mode `0700`,
     file `0600`). It cannot go inside `desktop-identity`: that directory admits exactly two
     files, and identities already published must stay valid.
   - It gets the identity's discipline, by the same code where that can be shared without
     copying: the same ownership, mode and path-ancestor checks on every use; creation as one
     unit with the same sync barriers; never repaired or replaced silently; an error that names
     the path and never the value. Independent random bytes: it is not derived from the
     certificate or its fingerprint, which identify the server and are public to any client.
     No general secret store is built.
   - `smd` creates it at start whenever it is missing, **whether or not the check is on**, so
     switching the check on later needs nothing from the app.
   - **Replacement is deliberate:** delete the directory, and the next `smd` start makes a new
     one. A running app keeps the old value, is refused, and shows the daemon as unavailable
     until it is restarted. That is the identity's own rule.

2. **Presenting it (the shell).**
   - **Order at start:** the credential, the origin and the pin are established, and the
     header hook installed, before the first probe or navigation. A credential that does not
     exist yet keeps discovery waiting and retrying, as a missing identity does. An unsafe or
     malformed one is reported on the waiting page; nothing is sent.
   - The main process adds one request header (proposed name: `X-SM-Operating-Credential`) in
     the session's `webRequest.onBeforeSendHeaders`, for requests to the pinned origin only:
     its exact scheme, host and port, and the `wss` partner of that origin. Any header of that
     name already on the request, in any letter case, is replaced, so exactly one value goes.
   - Electron uses only the last listener registered for an event, so this hook has one owner.
     The request blocker in `hardening.mjs` is a different event and stays as it is.
   - It must cover every kind of request the app makes: the page and its assets, `fetch`, the
     probe, the manual, the SSE streams and the WebSocket handshakes, and a reconnect after an
     earlier success. It must not go to another port, to a plain-HTTP downgrade of the origin,
     or along a redirect that leaves the origin. That coverage is to be **proven by test**,
     not assumed from Electron's documentation.
   - **In a loaded workspace,** a refusal for the credential (after a deliberate replacement,
     say) is shown as a diagnostic that keeps the operator's drafts; the app adopts a new
     credential when it is restarted. That the SPA needs no change for this is a goal to
     verify, not something this brief has established.

3. **The check (`smd`).**
   - One middleware at the one wrapping point, after the Host and origin guard and before the
     router. So it stands in front of `/v1`, the SPA, the manual and pprof alike, and in front
     of every stream handler: a refused subscription never reaches the code that counts a
     subscriber or asks for audio capture, and a WebSocket is refused before it can be
     hijacked. The existing origin, draining and request-context guards are kept.
   - **Default deny.** A request passes only if it arrived on the app listener and carries
     exactly one credential header whose value matches, compared in constant time. Which
     listener it arrived on is read from the server's own mark on the connection
     (`appConnKey`), never from anything the client sends.
   - A listener or handler added later needs the same boundary; a test proves the assembly.
   - **Exception list (ruled): `GET /v1/healthz`, on either listener.** Nothing else.
     `/v1/version` is not an exception: the shell's probe goes through the session, so it
     carries the credential.
   - **Refusal:** `403` with a stable code (proposed: `desktop_app_required`) and no detail.
   - **The notice (ruled: "a notice can be good").** A request for a page on the main listener
     gets one short static notice that Station Manager is operated from the desktop app. That
     is the whole of what the daemon does for a browser: one fixed response, with no data, no
     script and no route of its own to maintain.
   - **Logging:** one line per refusal with the listener, the method and the path, and whether
     a credential was present. Never its value, and never the expected one.

4. **Where the credential is accepted: only on the app listener.**
   - It gives every client one authenticated endpoint and no plain-HTTP fallback. With the
     check on, the main listener serves the health check and the notice, and refuses a request
     even when it carries a valid credential.
   - That rule does not by itself keep the credential from an impostor: a client that has
     already sent it to a counterfeit listener has disclosed it. What prevents disclosure is the
     client authenticating `smd` **before** it sends anything, as the shell does with its pin
     and as each tool must (point 6).
   - **Transports and states, stated in full:**

     | Main listener | Check off | Check on |
     |---|---|---|
     | TCP (today's default, 8080) | Serves the UI and API as today | Bound; serves `GET /v1/healthz` and the notice only |
     | Unix socket (`server.protocol=unix`) | Serves the API as today, to whoever the socket's permissions admit | **Ruled (2026-11-13):** the same as TCP. This is a restriction of its own, not a consequence of the TCP argument: an owner-private socket has no impostor problem |
     | App listener (8443, TLS) | Serves the UI and API; a credential is ignored | Serves a request that carries the credential |

   - Keeping a socket bound is not keeping its API usable: with the check on, the main listener
     answers two things. Scratch daemons, the rollback drill (which uses a Unix socket) and
     headless or daemon-only use run with the check off until they have a design of their own.
   - This brief does not choose a one-listener design or remove a transport.

5. **The switch.**
   - One configuration key (proposed: `server.require_desktop_app`, a boolean, in
     `docs/v2-design/config.md` in the same change). **Ruled: off by default, and
     temporary** (question 1, outcome (a)). The name is still a proposal.
   - It is read at start and takes effect at a restart; it is not changed under a running
     daemon.
   - **Off:** today's behaviour on both listeners; the credential is created, and ignored.
   - **On:** the check applies, and ADR 0103's rule takes effect: an app port that cannot be
     bound, an unusable identity or an unusable credential is a start-up error. There is no
     silent fall back to off.
   - `smd` reports the state at every start, in the log and as a Station Event, so a station
     running without the check is visible.

6. **Tools and the development loop (ruled: "2 - agreed").**
   - The Cabrillo reader and the tune probe keep working with the check on. Each uses the app
     listener and sends the header from `desktop-credential/`. Both files it reads are readable
     only by the operator's user, which is the limit ADR 0102 accepts.
   - **Each tool verifies the server before it sends the credential, and how is to be
     specified and tested, not assumed.** `curl --cacert` and a Python `ssl` context with the
     certificate as their only trust anchor are CA verification. That is not automatically the
     shell's rule, which pins the exact certificate and does not enforce its dates. The part
     states the intended verification for each tool and tests it against an impostor.
   - No insecure or plain-HTTP fallback, and the credential is not forwarded on a redirect.
   - The credential stays out of URLs, command-line arguments, traces and error text. A tool
     reads it from the file and passes it as a header by a means that does not show in the
     process list.
   - Development (the Vite proxy), load generation and scratch daemons run with the check off.
     That is a configuration choice visible in the start report, not an exemption in the code.

7. **A second client (ruled: reading A).** The operator: "a second+ client is refused", and,
   asked which of two readings was meant, "1 - A".
   - **What is ruled: no second user interface.** The supported product has one: the
     installed desktop app. This design gives that without more machinery: only the app's shell
     adds the credential; the app is single-instance, so a second launch raises the running
     window (`desktop/src/main.mjs`); another user cannot read the credential; and a browser
     has none.
   - **What that is not:** a guarantee enforced by `smd` that only the app connects. The daemon
     cannot tell the app from another program of the operator's own user that reads the
     credential and presents it. The ruled tools are such programs, and one could be a user
     interface. That is inside the accepted limit, and no machinery is added to exclude it.
   - **Not built: one connected client at a time, whoever it is** (reading B). It would have
     needed a per-start identity beside the credential and a rule for when the first client
     counts as gone, and it would have refused the operator's own tools while the app runs.
     "Which connection is the operator's" stays with C4.

**What C2 does not do.**
- **Presence (C4).** After C2 an unauthenticated client can no longer hold an FT8 stream. An
  authenticated one that is not the operator's window still could, for example a tool. Naming
  the one operating subscription, and what its loss does, is C4.
- Reload and asset compatibility (C5); the daemon submitter (ADR 0014); remote or view-only
  access (deferred); removing either listener.

**Acceptance criteria.**
1. Check on: the desktop app loads and operates as today (the UI, every stream, logging, FT8).
2. Check on: for **every declared route** except the health check, a request without the
   credential is refused with the check's own code, on both listeners. The same holds for a
   wrong credential, an empty one and a repeated header. The exception is exactly
   `GET /v1/healthz`, which answers as it does today on both. A page request on the main
   listener gets the notice, which is a refusal: it is not a route into the SPA or the API.
3. Check on: a refused `/v1/ft8/events` subscription adds no subscriber and asks for no
   capture. With the desktop app away from FT8, an unauthenticated client cannot stop the
   linger from disarming TX.
4. Check on: a valid credential presented on the main listener is refused.
5. The credential is sent to the pinned origin only: never to another origin, never to an
   impostor on the app port (C1's acceptance, extended), and it is not readable from page
   script.
6. A credential directory or file with unsafe ownership or mode, or a replaceable ancestor, is
   refused by `smd` and by the shell, with the path named and the value never printed.
7. Check off: every behaviour is as today, including the browser as an operating client.
8. Check on, and the app port, the identity or the credential unusable: `smd` does not start,
   and says why. Check off: it starts and reports it.
9. The request guards still refuse an action from an old archive or default context.
10. The Cabrillo reader and the tune probe work with the check on.
11. A second launch of the desktop app raises the running window and does not become a second
    client; another user's process cannot read the credential.

**Evidence planned.** Tests first, each with a reversion proof that fails for its own reason.
- **`smd`:** integration tests on a real server with both listeners in temporary directories.
  - A test enumerates the declared routes and holds each to criterion 2, so a route added
    later is covered without being listed by hand. It includes the routes a literal search
    misses: the default-change routes registered by a helper, the WebSocket paths built from
    constants, and the rig, FT8 and profiling surfaces that exist only when enabled.
  - Each request is otherwise acceptable: a valid Host and Origin, a daemon serving normally.
    The test asserts the check's own refusal code and that nothing ran behind it: no handler,
    no subscriber, no capture demand. A refusal by another guard is not evidence the check ran.
  - The exception and the notice are tested too, not only the protected routes.
- **The credential unit:** the identity's own cases: creation boundaries and partial
  publication, two starts creating at once, unconfirmed durability, unsafe modes and owners, a
  replaceable ancestor, a malformed file, and refusal without repair.
- **The shell:** scenario tests against the fixture harness.
  - One per kind of request: navigation, asset, `fetch`, probe, SSE, WebSocket, and a
    reconnect.
  - Requests that must carry no header: an impostor on the app port, a foreign origin, another
    port, a plain-HTTP downgrade, and a redirect off the origin.
  - The credential's life: first creation while the app waits, an unsafe or malformed file,
    a restart with the same credential, a deliberate replacement, and adoption at restart.
- **The tools:** each shown to refuse an impostor before sending anything, and to work against
  the real listener.
- **The station:** the operator switches the check on and operates; a browser and a `curl`
  without the credential are shown refused.

**Proposed parts, each reviewed before the next.**
- **C2a, `smd`:** the credential unit, the check, the switch (off), the start report, the
  configuration reference, and the tests above.
- **C2b, the shell:** discovery of the credential, the header, and the scenarios.
- **C2c, around it:** the notice, the two tools, and `docs/install.md` and the manual where they
  tell a reader to use a browser.
- **C2d, switching it on** at the operator's station: the acceptance run, recorded here.
- **After C2, not part of it: removing the switch** and its off state, when others can install
  the desktop app (D2 to D6). It has its own brief, which first gives development, load
  generation, scratch daemons, the rollback drill and headless use a supported route.

**Questions for the operator.**
1. **The switch and how long it lives. Ruled: "a - temporary switch, later removed".** The
   three outcomes that were put, which earlier wording ran together:
   - **(a) A temporary switch, later removed. Ruled.** Off by default in this release. You
     switch it on at your station once C2 is accepted there. When others can install the
     desktop app (D2 to D6), the switch and its off state are deleted, and the check is always
     on. That applies your 2026-11-11 ruling, and it ends where your statement points: no
     configuration in which a client without the secret is served. The removal is a later part
     with its own brief, not part of C2: development, load generation, scratch daemons, the
     rollback drill and headless use each need a supported route before the off state goes.
   - **(b) A permanent switch whose default changes. Not chosen.** As (a), but when others can
     install the app only the default becomes on; off stays a supported setting. Development
     and the drill need no further design. A station can then always be set to serve any client
     without the secret.
   - **(c) No switch. Not chosen.** The check is always on from the release that ships C2. The parts
     change: C2a and C2b ship together, since an always-on check locks out a shell that does
     not yet send the credential; the routes named in (a) are designed first; criterion 7 and
     the check-off half of criterion 8 fall away. That release retires the browser, so under
     your 2026-11-11 ruling it waits until its users can install and launch the desktop app.
   - The parts and criteria in this brief are written for (a), so the ruling changes none of
     them. The removal is listed after C2d as work outside C2.
2. **Which tools must keep working with the check on? Ruled: "2 - agreed".** The Cabrillo
   reader and the tune probe keep working; loadgen and the Vite proxy run with the check off.
3. **What a browser shows at the old address. Ruled: "3 - a notice can be good".** One short
   static notice.
4. **Is `GET /v1/healthz` open without the credential? Ruled: "4 - yes".**
5. **What "a second client" means. Ruled: "1 - A"** (design point 7): no second user
   interface.

**Questions for Codex.**
1. Is refusing a valid credential on the main listener (point 4) right, or does it cost more
   than the capture risk it removes?
2. Is a separate `desktop-credential/` directory the right home, given the identity
   directory's two-file rule? Is there a reason to prefer a value derived from something the
   shell already reads?
3. Does one middleware at `server.go:252` really cover the app listener's handler and every
   stream, including the hijacked WebSocket paths, or is there a registration that bypasses it?
4. Is anything in ADR 0103's "C2 on" start-up rule in conflict with a switch that defaults to
   off?
5. On design point 7: is reading A a fair statement of what the design already gives, and is
   there a second-client case it leaves open that does not need C4?

### C2a — the daemon's side of the check (built 2026-11-13; committed 90db4857)

Built on the operator's direction ("commit the brief and start C2a"), test-first. The switch is
off by default, so an installed build behaves as before until it is switched on.
**Do not switch it on yet:** the desktop app does not send the credential until C2b, so the check
would lock the app out too.

**What exists now.**
- **The credential** (`internal/desktopid/credential.go`): `desktop-credential/credential` in the
  working directory, directory `0700`, file `0600`. The file's whole content is the value: 43
  base64url characters for 32 random bytes, no line break. It is validated on every use and
  refused, never repaired. No error and no formatting of it prints the value; `smd` holds only a
  constant-time comparison.
- **Shared creation.** The identity's creation was factored into one function that both units
  use (`publishIfAbsent` in `desktopid.go`): the same lock, the same sync barriers, the same
  single rename. The identity's own tests were the characterisation and pass unchanged, bar one
  test helper that now names either unit's prepared directory.
- **The check** (`internal/api/operating_client.go`): one middleware after the Host and Origin
  guard and before the router. On, it serves `GET /v1/healthz`, or a request from the app
  listener with exactly one `X-SM-Operating-Credential` header that matches. Everything else is
  `403 desktop_app_required`. The header is removed before any handler runs, in both states.
- **The switch:** `server.require_desktop_app`, read when the server is built. False is never
  written to the file.
- **The start** (`cmd/smd`): the credential is made at every start, in both states. On, an
  unusable credential, a refused identity or an app port that cannot be bound ends the start,
  and so does the app listener stopping with an error later.
  The error goes down the path a failed browser bind already takes, so the daemon stops in order
  and an archive activation in flight is not blamed for it. Until it is down, nothing but the
  health check is served. After a credential failure the server holds no credential. After an
  identity or bind failure it holds one, but there is no app listener to present it on, and
  the main listener refuses it. Off, each problem is only reported, as before.
- **The start report:** one line in `smd.log` at every start saying whether the check is on.
- **The route inventory:** every route is registered through a mux that records its pattern
  (`DeclaredRoutes`), so the test sweeps the server's own list.
- **References updated in the same change:** `docs/v2-design/config.md`,
  `docs/v2-design/api-endpoints.md`, and the downgrade note in `docs/install.md`.

**Not in C2a, by the brief's parts:** the shell sending the credential (C2b); the notice, the
two tools and the user-facing documents (C2c); switching it on at the station (C2d). A browser
on the main listener with the check on gets the JSON refusal until C2c adds the notice.

**Evidence.**
- **The credential unit:** 26 tests: creation and reuse; one credential per install; independence
  from the identity, in either creation order; every refusal (missing, unexpected file, 11
  malformed contents, modes, owners, symlinks, a replaceable ancestor), each naming the path,
  changing nothing and never quoting the file; the creation barriers in order, each failing
  barrier, unconfirmed durability; 8 concurrent creators; a creation in progress; publication
  over an existing credential; a swapped alias.
- **The check:** the rule against a stub handler (27 cases across on and off, both listeners, the
  exception and its near misses, and a server with nothing loaded). Then the assembled server on
  both real listeners with the rig, FT8, profiling, page and manual surfaces all registered:
  **78 of the 79 declared routes, 7 requests each**, are refused with the check's own code; the
  79th is the health check, tested for an unchanged answer. Each request has a valid Host and
  Origin, so no other guard could have refused it. A refused FT8 subscription adds no
  subscriber, and the same test shows an admitted one is counted. With the credential, a page, a
  request, a mutation, an SSE stream and a WebSocket are served. Refusals are logged without any
  value.
- **The daemon:** through the real lifecycle graph, in temporary directories on free ports: the
  check enforced with the install's own credential on both listeners (the main one a Unix
  socket); each of the three start-up errors reaching the fatal path with its path or port
  named; after a refused credential, a protected request refused on both listeners while the
  start is ending; and the check off leaving every problem a report.
- **Reversion proofs: 41, every one caught.** Each breaks one rule (a mode not checked, the
  check out of the chain, the switch not read, a credential in the log, a start-up error only
  reported, and so on) and a named test fails for it.
  - The first run was serial and in the working tree, and took about an hour. Three of its 39
    were not proofs: two hung (with the check removed, the sweep reached a served WebSocket and
    waited on it until the ten-minute limit) and one did not compile.
  - The sweep's helper now reports anything but a `403` without reading its body, so a failure
    is prompt. Those three, and all 15 of the API server's, were then rerun in temporary copies
    of the repository, in parallel: 16 of 16 caught in under a minute, the working tree
    untouched.
  - The serve-error ruling added two proofs; with it, all 11 of the daemon's were rerun the
    same way: 11 of 11 caught in 25 seconds.
  - The two proofs that remove the check run the real handlers behind it. The fixture's rig and
    FT8 services are enabled but never started, so no rig command or audio capture is possible.
- **Gates, on the final tree:** `go build ./...`, `go vet ./...`, `go test ./...`, the four
  changed packages under `-race`, `golangci-lint` (0 issues), the maintainability gate (0
  regressions), `task docs:check`.

**Deviations and open points, for review.**
1. **A Station Event at every start is not built. Closed by the operator, 2026-11-13:**
   "Station Events are going to be retired for something more useful. If you look at the
   current list of events, they are not useful in any way". The log line stands, and C2 adds
   no Station Event. The station's list bears that out: seven events between 2026-10-21 and
   2026-11-07 and none since; five are `tx.disarmed` (four with the cause `dial_moved`, which
   a band change produces), one `forward.failed`, one `archive.activated`. What follows is the
   reasoning as first written. The brief said the state is reported in the
   log and as a Station Event. Only the log line is built. With the check off by default and
   not usefully switchable until C2b, an event at every start would tell the operator something
   they cannot act on, which two earlier rulings speak against (2026-09-13, false alarms;
   2026-09-15, no unactionable instructions). The 2026-11-11 ruling also made the listener's
   reports log-only. Whether and from which part the event appears is the operator's to rule.
2. **A serve error on the app listener, with the check on. Ruled by the operator, 2026-11-13:**
   "yes, end the daemon". Built the same day, test-first: with the check on, the app listener
   stopping with an error ends `smd` by the same path as the start-up errors, and the bundled
   unit's `Restart=on-failure` brings it back (five failures in 120 seconds stop the unit). A
   clean stop is not an error; with the check off it stays a report. The point as first
   written: it was only reported. ADR 0103
   rules the bind and the identity; it does not rule a listener that fails later. The station
   would then refuse every client until restarted. Whether that should end the daemon, so its
   service manager restarts it, is the operator's to rule.
3. **The route inventory is a small production change made for the test** (a recording mux,
   about 20 lines). The alternative was a source scan, which misses routes built from constants.
4. **`internal/api` does not import `internal/desktopid`:** the server is handed a comparison
   function, so the package's import boundary is unchanged.

### C2b — the shell presents the credential (built 2026-11-13; committed a1cc8128)

Built on the operator's direction ("commit C2a and start C2b"), test-first. C2a is committed as
`90db4857`. With this part, the desktop app can pass the check; the check itself is still off
by default and **is not to be switched on at the station before C2c and C2d**.

**What exists now.**
- **Finding it** (`desktop/src/discovery.mjs`): after the identity, the shell validates
  `desktop-credential/` (the user's own, `0700`, not a symlink) and reads `credential` (the
  user's own, exactly `0600`, a regular file, opened without following a symlink, checked on the
  open descriptor before any byte is read). The content must be exactly the canonical 43
  characters. No refusal quotes the file, and the value travels in an object that prints a
  placeholder.
- **Waiting and refusing.** No credential directory yet: the shell waits and says so, as for a
  missing identity. An unsafe or malformed credential: the waiting page names the path and the
  reason, titled as the credential, not the identity. In both cases nothing is pinned and
  nothing is sent.
- **Fixing it** (`desktop/src/daemon.mjs`): the pin, the origin and the credential are fixed in
  one synchronous step, once, for the app's lifetime. Neither file is read again.
- **Presenting it** (`desktop/src/credential.mjs`): one `onBeforeSendHeaders` hook, installed
  before the window exists, adds `X-SM-Operating-Credential` to a request for the pinned
  `https` origin or its `wss` partner, and to nothing else. Any header of that name already on
  the request, in any letter case, is removed first.
- **A refusal at the start** (`desktop/src/waiting.mjs`): the probe reads smd's error code. On
  `desktop_app_required` the waiting page says the daemon did not accept this app, to close and
  reopen it, and failing that to run `smctl restart`. It is shown once and the probe goes on.
- **A refusal in a loaded workspace** (`watchRefusal`): a sign of refusal on the pinned origin
  makes the main process ask smd itself; if smd confirms, the operator is told once, in a
  native message box, and the page is not navigated, so nothing typed is lost. The signs are a
  request that completes with `403`, whoever sent it, and a WebSocket that fails. The shell's
  own probes carry a mark made in the main process at start (`probeUrl`, `isShellProbe`) and
  never prompt the question.
- **The SPA is unchanged.** The brief called that a goal to verify; it holds.

**Evidence.**
- **Unit tests:** 79 pass (was 55): the credential's discovery states, 20 unsafe or malformed
  cases, the header rule (where it goes and 13 places it must not), the once-only holder, the
  connection logic, both refusal pages, and the refusal watcher against a fake session.
- **`check:c2b`, Electron against TLS fixtures, 35 checks:** nothing sent while the credential
  is missing or unsafe; the probe, the page, an asset, a `fetch`, an event stream and a
  WebSocket upgrade each carry it exactly once; a header the page supplies does not arrive;
  the page's own request, document and timing entries do not hold the value; another port
  serving the same certificate receives nothing, directly or by redirect; a plain-HTTP
  downgrade and an impostor at the address are refused; a reconnect carries it; a replacement
  under a loaded workspace is told once, with the page and a draft marker untouched and no file
  read again; a window started before the new credential reaches the disk waits and says so; a
  window started after presents the new one.
- **`check:c2b-smd`, Electron against a real `smd` with the check on, 18 checks.** This is the
  existing C1d acceptance, every phase unchanged: the real SPA loads its configured workspace,
  `fetch`, `wss:` and EventSource work, a draft survives an impostor and a restart, fresh
  windows pin and load. Two checks show the check was really on: a plain client of the browser
  listener gets `403 desktop_app_required` for the API and the page and `200` for the health
  check; and the credential smd made is the one the shell holds. `check:c1d`, the same with the
  check off, passes with 17 checks, one of which shows the browser listener still serving.
- **The other scenarios** (`check:a1`, `check:c1`, `check:c1d-shell`) pass; the last now has a
  credential in its synthetic install, as every smd start makes one.
- **Reversion proofs: 40, every one caught,** run in temporary copies of the repository, in
  parallel, in 44 seconds; the working tree was not touched. One candidate was not a proof and
  is dropped: with the explicit removal of a page's header disabled, Electron still delivered one
  value, the shell's, because its header map ignores letter case. The removal is kept so the
  rule does not depend on that; its proof is on the pure function.
- **Gates:** desktop lint, format and unit tests; all six scenarios; the maintainability gate
  (0 regressions); `task docs:check`. CI is wired for the two new checks (`ci.yml`,
  `Taskfile.yml`, `scripts/ci-local.sh`).

**Not run here, and why.** `task rpm:desktop` builds the RPM and would replace the built RPMs
on disk. The staging step copies the whole of `desktop/src` (`scripts/desktop-rpm.sh`), and the
content check compares the shipped sources with that directory, so the new file ships; the
smoke check's own anchors in `waiting.mjs` are unchanged. `task ci:local` and CI run it.

**Review finding, fixed (codex-112, P2).** As first built, the watcher acted only on a
completed `403` that named a window. Codex measured, on this Electron, two refusals it missed:
a refused WebSocket upgrade is reported as an error with no status, and a shared worker's
request names no window. A workspace whose first refused request was one of those was never
told.
- **The fix:** any `403` on the pinned origin prompts the question, and so does a failed
  WebSocket there. The shell's own probes are told apart by their mark, not by the missing
  window. smd's answer still decides: a daemon that is simply down fails a WebSocket the same
  way, and is not reported as a refusal.
- **Tests, seen failing first:** three unit tests and a fourth for the mark. In Electron: a
  shared worker's request as the only refused request, a WebSocket upgrade as the only one,
  each told once with the page left alone; and a `403` of another kind, after which smd is
  asked exactly once and nobody is told, which shows the question does not prompt itself.
- **Proofs:** the two original defects put back, in the unit tests and in Electron, are each
  caught; so is a probe that is not recognised.
- **A cost to know:** while the daemon is down, each failed WebSocket reconnect of a loaded
  page makes the shell ask once (one short probe at a time).

**Open points, for review.**
1. **The wording the operator sees** is mine, not ruled: the two waiting-page texts and the
   message box ("The daemon no longer accepts this app").
2. **An app newer than its daemon waits for a credential the daemon never makes.** The exact-
   version pair (ADR 0102 Q4) rules this out for an installed pair; a development run against
   an older installed daemon will wait, and the page says why.
3. **The message box is told once per app lifetime.** After it, further refusals are silent
   until the app is restarted, which is the only remedy.
4. **A main-frame navigation refused by smd** would replace the page with smd's JSON refusal.
   The shell offers no reload and the SPA navigates in the page, so this is not reachable in
   normal use; it is not handled.

### C2c — the notice, the tools and the documents (built 2026-11-13; committed dfbd90b8)

Built on the operator's direction ("commit C2b and the inbox note, start C2c"). C2b is committed
as `a1cc8128` and the inbox note as `d17c0308`. The check is still off by default and **is not
to be switched on at the station before C2d**.

**Incident during this part, 2026-11-13, 14:41:58 to 14:42:00 local.** The first run of the new
test for `scripts/tune-duration-probe.sh` executed the script as it then was. Its default
address was `127.0.0.1:8080`, the station's daemon, and the test answers the script's "THIS WILL
TRANSMIT" prompt with `y`. The station's `smd.log` shows five `GET /v1/version` (200) and four
`POST /v1/rig/tune`, each refused `503 rig_state_unknown`; there is no tune, key or TX line in
the window. Nothing was keyed. No agreement for a rig command had been given: this broke the
standing rule, and the daemon's refusal was the rig's state (the operator: "I never leave the rig
on while you are coding!"), not a safeguard of the work.
- **Cause:** a failing-first test run executes the code as it is, with its current defaults. The
  old script's target was not read before the test ran it.
- **Made safe:** the script has no address of its own (below), and the test passes an explicit
  environment. My first safeguard was a check of the script's text; Codex showed it proves
  nothing (codex-114: an address can be assembled so the text never shows it, and
  `--sm-config` can point elsewhere). It is kept only as an early diagnostic.
- **The boundary is now the kernel's** (`scripts/confined.py`). Before anything is executed, the
  test process confines itself with Landlock so that it, its threads and every process it starts
  can open **no TCP connection at all**. The domain cannot be left or widened. My first version
  allowed "the fixture's ports"; Codex showed that is not a boundary either (codex-115: a
  Landlock network rule names a port number, not an address or a socket, so an allowed port is
  reachable at every address and for whoever listens on it later; I reproduced it with a
  listener of my own on `127.0.0.2`). So nothing is allowed, and the probe is tested against a
  recording stand-in for the client command instead of a listener.
  The tests never run unconfined: without Landlock they stop with the reason. On entering, the
  boundary is measured before anything else runs, with synthetic endpoints only and never a
  station address: listeners of the process's own on `127.0.0.1` and `127.0.0.2` must each be
  refused with a permission error although they are listening, from the test and from a child
  process; a listener opened later is refused too; and at the end the run fails if anything
  ever reached one. Codex's own counterexample (codex-114), run for real and aimed at those
  endpoints, connects to nothing.
  Limits: TCP only; a daemon's Unix socket is a file and is not covered. A rule added to the
  helper for some other port would not be seen by a test (no test can know the port); the
  helper has no code that adds a rule.
- **Verified:** the station log's count of `/v1/rig/tune` lines was 391 after the incident and
  is 391 after every later run, including the reversion proofs.
- The operator was told at once and directed the work to continue.

**What exists now.**
- **The notice** (`internal/api/operating_client.go`): with the check on, a page asked of the
  main listener (a `GET` or `HEAD` outside `/v1`) is refused `403` with one short fixed HTML
  page, never cached. It has no script, link, style or data. The API, a mutation and the app
  listener keep the JSON refusal. Check off: no notice.
- **The tools' client** (`scripts/smd_client.py`, standard library only): finds the install as
  `smd` and the desktop app do; validates the certificate and credential files the same way;
  then verifies `smd` before sending anything, by the app's own rule: TLS 1.3 or later, and the
  certificate presented must be `smd`'s, byte for byte. Dates are not enforced, as for the app's
  pin (operator ruling 2026-11-10). It never follows a redirect and has no plain fallback. The
  credential is never an argument, and no error or printed form carries it. As a command it
  prints the HTTP status, `000` when `smd` was not reached.
- **The Cabrillo reader** (`scripts/ukeicc-cabrillo.py`): `--from-daemon` with no address reads
  the local install through that client; `--sm-config` names another install. An address is
  still asked plainly; if `smd` refuses it for the check, the error says to drop the address.
- **The tune probe** (`scripts/tune-duration-probe.sh`): asks only through the client. It has
  no address and no default; `SM_CONFIG` names another install; a set `SM_HOST` stops it with
  an explanation.
- **Documents:** `docs/install.md` (the desktop app at first run; a section "Serving only the
  desktop app"; the two new directories; a note on the plain `curl` examples),
  `docs/v2-design/api-endpoints.md`, `docs/v2-design/config.md`.

**Verification choice for the tools, stated as the brief asked.** The brief noted that
`--cacert` or a Python trust anchor is CA verification, which enforces dates the app's pin does
not. The tools use the pin itself instead: no certificate store, one byte-for-byte comparison
after a TLS 1.3 handshake, which proves the listener holds the key (RFC 8446 section 4.4.3). A
test shows an expired identity that is the pinned one is accepted, and an impostor is refused
before any request is written.

**Evidence.**
- **The notice:** two new tests (nine page cases, the page's content, `HEAD`, four cases that
  keep the JSON refusal, and check off), seen failing on behaviour before the implementation;
  the route sweep now expects the notice for the page routes on the main listener.
- **The client:** 23 tests: discovery and 14 unsafe modes or shapes, 10 malformed credentials,
  bad config values, another user's files; the genuine listener (exactly one credential, the
  body and type of a POST); an impostor, a plain-HTTP listener, TLS 1.2 and nothing listening,
  each refused with no request and no credential sent; a redirect returned and never followed;
  a path that names another host; the command line.
- **The Cabrillo reader:** 34 tests (29 before): every page over the pinned listener with the
  credential; an impostor; a refusal reported, not read as an empty log; `--sm-config`; the
  plain address refused by the check.
- **The tune probe:** 14 tests, with every TCP connection denied, against a recording stand-in
  for the client command (a `python3` first on `PATH` that writes down its arguments and prints
  a chosen status): the three confinement checks; a run asks for the version, the start and
  the stop, in that order, through the client in the tree; declining the prompt sends no tune;
  a daemon that was not reached is sent nothing more; a refused start is not a result;
  `SM_CONFIG`; `SM_HOST`; nothing is executed unconfined; a script the text check cannot see
  through still connects to nothing; and every command line the probe gives is one the
  client's own parser takes and means the same by. What these no longer show is the probe and
  the real client together over a socket: the two halves are proved separately and meet at
  the command line.
- **Against a real temporary `smd` with the check on** (`scripts/smd_tools_smd_test.py`; rig,
  FT8 and evidence capture disabled): a plain client is refused and a page gets the notice; the
  client is served; the command prints `200`; the Cabrillo reader reads the (empty) default
  logbook. The tune probe is not executed here and no rig route is named, so this suite needs
  a network and is not confined, like the desktop app's real-smd checks. The client's own
  tests, which also have a network, name no rig route either.
  The notice check was shown to fail against a binary built before the notice.
- **Electron:** `check:c2b-smd` now also requires the notice for a page; it and `check:c1d`
  pass.
- **Reversion proofs: 36, every one caught,** in temporary copies, in parallel. None puts an
  address into the probe. Six are of the confinement itself: executing without requiring it; a
  `require()` that passes an unconfined process; never restricting (the run stops with "NOT
  RUN" before any test); one port allowed again (the same); and two that also weaken the check
  on entering, which the confinement's own tests and the exit check then catch. The four that
  weaken the boundary run only the confinement's own tests, never the probe. Two more hold the
  probe and the client to one command line.
- **Gates:** `go build`, `go vet`, the `internal/api` and `cmd/smd` tests, the check's tests
  under `-race`, `golangci-lint` (0 issues), the maintainability gate (0 regressions), desktop
  lint and format, the four Python suites, `task docs:check`. CI and `scripts/ci-local.sh` run
  the new suites.

**Deviations and open points, for review.**
1. **The manual has bounded changes, not a rewrite** (after codex-114, which pointed out that
   the brief includes the manual and that the FT8 chapter's "use a second browser tab" advice
   cannot work with the check on). The landing page and the First Run outline name the desktop
   app; "More than one window" and the FT8 advice are qualified for the app and for a daemon
   that serves only the app. The full pass stays in W-0018.
2. **The tools are not in the RPM.** An RPM user with the check on has no shipped command-line
   client; the plain `curl` examples in the install guide need the check off. That is part of
   the later brief that removes the switch (headless and scripted use).
3. **The notice's wording** and the install guide's new section are mine, not ruled.
4. **Process:** the client and the tool tests were written before their code but could not
   fail on behaviour until it existed, except where noted; the proofs are the evidence. The
   tune probe's test must never again be run before the script it runs has been read.
5. **The confinement needs Landlock with network rules** (Linux 6.7 or later). It is measured on
   this machine; on CI it is unproven until the first run there, and a runner without it turns
   the step red by design.
   **Update 2026-11-14: proved on the hosted runner.** CI run 43474487129 (on `df958caa`,
   2026-11-13, completed: success) ran `python3 -I scripts/tune_probe_test.py` and its log shows
   "Ran 14 tests" and "OK". The 14 include the three `Confinement` tests, and the file runs no
   test where the confinement cannot be entered, so the runner has Landlock with network rules.
6. **The probe is no longer run against a listener or a real `smd` by any test** (after
   codex-115). That is the price of a boundary with nothing allowed. The probe's requests and
   the client's transport are each tested; their joint is the command line, checked against the
   client's parser.

### C2d — switched on at the station (accepted by the operator 2026-11-17)

**The operator's verdict, 2026-11-17, in chat: "C2d accepted".** The operator switched the check
on at their own station and has operated with it on since. Nothing here was done by an agent:
what follows was read afterwards from the station's `smd.log` and from `rpm` and `systemctl`,
without a request to the daemon.

**What the station shows.**
- **Switched on:** 2026-11-14 05:55:45 local, on the pair `2.0.0~alpha.5.245.gdf958caa-1`:
  "operating-client check: on; only the desktop app is served, with this install's operating
  credential". Every start since carries the same line: six more, through the operator's
  deploys of the pairs `250.g22f83624`, `252.g202ad0df` and `254.g41d2546b`, the last start on
  2026-11-17 05:59:57.
- **Refused without the credential:** four requests, all on 2026-11-14, each logged
  "operating-client check: request refused" with `credential_present: false`. A browser's
  `GET /` and `GET /favicon.ico` on the main listener at 06:01:44, which the operator's
  screenshot shows as the notice; then `GET /v1/version` on the main listener at 06:02:18 and
  on the app listener at 06:02:42. The log does not name the client that sent the last two.
- **Operated:** the operator's screenshot shows the desktop app in the Operate FT8 view with the
  rig connected. With the check on, `smd` logged 129 completed FT8 contacts on 2026-11-14,
  from 06:12:58 to 08:42:43 local.
- **No error line** in `smd.log` from the switch-on to this entry.

**Against the brief's station criterion** ("the operator switches the check on and operates; a
browser and a `curl` without the credential are shown refused"): the switch-on, the operating and
the browser are shown above. For `curl`, the two refused `GET /v1/version` requests are the
evidence; that they came from `curl` is not something the log records.

**Known with the check on, not part of this acceptance.**
- **The manual cannot be opened from the desktop app** (operator, 2026-11-17: "The scrollbars on
  the manual I cannot see as there is no manual link active"). The app's manual links ask for a
  new window, and the shell denies every new window on its own origin until A2 (the window set).
  A browser cannot reach the manual either, since it is refused. So the manual's scrollbars
  (`d6ac8f5f`) are measured in Electron 44 but not seen at the station.
- **The tools are not in the RPM** (C2c, open point 2), and the switch is still temporary: its
  removal is the later brief named above.

**Next:** C4 (presence). ADR 0100's build needs C2d, C4 and a brief.

### C4 brief — the operator's presence (2026-11-17; ruled; reviewed, no blocking finding)

Started on the operator's direction ("Start on C4"). It is a design for review by the operator
and Codex before any code. Where it proposes a rule, a name or a value, that is a proposal: the
questions at the end are the operator's to rule. They are ruled (below). Codex then reviewed
the design (codex-124) and it was revised; the two points that changed after the rulings were
put to the operator again and are ruled too. A second review (codex-125) changed three more
things inside what had been accepted; they were put as questions 8 to 10 and are ruled too.
A third review (codex-126) corrected the admission retry and the division of the parts, and
showed a consequence of the always-accepted disarm, which was put as question 11 and is ruled.
A fourth review (codex-127) found nothing blocking. Building C4a waits for the operator's
direction.

**Outcome the operator sees.** With the check on, FT8 and FT4 in the desktop app work as they do
today: arm, call or answer, complete, log. Leaving the FT8 view, closing the app or losing it
to a failure disarms TX and stops the run after the same short grace as today. Two things are
new. Only the app's own FT8 view keeps TX armed: another program of the operator's that reads
the FT8 stream no longer does. And an app that has just been started never finds TX armed or a
run in progress from the start before it. With the check off, nothing changes from today.
The rule is not FT8's alone: any scope in which the daemon can transmit by itself is held by
presence in that scope, FT8 and FT4 being the only such scopes today (ruled below).

**Nearest confusable outcome:** TX disarms when the app leaves in the ordinary case, exactly as
it does today, while a stream held by some other program still keeps it armed, or a restarted
app inherits an armed TX inside the grace.

**What is already ruled.**
- Closing the main window stops the FT8 run and disarms TX, and only the operating desktop app
  counts as presence (ADR 0102 ruling 6).
- The daemon must name the one subscription that carries the app's presence, and its loss
  stops and disarms through the existing guaranteed-stop controller. The credential alone does
  not define that. A normal close, an app or renderer failure and a reconnect stay distinct.
  Presence is a connection signal, not proof that a person is at the desk (ADR 0102,
  requirements; its "Update 2026-11-13").
- Restoring the workspace never restores operation: no TX armed and no FT8 run at start
  (ADR 0102, requirements).
- The accepted limit: `smd` cannot tell the app from another program of the operator's own
  user that reads the credential, and no machinery is added to exclude one (ADR 0102 ruling 1;
  C2 brief, point 7).
- Each app lifetime, from start to exit, has its own identity, presented with its
  authenticated connection, so the daemon can tell a reconnect of the same app from a new
  start (ADR 0100 §3, *App lifetimes*; Accepted 2026-11-13). ADR 0100's build depends on it.
- FT8 sessions are operator-initiated; a decode is not a QSO (project rules).

**Ruled on this brief (operator, chat, 2026-11-17).**
- On question 2 (the scope of presence): "Scope, if we add RTTY and it is in unattended
  operation, that scope (or any other scope) should be covered."
  - **What that rules:** presence is not a rule for FT8 only. Any scope of operation in which
    the daemon can transmit without the operator acting for each transmission must be held by
    presence in that scope. FT8 and FT4 are the only such scopes built; RTTY is the operator's
    example of one that may be added.
  - **Claude's reading:** "scope" is the unit, so presence stays with each scope's own view,
    as it is with the FT8 view today, and is not the app as a whole. Leaving the FT8 view for
    Settings still disarms after the grace. Confirmed by the ruling that follows.
  - **What it changes here:** design points 3 and 9, as now numbered, and the question of
    where the per-scope logic lives. No RTTY operation is designed or built by C4.
- On all six questions, after that revision: "1 - 6 accepted as is". So, as the brief stands:
  1. the grace stays 5 s;
  2. presence is per scope, with the scope's own view;
  3. tune is left as it is: its own auto-off is its stop, and losing the app does not end it;
  4. a new app start inside the grace disarms at once, with the cause `unattended`;
  5. an arm needs presence, and is refused without it;
  6. the app-lifetime identity is built here, in C4.
  - On question 3 the brief put two outcomes and proposed neither in so many words; "as is"
    is read as the brief's stated position, that C4 does not touch tune.
  - The names (`X-SM-App-Lifetime`, `ft8_not_present`) and the form of the identity were
    proposals in the brief and are accepted with it.
- On the two points that changed after Codex's review (below), asked as "ruling 4's
  mechanism" and "question 7": "1 - yes, 7 - inside internal/ft8". So:
  - a new app start disarms at once through **admission at app start**, in any view (design
    point 2), with its one new request and one new refusal;
  - the per-scope presence logic is a helper **inside `internal/ft8`** until a second real
    scope exists (design point 10, as now numbered).
- On the three points that changed after Codex's second review (questions 8 to 10): "8 - yes,
  9 - yes, 10 - yes". So:
  - a replaced app is refused FT8 operation only, and a request with no lifetime cannot
    operate FT8 at all (design point 3);
  - a restarted daemon takes no lifetime without an admission, and the running app admits
    again by itself (design point 2);
  - an app left running across the C4 install cannot operate FT8 until it is restarted, and
    is told so (design point 11). After Codex's third review this takes effect from C4a.
- On question 11 (a stop is never refused): "11 - yes, keep it". A disarm is accepted from any
  credentialed request, a replaced app's included, with the stated cost that such a disarm
  arriving late ends what the new app is doing (design point 3).

**Revised after Codex's review (codex-124, 2026-11-17).** Codex wrote before the six rulings
and had not seen them.
- **Its finding, which stands:** the brief's rule for a restarted app acted only when the new
  app opened the FT8 stream, and only once the old app had no stream counted. Neither follows
  from a restart: the new app may open on another view; `smd` may admit the new stream before
  it sees the old one close; and the new FT8 view's profile claim is refused while the old arm
  stands (`ClaimProfile`), so its stream may never open. Checked against the code and agreed.
- **The change:** an admission step at app start, independent of the view (design point 2).
  It replaces the old point 5. Ruling 4 is kept in substance and reaches further.
- **Also changed:** the per-scope logic is proposed as a helper inside `internal/ft8`, not a
  separate unit (point 9), and the synthetic second scope is dropped. Question 7 is new.
- **Taken into the design and the evidence plan:** Codex's answers on the transmit paths, the
  package boundary, the races, the check-off path and Electron's header hook.

**Revised again after Codex's second review (codex-125, 2026-11-17).** Codex had the rulings
this time and asked for none of them again. Its points, each checked and agreed:
- **A retired lifetime could take admission back.** As written, a late or repeated admission
  from the old app would have stopped the new one. Admission is now a read followed by an
  admit against the generation that was read (design point 2).
- **An old command could act under the new app's arm,** if it had passed the check at the
  door before the replacement. The lifetime is now checked again inside the scope, where the
  command takes effect, and a replacement seals before it stops (points 2 and 3).
- **A restarted daemon** took the first lifetime it saw, which need not be the surviving
  app's, and nothing made that app admit again. Now each daemon process is admitted to
  afresh (point 2; question 9).
- **Who is refused what** was unclear between "every route" and "any other subscription is
  still served". Now only FT8 operation is refused (point 3; question 8).
- **An app left running across the install** is still the old shell (ADR 0102 ruling 8). Its
  behaviour is stated and tested (point 11; question 10).

**Revised a third time after codex-126 (2026-11-17).** Each point checked and agreed:
- **The stale-answer retry could undo the order.** A shell that was refused as stale read
  again and admitted again, so a replaced app that was still running could take the station
  back. Now a lifetime that has been replaced is remembered as retired and is refused for
  good, and what the order does and does not promise is stated (point 2).
- **C4a both accepted and refused the old shell.** Point 11 said it worked after C4a; the
  parts said C4a refuses FT8 commands without the current lifetime. The parts were right and
  point 11 is corrected: the old shell cannot operate FT8 from C4a on.
- **More FT8 commands than the arm, the starts and the send change operation,** and several
  reach the sequencer without the gate. C4a starts from an inventory of all of them (point 3).
- **The daemon marker** is read when response headers arrive, and a differing marker is a
  reason to ask the daemon, not proof of a restart (point 2).
- **"Disarm can only stop" understated it:** a replaced app's late disarm can end the new
  app's contact in progress. Stated in point 3, excepted in criterion 5, and put to the
  operator as question 11.

**Fourth review (codex-127, 2026-11-17): "No remaining blocking design findings in this
revision."** It is agreement on the design, not a proof of behaviour and not a direction to
build. Its answers add these constraints for C4a, each checked against the code:
- **Retirement is recorded in the same step that publishes the replacement,** and a retired
  answer ends the attempt in every admission path of the shell, the restarted-daemon path
  included.
- **"The one admitted last stands" is about who owns the station,** until a further admission
  or a daemon restart. It does not promise that transmission continues: a disarm from anyone
  still stops it (question 11).
- **The gate is not taken twice.** The starts, the profile claim and abandon already hold
  `seqGate`; the check goes inside that hold, or into a variant that is clearly the locked
  one. The other commands commit under `txMu` or the sequencer's lock and are brought under
  the gate at the service boundary, in the established order. The coordinator's own lock is
  never held in an order that inverts a scope's.
- **The inventory is of FT8 commands only.** Settings and its live configuration behaviour
  stay served as ruled (question 8).
- **The idle app:** the page opens its event stream once and never closes it
  (`frontend/app/src/main.ts`, `openLogEvents`), and its retry does not wait for the window to
  be visible (`socket-reviving.ts`). So a daemon restart the socket notices leads to a new
  handshake. A frozen renderer, or a connection that has not yet signalled failure, tells the
  shell nothing; the refusal of the next FT8 command is the other trigger. Nothing is polled.
- **Still to be shown in C4a, not assumed:** that the main process sees a WebSocket
  handshake's response headers, and the header after a renderer crash.

**What exists today (read for this brief).**
- **Presence is a count of FT8 streams.** Every `GET /v1/ft8/events` subscription adds one
  (`internal/ft8/handler.go`, `Subscribe`; `profile_claim.go`, `SubscribeMode`). The same
  count drives the audio capture device.
- **It acts only on loss.** When the count reaches zero the daemon waits `captureLinger`, 5 s
  (`service.go:21`). If no subscriber has returned it disarms TX with the cause `unattended`,
  which aborts a transmission in flight, abandons the active contact and closes the output
  device, and then it releases capture (`onLingerExpired`, `disarmTx`). A return inside the
  5 s changes nothing.
- **Arming does not ask for presence.** `armTx` checks the keyer, the rig's readiness, the
  dial and the output device (`servicetx.go:148`), not the count. TX armed with no stream ever
  opened is never disarmed by this rule, because nothing ever leaves.
- **Whose stream it is, is not recorded.** With the check on, every subscriber holds the
  credential; with it off, anyone who reaches the listener.
- **In the app, the stream belongs to the FT8 view:** it opens when the view is shown and
  closes when the view is left (`Ft8View.svelte`; ADR 0100 §4). The app is single-instance and
  only that view opens the stream. A dead stream is revived only when it is seen to be dead,
  never on a schedule (`frontend/app/src/lib/api/sse-reviving.ts`).
- **The causes are already distinct in the record:** `operator`, `unattended`, `cat_lost`,
  `shutdown`, `band_change`, `dial_moved` (`servicetx.go:317`). `unattended` is recorded as the
  daemon ending an exchange (`sessionobserver.go`).
- **The shell has one owner of request headers:** `desktop/src/credential.mjs` adds the
  credential in `onBeforeSendHeaders`, to the pinned origin only. It has no lifetime identity.
- **Tune is separate.** The tune carrier has its own guaranteed stop: a hard auto-off timer
  and release when the rig disconnects (`internal/bridge/tune.go`). It shares the single-flight
  claim with FT8 keying. Presence does not touch it today.

**Proposed design** (revised after codex-124 and codex-125; what changed is listed above).

1. **The app-lifetime identity (the shell).**
   - At each start the shell's main process makes one random identity (16 bytes from the
     system's generator, base64url). It is held in memory only and never written down. It is
     a label, not a secret: the credential is what authenticates.
   - It is presented as one request header (`X-SM-App-Lifetime`), added by the hook that
     already adds the credential, under the same rules: only to the pinned origin, replacing
     any header of that name already on the request, on every kind of request. There stays
     one owner of that hook.
   - Because it lives in the main process, a renderer reload or a renderer crash that is
     reloaded keeps the identity; an app restart makes a new one. A second launch raises the
     running app and so makes none.
   - `smd` accepts a well-formed identity and refuses a malformed one (`400`). With the check
     off it ignores the header, as it ignores the credential.

2. **Admission (`smd` and the shell).** The start boundary codex-124 asked for, made ordered
   and safe to repeat as codex-125 asked. It does not depend on which view the app opens.
   - **What `smd` holds,** in memory, for one daemon process: the **current lifetime** and an
     **admission generation**, a counter that goes up by one at each replacement. A daemon
     that has just started has no current lifetime, generation zero, and nothing armed.
   - **Two requests, both from the shell's main process** (proposed: `GET` and `POST
     /v1/app/lifetime`). Both are answered for any request that passed the credential check,
     whatever its lifetime.
     - **Read:** the daemon's process marker (the one `/v1/version` already carries), the
       generation, and whether the caller's lifetime is the current one.
     - **Admit:** the caller sends back the marker and the generation it read.
       - The caller is already current: success, nothing changes. A repeat, or a retry after
         a lost answer, is therefore harmless.
       - The caller has been replaced in this daemon process: refused for good (proposed:
         `409 app_lifetime_retired`). `smd` remembers the lifetimes it has replaced, for as
         long as the process runs.
       - The marker or the generation is not the daemon's now: refused (proposed:
         `409 app_admission_stale`). The current app is not touched.
       - Otherwise the caller replaces the current lifetime (below) and the generation goes
         up by one.
   - **What the order promises, and what it does not** (codex-126, finding 1).
     - Admissions are handled one at a time, and each succeeds only against the generation
       it read. So successful admissions are in one order, and a request an earlier app had
       already sent cannot succeed after a later admission.
     - A lifetime that has been replaced never comes back: neither a repeat of its admission,
       nor a retry after a lost answer, nor a fresh read can make it current again.
     - It does **not** establish which of two shells was started later. A random label and a
       read cannot. The supported app is single-instance, so two shells of one operator do
       not run at once; if two did, the one admitted last would stand and the other would be
       retired, whichever was started first.
   - **The shell admits before it loads the workspace.**
     - **Stale:** it reads again and admits again. This is what lets the running app get past
       a dead app's admission that arrived late. Each repeat follows a definite answer; there
       is no timer.
     - **Retired:** the attempt ends. The app says that another start of Station Manager has
       taken over and that it should be closed, and does not ask again.
     - Any other failure is shown on the waiting page.
     - Outside this step the shell does not ask for admission because something was refused.
   - **Replacement, in this order, with no gap** (codex-125, finding 2):
     1. **seal:** from this moment no new arm, start or send can be accepted for the old
        lifetime, and its streams stop being presence, whether or not they have closed;
     2. **stop:** every scope's operation is stopped by that scope's own stop path, with the
        cause `unattended`, and the stop is waited for. A contact already earned still
        finishes logging; nothing here waits for the old streams to close;
     3. **publish:** the new lifetime becomes current, and the admit is answered.
     Between 1 and 3 no lifetime is current, so the old one cannot arm again and the new one
     cannot arm early.
   - **Each daemon process is admitted to afresh** (codex-125, on a restarted daemon). The
     shell keeps its lifetime across a daemon restart and admits again to the new process.
     Until it has, the app reads and logs as before, and cannot operate FT8. `smd` never
     takes a lifetime as current without an admission, and a new process has no retired
     lifetimes.
     - **How the shell learns of it:** `smd` puts its process marker on its responses to the
       app (a header, proposed), and the main process reads it when the headers arrive, not
       when the response ends, since a stream may never end. No listener owns that event
       today; the owners of the other request events stay as they are.
     - **A different marker is a reason to ask, not proof.** A late response from the old
       process can arrive after the new one has been admitted to. So the shell then does the
       read, and the read decides: admit only if this process has not admitted it.
     - Only responses from the pinned origin count; attempts in progress are joined, not
       repeated; and the admission requests' own responses start nothing.
     - **An app that is silent learns nothing,** and nothing is polled. The workspace keeps
       its event stream open in every view, and that stream's reconnection after a restart
       is a response from the new process; so is the refusal of an FT8 command. That the
       main process sees both is to be shown by test, not assumed.
   - This is not ADR 0100's session attachment: nothing is stored, and no session is opened
     or closed. ADR 0100's start and exit reports can be built on it.

3. **Who is refused what** (codex-125, its first detail). With the check on:
   - **Operating a scope needs the current lifetime.** Every FT8 command that can start,
     resume or alter operation is accepted only when the request carries the current
     lifetime. The check is made again **inside the scope, under the gate that a replacement
     takes, held until the command has taken effect**, not only when the request arrives. So
     a command of the old app that was already on its way cannot act under the new app's arm.
     Refusal: `409 app_lifetime_not_current`.
   - **Which commands, from an inventory, not from memory** (codex-126). Today's FT8 routes
     that change state are fifteen: the profile claim, arm, send, the start of a contact and
     of working a caller, the exchange path, abandon, stopping an auto-work run, skip, next,
     and for Call CQ start, pick, bag, unbag and resume. Several reach the sequencer without
     the gate a replacement would take (`NextAnswerer`, for one). C4a begins by listing each
     route against the place its effect is committed, and puts the check under the gate
     through that commit. A check that passes and then lets go before the scope acts is the
     defect of codex-125 again.
   - **Disarm is the one exception: it is always accepted,** from any credentialed request,
     so that a stop is never refused. It starts no transmission. It is not without effect:
     a disarm of the old app that arrives late ends whatever the new app is doing, an
     unfinished contact included (a contact already earned is still logged). Ruled: kept
     (question 11).
   - **Nothing else is refused for its lifetime.** Reading, logging a contact, settings and
     every stream are served as before, for the current lifetime, a retired one, or none. A
     contact the old app was logging when it was replaced is still logged.
   - **A request with no lifetime** (the operator's tools) can read and log; it cannot
     operate FT8, and its streams are not presence.
   - **A transmission already authorised** belongs to the arm it started under and ends with
     it. It never passes to the new lifetime.

4. **Scopes and their operating subscription (`smd`).**
   - **A scope** is a kind of operation in which the daemon can transmit by itself once the
     operator has started it. FT8 and FT4 are one scope, the only one built.
   - **Each scope names one stream as its presence:** its own event stream, opened by its own
     view. For FT8 that is `GET /v1/ft8/events`.
   - With the check on, the operator's presence in a scope is an open subscription to that
     stream whose request carried the lifetime that is current now. It is judged at the
     moment it matters: a stream opened before its lifetime was admitted counts once it is,
     and a stream of a replaced lifetime stops counting at once.
   - With the check on, the count that drives audio capture decides capture only. It no
     longer decides presence by itself.
   - Points 5 to 7 are stated for FT8 and are the rule for every scope: read "arm" as
     whatever lets that scope transmit by itself.

5. **Presence is required to arm, not only missed when lost.** With the check on, a new arm
   is accepted only from the current lifetime (point 3) and only while it has an operating
   subscription open. Otherwise it is refused (`409 ft8_not_present`). The check and the arm
   are one step under the gate that the stop paths take, so an arm cannot slip in beside a
   stop.
   - A run needs an arm: all seven sequenced starts pass `sessionTxGate`, and a manual send
     reaches `startTransmission`, which refuses when TX is not armed (codex-124, answer 1).
   - A repeated arm while already armed stays what it is today, accepted and changing
     nothing. It does not extend the grace.

6. **Loss.** When the current lifetime has had no operating subscription for the grace (5 s,
   ruled), the daemon disarms with `unattended`, by the path it uses today. A return inside
   the grace changes nothing. The decision at the end of the grace is tied to the arm it was
   made for: a timer that fires late never disarms a later arm (codex-124, answer 3).

7. **One stop path.** Every case above ends in the existing `disarmTx`: the transmission in
   flight is aborted with PTT guaranteed down, the active contact is abandoned, and a contact
   already complete is still logged (FT8 invariant 1). C4 adds no second way to key or unkey
   and exposes no new TX command.

8. **The three cases the ADR wants kept distinct.**
   - **Reconnect:** the current lifetime returns inside the grace. Nothing changes.
   - **Failure:** the app or its renderer dies, or the view's stream is lost. The grace runs,
     then `unattended`. A restarted app is point 2: its admission stops what was running.
   - **Normal close:** the app asks for the disarm itself, after the operator confirms, and
     waits for it before it exits. That request exists today (`operator`); the confirmation
     and the wait are A3, built on this.

9. **With the check off,** nothing changes from today: any subscriber is presence, it acts on
   loss only, an arm needs no presence, and the lifetime header and admission do nothing.
   Today's code path is kept for it, and its behaviour is pinned by tests of its own. The off
   state exists for development, load generation and scratch daemons, and goes with the
   switch.

10. **A later scope is covered by the same rule** (ruled: question 2, which requires the
    coverage; how the code is shared is ruled in question 7).
    - **What is already shared, by its nature:** the lifetime and admission belong to the
      app, not to FT8. A replacement seals and stops every scope through a list of scope
      boundaries, and a new scope adds its own to that list. That coordinator is handed the
      scopes' boundaries; `internal/ft8` imports nothing of the API, the configuration or
      session storage for it.
    - **The per-scope part** (which streams are presence, the grace, the arm) is a focused
      helper inside `internal/ft8`, with nothing of FT8 in its logic, and is moved out when a
      second real scope exists (ruled: "7 - inside internal/ft8").
    - **A new scope must:** have an event stream its view holds open; name it as the scope's
      presence; require the current lifetime and presence for whatever lets it transmit by
      itself; add its boundary to the coordinator's list; and end through the guaranteed-stop
      controller. That list goes where a new scope's author will meet it, with the
      references (C4b).
    - **Stated once in the project rules.** `AGENTS.md` says today that the enforced presence
      signal is an open FT8 event subscription. That sentence changes with C4b.

11. **An app that was already running when C4 is installed** (codex-125, its second detail).
    ADR 0102 ruling 8 lets a running app go on until the operator restarts it, and it is then
    still the old shell: it sends no lifetime and asks for no admission.
    - **From C4a on** it reads and logs as before, and it **cannot operate FT8** until it is
      restarted: every FT8 command but disarm is refused, because it carries no lifetime. The
      refusal says so in words the operator can act on (restart Station Manager). It is not
      treated as the operating app, and not passed off as a tool that still works. (An
      earlier wording here said it worked after C4a; that contradicted the parts.)
    - Installing either part restarts `smd`, so no run survives the upgrade in any case.
    - This is tested in C4a: a client that behaves as the old shell does, against the new
      `smd`.

**What C4 does not do.**
- **It does not make presence proof of a person.** A frozen app whose connection is still open
  is present. A Call-CQ or auto-work run still continues with nobody at the desk while the FT8
  view is open.
- **It does not exclude a determined program of the operator's own user.** One that reads the
  credential and asks for admission as the shell does is inside the accepted limit, and can
  take the station from the app. What changes is that this takes a deliberate act: a tool
  that merely reads the FT8 stream no longer holds TX armed, and cannot operate FT8.
- **Sessions.** ADR 0100's storage, its start and exit reports and the recovery question are
  ADR 0100's build.
- **The exit confirmation** and the wait for the disarm (A3); reload and asset compatibility
  (C5); the window set (A2); tune (ruled: question 3).
- **RTTY, or any other new scope.** None is designed here. C4 makes the rule ready for one.

**Acceptance criteria.**
1. Check on, app in the FT8 view: arm, call CQ or answer, complete and log, as today.
2. Leaving the FT8 view, closing the app, or killing it: the disarm begins when the grace
   ends; a return of the same app inside it keeps the arm and the run.
3. A client that holds the credential and the FT8 stream but is not the app does not keep TX
   armed once the app has left, and cannot arm or start FT8 by itself.
4. A newly started app finds TX disarmed and no run before its workspace is shown: when it
   opens on a view that is not FT8; when the old app's stream has not yet been seen to close;
   and when its FT8 view asks for the other profile.
5. Nothing of the old app starts or changes operation after the new one is admitted: a late
   or repeated admission is refused and leaves the new app untouched; a replaced app that is
   still running and asks again is refused for good; and an FT8 command of the old app that
   was already on its way is refused even if the new app has armed meanwhile. The one
   exception is a disarm, which stops the new app's operation (ruled: question 11).
6. A contact the old app was logging when it was replaced is logged, once.
7. A renderer reload inside the grace keeps the arm. After a daemon restart the running app
   is admitted again by itself and can arm, without a reload.
8. An app left running from before C4 reads and logs, cannot operate FT8, and is told to
   restart.
9. Every disarm above is logged with its cause.
10. Check off: today's behaviour, pinned by tests of its own, the old suite included.

**Evidence plan.** No live transmission, keyed test or rig command is part of building this.
- **Admission** (`internal/api` and the coordinator): the read; a first admit; a repeat and a
  retry after a lost answer; a replacement; a late duplicate; an earlier app's first admit
  arriving after the newer one; two replacements at once; a retired lifetime that reads again
  and asks again; a stale marker after a daemon restart; a malformed identity. The seal, stop
  and publish order, with barriers: an old command paused before its scope call while the new
  app is admitted, subscribes and arms, for each command in the inventory; and an old disarm
  paused the same way, with its effect on the new app's contact and a contact already earned
  still logged.
- **`internal/ft8`,** with the fake keyer set to model a keyed rung, and a clock the test
  controls. Each rule above, and the cases codex-124 names:
  - at loss: a queued next-slot manual send, a keyed rung, and an auto-work run between
    contacts; a completed contact still logged;
  - races: a reconnect at the deadline; an arm against the last unsubscribe; a stop in
    progress against a new arm; two streams of one lifetime; a late unsubscribe or a late
    timer after a replacement; a stream evicted by the hub; a refused stream admission;
    shutdown; and admission before the old handler has exited;
  - an old stream that stays open stops being presence at the replacement;
  - the existing lock order is kept, and no presence lock is held across a stop that waits
    for a transmission to end or across a completion that needs it.
- **`internal/api`:** the lifetime is read where the credential is and handed on before the
  header is removed; every FT8 command is shown to carry it to the scope, by a sweep of the
  FT8 routes; the other routes are shown not to refuse for it. The canonical references change
  in the same commit (`docs/v2-design/api-endpoints.md`).
- **Check off:** tests that pin today's rule, including an arm with no subscriber, any
  subscriber keeping operation alive, and reconnect with capture down.
- **The shell,** by scenario against the fixture harness: the header on every kind of request,
  the FT8 stream and its automatic reconnect included; never to another origin; the same value
  after a renderer reload and after a renderer crash that is reloaded; a new value after an
  app restart; admission before the workspace loads. **The whole retry loop,** not only the
  handler: a stale answer read and admitted again; a success whose answer was lost, followed
  by another lifetime's replacement, ending as retired with no further attempt; the waiting
  page on any other failure. **The marker:** seen on a page response, a stream and a WebSocket
  handshake; a late response from the old process after admission to the new one; attempts
  joined; no loop from the admission's own responses. Codex measured the reconnect and the reload
  with the credential's header in Electron 44.9.3; its crash case did not complete, so that
  one is unmeasured (codex-124, answer 5).
- **The old shell:** a client with the credential and no lifetime, against the new `smd`.
- **Electron against a real temporary `smd`** with the check on, rig disabled: admission; the
  app's stream is presence; a plain credentialed client's stream is not; a daemon restart
  under the running app.
- **The station:** the operator's own acceptance, with the rig, by agreement for that occasion.

**Proposed parts, each reviewed before the next.**
- **C4a, the lifetime and admission, end to end:** the shell's identity, header and admission
  steps; `smd`'s current lifetime, the two requests, the seal, stop and publish, and the
  refusal of FT8 commands that do not carry the current lifetime, from the inventory of
  point 3; and the old shell's refusal, its wording and its test (point 11). It delivers "a
  new start never inherits" by itself. Both sides ship in one commit, because the installed
  pair is always one version.
- **C4b, presence in the scope:** the operating subscription, the arm's requirement, the loss
  rule, the references and the project rule's sentence.
- **C4c, the station:** the operator's acceptance, recorded here.

**Questions for the operator. All ruled 2026-11-17:** "1 - 6 accepted as is"; "1 - yes, 7 -
inside internal/ft8"; on the three things Codex's second review (codex-125) changed inside
what "1 - yes" had accepted, "8 - yes, 9 - yes, 10 - yes"; and on question 11, from the third
review, "11 - yes, keep it" (above).
1. **The grace.** Ruled: 5 s.
2. **Presence stays with the FT8 view.** Ruled: per scope, and every scope that can transmit
   by itself is covered.
3. **Tune.** Ruled: left as it is.
4. **A new app start disarms at once, with the cause `unattended`,** by admission when the
   app starts. Ruled.
5. **Arming needs presence.** Ruled.
6. **The lifetime identity is built here.** Ruled.
7. **The per-scope helper lives inside `internal/ft8`.** Ruled.
8. **What a replaced app is refused** (point 3). The brief you accepted refused it on every
   route. Proposed now: only FT8 operation is refused; reading, logging and streams go on, so
   that a contact being logged at that moment is not lost. And with the check on, a tool with
   no lifetime cannot start or send FT8 at all, not only cannot arm. **Ruled: yes.**
9. **A restarted daemon** (point 2). The brief you accepted let a fresh daemon take the first
   lifetime it saw. Proposed now: it takes none without an admission, and the running app
   admits again by itself when it sees the new daemon. **Ruled: yes.**
10. **An app left running across the C4 install** (point 11) cannot operate FT8 until it is
    restarted, and is told so. **Ruled: yes.**

11. **A stop is never refused** (point 3). A disarm is accepted from any credentialed
    request, a replaced app's included. The cost, which Codex showed: a replaced app's
    disarm that arrives late ends what the new app is doing, an unfinished contact included.
    It needs an old request to be held up across a whole app restart. Proposed: keep it,
    because refusing a stop is the worse error. **Ruled: yes, keep it.**

**Questions for Codex** (earlier ones answered in codex-124, codex-125 and codex-126; the
answers are in the design).
1. Does the retired set close finding 1 for the whole retry loop, and is "the one admitted
   last stands" the right statement of what is promised when two shells run at once?
2. Is anything missing from the inventory of fifteen routes, or is there state a command
   changes that no single gate can cover without a lock order the package does not allow?
3. Is the workspace's event stream a sound guarantee that an idle app learns of a new daemon,
   or is there a state in which it is closed?

### C4a — the app lifetime and admission (built 2026-11-17; committed 93ae68d6)

Built on the operator's direction ("commit the brief and start C4a"), from the brief above
(committed `7104471c`). Both sides are in one change, as the brief says. With the check off
nothing changes; with it on, the desktop app is admitted before it shows its workspace, and FT8
is operated only by the admitted app. **Not deployed, and no rig, key or transmission was part
of building it.**

**What exists now.**
- **The FT8 rule** (`internal/ft8/operating.go`): the app lifetime that may operate FT8 is held
  under `seqGate`. Each of the eighteen service methods behind the fourteen gated routes now
  takes its caller and asks, under that gate and held until it has taken effect, whether the
  caller is the current lifetime. The nine that took no gate before take it only when
  ownership is required. `RetireOperating` seals and stops in one hold of the gate, with the
  cause `unattended`; `PublishOperating` names the new lifetime. A disarm is accepted from
  anyone (question 11).
- **Admission** (`internal/api/app_admission.go`): one current lifetime, an admission
  generation and the lifetimes replaced in this process, behind one lock that is always taken
  before a scope's. `GET` and `POST /v1/app/lifetime`. The check's middleware reads and
  removes `X-SM-App-Lifetime`, refuses a malformed one, and puts the process marker on what it
  serves for the credential on the app listener. Every FT8 handler reaches the scope as the
  request's caller.
- **The shell** (`desktop/src/admission.mjs`): the lifetime is made once per launch in the
  main process and sent with the credential by the one existing hook. The app reads and admits
  before the application is loaded; a stale answer is read again; a retired one ends the
  attempt on a page that says so. A new listener, the only owner of `onHeadersReceived`,
  reads the marker as headers arrive; a different marker makes the shell ask, and the read
  decides.
- **The reference:** `docs/v2-design/api-endpoints.md`, in this change.

**Two defects found in review and fixed (codex-129 and codex-130, both in the shell).**
1. **A newer daemon seen while an admission was in progress was forgotten.** The shell joined
   the work in progress and kept no note of what it had seen meanwhile; if that attempt then
   succeeded late or failed, an idle workspace stayed unadmitted to the newest process. Now
   everything seen during an admission is kept, and if any of it differs from where the
   admission settles, `smd` is asked. My first fix kept only the last thing seen, and
   codex-130 showed that a late response from an older process then hid the newer one: the
   order responses arrive in is not the order of the daemon's processes.
2. **An app replaced within the same daemon process was never told.** Its responses carried
   the same marker, so nothing made it ask. `smd` now puts the admission generation beside
   the marker (`X-SM-Admission-Generation`); a changed generation makes the shell ask, and
   `smd` answers that it is retired. It cannot take the station back by asking: a replaced
   lifetime is refused for good. A refusal of some other kind, with nothing changed, asks
   nothing and warns of nothing.

**Deviations from the brief. Accepted by the operator 2026-11-17: "all five accepted".**
1. **The profile claim is not gated** (the brief listed fifteen routes; fourteen are gated).
   A claim changes the profile only on a daemon with no subscriber, no arm, no session and
   nothing in flight (`ClaimProfile`), so it cannot start, resume or alter operation. And the
   FT8 view sends it by itself when its stream returns, which after a daemon restart can be
   before the app has been admitted again: gated, it would leave the view showing a refusal.
2. **A start refused for its own content before the gate is refused for that,** whoever sent
   it: the two Field Day starts check that the exchange is configured before they take the
   gate, as they did.
3. **The two type-4 starts now share one body** (`startT4As`). Adding the caller check took
   them over the duplication limit; they differed only in the sequencer call.
4. **The identity is held to one spelling:** 22 base64url characters whose last carries no
   stray bits. Another spelling of the same bytes is refused, so two strings never name one
   lifetime.
5. **In a loaded workspace a retirement is said once, in a dialog;** at start the waiting
   page says it. The brief named the page only.

**Evidence.**
- **`internal/ft8`** (twelve new tests): every gated command refused for no lifetime, another
  lifetime and no current lifetime, and not refused for the current one; a refused command
  changes nothing; the check-off path unchanged, including that the ungated commands take no
  gate; a replacement stops and seals, and the old lifetime's commands do not act under the
  new arm; an arm that is half made when the app is replaced is undone; the gate is held
  until it is released; old starts racing a replacement never survive it; the disarm; the
  claim; and two structural guards: every caller-taking method checks its caller or hands it
  to one that does, and every command that takes the gate releases it only by `defer`.
- **`internal/api`** (thirteen new tests, over the real listeners): read then admit; a
  replaced lifetime refused for good, by a late duplicate and by a fresh read; a stale admit
  refused and read again; another process's marker; four admits against one generation, one
  winner; the identity's form; the marker on what the app is served and nowhere else; the
  check off; the seal-stop-publish order against two recording scopes; a sweep of the
  declared FT8 command routes, each with a body its handler accepts, refused for no lifetime,
  a lifetime never admitted and a replaced one; the claim and the disarm; the restart wording;
  and a structural guard that no handler calls an FT8 command except as the request's caller.
- **The shell, unit** (twenty-two new tests): the admission loop against a model of the daemon's
  rule, including a success whose answer was lost followed by another lifetime's replacement;
  joined attempts; a marker seen during an admission, with that admission succeeding late and
  failing, and with an older response arriving after it; a replacement within one daemon
  process; the marker; the header rule; the retired
  page.
- **The shell, in Electron 44.9.3** (`npm run check:c4a`, 26 checks, fixture harness answering
  as `smd` does): admission before the page; the lifetime on the page, an asset, a fetch, an
  event stream and a WebSocket; a new daemon process admitted to again when its marker
  arrives on a fetch, on a WebSocket handshake and on a stream's own reconnection, each alone
  and with no navigation; a late marker; a renderer reload and a renderer crash that is
  reloaded; a refusal of another kind; a second start, and the first window, still loaded,
  told once and not navigated; the retired page; a stale admit; a failed read.
  **Two things the brief left to be shown are shown:** the main process does see a WebSocket
  handshake's response headers, and the header survives a renderer crash.
- **Electron against a real temporary `smd`** (`task desktop:check:c1d`; rig, FT8 and
  evidence capture disabled): check off, 18 checks, "nothing to be admitted to"; check on, 19
  checks, "the app was admitted before its workspace loaded", generation 1. So the main
  process's `POST` passes the app listener's origin guard.
- **Unchanged checks:** `check:c1d-shell` 16 and `check:c2b` 35 pass.
- **Reversion proofs: 49, every one caught,** each in its own temporary copy, in parallel;
  the shared tree was not touched.
  - **Go, 25:** the generation header missing, and the published generation never advanced
    (both after codex-129); the caller never checked; a caller with no lifetime let through;
    a replacement
    that does not seal, and one that does not stop; the gate let go before the command acts,
    and let go part-way by one command; a gated disarm; a command that forgets its caller; the
    check-off path taking the gate; a gated claim; a handler calling the scope directly; no
    retired set; the generation, and the process marker, not compared; publish before stop; a
    repeat that replaces itself; a malformed identity and another spelling accepted; the
    marker missing, and on every response; admission and ownership taking effect with the
    check off; and no restart wording.
  - **The shell, 24:** seventeen by unit test and seven by the Electron scenario, among them
    the application loaded before admission, the app never admitted, the marker never watched,
    a retired start loading the application, a failed admission loading it, and, after
    codex-129 and codex-130, what was seen during an admission forgotten or dropped, only
    the last thing seen kept, a changed generation ignored, the generation header not read,
    and a retirement not said.
  - **The first round was not clean, and that is why three tests exist.** A proof that the
    gate was let go early passed: the test I had written for it used an arm, which `txMu`
    also protects, so it could not show the gate was held. The direct test of the gate and
    the `defer` guard were added for it. A second proof hung for ten minutes instead of
    failing: the test failed while holding the gate and its own clean-up then waited on it.
    Both tests now let the gate go before they fail.
- **Gates, on the final tree:** `gofmt`, `go vet ./...`, `go test ./...` (67 packages),
  `internal/ft8` and `internal/api` under `-race`, `golangci-lint` (0 issues), the
  maintainability gate (0 regressions), desktop lint, format and 102 unit tests,
  `task docs:check`, the context gate. `task ci:local` was not run.
- **Process, stated as the brief asks:** the Go tests were written before the code they test
  and were seen failing. The shell's first unit tests and its Electron scenario were written
  after the shell's code and passed at their first run; for them the proofs are the evidence.
  They did not find the two defects of codex-129, which is the cost of that order. The tests
  for those fixes, and for codex-130's correction, were written first and seen failing.

**Limits and open points.**
1. **The station has not run it.** With the check on at the station, C4a changes what an
   app left running across the install can do (design point 11): it must be restarted.
2. **Presence is not built.** Until C4b, an arm still needs no FT8 view open, and its loss
   is still judged by any stream. C4a alone gives "a new start never inherits" and "only the
   admitted app operates FT8".
3. **The at-loss cases of the brief's evidence plan belong to C4b:** a queued manual send, a
   keyed rung and an auto-work run between contacts when presence is lost.
4. **A completed contact at a replacement** rides on the existing invariant-1 tests of
   `disarmTx`; C4a adds no test of its own for it.
5. **The silence boundary stands** as the brief states it: a frozen renderer, or a connection
   that has not signalled failure, tells the shell nothing of a new daemon. The same holds
   for a replacement: a replaced app that makes no request is not told until it does.
6. **One read more than is needed** follows an admission when a response that differs from
   where it settles arrived during it, a second response from the new process with the
   generation as it then was, for one. The read changes nothing and it ends there.
7. **The retired dialog's wording** and the waiting page's are mine. The operator accepted
   the dialog as a deviation; the words themselves were not put to them.

### C4b — presence in the scope (built 2026-11-17)

Built on the operator's direction ("Start C4b"), from the brief above. With the check off
nothing changes. With it on, an arm needs the desktop app's own FT8 view connected, and is
held only while it is. **No rig, key or transmission was part of building it, and it has not
run at the station.**

**What exists now.**
- **The helper** (`internal/ft8/presence.go`), with nothing of FT8 in it: it counts the open
  subscriptions of each app lifetime, holds an arm for the lifetime that made it, runs the
  grace while that lifetime has none open, and reports the loss naming the arm the decision
  was made for. Its lock is a leaf. A return inside the grace ends the grace; a timer that
  fires late, for a grace since ended or an arm since released, reports nothing.
- **The arm** (`armTxAs`): under the gate, an arm that already stands is left as it is;
  otherwise the current lifetime must have its stream open, or the arm is refused
  (`ErrNotPresent`, `409 ft8_not_present`). A new arm is then held for that lifetime.
- **The loss** (`onPresenceLost`): the decision names the arm and the grace it was made
  for, and is checked again under `seqGate`, the gate a new arm and a replacement take: the
  loss must still stand, which is that same hold, that same grace, and the lifetime still
  without a stream. Then `disarmTxLocked(unattended)`, the path every other stop takes.
  Every disarm, whatever its cause, ends the hold.
  - **Found in review and fixed (codex-134).** The decision first named only the arm.
    Streams come and go without the gate, so if the app returned and left again while a
    decision waited for it, a new grace had begun for the same arm and the old decision
    ended the arm before that grace had run. The decision now names its grace, and the
    helper says whether that loss still stands (`lossStands`).
- **The stream** (`SubscribeModeAs`, `SetStreamLifetime`): an admitted `/v1/ft8/events`
  subscription that names a lifetime is that lifetime's presence while it is open.
  `internal/api` gives the handler the way to read the lifetime it validated; the FT8 package
  takes no part in validating it.
- **The capture count decides capture only:** with ownership required `onLingerExpired` no
  longer disarms. With it not required it disarms as before, by the same code as before.
- **The grace** is the capture linger, 5 s, read when a grace starts (question 1).
- **The references:** `docs/ft8.md`, `docs/v2-design/api-endpoints.md`, the package comment,
  and the sentence in `AGENTS.md` naming the presence signal. What a new scope must do is
  listed on `operatingScope` (`internal/api/app_admission.go`), which is the interface a new
  scope has to implement.

**Deviations and decisions of mine. Accepted by the operator 2026-11-17: "all three
accepted".**
1. **The checklist for a new scope is on the interface it must implement,** not in a document
   of its own. The brief said only "where a new scope's author will meet it".
2. **The app is unchanged.** It shows the daemon's own message for a refusal, so the two
   wording (`ft8_not_present`: "TX can be enabled only while the FT8 view is connected; open
   the FT8 view and try again") is mine. The operator accepted the deviation with that
   wording before them.
3. **The C4a tests now open the current lifetime's stream in their fixture.** They are about
   who may operate; under C4b an arm needs presence too, and without it one of them waited
   for ever on an arm that was refused.

**Evidence.**
- **The helper** (nine tests, a clock the test owns): the counts; the grace at the holder's
  last close; a return inside it; only the holder's streams count; an arm held with no
  stream; release; a late timer and a later arm; the grace read when it starts; and a loss
  standing only for the grace it was decided in.
- **The service** (fourteen tests): a decision that waited for the gate while the app
  returned and left again, with its two controls, the gate held by the test so that no
  timing decides the order; an arm refused with no stream, with a tool's, and with
  another lifetime's; loss for the grace disarms as `unattended` while a tool's stream stays
  open; a return inside the grace; a repeated arm neither refused nor extending the grace; a
  disarm ends the hold; a late decision and a later arm, at the helper and again past it,
  each part of the check on its own; a Call-CQ run and a queued
  manual send stopped at the loss; a replacement, with the replaced lifetime's stream still
  open and a stream opened before admission; the capture linger in both states; streams not
  counted with ownership not required; a refused stream; the stream handler; and the
  service's own grace on real timers.
- **`internal/api`, over the real listeners** (two tests): an arm is refused for presence
  with no stream, and with only a tool's and a never-admitted lifetime's stream open; it gets
  past presence with the app's own stream open; and is refused again once that closes. With
  the check off an arm asks for no stream.
- **The at-loss cases the brief names beyond those** (a keyed rung, an auto-work run between
  contacts, a contact already complete) are not tested again here. The loss ends in the same
  `disarmTx` call, with the same cause, as the capture linger always made, and that call has
  its own tests (`TestSequencer_AbandonsWhenDisarmedMidQso`,
  `TestAutoWork_DisarmingTxStopsTheRun`,
  `TestSessionObserver_UnattendedMidExchangeIsOneTerminationAndNoDisarmRow`,
  `TestTransmitNext_PublishesAndCancelsOnDisarm`). C4b's own tests show that a loss reaches
  that call with that cause.
- **Reversion proofs: 24, every one caught,** each in its own temporary copy: after
  codex-134, the waiting decision checked only against presence now, and a loss standing
  whatever its grace, for another hold, and although the holder is back; and before it: an arm that
  does not ask; any open stream counting; a stream with no lifetime counted; the loss
  disarming nothing, and recorded as the operator's; a return not ending the grace; a late
  timer taken for the pending grace; the decision not checked against its arm, and not
  against presence returned; a repeated arm asked for presence; a disarm leaving the hold; a
  new arm not held; the capture linger still disarming with ownership required, and never
  disarming; a stream's lifetime never counted, and counted with ownership not required; the
  handler ignoring the lifetime; the grace not the capture linger; the API not giving the
  handler the lifetime; and the refusal having no code.
  - **Two proofs passed at first, and two tests were made stronger for them.** A disarm
    leaving the hold in place changed nothing the tests looked at: a test now requires that
    no grace starts for an arm already disarmed. And the arm check in `onPresenceLost` was
    hidden behind the presence check in the test that named it: the test now puts each
    part of the check on its own.
  - **A third passed after the fix, and showed two conditions that could not fail.** With
    `lossStands` asking about the hold, the service's own "still armed" and "still that
    arm" could not be false when it was true: the hold is taken and released under the same
    gate as the arm. They are removed, and the one question is the whole check.
- **Gates, on the final tree:** `gofmt`, `go vet ./...`, `internal/ft8` and `internal/api`
  under `-race`, `golangci-lint` (0 issues), the maintainability gate (0 regressions),
  `task docs:check`, the context gate (kernel 7826 of 8192 bytes, automatic 10058 of 10240).
  `go test ./...` passed (67 packages) before the last tests were added and before the
  fix of codex-134.
- **Process:** the tests for the helper, the service and the API were written before the
  code they test. The helper's and the service's first failed only by not compiling; the
  API's were seen failing on behaviour (`ft8_tx_failed` where `ft8_not_present` was wanted).

**Limits and open points.**
1. **Not shown in Electron against a real `smd`.** The real-`smd` scenario runs with FT8
   off, and turning it on there could open an audio device on this machine, which is a
   hardware-dependent step. What is shown is each half: the app sends its lifetime on an
   event stream and its reconnection (C4a's scenario, Electron 44.9.3), and `smd`, over its
   real listeners with a real FT8 service, counts such a stream and no other. The two meet
   first at the station.
2. **The station has not run it.** With the check on there, an arm now needs the FT8 view
   open in the app. That is how the app arms today.
3. **Presence is still not proof of a person** (the brief): a frozen app whose connection is
   open is present.
4. **A stream opened and closed without ever arming costs nothing:** no hold, no timer.
5. **Tune is untouched** (question 3).

**Next:** C4c, the operator's station acceptance, recorded here.

### D1 follow-up — the Electron archive is kept between builds (2026-11-17; built; all three decisions accepted)

**The operator's report (chat, 2026-11-17):** `task deploy:local:dev` "downloads electron every
time it is run. We should implement some for of check for a newer version/caching".

**Cause.** By design, not by fault: `scripts/desktop-rpm.sh` gave Electron's downloader a new,
empty cache in the build's temporary directory at every run (the D1 record above: "the archive
is always downloaded and verified"). The desktop RPM must be rebuilt whenever the version
changes, which is every commit, so every such deploy fetched the 118 MB archive again.

**What a build does now.**
- The downloader's cache is `build/desktop/electron-cache`, which outlives the build. The
  archive is downloaded once for each Electron version the lockfile pins.
- **"A newer version" is the lockfile's.** The version is pinned by
  `desktop/package-lock.json`, and the archive's file name carries it, so a changed pin is
  another file and is downloaded. Nothing asks the network whether a newer Electron exists:
  moving the pin is a dependency upgrade, a change of its own.
- **What is trusted is unchanged.** A kept archive is used only if it has the pinned SHA-256.
  The pinned downloader (`@electron/get` 5.1.0) checks a cached archive against
  `checksums.json`, and downloads again and replaces it if it does not match; the script's own
  assertion on the archive stands behind that, as before. So a kept archive that is accepted
  is, byte for byte, what a download would have been.
- The script now requires exactly one archive of the pinned version in the cache. With a
  cache that lasts, two could exist, and which one the downloader used cannot be told from
  outside it; that is refused, with the directory to remove.
- The build says which it is: "nothing to download", "not in the cache; downloading it", or
  "the kept archive is not the pinned one; downloading it again".
- `SM_DESKTOP_RUNTIME_CACHE` (an absolute path) puts the cache elsewhere. The tests need it.
  It is not among the refused overrides because it cannot change which archive is accepted.
- The downloader's environment is still exactly `PATH`, an empty `HOME` and the cache.

**Measured.** In a temporary copy of the build's inputs, with the real npm and downloader:
the first build downloaded and took 61 s; the second ran with no network at all
(`unshare -rn`; a control `curl` in the same namespace failed to connect) and built the RPM in
2.7 s, announcing "nothing to download".

**Evidence.**
- `desktop/test/runtime-cache.test.mjs` (4 tests) measures the library's behaviour the build
  relies on, at the pinned version, with a stand-in for the network that counts its calls: a
  matching cached archive is used with no download; one that does not match is downloaded
  again and replaced; a download that is not the pinned archive is refused and not kept;
  another version is another archive. These passed when written: they describe the library,
  not a change of ours.
- `scripts/desktop_rpm_guard_test.py`: 13 tests (5 as before, 8 new). The 8 new ones and one
  of the old failed against the script as it was.
- 7 reversion proofs of the script, each in its own temporary copy, all caught. One passed at
  first: the "kept archive is used" test did not notice the downloader being handed a
  different cache from the one the script had inspected. It now checks that.
- `scripts/deploy_local_dev_test.py` (20), the desktop lint, format check and unit tests (106)
  pass.

**A fault of mine on the way.** The first failing run executed the old script in place. Two of
the new tests got past its archive check, and it went on to its staging step, which emptied
`build/desktop/stage` in this repository. That directory is intermediate output, cleared and
refilled by every build, and the built RPM was not touched; but a test must not reach it. Each
test now runs a copy of the script in a temporary repository of its own, so the script's
stage, RPM and default cache are that copy's whatever the script does.

**Decisions of mine, all accepted by the operator 2026-11-17 ("1 - build is fine"; "2 and 3
accepted"):**
1. The cache is in the repository's ignored `build/` directory, not the account's
   `~/.cache`. Removing `build/` costs one download. **Ruled by the operator 2026-11-17:
   "1 - build is fine".**
2. No check for a newer Electron on the network (above).
3. Earlier versions' archives are not removed: 118 MB each time the pin moves.

**Limits.** The repository's own cache is empty until the next real build, so the next deploy
that builds the desktop RPM downloads once more. CI's runner keeps no cache and downloads at
every run, as before. `task rpm:desktop` was not run in the repository for this change (it
would replace the deployed pair's RPM file with one of a `-dirty` version).

### C4c — the station (2026-11-18; accepted by the operator)

The operator ran C4b at the station on the installed pair `2.0.0~alpha.5.265.g8e64d2ed-1`,
with the operating-client check on and the rig connected. The steps were the brief's
criteria 1, 2, 4, 7 and 9 as things a person can do. What follows is what `smd.log` recorded
(times local, 2026-11-18); the reading of the log is mine, the steps and the transmissions
are the operator's. No request to the daemon and no rig command was made in reading it.

| Step | What the operator did | In the log |
|---|---|---|
| 1 | normal operation | 07:30:07 armed, calling CQ; 07:31:58 and 07:32:58 `caller QSO complete (RR73 sent)`, `QSO stored`, `ft8: completed QSO logged`, one each |
| 2 | left the FT8 view, back inside the grace | 07:09:53.717 the FT8 stream closes; 07:09:54.975 claimed again; no disarm |
| 3 | left the FT8 view for longer | 07:10:06.204 the stream closes; 07:10:11.204 `ft8 tx: disarmed`, `cause=unattended`, 5.000 s later |
| 4 | closed the app and stayed away | 07:20:35.572 the app's three streams close; 07:20:40.573 disarmed, `unattended`, 5.001 s later, with no app connected |
| 5 | closed the app and started it again inside the grace | 07:07:43.544 closed, 07:07:47.698 disarmed `unattended` at the new start's admission, before its page was asked for (07:07:47.701); again at 07:10:23.904 and 07:10:26.154 |
| 6 | restarted the daemon under the running app | 07:16:53 `shutdown signal received`, disarmed `cause=shutdown`, `smd starting`; 07:16:55.075 the app reads and is admitted again (200, 200) with no load of its page; 07:17:16.280 arm accepted |

No request was refused, from the daemon's start that day to the last reading at 07:33 (no
status of 400 or above). Every disarm carried its cause:
`unattended` four times, `shutdown`, `operator`, and `band_change` twice. The log cannot tell
a closed app from a killed one: both are three streams closing together.

**The criteria that cannot be made by hand. Ruled by the operator 2026-11-18: "Write the
test for 6, the rest accepted on tests".** That is criteria 3, 5, 8 and 10, and the first
sentence of 7 (a renderer reload inside the grace), accepted on the tests named in C4a and
C4b above. For 7, step 2 showed the daemon's half: a stream of the same lifetime that
closes and returns inside the grace keeps the arm.

**Criterion 6, the test the operator directed** (`internal/ft8/operating_contact_test.go`). C4a had none of its own (its limit 4). Three tests,
through the service under the operating-client rule, its own sequencer and completion
policy, the real single-flight transmission and its cancellation, and the injected log
hand-over; only the waveform and the wait for a slot are left out:
- the partner has rogered and our closing 73 is keyed and playing when the replacement
  arrives: it is cut short, the rig is unkeyed, and the contact is logged once, in the
  logbook and on the frequency of the exchange;
- the 73 has played out and the contact is being handed to the log when the replacement
  arrives: the replacement waits for the hand-over and the contact is logged once;
- the partner has not rogered: nothing is logged, then or when the roger arrives under the
  new app.
In each, the partner's repeated RR73 under the new app's arm logs nothing more and sends
nothing.
- **They passed at their first run:** they test behaviour that was already there. The
  evidence that they can fail is four reversions, each in its own temporary copy, eight runs
  each, all caught every time: the stop abandoning the session before waiting for the
  transmission (the earned contact is lost); a replacement that seals but does not stop; a
  stop that does not wait for the transmission in flight; and the contact handed to the log
  twice.
- **Limits.** The second test's "the replacement waits" is a bounded look of 250 ms, not a
  barrier: nothing observable says a stop is waiting. With an immediate look, one run in two
  missed the third reversion; with the bounded one, none of eight did. The third test's
  "nothing is logged" was not shown failing by any reversion; only its stop was. A Group B
  closing rung (our own RR73 still owed) is not covered here: by the standing rule it logs
  only on success on the air, and that rule has its own tests. What the tests count is one
  hand-over to the injected logger (`SetQsoLogger`), not a row committed to the database or
  a forward; capture and decode, and the admission's HTTP path, are not in them (codex-144).
- **After the commit (`b4c6f4b7`), its automatic review found one thing, P2, and it is
  fixed:** the fixture waited for the closing 73 to be keyed, not for it to be playing, and
  the controller keys before it plays, so the "keyed and playing" case depended on
  scheduling. The fixture now waits for both. The three tests then passed 20 times under
  `-race`, and the four reversions were caught 8 of 8 again.
- **Gates:** `gofmt`, `go vet ./...`, `golangci-lint` (0 issues), the maintainability gate,
  `internal/ft8` and `internal/api` under `-race`, and `go test ./...` (67 packages).

**Verdict. Accepted by the operator 2026-11-18: "C4c accepted".** With it C4 is done: C4a
(`93ae68d6`), C4b (`b50f74ee`) and this acceptance.

### D1 — accepted by the operator (2026-11-18)

**Accepted by the operator 2026-11-18: "D1 - accepted."** D1 is the desktop app as an
installable package: `station-manager-desktop`, installed with the daemon as one pair, with
its menu launcher. The operator has installed and run that pair at the station since
2026-11-12, through C2d and C4c, most recently `2.0.0~alpha.5.265.g8e64d2ed-1`.

**What this record does not hold.** The D1 brief's acceptance named a clean Fedora guest,
and left some observations to it: the Wayland app id, where a denied namespace creation
shows its error, and what reaches the user journal. The operator's acceptance is recorded as
given. Whether a guest run was made, and what it showed, was not said, and nothing about it
is recorded here. D2 to D6 are not part of D1 and are not started.

### C2e brief — the operating-client switch removed (2026-11-18; directed; questions 1 to 3 ruled; under review)

Written on the operator's direction of 2026-11-18 (Rulings: "deal with the tmp feature
first!"). It is a plan for review by the operator and Codex before any code. The switch is
`server.require_desktop_app`, ruled "a - temporary switch, later removed" on 2026-11-13 (C2
brief, question 1). W-0029's build waits for this.

**Outcome the operator sees.** At the station, nothing: the check is already on there. In
the product there is no setting, and no state of `smd`, in which a client without this
install's credential is served. `config.json` loses the key. A first-time install serves
only the desktop app from its first start.

**Nearest confusable outcome.** The key is gone and the default behaves as "on", while a way
to run `smd` unguarded survives under another name: a test hook reachable from the binary, an
environment variable, or FT8's any-subscriber presence still compiled in and still tested.

**What exists** (read 2026-11-18).
- The key (`internal/config/config.go`), written only when true. Config version 6; a file
  with an unknown key is refused after migrations (`config.go`), so a build that no longer
  knows the key cannot simply ignore it: the station's file holds it.
- Three places in `internal/api` read it (`server.go`, `operating_client.go`,
  `app_admission.go`); the check wraps the whole handler chain (`server.go`).
  `cmd/smd/desktop_listener.go` makes identity and port failures fatal only when it is on.
- `internal/ft8` has `ownershipRequired`, read in six places. Off, any subscriber is
  presence, the capture linger disarms, and no command asks who is calling.
- What runs against an unguarded daemon today: the shared test fixtures (below), the Vite
  dev proxy (`frontend/app/vite.config.ts`, to `localhost:8080`), `cmd/loadgen` (default
  target 8080), and the rollback drill's scratch daemon on a Unix socket
  (`scripts/rollback-drill.sh`), which the check refuses as it refuses TCP. Able to present
  the credential already: `scripts/smd_client.py` and the tools built on it.
- **A first measurement of the tests, with its limit.** Counting test functions:
  `internal/ft8` 636, `internal/api` 645, `cmd/smd` 287; about 40, in five files, turn the
  check on. Those totals do not show how many depend on the off state (codex-147). Closer:
  in `internal/ft8`, 186 tests are in files that use only the sequencer fixture and 153 in
  files that use neither fixture, and neither group touches ownership; 297 are in files that
  build a service, through two shared fixtures (`newTxTestService`, `dialGuardService`). In
  `internal/api`, the tests reach the handler chain at 71 `ServeHTTP` call sites and 12 test
  servers. So the dependence is on a few shared fixtures, not on some 1,500 tests one by
  one. How few is part 1's to measure.

**Approach** (rewritten 2026-11-18 after the operator's rulings and codex-148; the earlier
draft's downgrade rule, its credentialed drill and its FT8-first order are gone).
- **Delete the off state; do not rename it.** `smd` applies the check always. FT8's
  not-required behaviour is removed, not kept for tests.
- **What is removed with it, all ruled:** the Vite dev server, `cmd/loadgen`,
  `ukeicc-cabrillo.py`'s plain-HTTP mode, and the downgrade tooling. No replacement route is
  built for any of them. Staying, and distinct from those: Vite's build and Vitest; the
  Cabrillo script's local reader and its ADIF-file input; forward migrations; the refusal of
  a config file newer than the build.
- **The key goes by a config migration** to version 7 that drops it. There is no way back to
  version 6.
- **The safety outcomes outlive the mechanism.** Where a test of the old capture-linger rule
  protects something that stays true under presence, the assertion is kept and moved, not
  deleted with the mechanism (the list is in part 2).
- **The protected surface keeps its two exceptions:** `GET /v1/healthz` and the fixed notice.

**Parts, in the order of the goal. Every commit must leave a fresh install and the station's
own file both working** (codex-148, finding 1).

**Refocused 2026-11-18 by the operator:** "We seem to have switched over to a focus on
downgrading - rollback. Our focus is security. Removing the switch from the config and
defaulting to on always (required) so that only the local UI (Electron) can access the daemon
functionality?" Yes: that is C2e. The order below puts it first. The downgrade tooling's
removal is ruled and is not needed for it: a config migration step may be one-way (a step
with no `down` is "refused by name", `internal/config/migrations.go`), so the key can go
without touching that tooling. It follows the security change as a separate piece.

1. **Measure and classify.** Done; recorded below.
2. **The tests moved onto the supported mode; no product change.** The FT8 fixtures give a
   service a current lifetime with its stream open; the API and `cmd/smd` helpers that go
   through the handler chain present the credential as the app does; pure-logic tests are
   left alone. The assertions that stay, each named with where it is covered afterwards:
   - an evicted stream of the current lifetime ends its presence and TX is disarmed after
     the grace, **through the real stream handler** (from
     `TestFt8HubEviction_EvictedLastSubscriberStillDisarmsTx`; the present handler test
     covers a cancelled request, not an eviction);
   - the loss disarms when capture is down too (from
     `TestCapture_DisarmsTxWhenLastSubscriberLeaves_CaptureDown`);
   - a return inside the grace keeps the arm, including one that races the fired timer
     (from the two hub-eviction reconnect tests, which would otherwise pass while testing
     nothing);
   - a profile claim refused during the grace succeeds once the arm has ended (from
     `TestClaimProfile_MidLingerOfALiveSession_RefusedThenSucceeds`).
   Removed outright: the tests that only pin the not-required mode.
3. **The security change, as one commit: the check is always on.** FT8's ownership always
   required; the API's lifetime, stream and scope wiring unconditional; the admission's off
   path, the listener's non-fatal branches and the shell's `'not-required'` branch removed;
   the key removed by a one-way version 7 migration; the tests of the off state deleted;
   the three clients that only an unguarded daemon could serve removed with it (the Vite dev
   server's task and proxy, `cmd/loadgen`, the Cabrillo script's plain-HTTP mode); and the
   references changed in the same commit (`docs/v2-design/config.md`, `api-endpoints.md`,
   `docs/install.md`, `docs/ft8.md`, the manual's two chapters, the project rule's "with the
   operating-client check on"). These go together because FT8 requiring a lifetime with no
   wiring to supply one cannot arm.

**After the security change, and separate from it:** the downgrade tooling removed, to the
caller boundary recorded below: the two commands, the drill, the config's way down, the
schema's way down and the 22 SQLite down files, the rollback-only producer of
`config.v5.json`, and the helpers nothing else reaches. Forward upgrades and their tests
stay; no file the operator already has is deleted.

**Evidence plan.** No transmission, keyed test or rig command. Part 2 is shown by the suite
passing with the fixtures on the supported mode **while the off state still exists**, and
by reversion proofs for the four kept assertions. Part 3 adds behaviour only in the
migration, built test-first; its evidence is also one real-`smd` Electron scenario on a
**fresh** temporary install (no key in its file) running every authentication and admission
check, a search showing no reader of the key and no unguarded route remains, the whole
suite and the hosted gate, and the station: the pair deployed, the file migrated, the app
admitted and operating. The later removal of the downgrade tooling is a deletion: its
evidence is a search showing nothing still names what went, and the suite green.

**Not settled here.** The exact fixture change (part 2, from the trace). Whether the main
listener keeps anything beyond the health check and the notice (listed in part 1; a later
ruling). Whether the strip of deprecated forwarder fields still has a job on this install:
the downgrade tooling's removal takes only its rollback copy and leaves the strip. ADR 0014's daemon submitter,
deferred, is not touched.

**Questions for the operator.** All four are closed: 1 (the Vite dev server), 2
(`cmd/loadgen`) and 3 (other installs) are ruled, in the Rulings above; 4 (the downgrade
rule) was withdrawn, and the wider question it became is ruled too ("remove the downgrade
tooling").

#### C2e part 1 — measured (2026-11-18; no code; the deletions below are proposals, not ruled)

Two read-only searches made the inventory; they ran nothing. I checked the claims this
section rests on at the lines named. Counts of tests are the searches' own, by matching call
sites inside each test function; a test that reaches a helper only through another helper is
missed, so they are lower bounds.

**The switch in the product.**
- `internal/config/config.go:608`, the key. It was added inside config version 6 with no
  version bump, and nothing validates or defaults it.
- `internal/api`: `server.go:249` turns on FT8's ownership, the stream's lifetime and the
  admission's scope **only when the key is set**; `operating_client.go:57` passes every
  request when it is not; `app_admission.go:206` and `:220` answer "not required".
- `cmd/smd`: identity, port and credential failures are fatal only when it is set
  (`desktop_listener.go`, `lifecycle_adapters.go:1257`).
- `internal/ft8`: five reads of `ownershipRequired` (`operating.go:72`, `:89`,
  `servicetx.go:160`, `service.go:544`, `profile_claim.go:236`). Off, no command asks who is
  calling and takes no gate, an arm needs no presence, the capture linger disarms, and a
  stream's lifetime is not recorded.
- The shell has an off branch of its own: `desktop/src/admission.mjs:75` returns
  `'not-required'`, with a fixture for it in the harness and an off run of the real-`smd`
  scenario (`check:c1d`; `check:c2b-smd` is the on run).
- **No production code calls a plain, no-lifetime FT8 command.** Every handler goes through
  `ft8As(r)`. The plain commands in `internal/ft8/operating.go` are reached by tests only.

**What a request needs to pass the check in a test, without a TLS listener:** the
app-listener mark in its context (`appConnKey`, `server.go:717`), a credential the server
was told to accept, one credential header, and the app listener's own Host. No shared helper
presents all of them today.

**The tests.**

| Package | Build the thing under the check | Already run with it on | Exist only for the off state | Ride on shared fixtures |
|---|---|---|---|---|
| `internal/ft8` (636) | 207 build a service; 429 never do | 25 | 2, and half of a third; 6 more test behaviour that exists only when off (the last subscriber's disarm, three hub-eviction cases, a profile claim in mid-linger) | 174, through `newTxTestService` and its wrappers and direct `newService`; about 190 gated call sites in 82 tests |
| `internal/api` (645) | 94 go through the whole handler chain, by about eight helpers; about 550 call a handler directly | 20 | 4 tests and 5 cases of a table | the 94 |
| `cmd/smd` (287) | 22 ask a started daemon over its Unix socket with no credential | 3 | about 10 | the 22, through one request helper |
| `internal/config` | | | 1 of its 2 | |

Two of the six off-only FT8 tests would pass while testing nothing if the flag were simply
set to true (a reconnect "keeps" an arm the linger no longer ends). Three plain commands
(`SetExchangePath`, `AbandonQso`, `StopAutoWorkRun`) are gated but return nothing, so a
plain call would silently do nothing.

**Scripts, tasks and tools.**

| What | Under the permanent check | Proposed |
|---|---|---|
| `scripts/smd_client.py`, `tune-duration-probe.sh`, `ukeicc-cabrillo.py` in its local mode, `ci-local.sh`, `deploy-local-dev.sh`, `dev-bootstrap.sh`, `reset-local-dev.sh`, `smctl.sh` | unaffected: the credential, the health check, or no request at all | nothing |
| The Vite dev server (`task frontend:app:dev`, the proxy in `vite.config.ts`) | broken | deleted (ruled) |
| `cmd/loadgen` and its references | broken | deleted (ruled) |
| `ukeicc-cabrillo.py --from-daemon http://…` (a named address over plain HTTP) | broken | that mode deleted; the local mode stays |
| `scripts/rollback-drill.sh` | broken, and by my reading of it **already broken against this station's file**: it copies `config.json` without touching the key, the station's has had the key true since C2d (2026-11-14), and its one request is `/v1/version` over the Unix socket, which the check refuses. Not run to confirm | removed with the downgrade tooling (ruled) |
| `task run`, `task run:smd` | the daemon still runs; the dev loop they describe, pairing it with Vite, is gone | their text corrected, or the tasks removed, with the Vite task |

**The key in `config.json`.** A removed key is refused as unknown unless a migration consumes
it. The precedent is version 2 to 3, `delete(tx, "auto_work_callers")`
(`internal/config/migrations.go:347`). So: version 7, one step that deletes the key. The
station's file is migrated at the first start of the new build.

**What the permanent check leaves unreachable on the main listener** (listed, not proposed
for this change): the UI's pages and the manual (`spa.go`, `manual.go`), pprof, every `/v1`
route but the health check, and the route fallback for that listener. They stay reachable
on the app listener. The main listener still serves the health check and the notice.

**Documents that describe the switch or the off state:** `AGENTS.md:31`, `docs/ft8.md`,
`docs/install.md` ("Serving only the desktop app"), `docs/v2-design/config.md`,
`docs/v2-design/api-endpoints.md` (eight places), `desktop/README.md`, and two manual
chapters (`first-run.md`, `ft8.md`).

**The deletions, with the standing of each** (the heading first read "Proposed deletions, for
the operator's ruling"; items 4 and 5 are now ruled, and 1 to 3 follow from the directed
permanence and are carried out as the brief's parts 2 and 3).
1. The key, by the version 7 migration; every off branch in `internal/api`, `cmd/smd` and
   `internal/ft8`; the shell's `'not-required'` branch, its fixture and the off run of the
   scenario.
2. The tests that exist only for the off state (the table's fourth column), and the six
   FT8 tests of behaviour tied to the old linger are **not** simply deleted: the outcomes
   that stay true under presence are kept and moved, as the brief's part 2 names them
   (codex-148, finding 2).
3. The plain FT8 commands leave the production file. Whether the tests keep them as
   helpers that name the fixture's lifetime, or name it at each call, is part 2's choice.
4. **Ruled:** the Vite dev server ("No, I don't use Vite"), `cmd/loadgen` ("No, don't use
   the loadgenerator") and `ukeicc-cabrillo.py`'s plain-HTTP mode ("Delete the Cabrillo
   plain-HTTP mode").
5. The downgrade tooling. **Ruled 2026-11-18: "remove the downgrade tooling".** Its inventory
   follows; it is to be verified by Codex before anything is deleted.

**The downgrade tooling: an inventory for its removal** (read 2026-11-18; a first pass, with
its open points named).

- **Plainly the tooling:**
  - `smd config-downgrade`: `cmd/smd/config_downgrade.go`, `config_downgrade_v6.go`, their two
    test files, and its entry in the subcommand table (`cmd/smd/main.go:133-141`);
  - `smd db-downgrade`: `cmd/smd/db_downgrade.go` and its test; the function it calls,
    `DowngradeLogSchemaTo` (`internal/database/sqlite/downgrade.go`), and its test;
  - `scripts/rollback-drill.sh` (408 lines);
  - the config's way down: `DowngradeDocument` and the three `down` steps
    (`internal/config/migrations.go`), with the tests of them (`migrate_v4_test.go`, parts of
    `config_v6_test.go`);
  - the live documents: the rollback text of `docs/install.md` and of
    `docs/v2-design/config.md`.
- **Entangled, each needing a decision before it is touched:**
  1. **`cmd/smd/config_v6_strip.go:190` calls `DowngradeDocument`** outside any downgrade
     command: at start, before it strips the deprecated forwarder fields, it writes a
     once-only copy of the config in its version 5 shape (`config.v5.json`; ruling R3,
     2026-10-06), "recovery material for a rollback that went wrong". That copy is rollback
     material too. Whether the strip step and its copy still have a job on this install, now
     that it is the only one, is not established here.
  2. **The 22 `.down.sql` migration files** (19 for the log) are run by `db-downgrade` and by
     eight test files that migrate down and up again. If they go, a new migration needs no
     way down, which is where the recurring cost was. Whether the migration library accepts
     a set with no down files is not verified.
  3. **Those eight test files** would lose their down halves or be rewritten.
- **Not the tooling, and staying:** the guard by which a build refuses a config file of a
  newer version than it knows (`internal/config/config.go`). Unrelated uses of the word
  (TLS, an `rpm` comment, the UI's response handling) were read and set aside.
- **History, left as written:** W-0021, ADR 0082 and the acceptance reports. W-0021 gets a
  dated pointer to this ruling when the removal is made.
- **A rule that relaxes with it:** defaults such as `app_port` are "resolved, never written,
  because an older daemon refuses a key it does not know". With no older daemon to start,
  that constraint has no reader. Nothing is changed for it here.

**Corrected and completed after Codex's verification (codex-148), each point checked in the
source.**
- **The migration library needs no down files.** Codex ran the pinned library on the
  repository's up files alone, in memory: the log set fresh to 19, 14 to 19 and 18 to 19;
  the reference set fresh to 3 and 1 to 3. That is Codex's proof, not mine.
- **The 22 down files are SQLite's** (log and reference). `internal/cloud` has 8 of its own,
  with separate tests; they are SM Cloud's and are not part of this removal.
- **The rollback copy's chain:** `stripLegacyForwarderFieldsAfterSeed` calls
  `ensureV5RecoveryCopy` (`cmd/smd/config_v6_strip.go:51`), which calls `v5RecoverySource`
  and `DowngradeDocument`; the bytes it copies are captured at start (`configAtStart`,
  `cmd/smd/main.go:263` to `lifecycle_adapters.go:85`). The tooling's removal takes the producer of the
  copy and what only it used, and **keeps the strip** and the rule that the Home archive's
  seed commits before it. A `config.v5.json` that already exists is left where it is.
- **A helper chain the word "downgrade" did not find:** `config_downgrade_v6.go` calls
  `archive.ReadHomeBindings`, which calls `HomeFile` and `sqlite.PeekDestinationBindings`.
  `ReadHomeBindings` and `HomeFile` have no other production caller and go.
  `PeekDestinationBindings` is also used by `cmd/smd/restore.go:281` and **stays**; of what
  it returns, `CollapseTargets` and `IdentityStamped` (with `peekCollapseTargets` and
  `peekIdentityStamped`) are read only by the path being removed, and go.
- **`cmd/smd/provision_entry_test.go`** names the two subcommands in its routing cases
  (`:73`, `:74`); those cases go, the file stays.
- **Mixed test files are edited, not deleted:** a file that holds both a down-migration
  test and a test of restore, of a constraint or of a forward upgrade keeps the latter.

### C2e part 2 — the tests moved onto the supported mode (built 2026-11-18; reviewed; committed e414c6b7 on direction; no product behaviour changed)

Part 2 of the C2e brief: every test that does not exist for the "off" state now runs as the
desktop app does, so that part 3 can remove the switch without the suite depending on it.
Reviewed by Codex in three steps, each with its own gate (FT8: codex-153; API: codex-157;
daemon: codex-159). Committed on the operator's direction ("commit and push part 2").

**What was built.**
- **`internal/ft8`.** The eighteen plain commands that named no app lifetime, and plain
  `ArmTx`, left the production files (`operating.go`, `servicetx.go`); nothing in production
  called them. The tests keep them as wrappers that name one fixture lifetime
  (`fixture_owned_test.go`). The shared test service is owned and present; a test of "no
  operator" builds the bare one. The four safety outcomes that had been tested through the
  old linger are kept and now tested on presence: eviction through the real stream handler;
  loss while capture is down; a return inside the grace; and a profile claim after the arm
  ends.
- **`internal/api`.** A fixture app (`fixture_app_test.go`): a valid lifetime, a credential,
  the real admission, the app listener's mark and TLS state. Handler tests, the real-listener
  tests and the WebSocket tests run through it. The WebSocket origin table is the app
  listener's rule: only `https` on the app's own address and port is accepted. Refusals by
  the origin guard are asserted by their code (`cross_origin`), so the check cannot stand in
  for a broken guard. One test changed its claim: the app's write is found in the station's
  database and the main listener refuses to serve it.
- **`cmd/smd`.** The test daemon runs with the check on. An ordinary request (`apiCall`)
  goes over the app listener, accepting only this install's certificate and presenting its
  credential; a test of the main listener says so (`mainCall`). There is no fallback from one
  to the other.

**The request helpers' contract, kept and extended** (`cmd/smd/apicall_test.go`, AC1 to AC9):
a listener not yet up is waited for; once a request may have reached the daemon it is never
sent again; a response cut short is an error. AC6 to AC8 hold the app transport to it,
including that a listener with another certificate is sent nothing. AC9 holds the aggregate
POST of the reconcile tests to it: Codex found that helper still replaying a completed POST
after a dropped response (codex-158, P2), reproduced it, and confirmed the fix against the
same reproduction (codex-159).

**The commit's automatic review (e414c6b7), one P2, fixed.** The test clients followed
redirects. A redirect from the pinned app listener would have sent the request again, with
the credential, to a listener the pin never checked; and a redirect to a port that refuses
would have read as "a dial that never connected" and replayed the POST. Shown before the fix:
the plain listener answered the redirected request, and the aggregate POST retried until its
deadline. The clients now return a redirect to the caller (`neverRedirect`; AC10, three
cases: the app listener, the aggregate POST, the main listener). No daemon test depended on
following one. Test code only. Codex verified the fix and asked that the `internal/api`
fixture clients match (codex-160): the app socket's client, its plain variant and the
real-listener pinned client now return a redirect too, with one check through the real
`/app` redirect (`TestFixture_TheAppSocketClientReturnsARedirect`), which failed before the
change by answering 200 for `/`.

**Two fixtures that are not what they look like, named in the code.** `appSocketPlain` is a
plain socket with a modelled TLS state, for the one shutdown test whose connection double
must read the server's plaintext; it is no evidence about TLS or the origin rule's scheme.
Three stream tests serve from a private mux outside the handler chain, as they did before.

**Measured.** With part 3 simulated in a temporary copy (the check forced on, FT8 ownership
forced required), 13 test functions fail: 3 in `internal/ft8`, 6 in `internal/api`, 4 in
`cmd/smd`. **Corrected 2026-11-18 (codex-162):** this first read "the tests that fail are
those of the off state and no others". A failing function is not one kind of test: one of
the three in `internal/ft8` is mixed and fails only on its off half, and the simulation
cannot see a test that never builds a server. Part 3's section lists what was deleted and
what was edited, function by function. Before their steps the same simulation
failed 78 in `internal/api` and 24 in `cmd/smd`.

**Two daemon tests pinned to the off state, and their ruling.** The behaviour exists only
there: a busy app port that does not stop the station
(`TestDesktopListener_ABusyAppPortNeverStopsTheStation`) and the app listener never taking
the main listener's port (`TestDesktopListener_NeverTakesTheBrowserListenersPort`). With the
check on, both situations end the start, which the on-state tests cover. **Ruled by the
operator, 2026-11-18: "delete both tests in part 3".**

**Proofs.** Reversions in temporary copies, each confirmed applied: the four FT8 outcomes
(the return inside the grace needs all three of its guards reverted to fail); the shared
request loop retrying every error (AC2, AC7, AC8 fail); the aggregate POST retrying every
error (AC9 fails); the certificate pin removed (AC8 fails); a body-read error discarded (AC3,
AC9 fail). The API step changed assertions only where the
supported rule differs, and Codex required no reversion proofs for it (codex-157).

**Not shown by part 2.** Nothing about a real install: no product behaviour changed, and the
station was not touched. The switch still exists and still defaults as before. The
always-on state is simulated, not built; it is part 3.

### C2e part 3 — the check is always on (built 2026-11-18; reviewed; committed and pushed on direction 2026-11-19)

Built on the operator's direction ("yes, start part 3") from the plan verified by Codex
(codex-162), and widened by the operator's ruling "(a)" after a finding made while
building it.

**What the operator sees.** `smd` has no setting and no state in which a client without
this install's credential is served. `config.json` is rewritten once, at the first start, to
version 7 without `server.require_desktop_app`. A first-time install serves only the desktop
app from its first start. What is checked is the credential on the app's listener: the app
presents it, and so can a program of the operator's own user that reads the file; nothing
here shows that the daemon recognises an Electron executable. `GET /v1/healthz` and the
fixed notice stay public.

**One-way, stated because it is.** A version 7 file is refused by every older build, and
this build has no config downgrade. The previous pair (265.g8e64d2ed) cannot start on the
migrated file.

**The finding that changed the scope.** The brief said the key could go "without touching
that tooling". That was wrong. With the head at version 7 and no way down, the once-only
`config.v5.json` could no longer be made from a file already at version 7, and the start-up
strip of the deprecated forwarder fields waited for that copy for ever
(`cmd/smd/config_v6_strip.go`). Codex reproduced it (codex-163). The station was not
affected: its strip ran on 2026-10-10. Ruled (a): the config half of the downgrade tooling
leaves in this part.

**Built.**
- **Config** (`internal/config`): `migrateV6toV7` deletes the key from a `server` block that
  is an object, whatever it said; a number or an array is left for the typed load to refuse;
  an absent or `null` block loads as before. The head is 7; the field is gone. Forward
  migrations only: `DowngradeDocument`, the three `down` steps and the helpers only they
  used (`CheckDocument`, `DocumentSchemaVersion`, `WriteDocument`) are removed.
- **FT8** (`internal/ft8`): `ownershipRequired` and `RequireOperatingLifetime` removed; the
  five readers lose their off branch; the capture linger no longer disarms. Presence is the
  only disarm for a lost operator. `lingerWindingDown` stays: the capture release still
  drops the lock while it drains.
- **API** (`internal/api`): the check and the FT8 wiring unconditional (`ft8Svc != nil`
  kept, codex-162); the admission's off paths gone; `required` gone from the reply of both
  admission routes (ruled).
- **Daemon** (`cmd/smd`): a refused identity, a busy app port, an unusable credential and a
  later serve error always end the start or the daemon; the messages say "smd serves only
  the desktop app, and …". The start-up strip takes no copy first; an existing
  `config.v5.json`, or anything else under that name, is not read, changed or removed.
  `smd config-downgrade` is removed, and **its name is kept as a refusal**: any argv that
  names no subcommand starts the daemon, so without it `smd config-downgrade --to 5 --yes`
  started one (seen in the entry-point test before the refusal was added).
  `archive.ReadHomeBindings`, `HomeFile`, the two rollback-only peek fields and
  `scripts/rollback-drill.sh` are removed; `PeekDestinationBindings` stays for restore.
- **Shell** (`desktop/`): the `'not-required'` state is gone from `admission.mjs`; a reply
  that claims `required: false` is not believed and the app admits all the same. The
  harness's off mode is gone. `check:c2b-smd` and `SM_DESKTOP_REQUIRE_APP` are removed from
  `package.json`, `Taskfile.yml`, `scripts/ci-local.sh` and CI; the real-`smd` scenario is
  one run on a fresh version 7 file.
- **The three clients:** the Vite dev server (task, script, proxy), `cmd/loadgen` (with its
  lint exclusion and three baseline entries) and the Cabrillo script's address form of
  `--from-daemon`.
- **Documents:** `api-endpoints.md`, `config.md`, `install.md` (the desktop app is the way
  in; there is no switch and no rollback), `docs/ft8.md`, `AGENTS.md`, `desktop/README.md`,
  `DEVELOPING.md`, `frontend/app/README.md`, the top paragraph of `README.md`, and four
  passages of the manual.

**Behaviour that changed beyond the switch, each on purpose.**
1. A file at version 7 with deprecated forwarder fields is stripped at its first start with
   Home seeded; before, it never was.
2. Two shapes that used to wait because no version 5 copy could be made of them are now
   stripped like any other: an entry with no name, and evidence sync on with SM Cloud
   disabled.
3. Staging files that an interrupted copy left beside `config.v5.json` are no longer
   removed by a later start, since no later start makes a copy. The station has none.
4. `onLingerExpired` is one critical section.

**Tests: what was deleted and what was edited** (each whole function read first).
- *Deleted, off state only:* FT8 `TestOperating_NotRequired_NothingChanges`,
  `TestPresence_NotRequired_StreamsAreNotCounted`. API `TestAppAdmission_OffDoesNothing`,
  `TestAppAdmission_OffAnArmNeedsNoStream`, `TestOperatingClient_NoNoticeWhenTheCheckIsOff`,
  `TestOperatingClient_OffChangesNothing`, `TestApp_TheBrowserListenerIsUnchanged`. Daemon
  `TestOperatingClient_OffTheCredentialIsStillMadeAndNothingAsksForIt`,
  `TestOperatingClient_CheckOff_ARefusedCredentialNeverStopsTheStation`, the two ruled by
  the operator, and four function-level tests of "reported, never fatal", each with a twin
  that asserts the same input as a start-up error.
- *Edited, the supported half kept:* the mixed FT8 test (now
  `TestPresence_TheCaptureLingerDoesNotDecidePresence`); `TestOperatingClient_TheRule` (22
  rows kept); `TestOperatingClient_TheHealthCheckStaysOpen`, whose baseline is now the health
  handler's own answer; the credential tests (one state). The check that a served request's
  credential is not logged moved into the logging test.
- *The downgrade path:* the config command's tests and the tests of the down steps are
  deleted with them; their forward halves stay. The strip tests are edited, not deleted:
  bindings and routes unchanged, another archive active strips nothing, a failed write is
  retried, an existing copy is left byte for byte.
- *The Cabrillo tests:* the plain-address class's assertions moved onto the pinned fixture.

**Written first and seen to fail:** the version 7 migration tests; the strip with no copy
(failed on "the stub account still holds name"); the refused command name (the daemon
started); the shell's reply that claims no admission is required; the Cabrillo address.
**Reversion proof in a temporary copy:** the migration step emptied, and the station-shaped
file is refused as an unknown key.

**Not shown by any of this.** A second install's upgrade. The station: the pair is not
deployed; its file is still version 6 with the key. Nothing was transmitted.

**Review of the built part 3 (codex-164, 2026-11-18): two P2 findings, both fixed in the
working tree.**
- *The install guide left the desktop app out.* `docs/install.md` installed and updated the
  daemon's package only, while the same change makes the desktop app the only way in. The
  guide now installs, updates and removes the two packages together, says that the desktop
  package requires exactly the daemon's version, lists what each installs, and tells a
  maintainer that `scripts/release.sh` builds the daemon's package only. Two sentences that
  were false went with it: one of mine in §6 ("To switch the setting off ..."), and an older
  one ("The post-install scriptlet prints the next steps"; neither package has a scriptlet).
  *Measured:* the two packages accepted in one `rpm -U --test` transaction and the daemon
  alone refused (D1, above). *Not measured:* the `dnf` commands as written, with two local
  files, and the `dnf remove` of both.
- *The version 5 first-start test never loaded its version 5 file.* It built the daemon on
  the service that had seeded the fixture at the head version. It now builds it on what
  `config.Load` returns for the file, as `run()` does. Proofs by build overlay, the tree
  untouched: with the 5-to-6 step out of the registry the test fails at the load ("no config
  migration registered from version 5") while the head-version test passes; with the strip
  made a no-op both fail on "still holds name".
- *Found on the way, not part 3, not fixed:* loaded through `config.Load`, the shared
  fixture failed its first start: `UNIQUE constraint failed:
  logbook_destination.forwarder_name`. The load adds a nameless default account for every
  registered type the file has no entry of, a nameless account seeds under its type's name,
  and the fixture names an account of another type "qrz". So a file that names an account
  after a registered type it has no entry of cannot seed an unseeded Home. The station's
  Home is seeded. **Closed by the operator, 2026-11-19 (Rulings): it cannot happen. There
  are no installs but the operator's, and the one other known is handled by hand.**

**Committed 8cccbd09 on direction (2026-11-19: "README approved, remove preview, commit and
push part 3") and pushed. Hosted run 44462965649 on it: success.** Its clean-room review gave
two findings; the follow-up below was reviewed in the exchange (codex-166, codex-167) and is
committed on direction ("commit and push the follow-up and the Fox notes after CI").
- **[P1] "A listener conflict can take a working station offline on upgrade": the upgrade
  case is closed by the ruling on other installs; the rule behind it was wrong and is
  fixed.** The review's case is an existing install whose main listener sits on 8443 with
  the check off, which is not owed compatibility. What it showed holds for any install from
  here on: a `config.json` whose desktop app listener shares the main listener's port was
  only a *warning* (`app_port_conflict`), so the file loaded and `smd config-check` passed,
  and the daemon then stopped at the bind. The warning dated from when a failed app
  listener was only reported ("reported until C2, fatal after", 2026-11-10); part 3 made
  the failure fatal and left the warning behind. **Now a refusal:** the file is refused at
  load, and so by `smd config-check`, with a message naming the port, `server.app_port` and
  `socket_path`. (The two settings are file-only: the config API neither shows nor accepts
  them.) Tests first and seen to fail (the two validation rows, and a load of each
  conflicting file); a file with separate ports is the control.
  - **Corrected in review (codex-166, P2): my first refusal compared only the port
    numbers,** so it turned away files that do not conflict, such as a main listener on
    `127.0.0.2:8443` or `[::1]:8443` beside the app's `127.0.0.1:8443`. The rule is now the
    endpoint: the same port on `127.0.0.1` (also written `::ffff:127.0.0.1`) or on every
    address (no host, `0.0.0.0`, `[::]`). The same port on any other address is no
    conflict. A host name is not judged and is left to the bind, as is a port another
    program holds. Measured on free ports before the change and held by a test that binds
    each kind of address and requires the validation to agree with the second bind
    (`TestAppPort_TheRuleAgreesWithARealBind`). Seen to fail both ways: the port-only rule
    fails the other-address cases, and a rule that knows only `127.0.0.1` fails the
    every-address cases. `docs/v2-design/config.md` said of the conflict "and
  the station starts", which part 3 had made false and I had not corrected; corrected.
- **[P2] "The v7 migration can change the configured default logbook ID": true of the
  mechanism, no change.** A migrated document's numbers pass through `float64`, exact up to
  2^53 and rounded above it, as in every step since version 1 to 2. Codex showed it through
  the loader (codex-166): a version 6 file holding 9007199254740993 loads as …992, and the
  same file at version 7 keeps it. Nothing in the code keeps the file's 64-bit integers
  (`default_logbook_id`, the rig ids, `max_body_bytes`) under that size; it is left as it
  is on the assumption that none is ever that large. I first wrote "not reachable", which
  claimed more than that. The assumption is stated in a comment at `migrateDocument`.

**The follow-up is committed d3d3476e and pushed, with the Fox notes as a7e44f45 (documents
only), on direction (2026-11-19). Hosted run 44466913122 on a7e44f45: success.** The
clean-room review of d3d3476e gave one finding.
- **[P2] "TCP service names bypass the load-time conflict check": true, and left to the
  bind; a comment, a test row and one sentence of `config.md`, uncommitted.** A
  `socket_path` of `127.0.0.1:http-alt` with `server.app_port` 8080 loads (measured), and
  where the system's services table gives `http-alt` as 8080 the daemon then stops at the
  bind. It is the host-name case over again: which port a service name means is not in the
  file, and the operator's ruling on host names ("item 2 agreed") was to keep such
  look-ups out of the validation and leave them to the bind. Applying that ruling to a
  service name is my reading, not a ruling in those words.
  - **Operator, 2026-11-19, not yet a ruling: "I would go with refusing any non-numeric
    port, but let's wait for Codex, then I'll decided".** Nothing is built for it. Measured
    for that decision: today the validation does not look at `socket_path`'s port at all.
    Under `tcp`, a service name, an empty port, `0`, `99999` and `-1` all pass it.
  - **Operator, 2026-11-19, after Codex's review (codex-168), not yet a ruling: "I agree
    about the port range, but wonder if 1024-65535 as the lower ports are usually reserved
    for other services?"** Measured for that question: on this machine an ordinary user may
    not bind a port below 1024 (`net.ipv4.ip_unprivileged_port_start = 1024`, the Linux
    default), and `smd` runs as a user service with no capability granted
    (`packaging/smd.service`). So a main listener below 1024 fails at the bind with
    "permission denied" unless an administrator has lowered that setting. The same holds
    for `server.app_port`, whose accepted range today is 1 to 65535.
  - **Operator, 2026-11-19, the ruling: "approved, leave explicit 0 as the default".** What
    was approved (put in chat after codex-169): under `server.protocol` `tcp`,
    `socket_path` must be host:port with a plain decimal port from 1024 to 65535, and a
    service name, an empty or missing port, a signed number, zero and anything out of range
    are refused at load; `server.app_port` left out or 0 still means the default 8443, and
    a value that is given must be from 1024 to 65535; the conflict refusal stays. An
    explicit `"app_port": 0` keeps meaning the default.
  - **Built on that ruling (2026-11-19; uncommitted; for review).** It replaces "left to
    the bind" above: a service-name port is now refused with every other port that is not
    a plain number in range.
    - `validateMainListenerAddress`: under `tcp`, `socket_path` must split as host:port,
      the port must be ASCII digits and from 1024 to 65535; the finding is
      `invalid_field_value` on `socket_path` and says which of four things is wrong. It
      runs before the exposure check, so that `127.0.0.1` with no port is refused as a
      malformed address and not explained as a network exposure. The host is not judged.
    - `validateAppPort`: a given value outside 1024 to 65535 is refused, under a
      Unix-socket main listener too (the app's listener is always TCP); 0 stays the
      default.
    - Tests first, seen to fail against the committed code
      (`internal/config/listener_ports_test.go`, and new rows in `app_port_test.go`):
      1023 and 65536 refused and 1024 and 65535 accepted for both settings; zero, an empty
      port, no port, a service name, signs, a space, non-ASCII digits, an unbracketed IPv6
      address and more digits than an integer holds, each refused on `socket_path` as the
      first blocking finding; a host name with a numeric port and Unix-socket paths
      accepted; the same through `Load`; the app port left out and an explicit 0 both
      giving 8443. Proof of the ordering by overlay: with the rule moved after the
      exposure check, the no-port and not-an-address cases fail on "the first blocking
      finding".
    - `docs/v2-design/config.md` states the range as Station Manager's supported range,
      not as a claim about every system (codex-169).
    - *Not changed:* the pinned client (`scripts/smd_client.py`) and the desktop shell
      still accept any port from 1 to 65535 when they read `app_port`. A daemon does not
      start on a value below 1024, but a reader can still meet one: the file can be edited
      while a daemon is running (codex-170). Aligning the readers' range and message is
      separate work.
    - Gates: the whole Go suite (66 packages), lint, vet, the maintainability gate and the
      document gates pass; a temporary `smd` built from this tree started and served the
      operator's tools test (4 tests).
    - Reviewed in the exchange: codex-170 closed it with no blocking finding.
    - **Committed and pushed on direction (operator, 2026-11-19: "commit and push the port
      rules").**

### C2e — at the station (2026-11-19; deployed and accepted by the operator)

**The port rules are committed 8e957293 and pushed. Its clean-room review: no actionable
findings. Hosted run 44470618763 on it: success.**

**Deployed by the operator** with `task deploy:local:dev` (operator, 2026-11-19: "deployed,
run the read-only checks"). Read-only checks by Claude, about a minute after the start; no
request was made to the daemon and nothing was keyed:

- *Packages:* `station-manager` and `station-manager-desktop` are both
  `2.0.0~alpha.5.275.g8e957293-1` (before: `…265.g8e64d2ed-1`).
- *Service:* active and running since 08:44:53 CAT, no restarts, result success.
- *Listeners:* `127.0.0.1:8080` and `127.0.0.1:8443`, both held by that `smd` process.
- *`config.json`* (key names and counts only): version 7; `server.require_desktop_app` is
  gone; no forwarder entry holds `name` or `enabled`; mode 0600, rewritten at 08:44.
  `config.v5.json` is untouched (10 October, 0600).
- *`smd.log` since this start* (33 lines, all from `2.0.0-alpha.5-275-g8e957293`): "config
  saved"; the databases opened and verified; **"operating-client check: only the desktop
  app is served, with this install's operating credential"**; "HTTP server listening";
  "desktop app listener listening (TLS, loopback)". No error. Four warnings: the two
  standing ones (the serial-line notice; SM Cloud over plain HTTP), and "bridge: rig went
  quiet" then "rig unreachable". The same warning pattern was already observed after this
  morning's 06:49 start of the previous build, so the pattern predates this deploy; its
  cause has not been established (codex-171). That the rig is switched off is my
  inference, not checked: the two messages report a window with no data and then the
  strike limit, nothing more.

**Not shown by these checks:** the desktop app opening and operating; the browser's notice;
the refusal of a plain client; the rig; FT8. Those are the operator's to look at, or need
requests to the station that I have not made.

**Accepted by the operator, 2026-11-19: "accepted, commit and push the dossier".** C2e is
done: the operating-client check is permanent and is what the station runs. What the
operator looked at beyond the checks above is not recorded here.

### After the acceptance (2026-11-19): the readers' port range; a red hosted run and what it showed

**The operator, 2026-11-19: "align them. I want to finish the security work and then move
on, not leave items dangling..."** The desktop shell (`desktop/src/discovery.mjs`) and the
pinned client (`scripts/smd_client.py`) read `server.app_port` for themselves and still
accepted 1 to 65535. Both now hold the daemon's range, 1024 to 65535, with the same words;
left out or 0 is still 8443. Tests first, seen to fail (1, 443, 1023 accepted; 65536
refused with the old range in the message). Uncommitted, for review.

**Hosted run 44475639670 on 3873ab6e (documents only) failed**, in the desktop credential
scenario (C2b): "…neither window is navigated or told again, and a window that asked for
nothing is told nothing — before any request 0,1,0". The code is that of 8e957293, whose
run passed.

- **Cause, established.** A diagnostic added to the check names the requests that reached
  the fixture in the scenario's quiet time: `POST /v1/app/lifetime`, then the shell's
  probe. Each window the scenario launches is a start of the app with a lifetime of its
  own, and since part 3 the fixture always admits (before, its admission was off in this
  scenario). So the scenario's two windows, started together, replaced one another's
  lifetime; the replaced one asked `smd` about it on its next response; and when that
  asking straddled the scenario's replacement of the credential, its second request was
  refused and the window was told. **A fault of the scenario, introduced by my part 3
  change to the fixture; the shell did what it is designed to do.** Reproduced locally: 5
  of 12 runs of the committed scenario.
- **Fix, in the scenario only (uncommitted).** The two windows are started in order, so
  that it is always the first whose lifetime is replaced, and the scenario waits until
  the fixture has that window's own admission request before it replaces the credential.
  The diagnostic stays. After it: 24 runs, none failing on that check.
- **The same commit passed on a re-run.** I re-ran the failed job (attempt 2 of run
  44475639670): success, every step. So `3873ab6e` is green on its second attempt and red
  on its first, with no change between them: the failure is intermittent, as the cause
  above predicts.

**A second intermittent failure, found while repeating the scenario, and not fixed.**
"Another port serving the same certificate receives nothing, directly or by redirect" fails
in about one run in three on this machine today: the other listener completes **one TLS
handshake and receives no request**, so no credential and nothing of the page reaches it.
- It follows the *redirect* case, not the direct one (measured in a temporary copy, 14
  runs: after the direct fetch 0 every time; after the redirected fetch 1 in 4 runs). The
  page asks the pinned origin for an address that answers with a redirect to the other
  port; the shell blocks the redirected request and the fetch fails, as it should; but a
  connection to the redirect's target is sometimes made first.
- It is not from part 3 or from today's changes: the scenario at 4c7e739e, before part 3,
  fails the same way in 5 of 12 runs here today. No hosted run I have read has shown it,
  and I had not seen it on this machine before today (the machine was restarted this
  morning on a newer kernel; whether that matters is not established). Why it shows now
  is not established.
- What it means, as far as established: a redirect sent by the pinned daemon can make the
  app open a TLS connection to the address the redirect names. No HTTP request, and so no
  credential, is sent on it; the handshake's own bytes are exchanged, so it is not "nothing
  sent" (codex-172, which reproduced it: 6 of 20 trials, with a TCP accept each time).
  What opens it, and whether the shell can stop it before the connection, are not
  established. **Open; for the operator and Codex.**

### The redirect guard (2026-11-19; built on the operator's direction; reviewed; committed and pushed on direction)

**Operator, 2026-11-19: "commit and push 1, fix the redirect now, keep the stronger
guarantee".** Item 1 is committed 90a7b830 and pushed (its clean-room review: no actionable
findings). This section is item 2, held to item 3's guarantee: **an address other than the
daemon's gets no contact at all from the app.**

**Acceptance.** A redirect from the daemon's address to any other address is refused, the
request fails, and no connection is opened to the address it names, not even TCP. A
redirect that stays on the daemon's address is followed and carries the credential once.
The nearest confusable outcome is what there was: the request fails and nothing is sent,
but a connection was made.

**What was read first.** The pinned Electron's own type definitions
(`node_modules/electron/electron.d.ts`, 44.9.3): the response-header hook's answer may hold
`cancel`, and its details give the status and the headers as lists. Electron's
documentation and source for what a cancel does to a redirect are Codex's citations
(codex-172), not read by me; the behaviour is measured below.

**Tests first.**
- *The rule* (`redirectsOffOrigin`, a pure function): the five statuses a redirect is
  followed on and eight that are not; relative, absolute and query-only addresses that
  stay; another port, another host, a downgrade, scheme-relative, another scheme and two
  look-alike hosts that leave; the header in four spellings, as a string and as a list; a
  Location that does not parse; two Locations (either off, both on but different, the same
  twice, under two spellings); no Location; no origin pinned.
- *The hook:* an off-origin redirect is answered exactly once with a cancel, the report is
  told the two addresses, and the marker on that response is still read; a redirect within
  the origin and an ordinary response pass unchanged.
- Both failed against a stub that refuses nothing, on "301 to another port was let
  through" and on the missing cancel.

**The change.** `desktop/src/hardening.mjs`: `redirectsOffOrigin` and `redirectTarget`.
`desktop/src/admission.mjs`: `watchDaemon`, still the one owner of the response-header
hook, cancels such a redirect and otherwise answers as before; marker reading is unchanged.
`desktop/src/launch.mjs`: the refusal is recorded with the blocked requests and logged as
"shell: refused a redirect off the served origin". The request filter and the credential
filter are untouched. What cannot be read as one address is refused. A redirect must keep
the origin's own scheme: its WebSocket partner is for upgrades, not redirects.

**The scenario (C2b), extended.** The fixture now counts TCP accepts, and its redirect
route takes a status, a header spelling and a second Location. A third listener serves a
certificate that is not the daemon's. New checks: twelve forms of redirect to the other
port (five statuses, `LOCATION`, scheme-relative, two Locations either way round, a chain,
plain HTTP, not an address), each refused, with **no accept** at the other port; none at
the wrong-certificate port; no plain-HTTP attempt at the daemon's own port; and three
redirects that stay (relative, absolute, a chain) followed with the credential once. The
old check now counts accepts too. 39 checks.

**Measured, by repetition** (the fault is intermittent, so one pass shows nothing):
- *With the guard:* 20 runs of the whole scenario, 20 pass.
- *Without it* (a temporary copy, the rule made to refuse nothing): 10 runs, **9 fail**,
  always on the new redirect check, each time with accepts and no request. Once the
  wrong-certificate port was reached as well: one accept, one failed handshake, no request.
  So before the guard a redirect could also make the app try a handshake with a server
  that is not the daemon; the pin refused it.
- The two-Location forms are refused with no line from the shell's hook in the log. I take
  it that Chromium refuses them itself before the hook; I have not established that.

**Gates.** Desktop lint and format check; 108 unit tests; A1, C1 and C1d-shell; C1d against
a temporary real `smd` built from this tree (19 checks: the daemon's own redirect within
its address still works).

**Review (codex-173): one P2, fixed. The new log line printed addresses in full.** It
interpolated the redirect's source and target as they came, so a query, user-info, a path
or a fragment could reach the log; the older "blocked a request" line beside it did the
same. That the daemon chooses a redirect does not make its address free of data. I had
named the full addresses to Codex as a point for its judgement and judged it wrongly
myself.
- *Now:* both lines name an address by its scheme, host and port only (`addressLabel` in
  `trace.mjs`, with the module's one-line escaping and a length bound); a target that does
  not parse is "[not an address]", and none or several is "[no single address]". The full
  addresses stay in memory for the scenarios' own assertions and are not printed.
- *Tests first, seen to fail* against line builders that printed the addresses as before:
  the label; the blocked-request line; the refused-redirect line, with seven synthetic
  markers (source query and path; target user, password, query, fragment and path), none
  of which may appear. The hook's test also delivers such a laden redirect: still one
  cancel, and the marker and the admission generation still read (that part passed
  already and pins the behaviour; it was not one of the failing tests).
- *After it:* 111 unit tests; A1, C1, C1d-shell; C2b 8 runs of 8; C1d against a temporary
  real `smd`.

**Not shown.** A redirect to a host off this machine (every target here is a loopback
port). Any way Chromium might connect elsewhere that is not a followed redirect. Hosted CI.
The installed app at the station: this reaches it with the next deploy.

**Hosted run 44482773341 on 90a7b830 (the readers' range and the scenario's race fix):
success.**

**The guard is committed 99c9c840 and pushed** (eleven paths; before it, lint, the format
check, 111 unit tests and the four fixture scenarios). Its clean-room review gave one
finding.
- **[P3] "The same-port downgrade check can miss a TCP connection": true, fixed in the
  scenario (committed locally on direction, not pushed).** The check that a redirect does not go down to plain HTTP at the
  daemon's own address read the failed-handshake count and the requests, but not the
  accepts, so a connection that was accepted and then stayed idle would have passed it.
  The daemon's own port is in use throughout, so a bare count of accepts cannot be held to
  zero there. The check now also holds the number of accepted connections that began no
  TLS (accepts less completed and failed handshakes) to what it was before the redirect.
  *That the measure sees what the review named,* shown with the fixture alone: an idle TCP
  connection held for 700 ms gives 1 (one accept, no handshake); a plain-HTTP request gives
  a failed handshake, which the check already read. The scenario then passed 6 runs of 6,
  the check reporting "connections with no TLS 0 -> 0".

**The strengthened check is committed 36f6861d, locally, not pushed** (operator: "commit, but
no push"). Its clean-room review gave one finding.
- **[P2] "An earlier TLS handshake can mask a new idle connection": true, fixed (the
  fixture and the scenario; uncommitted).** My fix compared a total (accepts less handshake
  outcomes) before and after. A connection accepted before the window that finishes its
  handshake during it lowers the total by one, which would hide one new idle connection;
  the reverse gives a false failure. The fixture now keeps one record per accepted
  connection with what became of it (`secure`, `failed`, or nothing yet), and the check
  judges the connections accepted during its own window one by one: each must be `secure`.
  *Reproduced with the fixture alone:* a connection accepted before the window completes
  TLS inside it while a new one is opened and left idle; the old total reads 1 before and 1
  after, and the window's own record reads `["none"]`. A plain-HTTP request reads
  `["failed"]`, a TLS connection `["secure"]`. After it: 111 unit tests; A1, C1, C1d-shell,
  C4a; C2b 8 runs of 8, the check reporting "connections accepted meanwhile: []".

**Deployed again by the operator (2026-11-19: "deployed"), from the local commit 36f6861d.**
Its product code is 99c9c840's; 36f6861d changes a test and this dossier only. Read-only
checks by Claude, no request made to the daemon:
- both packages are `2.0.0~alpha.5.279.g36f6861d-1`;
- the service is active and running since 11:56:08 CAT, no restarts; `127.0.0.1:8080` and
  `127.0.0.1:8443` are held by that `smd` process;
- `config.json` is version 7, with neither `require_desktop_app` nor `app_port` in it;
- `smd.log` since this start (55 lines, all from that version): the operating-client line,
  both listeners listening, no error; the two standing warnings and the same two rig
  warnings as before (cause not established).
- **Not shown:** the app itself, and so the redirect guard in the installed app (no
  redirect off the daemon's address is expected in ordinary use; the guard is held by the
  scenario, not by anything read here). The installed build is ahead of what is pushed:
  36f6861d is not on the remote, and the hosted run on 99c9c840 had not finished when this
  was written.

**The per-connection check is committed 92002312, locally, not pushed** (operator: "commit").
The hosted run 44498126643 on 99c9c840 finished with success (read from the run: its one
job, the releasability gate, completed with success); it does not cover the two later local
commits. The clean-room review of 92002312 gave one finding.
- **[P3] "A TLS error emitted on close goes unrecorded for that connection": true, fixed
  (the fixture and the scenario; uncommitted).** The fixture found a connection's record by
  the peer's port. A client that closes before any handshake gets a TLS error on a socket
  that no longer has a port, so the failed count rose while the record stayed unsettled and
  read `none`. The check still refused that connection; only its diagnostic disagreed with
  the count. The record is now found through the raw socket under the TLS one (`_parent`,
  which Node sets but does not document; the fixture's comment says so). Because an
  undocumented field could be missing in another runtime, the scenario's check also
  requires that at least one record has settled to `secure` since the start: without that,
  a window with no connection in it would prove nothing.
  *Reproduced with the fixture alone:* connect, close before any TLS; before the fix
  "failed 0 -> 1; record `["none"]`", after it `["failed"]`. The masking case, a plain-HTTP
  request and a TLS connection read as before (`["none"]`, `["failed"]`, `["secure"]`).
  After it: lint, format, 111 unit tests; A1, C1, C1d-shell, C4a; C1d against a scratch
  build of `smd`; C2b 4 runs of 4, the check reporting "connections accepted meanwhile: [];
  all since the start: `["secure","secure","secure","secure","secure"]`", which shows the
  records settle in the scenario's own runtime (Electron's).
- Codex (codex-175) checked both the committed check and this follow-up as it stood in the
  working tree: with a connection injected in the scenario itself, the per-connection check
  fails the run where the total-based one passed all 39; the follow-up records `failed` for
  the closed-before-TLS case. Its agreement is not the operator's approval.

**The follow-up is committed 7adb5d48 and all three test commits are pushed** (operator:
"commit and push"); `main` and the remote agree at 7adb5d48. Its clean-room review: no
actionable findings. The hosted run 44515000943 on 7adb5d48 was in progress when this was
written.

**Deployed a third time by the operator (2026-11-19: "deployed"), from 7adb5d48.** Its
product code is still 99c9c840's; the three commits since change tests and this dossier.
Read-only checks by Claude, no request made to the daemon:
- both packages are `2.0.0~alpha.5.281.g7adb5d48-1`;
- the service is active and running since 13:28:28 CAT, no restarts; `127.0.0.1:8080` and
  `127.0.0.1:8443` are held by that `smd` process;
- `config.json` is version 7, mode 0600, with neither `require_desktop_app` nor `app_port`;
- `smd.log`: the earlier process stopped cleanly at 13:28:21; from the start at 13:28:28,
  the operating-client line and both listeners listening at 13:28:30, no error; the two
  standing warnings, then "rig went quiet" at 13:28:35 and "rig unreachable" at 13:28:40,
  5 and 10 seconds after the pipeline started, as after both earlier deploys (cause not
  established; no rig command was sent to find out).
- **Not shown:** the app itself, and the redirect guard in the installed app. The installed
  build is the pushed head.

**Accepted by the operator, 2026-11-19: "accepted, the rig was off, commit and push the
dossier".** The deploy of `2.0.0~alpha.5.281.g7adb5d48` is accepted. The two rig warnings
have their cause on the operator's word: the rig was off. The hosted run 44515000943 on
7adb5d48 finished with success (read from the run: its one job, the releasability gate,
completed with success), so the installed build is the pushed head and that head is green.

### The database half of the downgrade tooling: the removal plan (2026-11-19; revised once after Codex's verification, codex-180; nothing deleted)

Ruled: "remove the downgrade tooling" (2026-11-18), option (a) for its two halves, and on
2026-11-19 that this half goes before W-0029's part 1 and that the up files are not folded
(Rulings). The config half left with C2e. This plan is read from the tree on 2026-11-19; it
replaces the first-pass inventory above for this half. Codex's verification (codex-180)
found the inventory incomplete in two places and the test sorting too coarse; each point
was checked in the source and is written in below. `scripts/rollback-drill.sh`, listed in
the first pass, is already gone from the tree.

**Outcome the operator sees.** `smd db-downgrade` is refused by name, says it was removed,
changes nothing and starts nothing. A database at an older schema still comes up to the
newest when it is opened. A new migration is one file, upward only.

**Nearest confusable outcome.** `smd db-downgrade --to 18 --yes` **starts the daemon**: any
argv that names no subcommand does (the reason `config-downgrade` is refused by name,
`cmd/smd/main.go:144`). So the name is not just deleted from the table; it is refused as
`config-downgrade` is.

**Why not just leave the tooling and write no down step for 0020.** Measured by Codex and
repeated by Claude (W-0029's return section): the library treats a missing down file as an
empty step, so a step down "succeeds", records the lower version and leaves the newer
tables; the next step up fails and leaves the file dirty.

**What goes.**
- `cmd/smd/db_downgrade.go` and `db_downgrade_test.go`; the `db-downgrade` entry of the
  subcommand table becomes a refusal by name (`removedSubcommand`), the table's comment
  (`cmd/smd/main.go:133`) says so, and its row in `provision_entry_test.go` moves from the
  usage table to the removed-names test.
- `internal/database/sqlite/downgrade.go` (`DowngradeLogSchemaTo` and its three refusal
  helpers) and `downgrade_test.go`.
- The 22 SQLite `.down.sql` files: 19 under `migrations/log`, 3 under `migrations/reference`.
- One live comment that names the tooling: `internal/archive/bindings.go:714`.
- The live text: `docs/install.md` ("`smd db-downgrade` is still in this build and is being
  removed too").

**What stays.** All 22 up files, **byte for byte**: the comments in 0015 and 0018 that
describe the old way down stay as history (codex-180; this settles the first draft's
question about 0018's comment). The guard by which a build refuses a
`config.json` newer than it knows. SM Cloud's 8 down files and their tests
(`internal/cloud`), which are not this tooling. History: W-0021, ADR 0082, the acceptance
reports, left as written; W-0021 gets a dated pointer to the ruling.

**The tests: three kinds, sorted function by function as the first step of the build.**
Read so far: 17 functions call `DowngradeLogSchemaTo` (in `contest_serial_test.go`,
`downgrade_test.go`, `identity_since_test.go`, `migration_adoption_confirmation_test.go`,
`migration_adoption_reservation_test.go`, `migration_logbook_destination_test.go`,
`migration_operator_event_archive_test.go`), and the test helper `migrateToVersion`
(`service_test.go:227`, used in nine files) goes down as well as up
(`TestMigrate0007_DownPreservesEveryPreExistingColumn`,
`TestMigrate0008_DownDropsTableUpRestoresIt`,
`TestMigrate0009_DownKeepsNotificationRowsAndDiscardsAlarmRows`).
1. **An assertion about a down step or a downgrade refusal:** deleted with what it tests.
2. **An assertion about an up step whose test reaches its starting version by going down
   from the head** (for example `TestMigrate0015_UpgradesAFileAlreadyAt14`): kept, and its
   start rebuilt by migrating a fresh file **up** to that version, which is how a real
   older file got there.
3. **A test holding both:** the upward assertions kept, the downward ones removed.
- **The sorting is by assertion, not by a test's name (codex-180).** A test named "Down"
  can hold upward proof. Named so far, each to be kept in its upward part:
  `TestMigrate0013_..._DownDiscardsOnlyThem` (at the head, the two archive kinds accepted
  as notifications and refused as alarms); `TestMigrate0009_DownKeeps...` (its tail: rows
  surviving the way up, alarms accepted); `TestMigrate0009_RebuildKeepsTheIdHighWaterMark`
  (an upward rebuild with an evicted high id); the 0006 and reference-0003 round trips
  (their upward row-survival checks); for 0015, both schema-14 variants, the legacy-name
  behaviour, the constraints and the AUTOINCREMENT proof; the partial-index and foreign-key
  round trips (one is in `migration_origin_test.go`; the rest are found by the sorting).
- **Two helpers the first draft missed.**
  - `riMigrateTo` (`cmd/smd/restore_identity_test.go:808`) calls the library directly, so a
    guard on the sqlite package's helper would not cover it. RI13 builds files at the
    head and steps them **down** to 12 and 13 with it. Codex showed by overlay that with
    the down files gone RI13 still passes while its fixture keeps the newer table: the
    version marker moves and the schema does not. RI13 is kept; its files are built upward
    from empty to 12 and 13 with fixtures of that version, keeping their archive and
    logbook identities; it asserts the old table shape (no `logbook_destination` table)
    before the read-only restore; and its no-migration and no-file-change assertions stay.
  - `applyReferenceMigrationSteps` (`migration_reference_test.go:12`) is called with -1 in
    `TestMigrate0003Reference_DownRoundTrips`. The reference set is part of this removal:
    its upward row-survival check is kept on a real version-2 file.
- **Every retained helper refuses to go down:** `migrateToVersion` and `riMigrateTo` (if it
  remains) refuse a target below the file's version, and the two relative-step helpers
  refuse a step that is not positive. Without that, once the down files are gone a test
  that still steps down passes through an empty step and asserts on a schema that never
  changed. This is the confusable outcome inside the test suite itself.
- **Coverage is compared before and after by what is asserted,** not by a count of up steps
  crossed: the fixture states and the surviving assertions on rows and values, constraints
  and indexes, foreign keys and sequence state. The build's first step is that table,
  function by function, put to Codex before any test is changed.

**The sorting table (2026-11-19; the build's first step, on the operator's "build the
removal"; for Codex before any test is changed).** Every test function that moves a file's
schema version was listed by script (the service it starts from and each move, in order)
and then read: 39 functions, 33 in `internal/database/sqlite` and 6 in `cmd/smd`. Three
tests that only go up (`TestMigrate0002Reference_...`, `TestMigrate0004_...`,
`TestMigrate_RelaxesRSTLengthFromVersion1`) and two helpers in `cmd/smd` that only go up
(`seedFailedUploads`, `seedStationQueue`) are not affected.

- **A check made first, because every rebuilt fixture depends on it:** is a log file at
  version N the same shape whether it got there upward from empty or downward from the
  head? Measured by a throwaway overlay test over every N from 1 to 18, comparing the
  stored definition of every table, index and trigger (comments stripped, whitespace,
  quoting and case folded): **the same for 2 to 18; different at 1** (the `qso` table).
  The fixtures to be rebuilt start at 5, 6, 7, 8, 9, 10, 11, 12, 13 and 14, so rebuilding
  them upward does not change where any test starts. The one test that goes to 1 is of the
  down step itself and is deleted.

| Function | Today | What it asserts | Disposition |
|---|---|---|---|
| `TestMigrate0019_DownDropsTheCounter` | head, down to 18 | the down step drops the table | **Deleted** |
| `TestDowngradeLogSchemaTo_*` (2; the file) | head, down | the downgrade and its refusals | **Deleted** with `downgrade.go` |
| `TestMigrate0018_IdentitySince_UpAndDown` | head, down to 17 | at head a new binding is unstamped; then the down step drops the column and keeps the binding and its reservation | **Head assertions kept**, down part removed; renamed `_Up` |
| `TestMigrate0018_Down_RefusesToEraseAStamp` | head, down refused | the refusal, the version unchanged, not dirty | **Deleted** |
| `TestMigrate0017_AdoptionAccount_UpAndDown` | head, down to 16 | at head a new binding has no account; then the down step | **Head assertions kept**; renamed `_Up` |
| `TestMigrate0017_Down_RefusesToEraseAConfirmation` | head, down refused | the refusal | **Deleted** |
| `TestMigrate0016_Down_DropsTheColumnWhenNothingIsReserved` | head, down to 15 | the down step | **Deleted** (`TestMigrate0016_AdoptionReservation_Up` holds the head assertion already) |
| `TestMigrate0016_Down_RefusesToEraseAReservationOrAnAdoption` | head, down refused | the refusal | **Deleted** |
| `TestMigrate0015_Down_Collapses...` (5 functions) | head, down to 14 (one on to 13) | which name the queue rows collapse to **on the way down** | **Deleted**: each assertion is of the down step's rule. See the question below |
| `TestMigrate0015_UpgradesAFileAlreadyAt14` | head, down to 14, seed, up, then down again | a file at 14 with bindings reaches the head, both rows listed with no legacy name; then the down collapse | **Kept**, its start built upward to 14; the trailing down and its assertion removed |
| `TestMigrate0015_UpgradesAFileAt14ThatAlreadyHasTheColumn` | head, down to 14, add the column, seed, advance the sequence, up | both bindings kept, the unique constraint holds, the id sequence continues from 9 | **Kept whole**, its start built upward to 14 |
| `TestMigrate0013_..._DownDiscardsOnlyThem` | head, down to 12 | at head the two archive kinds insert as notifications and are refused as alarms; then the down step discards them | **Head assertions kept**, down part removed; renamed |
| `TestMigrate0012_DownDropsIdentityKeepsLogbooksAndQsos` | head, down to 11, up to 12 | the down step; then "up again is clean" with a logbook and a QSO in place | **Rebuilt as an upward test:** a file built to 11 holding a logbook and a QSO goes up; both rows kept, the logbook's uuid NULL, the name unchanged |
| `TestMigrate0011_LeavesExistingFailedRowsUnclassified` | head, down to 10, seed, up to 11 | a failed row from before 0011 stays unclassified | **Kept whole**, start built upward to 10 |
| `TestMigrate0011_DownDropsFailureClassWithoutLosingQueueRows` | head, down to 10 | the down step | **Deleted** |
| `TestMigrate0010_BackfillsOnlyRecoverableSuccessOrder` | head, down to 9, seed, up to 10 | the backfill's three cases | **Kept whole**, start built upward to 9 |
| `TestMigrate0010_DownDropsGenerationWithoutLosingQueueRows` | head, down to 9 | the down step | **Deleted** |
| `TestMigrate0009_DownKeepsNotificationRowsAndDiscardsAlarmRows` | head, seed, down to 8, up to 12 | the down step; then, upward, the notification row survives with its detail and an alarm row inserts | **Rebuilt upward:** a file built to 8 holding a notification row (an alarm refused there, as the starting state) goes up; the row and its detail survive, an alarm inserts |
| `TestMigrate0009_RebuildKeepsTheIdHighWaterMark` | head, insert and delete, down to 8, up to 12 | the id after the rebuild is 2, not 1 | **Kept**, rebuilt upward: built to 8, a row inserted and deleted there, up, the next id is 2 |
| `TestMigrate0008_DownDropsTableUpRestoresIt` | head, down to 7, up to 12 | the table is there, gone, there again | **Rebuilt upward:** built to 7, the table absent; up, present |
| `TestMigrate0007_ExistingRowsBecomeLegacyAndSurviveRetry` | head, down to 6, seed, up to 7 | origin becomes `legacy` and survives retry and orphan reset | **Kept whole**, start built upward to 6 |
| `TestMigrate0007_PartialIndexesSurviveBothDirections` | head, down to 6, up to 12 | both partial indexes with their predicates at each point, the head included | **Kept upward:** at 6 (the starting state), straight after 0007, and at the head (codex-182: the head check is kept explicitly) |
| `TestMigrate0007_ForeignKeysIntactBothDirections` | head with a row, down to 6, up to 12 | no foreign-key violation at each point, the head included | **Kept upward:** built to 6, a queue row seeded in that version's shape; checked there, straight after 0007, and at the head |
| `TestMigrate0007_DownPreservesEveryPreExistingColumn` | head with every column set, down to 6 | the down rebuild keeps every column | **Replaced in the same change by its upward form** (codex-182): two rows at 6, every column of the first set to a value of its own, every nullable column of the second NULL; up through 0007; every value read back, timestamps included, and `origin` is `legacy` |
| `TestMigrate0006_DownRoundTrips` | head, down to 5, up to 6 | the narrow limit is back at 5; the wide value stores at 6 | **Rebuilt upward:** built to 5, the wide value refused (the starting state); up to 6, it stores |
| `TestMigrate_DownRestoresRSTLengthConstraint` | head with rows, down to 1 | the down step keeps uploads and history and restores the limit | **Deleted** (`TestMigrate_RelaxesRSTLengthFromVersion1` is the upward test and stays) |
| `TestMigrate0003Reference_DownRoundTrips` | head with a row, one step down, one up | the narrow limit at 2; the row survives both ways; the wide value caches at 3 | **Rebuilt upward:** the reference set built to 2, a row seeded, the wide value refused; one step up, the row survives, the wide value caches |
| `TestPeekDestinationBindings_ReadsLogbooksBelowTheBindingTable` | a head file with identity and two logbooks, down to 13, 12, 11 | what the peek reads from a file at each of those versions | **Kept whole**, each file built upward to its version and seeded in that version's shape (logbook uuids and the identity row at 12 and 13; neither at 11). Not a migration test: a reader's test whose fixture went down |
| `TestRestoreIdentity_RI13_...` (`cmd/smd`) | a head bed, down to 13 and 12 | a dry-run restore at an older schema resolves, counts, asks the right requests, changes no file and leaves the version | **Kept whole**, files built upward with that version's fixtures and the same identities; plus the shape assertion codex-180 asked for (no binding table) |
| `TestDBDowngrade_*` (3; the file, `cmd/smd`) | the command | the command | **Deleted** with it |
| `TestEntryPoint_EP1_...` (`cmd/smd`) | routes | `db-downgrade` routed to its own flags | its `db-downgrade` row **removed** |
| `TestEntryPoint_EP1b_...` (`cmd/smd`) | removed names | `config-downgrade` refused, no daemon | **gains** `db-downgrade`, with and without flags |

- **Counted, as built:** 15 functions deleted outright (the five collapse tests, the two
  of `downgrade_test.go`, and eight single ones), 3 more with the command; 3 keep their
  head assertions and lose a down part; 16 are kept, rebuilt upward or replaced by their
  upward form; 2 entry-point tests change a row. 39 in all.
- **What changes for the better.** Sixteen tests start from a file really built at the
  older version, where before they started from a head file brought down. Nine of them
  (0006, 0008, 0009 twice, 0012, reference 0003, and 0007's indexes, foreign keys and
  columns) become plain upward tests of their step.
- **The shape comparison above is supporting evidence only** (codex-182): folding case
  and quoting could hide a difference inside a string literal, and it says nothing of
  rows. What counts is that each fixture is really built to its version and asserts its
  own older shape before it is used.
- **What is lost, by assertion.** Only assertions about a down step or a downgrade
  refusal. No assertion about an up step, a head-state constraint, a row surviving the way
  up, an index, a foreign key or a sequence is removed.
- **Two questions inside the table, for Codex.**
  1. **0015's five collapse tests.** codex-180 asked to keep "the legacy-name behavior".
     I find one upward legacy-name assertion, and keep it: after the upgrade from 14, the
     listed bindings have no legacy name. The collapse to a recorded legacy name happens
     only in the down file. If Codex meant something of those five that I am reading as
     downward, name it.
  2. **0007's every-column test.** Its assertions are of the down rebuild. 0007's up step
     is also a rebuild with an explicit column list, and no test today checks that it
     keeps every column: the existing upward test reads `origin` only. I propose to turn
     the test round instead of deleting it: a file built to 6 with every pre-0007 column
     set, up, every column read back. That adds coverage the removal does not owe, on the
     only direction left; the station's July files at 5 and 6 would cross it on a restore.
     Say if that belongs in this commit or not at all.
- **Not removed, and noted:** the `legacy_name` column and its writes stay. With the way
  down gone nothing reads it but the listing. Removing it would be a new migration and is
  not part of this.

**New tests, written first and seen to fail on their own assertion.**
- `smd db-downgrade`, with and without flags, is refused naming the command as removed, and
  no daemon starts (the shape of the `config-downgrade` test). Fails today: the command
  runs.
- The embedded migration sets hold no file that is not an up file. Fails today: 22 down
  files. It is the assertion for the deletion, and it keeps a later migration from bringing
  one back by habit.
- A log file built at 5, at 14 and at 18 comes up to the head with rows kept. Passes today;
  it is a characterization that must still pass afterwards, not a red test.

**Reversion proofs,** in a temporary copy: removing the reserved name from the table makes
the first test fail on the daemon marker, which is the confusable outcome itself (restoring
the old entry alone would not compile, the function being gone); restoring one down file
makes the second fail.

**Gates.** `gofmt -l cmd internal`, `go vet ./...`, `golangci-lint`, the maintainability
metrics, `go test ./... -count=1`, the docs and context checks. One commit, tests and
deletion together.

**Not part of this.** Folding the up files (ruled: not folded). Any new migration. The
station: nothing is run there; the installed build changes only at the operator's next
deploy, and its files are not touched by a build that has no way down.

**Asked of Codex, and answered (codex-180).** (1) The inventory: two helpers were missing,
now above. (2) The sorting: by assertion, now above. (3) The 0018 comment: left alone.

**Built (2026-11-19; uncommitted; on the operator's "build the removal", after Codex's
word on the table, codex-182).** Codex's two answers were taken: the five 0015 collapse
tests are deleted, and 0007's every-column test is replaced by its upward form in this
change.

- **Tests first, each seen to fail on its own assertion before the deletion:**
  - `TestEntryPoint_EP1b_...` with two `db-downgrade` rows: failed with the command's own
    error ("reading config file ... no such file") where it wants "removed". Its rows name a
    config file in a temporary directory, so that neither the old command nor a daemon
    could have reached a real installation while the test was red.
  - `TestMigrationSets_HoldOnlyUpFiles`: failed naming all 22 down files.
  - `TestMigrate_OlderFilesComeUpToTheHead` (files at 5, 14 and 18, a logbook and a QSO in
    each): passed before and passes after, as a characterization should.
- **The helpers refuse to go down** (`migrateToVersion`, `applyMigrationSteps`,
  `applyReferenceMigrationSteps`); `riMigrateTo` is replaced by a builder that makes the
  bed's Home upward at 12 or 13 with the same logbooks and identity. With the guards in and
  the down files still present, exactly the sixteen helper-driven tests of the table
  stopped at the guard, and no other: the guard finds what the table lists.
- **The test changes as tabled,** then the deletion: `cmd/smd/db_downgrade.go` and its
  test, `internal/database/sqlite/downgrade.go` and its test, the 22 down files.
  `db-downgrade` is refused by name. The 22 up files are untouched, byte for byte.
- **Every rebuilt fixture passed against the unchanged migration set first** (down files
  still present), so the upward tests are not passing because of the deletion.
- **Reversion proofs, in a temporary copy, each failing for its own reason:**
  - the reserved name taken out of the table: EP1b fails with "exit 0, stdout
    `ENTRY-HELPER: the daemon would start`": the confusable outcome itself;
  - one down file put back: the up-only test fails naming it;
  - 0007's rebuild made to drop `upstream_id`: the every-column test fails on row 41's
    `upstream`; made to copy `created_at` into `modified_at`: it fails on both rows.
- **Gates, by exit code:** `gofmt -l cmd internal` (nothing listed), `go vet ./...`,
  `golangci-lint` (0 issues), the maintainability metrics (0 regressions),
  `go test ./... -count=1` (66 packages ok), the docs and context checks.
- **Documents:** `docs/install.md` (both commands removed; a database goes up only),
  `docs/current.md` (Next), a dated pointer in W-0021.
- **Not shown:** the station. Nothing was run there. Its files are untouched until the
  operator next deploys, and a build with no way down changes none of them. RI13's
  shape assertion was proved to bite by Codex's overlay run (codex-180), not rerun here.

### A3: acceptance criteria — closing the app (2026-11-23; revised after Codex's reviews, codex-232, codex-234, codex-235 and codex-236; Codex's review closed with no finding open, codex-237; its six questions ruled by the operator the same day; nothing built)

On the operator's direction ("commit and push, then A3"), after W-0029 parts 1 to 4 went
to the station. Read against ADR 0102 ruling 6 and its Consequences; ADR 0100 §3
(Closing); the C4 brief's point 8 (a normal close) and its question 11; W-0029's brief,
proposals C and D, and what it agreed for A3 with Codex (codex-178, codex-179,
codex-187); and the code as it stands: `desktop/src/main.mjs`, `launch.mjs`,
`admission.mjs`, `window.mjs`, `internal/api/operating_session.go` (the exit report),
`internal/ft8/operating.go`, and the page's `lib/config/unsaved.ts`,
`lib/operate/qso.svelte.ts` and `lib/operate/ft8.svelte.ts`. "Proposed" marks what those
leave open. The questions at the end are the operator's.

**Ruled (operator, 2026-11-23): "go with recommendations, commit and push".** Each of
the six questions at the end is ruled as it was recommended there. Where the criteria
say "proposed", or "if question 6 is answered (a)", read them as ruled so.
1. **The daemon does not confirm the close:** the box of E, said once.
2. **The wait:** 5 seconds for each request (the read of FT8's state, the disarm, the
   read of the session's state, the exit report), and no other timer.
3. **The words:** as proposed in question 3.
4. **After an unconfirmed close:** Cancel returns to the window only when no exit
   report was sent. Once one was sent there is no way back: the window is closed before
   the box, which offers Try again and Close anyway, and the app stays running for it.
5. **Closing while Settings is saving:** the question names it, with Close and Cancel.
   The close is not refused.
6. **FT8's state:** (a), the daemon's own read (section I), the one change A3 makes to
   the daemon. When no valid answer can be had, the question carries the line "FT8's
   state could not be read. If it is running, it will be stopped." The snapshot's race
   (I5) is accepted as described. The page reports nothing about FT8.

**What A3 is.** Closing the app becomes a confirmed exit. Today the window simply goes:
the daemon notices the app's FT8 stream has gone and disarms after its grace
(`unattended`), and nothing tells the daemon the sitting has ended. With A3 the app asks
first when something would stop, stops FT8 itself, tells the daemon it is exiting, and
waits for the daemon's acknowledgement before it goes.

**What is already there.**
- The daemon's exit report (`POST /v1/operating-session/exit`, W-0029 part 2), with every
  answer A3 needs. Nothing sends it yet. For the current lifetime it also stops FT8,
  seals contact entry and waits for completions already earned, under the daemon's one
  order, before it closes the session (`exitCurrent`).
- The disarm (`POST /v1/ft8/tx/arm`, `armed: false`), which the daemon records as the
  operator's own.
- The page's guard for unsaved Settings, which vetoes an unload.
- The shell's admission, which today answers a daemon restart by admitting again.

**A defect of today that A3 removes (an inference from Electron's documentation, not
seen at the station).** The shell does not handle a vetoed unload. Electron's own
declarations say "In Electron, returning any value other than `undefined` would cancel
the close" (`desktop/node_modules/electron/electron.d.ts`, the `close` event), and no
question is shown for it. So closing the window with unsaved Settings should today do
nothing, without a word. H below replaces that with the one question.

**What the operator sees.**
- **Closing with nothing in progress:** the window closes, as now, a moment later: the
  app has told the daemon and had its answer.
- **Closing with something that would stop or be lost:** one question, naming it.
  Cancel changes nothing. Close stops FT8, then closes.
- **If the daemon does not confirm the close:** the app says so and does not vanish
  silently (question 1).
- **At the station today:** every session is empty until part 5, so a confirmed and an
  unconfirmed close leave the same thing behind. What can be seen is the question, and
  that FT8 is disarmed at once by the operator's own request, not after the grace.

**Nearest confusable outcome.** The window closes at once and looks the same, while no
exit was reported: once contacts join sessions (part 5), the next start would then ask
Continue or New after every ordinary close. The others: a close that asks when nothing
would stop; a Cancel that has already disarmed; a second question from the page after
the shell's; a window left open, unable to close, after its lifetime was retired; a
window that knows it has been replaced stopping the other app's FT8 (B2).

**A. When the question is asked, and what it may say** (ADR 0102 ruling 6).
1. **Asked** when, and only when, there is a reason (A5): one the page reports, or
   FT8's state from whichever source question 6 selects, its "not known" rule
   included. The reasons are gathered from both and shown in the one question. **Not
   asked** otherwise. A close with nothing in progress is still a confirmed exit: B
   and C run without a question.
2. **It names each thing once,** and nothing that is not so. No instruction that cannot
   be acted on.
3. **Two answers: Close and Cancel.** Cancel changes nothing at all: nothing is
   disarmed, no exit is reported, the admission carries on, drafts and unsaved edits
   are as they were, and the window is as it was.
4. **The page's word decides only whether to ask and which lines are shown; what a
   close does never depends on it** (proposed; Codex agrees with the boundary,
   codex-232). The page publishes its reasons as read-only attributes of their own on
   its root element, separate from the binding marker (whose contract is pinned
   elsewhere). The shell reads them with one fixed expression when a close is asked
   for, and accepts only the values it knows; anything else is "not reported". The
   renderer stays sandboxed with no preload and no new channel. A page that cannot be
   read (the waiting page, a renderer that has gone) reports none of the page's own
   reasons; it never means a step of the close is skipped. **It does not silence a
   reason that is not the page's:** under question 6 (a), an app not known to be
   retired, whose page cannot be read and whose daemon answers "armed", still asks
   the FT8 question (codex-236).
5. **The reasons, and where each comes from.**
   - **FT8,** from the daemon's own word, never from the view's flags (the view clears
     those when it is left: `stopFt8`) and never from the absence of a word. Three
     values: *active* (TX armed or a run under way), *idle* (the daemon said neither),
     *not known*. Where the word comes from is question 6. **A window that knows it
     has been retired shows no FT8 line and reads no FT8 state:** it sends no disarm
     (B2), so it must not say this close stops FT8, which may be another app's. If the
     word is the daemon's own read (question 6, a), it is the snapshot of I below, and
     a read that cannot be established is "not known", never "idle". If it is the page's FT8
     stream, then "idle" and "active" hold only once **this connection** has delivered
     the daemon's TX state and its run state, and everything else is "not known"
     (codex-234): no stream opened since the page loaded, which includes a page reload
     inside one lifetime, when an arm from before can still stand for the grace; a
     stream connected whose state has not yet arrived (`onOpen` marks it connected
     before any state frame); a replay that brought only one of the two; a reconnect,
     to this process or another; the view left; the connection lost. **The daemon
     replays those states only if it has them** (`hub.subscribe`: "if present"), so a
     stream to a daemon that has had nothing to say stays "not known".
   - **A contact being entered:** `draftInProgress()`, which does not count a report
     left at its default as work.
   - **A Log whose outcome is not known:** `logOutcomeUnknown()` (one in flight, or one
     reported uncertain). Its line never says the contact is lost or unlogged: it may
     already be stored.
   - **Unsaved Settings:** `unsavedSections()`, named by section.
   - **Settings being saved:** `savingSections()`. A write on its way cannot be
     recalled, so the question cannot promise it is discarded or stored (question 5).
     **A section is named once.** One that is being saved is usually still unsaved too,
     until its answer comes (`general.svelte.ts`, `save`), and the form stays live
     meanwhile. It is named as being saved, never also as "will be lost", and its line
     covers what was changed after the save began (codex-234).
6. **One close at a time.** A second close asked for while the question shows, or while
   the close is under way, starts nothing more. Every ordinary way of closing the app
   that the shell offers runs this one flow.

**B. Stopping FT8, by the app itself.**
1. After Close (or with no question), the shell sends the disarm, as the operator's own
   request, and waits for the daemon's answer before anything else. The daemon's record
   of why TX stopped says the operator, not `unattended`.
2. **A window that knows it has been retired sends no disarm.** A disarm needs no
   current lifetime (the operator's ruling on question 11: "11 - yes, keep it"), so one
   sent by a window another start has replaced stops the other app's FT8. The shell
   learns of its retirement only from an admission's answer, so this is what it can
   promise: none is sent once the retirement is known. **A disarm already on its way
   when the replacement happens still stops the other app.** That is the cost accepted
   with question 11 (`internal/ft8/operating.go`), kept as it is; closing it would need
   a change to the daemon's order, which A3 does not make.
3. **Nothing armed:** the disarm changes nothing and the close goes on. **FT8 not
   served:** the daemon has no such route then (it is registered only when FT8 is
   enabled) and answers `404`; the shell takes exactly that answer as "nothing to stop"
   and goes on. Any other refusal is B4.
4. **The disarm cannot be sent or is refused:** the close does not go on to report an
   exit as if FT8 had stopped. It is the unconfirmed close of E, in its first kind, and
   the app says that stopping FT8 was not confirmed. (The daemon's own stop when the
   app's stream goes is still there, after its grace.)
5. **From the confirmation the page starts nothing new; what it had already sent is
   the daemon's to decide** (two different things: codex-234).
   - **Nothing new.** The window is hidden, and the shell refuses every request from
     the page that is not a read, until the window goes or a Cancel of the first kind
     (E1) gives it back. It is decided per request, so it covers what the page had
     deferred and its own continuations, not only the operator's hands. The shell's own
     requests (the disarm, the reads, the exit) are not the page's and still pass.
     (Proposed mechanism: added to the shell's existing request filter in
     `hardening.mjs`, whose restriction to the pinned origin stays as it is.) Refusing
     a new request is no proof that one sent earlier was cancelled.
   - **Already sent, and the exit is acknowledged:** the daemon's exit stops FT8 and
     seals contact entry before the session closes, and waits for completions already
     earned, so those are logged. Under its one order.
   - **Already sent, and the exit is refused or fails:** the daemon stops nothing for
     good. A failed or stale exit gives the lifetime its authority back (`exitCurrent`,
     `restore`), and the terminal refusal retires nothing (`exitNotCurrent`). So a
     request the page sent before the confirmation may still take effect, and the app
     does not claim otherwise. What ends it is on the app's own side: in the second
     kind the window is closed before the box is shown (E2), and Try again opens no
     FT8 stream. Once the daemon has removed this lifetime's last FT8 subscription, no
     arm can be *made* for it (`ft8_not_present`). An arm made while the stream was
     still open ends as `unattended` when the daemon's grace runs out; a repeat of an
     arm that already stands is accepted, changes nothing and does not extend the
     grace (`TestPresence_ARepeatedArmChangesNothingAndDoesNotExtendTheGrace`). **The
     grace starts when the daemon sees the subscription go, not when the window
     closes,** so no time is promised from the window's closing (codex-235).

**C. The exit report.**
1. After the disarm is answered, the shell reads the session's state, then reports the
   exit naming its lifetime, the generation it read and the archive it read. The
   daemon's answer is the acknowledgement the close waits for.
2. **Acknowledged** (`200`, whether or not `already_recorded`): the app goes.
3. **Stale** (`operating_session_stale`): the exit, and only the exit, is sent again at
   the generation returned, without admitting again (agreed for A3, codex-187).
4. **Not current and still holding the session** (`operating_session_exit_not_current`):
   terminal. The close was not confirmed. It is not repeated.
5. **No answer, or `503`:** the close was not confirmed. No acknowledgement is made up.
   **A request the shell gave up on is not one the daemon gave up on:** the daemon
   decides an exit whatever becomes of the request (`exitOperatingSession` does not
   follow the request's context), so an exit sent and not answered may be stored later.
6. **A lost answer is safe to repeat:** an exit already recorded is acknowledged.
7. **The limit of question 2 is per request, not for the whole close.** Admission work
   settling (D3), a stale exit sent again, and the daemon's wait for completions all
   add to it.

**D. A closing app never admits again** (W-0029's brief, proposal C; codex-178,
codex-179). A barrier, from the moment the close is confirmed.
1. No new admission is launched: a daemon restart seen while closing starts none.
2. The admission's loop and the markers it kept meanwhile start nothing.
3. The exit report is sent only after admission work already issued has settled at the
   shell. **That is not proof the daemon will not still apply it** (an admission the
   shell timed out on may land later; codex-232), so the safety is the daemon's order,
   and each late arrival has a named end:
   - the admission lands before the exit: the exit closes what it opened;
   - it lands after an acknowledged exit: refused as retired;
   - it lands after the terminal answer of C4 (a restarted daemon): **the daemon admits
     the lifetime and gives it the authority to operate** (the refusal retired
     nothing; the admission publishes the lifetime to its scopes), and it holds its
     session open. The barrier cannot prevent that, and ignoring the answer does not
     undo it (codex-234). What the app guarantees is its own side: the window has
     gone (E2), so it sends nothing new and holds no FT8 stream; B5 says what becomes
     of a request sent earlier. The session is an unconfirmed exit and the next start
     classifies it.
   In none of them is a session closed that the exit did not close, and in none does
   this app send a new operating request.
4. An old admission's answer arriving after closing began restores nothing and starts
   nothing.
5. **A close that is cancelled at the question** (before it is confirmed) leaves
   ordinary admission working: a daemon restart afterwards is answered as today.
6. **The archive switched under a closing app** (the A to B case): the exit is recorded
   in the archive now served, the app is never admitted there, and holds no session
   there.

**E. The close that was not confirmed** (questions 1 and 4; revised after codex-232
and codex-234). The shell says so once, in a box of its own. What it offers depends on
whether an exit report has been sent, because an exit that was sent may have been
stored.
1. **First kind: nothing of the exit was sent** (the disarm, or the read of the state,
   failed). Offered: Try again, Close anyway, Cancel.
   - **Cancel** shows the window again, lifts the barrier and lets ordinary admission
     resume, so the app can operate, and the page's requests are let through again.
     FT8 is as the daemon has it: a disarm that was answered stays; one that was sent
     and not answered may still arrive and disarm later. That is a stop, and it is
     said in the box.
2. **Second kind: an exit report was sent and not acknowledged.** There is no way back
   to the window (proposed; question 4). **The window is closed before the box is
   shown,** and the box offers Try again and Close anyway.
   - **Why no way back.** The exit may already be stored, or may be stored later, and
     then this lifetime is retired for good; or it was refused, and the daemon still
     counts the lifetime able to operate (B5, D3). A window kept open would have to be
     held non-operating by the app alone, for as long as it stayed: its FT8 stream,
     its Log, its shortcuts and whatever it had deferred. Closing it is the one state
     that needs no such promise. No new lifetime is made to get round a retirement.
   - **What is lost is what the operator was already asked about:** the question named
     the entry and the unsaved edits, and Close was the answer.
   - The terminal answer of C4 is of this kind, without Try again: trying again cannot
     change it.
   - **The app outlives its window for this box** (codex-235). Today the app quits
     when its last window closes (`window-all-closed` in `desktop/src/main.mjs`). In
     this state it does not: the main process and its authenticated session stay for
     the box and for Try again, and the app quits on an acknowledgement or on the
     final Close anyway. The box has no parent window. A second start of the app, or
     another request to quit, while the box is up neither skips the box, nor shows a
     second one, nor makes a new window that could operate.
3. **Once an exit report has been sent, the close is of the second kind until it
   ends,** whatever a later attempt fails at. A Try again that fails at its disarm or
   its read does not make it the first kind again: the earlier exit is still out
   there (codex-234).
4. **Try again** runs B and C again from the start, under the same barrier.
5. **Close anyway** ends the app and claims nothing. The daemon's own record is left
   as it is: the session may already be closed, or may still be open, and the next
   start treats whichever it finds as ADR 0100 says.
6. **The box says what is known.** That the close could not be *confirmed*, never that
   the daemon was not told; and when it was the disarm that failed, that stopping FT8
   was not confirmed.

**F. What a close is not.**
1. **The operating system ending the app** (logout, shutdown, a kill): no question can
   be asked and none is. It is the failure case of the C4 brief: the daemon stops FT8
   after its grace, and the exit is unconfirmed.
2. **A second start of the app** while one is open: unchanged (the first is shown).
3. **The window's own reload:** there is no user reload (ADR 0102 ruling 6). No key and
   no menu reloads the page; pinned by a check. A reload the page itself asks for (the
   archive gate's, a changed daemon build's) is not a close and runs none of this, and
   the page's own guards on navigation and reload are left as they are.

**G. What A3 must not do, and the reference.** No change to the daemon: every route it
uses exists. (One exception if question 6 is answered that way: the read of FT8's state in I,
which changes nothing.) No new TX command. No session control. No window set (A2). No change to
what a page reload does (C5).
`docs/v2-design/api-endpoints.md` gains, in the same change: that the app now sends the
exit report and the disarm at a close, and repeats a stale exit. `desktop/README.md`, if
it describes closing.

**H. The window goes** (codex-232, finding 4).
1. **Exactly one question about loss.** After the shell's question is answered Close,
   the page's veto of the unload asks nothing and stops nothing.
2. **After an acknowledgement, at Close anyway, and before the box of the second kind,
   the window closes.** A page that vetoes the unload cannot leave a window open whose
   lifetime has been retired.
3. **Only for that close.** The veto is overridden for the approved final close and at
   no other time: Cancel at the question leaves drafts and unsaved edits exactly as
   they were, with the page's guard in force.
4. Shown in the real app against fixtures, with unsaved Settings: Cancel, Close, and
   Close anyway.

**I. The daemon's read of FT8's state** (only if question 6 is answered (a); reviewed
as a design by Codex, codex-235).
1. **Passive.** It subscribes to nothing, claims no profile, starts no capture, takes
   no device, publishes no status, retires no session, waits for no completion and
   calls no operating command. It needs no current lifetime and takes no admission
   lock: it authorises nothing. The credential check of every route stays.
2. **Live state, read together.** From the FT8 service's own state, not from the
   stream's replay cache, under the order the package already uses (`seqGate`, then
   `txMu`, then the sequencer's lock: `SealTxAdmission`). The locks are released
   before the answer is written. None is held across the question, a wait or the
   disarm.
3. **What counts as something that would stop:** TX armed with nothing running; an
   exchange under way; a Call-CQ run; a run waiting between contacts (auto-work
   armed); a transmission in flight. Idle is none of those.
4. **FT8 not served is an answer of its own,** given by a route that exists whether or
   not FT8 is enabled. A missing route, a malformed answer, a failure or a wait that
   runs out is "not known", never idle and never "not served".
5. **It is one observation, not a guarantee.** An arm already sent before the close
   can land after an idle answer and be stopped by the disarm without the question; a
   run can end after a busy answer and before the question is read. Both are stated,
   and the first is shown by a test that holds an arm until after the snapshot. A
   stronger promise would need the daemon to hold FT8 while the operator is asked,
   which is not proposed.
6. The read lives in `internal/ft8`; the API calls it through the service it already
   holds. `docs/v2-design/api-endpoints.md` gains the route in the same change. Tests:
   each state of I3, idle, not served, and that the read changes nothing (no
   subscriber, no capture, no publication).

**Evidence.** Tests first.
- **The close flow as a module of the shell,** with the session, the dialog and the
  daemon given by the test: every answer of C; the barrier's points of D, each placed by
  holding one request and releasing it, never by sleeping, including a request the
  daemon applies after the shell has given up on it (an admission, and an exit); both
  kinds of E and each choice; the window closed before the box of the second kind,
  with the app still running, and a second start and a second quit while it is up; a
  first exit that times out, then a Try again that fails at its disarm, still of the
  second kind (E3); the known-retired window sending no disarm, reading no FT8 state
  and showing no FT8 line; and, under question 6 (a), the reasons combined from both
  sources, including a page that cannot be read beside a daemon that answers "armed".
- **In the daemon's tests,** each where not already pinned: an arm or a submit between
  a disarm and an acknowledged exit (B5); the terminal refusal, then an admission that
  was held, then an arm, with the lifetime's FT8 stream open and with it removed (D3,
  B5: made, then ended by the grace, driven from the removal of the subscription with
  the test's clock; refused as not present). The grace and the repeated arm are pinned
  today (`internal/ft8/presence_service_test.go`). The accepted cost of B2 is pinned today
  (`TestOperating_DisarmIsAlwaysAccepted`, `internal/ft8/operating_test.go`: a replaced
  lifetime's disarm ends the current one's run) and is left as it is.
- **The page's reasons:** the page's own tests, each reason alone and together, and
  none; a section both saving and unsaved named once; and, if FT8's word comes from the
  page (question 6), each "not known" case of A5.
- **Through the app's real quit wiring,** not the automated path, which is exempt from
  quitting when the last window closes and so would hide this: the second kind's box
  with no window, Try again, and the quit that follows.
- **In the real app against fixtures** (the A1 harness): the question's two answers, the
  order of the requests a close sends, a write the page attempts after the confirmation
  refused and let through again after a Cancel of the first kind, H, and that no key
  reloads.
- **In the real app against a real `smd`:** a close with nothing in progress leaves the
  exit recorded in the archive file and the session closed; a second start then meets no
  open session. With a contact put into the session by the scenario (as the recovery
  scenario does): a confirmed close, then a start that is not asked.
- Named tests while building, reversion proofs once, the whole local gate once before the
  push (ruled 2026-11-22). No transmission, keyed test or rig command: FT8 is off in the
  scenarios' daemons, so the real-`smd` scenario also shows B3's `404`; a disarm that
  finds something armed is shown against the fixture harness and in the module's tests,
  never against a rig.

**Proposed slices** (revised: codex-232, finding 6; if question 6 is answered (a),
the daemon's read route is built first, on its own). The order of building is the close
with no question first (B to E), then the question, the page's reasons, the final
close of the window and the reload check (A, H, F3). **The new close is not wired to
the window until both are built:** wired after the first alone, a close would stop a
run or drop a draft with no question, against ADR 0102 ruling 6. Until then only the
scenarios reach it. Once both are in, every ordinary close the daemon acknowledges is a
confirmed exit; E remains, by design, an unconfirmed one.

**Questions for the operator** (all six ruled as recommended, 2026-11-23; see "Ruled"
at the head of this section).
1. **When the daemon does not confirm the close, what should the app do?** I recommend
   the box of E: say so once, with the choices of E1 and E2. The alternatives: close
   anyway without saying (the next start then asks, once contacts join sessions); or
   refuse to close until the daemon answers, which leaves no way to close the app while
   the daemon is down.
2. **How long does each request wait for the daemon before the close counts as not
   confirmed?** I recommend the limit the shell already gives each admission request,
   5 seconds, for the disarm, the read and the exit report each (and the read of FT8's
   state, if question 6 is answered (a)), and no other timer. It is per request, not a
   bound on the whole close (C7). It is a threshold, so it is
   yours.
3. **The words.** Proposed, for you to change; each must stay true to E6 and A5.
   - The question. Title: "Close Station Manager?" Lines, each only when true: "FT8 will
     be stopped and transmit disarmed." / "The contact you are entering will be
     discarded." / "A contact was sent to be logged and its outcome is not known. It
     may already be logged." / "Unsaved changes in Settings (*sections*) will be lost."
     Buttons: "Close" and "Cancel".
   - The unconfirmed close. Title: "The daemon did not confirm the close". Text, first
     kind, when the disarm failed: "Station Manager could not confirm that FT8 was
     stopped. The daemon stops it by itself shortly after this window goes." Otherwise:
     "Station Manager could not confirm with the daemon that it is closing, so the
     daemon may treat this sitting as interrupted." Buttons, first kind: "Try again",
     "Close anyway", "Cancel". Second kind: "Try again", "Close anyway"; after the
     terminal answer, "Close" alone.
   - A section being saved (question 5): "Settings (*sections*) is still being saved.
     Closing does not wait: the save may not complete, and anything changed since it
     began will be lost."
4. **After an unconfirmed close, may the app go back?** Only in the first kind, where
   no exit was sent: Cancel returns to the window and the app operates again (Codex's
   recommendation, codex-232, and mine). **In the second kind I recommend no way back:
   the window closes and the box offers Try again and Close anyway (E2).** The other
   choice keeps the window open for reading and copying what was typed. Codex has shown
   what that costs (codex-234): the app alone would have to hold that window
   non-operating for as long as it stayed, FT8 stream, Log and shortcuts included,
   because the daemon may still count its lifetime able to operate. What it would save
   is what you had already agreed to lose at the question.
5. **Closing while Settings is saving.** I recommend the question names it truthfully
   (the words in question 3) with Close and Cancel, so you can cancel and wait. The
   other choice follows the page's rule for leaving Settings (ruled 2026-08-04: leaving
   is refused while a section saves): the close is refused with a line saying so. I do
   not recommend that here: a page that wrongly reports a save under way could then
   keep the app from closing. Codex calls the exception coherent and does not choose.
6. **Where FT8's state comes from, and what "not known" does.** The page knows it only
   from its FT8 stream, and only once the daemon has said it on that connection. With
   FT8 enabled and the FT8 view not open, the ordinary case, the page does not know.
   - **(a) The shell asks the daemon** when a close is asked for: a small read-only
     route, new, that says whether TX is armed or a run is under way (I). The answer
     is the daemon's own, at that moment. Cost: one read route and one read of the FT8
     service's state, in the daemon, which G otherwise leaves alone. **I recommend
     this, and Codex supports it in principle** (codex-235). Two limits go with it.
     *It is a snapshot* (I5): a run can start or end between the answer and the
     disarm. *The read can fail* while the disarm that follows succeeds, so (a) needs
     its own rule for "not known": **I recommend, as Codex does, the qualified line**
     ("FT8's state could not be read. If it is running, it will be stopped."), which
     appears whenever a valid answer cannot be established (I4): no answer, a missing
     route or a malformed one. The other rules: stop there
     and show the box of E's first kind; or go on without an FT8 line, accepting what
     (c) accepts.
   - **(b) The page's word, and "not known" asks:** "FT8 may still be running. It will
     be stopped." Codex recommends this of the page-only choices. Its cost: at a
     station with FT8 enabled it would ask at most closes when nothing is running.
   - **(c) The page's word, and "not known" does not ask.** Its cost, to be accepted
     in so many words: a run or an arm that still stands (inside the daemon's grace
     after the view was left or the page reloaded, or held by a stream the page has
     not yet heard from) is stopped by the close without the question. The disarm is
     sent in every case.

**For the operator's eye, not a question.** A close that asks nothing will take a moment
longer than today, the time of three requests to the daemon on the same machine, or
four if question 6 is answered (a). If the
daemon is down, that moment is the limit of question 2 before the box of E appears.

### A3, first part — the daemon's read of FT8's state (built 2026-11-23; reviewed by Codex with no finding open, codex-238; committed and pushed on the operator's direction, "commit and push")

Built on the operator's direction of A3 ("commit and push, then A3") and the ruling of
question 6 as (a). It is section I of the criteria and nothing else: no part of the
shell is touched, and nothing sends the new request yet.

**What it is.** `GET /v1/ft8/state`. FT8 not served: `{"served": false}`. Served:
`served`, `would_stop`, `tx_armed`, `tx_in_flight`, `run_active`, `auto_work_armed`,
every field present. `would_stop` is the daemon's rule (any of the four), so the shell
will read that one word and decide nothing from the parts.

**Where.**
- `internal/ft8/operatingstate.go`: `Service.OperatingState()`, read under `seqGate`,
  then `txMu`, then the sequencer's lock, from the service's live state; and
  `OperatingState.WouldStop()`. No existing method is changed.
- `internal/api/handler_ft8_state.go`: the handler, the answer's shape and the route's
  registration, beside the other small registrations in `New` and not inside the block
  of routes that exist only when FT8 is enabled.
- `docs/v2-design/api-endpoints.md`: the route.

**Against section I.**
1. *Passive:* `TestOperatingState_ChangesNothing` reads an armed, running service three
   times and requires the arm and its generation, the run, the subscribers, the three
   replay frames, the presence hold, the open streams and the operating lifetime all
   exactly as before. No lifetime is asked for
   (`TestFt8State_NeedsNoCurrentLifetimeButStillTheCredential`: another lifetime and no
   lifetime are served; a wrong credential is not).
2. *Live state, read together:* `TestOperatingState_IsReadUnderTheEstablishedOrder`
   holds `txMu` and requires the read to be found holding `seqGate`, and not to answer
   until `txMu` is released.
3. *What would stop:* armed with nothing running; a manual send waiting for its slot; a
   Call-CQ run; an exchange; a run waiting between contacts (the pick-run fixture of
   ADR 0067, after its seeding contact), alone enough to count; idle is none.
4. *Not served is its own answer:* through the whole handler chain, with no FT8
   service and with one configured off: `200` and only `served: false`.
5. *One observation:* `TestOperatingState_IsOneObservation` (an idle answer, then an
   arm, which stands).
6. *The reference* is updated in the same change.

**Seen to fail first,** against empty stubs: every test but the idle one, each on its
own assertion (the route's tests on `404`).

**Reversion proofs,** each in its own temporary copy, each failing the named test:
- `WouldStop` without the waiting run: the waiting-run test, and the API's rule test.
- the read without `seqGate`: the order test.
- the run read from the replay cache and not the sequencer: the run tests.
- the route registered only when FT8 is enabled: the not-served test (`404`).
- a served answer missing one field: both field tests.

**Gates, by exit code:** `gofmt`; `go vet`; `golangci-lint` on the two packages (0
issues); the maintainability metrics (0 regressions); `go test -race ./internal/ft8`
and `go test ./internal/api`, both in full. The whole local gate (`task ci:local`), run
once after the review closed: exit 0, "All CI gates passed locally."

**Found while building.** The first placement of the route, inside `registerRoutes`,
took that function below the maintainability floor it already sits on (index 19, policy
20: the gate counts comment lines). It is registered from `New` by a helper of its own,
as the session routes are.

**Codex's review (codex-238): no finding open.** It confirmed the lock order adds no
reverse edge, that the four flags cover section I's stop reasons, that the layered
tests are enough for a handler this thin (no production seam is wanted only to arm a
service from the API's tests), and that no further capture assertion is needed. It
reran the FT8 tests with the race detector and the route's tests; the full-package
runs, the red-first order and the reversion proofs remain Claude's.

**Not done here:** the shell's use of the read; the armed states through the HTTP route
(an armed service cannot be made from the API's tests: its keyer is the FT8 package's
own; the states are shown in `internal/ft8`, and the answer's rule on the body the
handler builds). No transmission, keyed test or rig command: fake keyer and player only.

**Committed and pushed: `b86f9ca4`** "smd: the daemon says whether a stop would stop
FT8" (operator, 2026-11-23: "commit and push"), after the whole local gate passed.

**Clean-room review of `b86f9ca4`: one finding, P3, fixed; committed and pushed on the operator's direction ("commit and push").**
- **The finding:** the contract said `auto_work_armed` is a run "waiting between
  contacts, with no exchange under way", while the read returns the run's flag as it
  is, and that flag is set when a contact of the run starts. So during such a contact
  the answer is `run_active: true` and `auto_work_armed: true`. `would_stop` is right
  either way. The finding is correct: the words were wrong, not the value.
- **The fix is to the words.** The field now says what the stream's field of the same
  name says (`QsoStatus.AutoWorkArmed`: "an auto-work-callers run is live"): true
  during a contact of the run as well as between contacts, and "waiting between
  contacts" only beside `run_active: false`. Changed in the struct's comment and in
  the API reference. Making the field mean "waiting" instead was considered and not
  done: one name would then carry two meanings, in the stream and in this read.
- **Pinned:** `TestOperatingState_ALiveRunIsReportedDuringItsContactToo` (a contact
  started in a pick run: both true, and equal to what the stream's last frame says).
  Reversion proof in a temporary copy: with the flag reported only when idle, that
  test fails.
- **Gates:** `gofmt`, `golangci-lint` (0 issues), the maintainability metrics (0
  regressions), `go test -race ./internal/ft8` in full.

### A3, the shell's first slice — the close with no question (built 2026-11-23; Codex's two findings fixed 2026-11-24 and its review closed, codex-241; committed and pushed on the operator's direction, "commit and push"; not wired to the window)

On the operator's direction ("build it"). Sections B, C, D and E of the criteria, with
the app outliving its window. **Nothing an operator can reach has changed:** the window's
own close is not wired to this flow, and stays so until the question that goes before
it is built (the second slice). Only the test scenarios run it.

**What is built.**
- **`desktop/src/closing.mjs` (new): the close.** `closeFlow` runs the order of the
  criteria: the barrier, the page's writes refused, the window hidden, then, once
  admission work already issued has settled, the disarm, the read of the session's
  state, and the exit report at the generation and archive read. Acknowledged: the
  window is closed and the app quits. Otherwise the box, of the first kind (nothing of
  the exit was sent: Try again, Close anyway, Cancel, which gives back the window, the
  page's writes and ordinary admission) or the second (an exit was sent: the window is
  closed before the box; Try again or Close anyway; after the terminal answer, Close
  alone). Once an exit was sent the close stays of the second kind. One close at a
  time. Each request has its own limit of 5 seconds.
- **`desktop/src/admission.mjs`: the barrier.** `beginClosing` launches nothing more (by
  a marker, by the waiting page's loop, or by work that settles), keeps the markers it
  sees, and resolves when work already issued has settled. A retirement learnt
  meanwhile is known to the close and not announced while closing. `endClosing` (a
  first-kind Cancel) asks about what was seen and says a retirement learnt, once.
- **`desktop/src/hardening.mjs`: the page's writes.** The request filter gains a switch.
  While refused, a request on the served origin that is not a read is cancelled, each
  one, unless it carries the mark of the shell's own close requests. The origin rule is
  untouched.
- **`desktop/src/launch.mjs`, `main.mjs`:** each launch builds its close from its own
  session, admission, filter and window, and holds it. The main process has the box
  (no parent window) and the rule that the app does not quit at its last window while a
  close is under way.

**Decided while building, for review. None changes what was ruled; each is the
operator's to overrule.**
1. **A lifetime the daemon was never asked to admit just closes,** with no request and
   no box. The criteria did not have this case. Without it, an app started while the
   daemon is down, and then closed, would wait 5 seconds and show "The daemon did not
   confirm the close": a false alarm. Such a lifetime is unknown to the daemon: it
   holds no session and can have armed nothing, and its disarm could only stop
   somebody else. The test is strict: no admission was ever asked for. (That turns
   true when one is asked for, before anything is sent, so a doubt falls on the side of
   running the whole close: codex-239.) The barrier is entered all the same.
2. **The shell's close requests carry a mark** (a value made in the process, known to
   no page, as the waiting page's probe has), and the filter lets a write through by
   that mark, not by who Electron names as the sender. A worker's request names no
   window and would otherwise pass for the shell's (codex-112).
3. **One sentence added to the first kind's box when the disarm failed,** which E1
   requires and the ruled words did not contain: "If you cancel, the stop already sent
   may still take effect."
4. **Dismissing the box** (Esc, or its close button) is Cancel in the first kind and
   Close anyway in the second, where there is nothing to cancel to.
5. **Any answer to the exit that is not the acknowledgement, the stale answer with a
   generation, or the terminal refusal** (`archive_mismatch`, a `400`, a `200` without
   the acknowledgement's shape) is "not confirmed", second kind. Try again reads the
   state afresh.
6. **The window is closed with `destroy()`,** so the page's veto of an unload has no
   say in a confirmed close (section H). H's own evidence, with unsaved Settings, is
   the second slice's.

**Evidence.**
- **Seen to fail first:** the barrier's, the flow's and the filter's tests, each
  against an empty stub.
- **Unit tests** (`npm run test:unit`, 160 in all, with the four of the findings below):
  `closing.test.mjs` (30 at first: every answer
  of the disarm and of the exit; the order of a close; the wait for admission work;
  the known-retired window, also when the retirement is learnt during that wait; both
  kinds and each choice; the window closed before the second-kind box; the close
  staying second when a retry fails at its disarm and at its read; the terminal answer;
  one close at a time; the box's words), `admission.test.mjs` (8 new),
  `hardening.test.mjs` (5), `trace.test.mjs` (1 new). Requests are held and released by
  the test; the one real wait is a 20 ms limit on a request that never answers.
- **In the real app against a real `smd`** (`npm run check:close`, 12 checks at first,
  13 with the finding below; FT8 off,
  temporary working directory, synthetic data): a disarm with no answer gives the
  first kind; while its box is up the window is kept and hidden, the page reads and
  cannot write; Cancel gives back the window, the writes and the admission, and smd
  still says the start holds its session. The close again: disarm, read, exit, in that
  order; the archive file holds the session closed as `app_closed` and the exit
  recorded. The next start is not asked. Then, **under the app's own quit rule,** an
  exit whose answer is lost: the window is closed before the box, the app is still
  running with no window, smd had stored the exit; a second start of the app and a
  second close add nothing; Try again is answered "already recorded" and the app quits
  by itself.
- **In the daemon's tests.** New:
  `TestExit_AfterTheTerminalRefusalALateAdmissionStillGivesTheLifetimeItsAuthority`
  (D3's third case, so the app's account of the daemon stays true). Already pinned and
  left as they are: the acknowledged exit stops the scopes and seals live entry before
  it drains (`TestExit_TheCurrentLifetimeClosesItsSessionAndIsRetired`,
  `TestExit_TheLiveEntrySealWaitsForAdmittedSubmits`); an arm needs the stream and ends
  at the grace (`internal/ft8/presence_service_test.go`).
- **Reversion proofs,** each in its own temporary copy, all failing the test named for
  the rule: fifteen on the modules (the retired window's disarm; the window before the
  box; staying second; a failed disarm going on to the exit; the wait for admission
  work; any `404` read as "not served"; the terminal answer retried; a stale answer
  with no generation; the never-admitted lifetime; the marker kept while closing; an
  admission launched while closing; markers lost when work settles; a retirement said
  while closing; the shell recognised by sender; a write let through as a read), and
  three in the real app (the quit rule; the filter's wiring; the barrier's wiring).
- **What the proofs found.** Three did not bite at first. Two rules were each held by
  a second guard, so two tests were missing (a retry failing at its read; a marker seen
  while earlier work was out): added, and the proofs bite. And **the scenario could end
  with no verdict:** with the quit rule broken on purpose the app quit under the box,
  and the process exited 0 with nothing printed, which CI would have read as a pass.
  The scenario now holds every quit until it has given its verdict; that proof now
  ends "Close FAIL", exit 1. The run left one temporary directory behind, removed by
  its recorded path; no process was left.
- **Gates, by exit code:** the shell's `eslint`, `prettier --check` and unit tests; the
  seven existing scenarios (A1, C1, C1d-shell, C2b, C4a, C1d, recovery), all PASS; `go
  vet`, `golangci-lint`, the maintainability metrics and `go test ./internal/api` in
  full. The whole local gate (`task ci:local`), run once after the review closed and
  with the two findings' fixes in: exit 0, "All CI gates passed locally."
- `check:close` is added to `scripts/ci-local.sh` and to the hosted workflow.
  `scenario-recovery.mjs` changes only by exporting the helpers the new scenario
  shares.

**Codex's review (codex-239): two findings, both P2, both right, both fixed
(2026-11-24).** The ordinary flow, the exit-sent state, the stale retry, the retired
window and the filter were found sound, and Codex has no objection to decisions 2 to 6,
or to decision 1 once its finding is fixed.
1. **The never-asked close did not enter the barrier, and a start-up probe still out
   could become that lifetime's first admission after the app had "closed".** The
   waiting page's loop called the admission before it looked whether its window was
   still there. Codex reproduced it with the real modules and a held probe.
   - **Fixed in two places, each with its own test.** The close enters the barrier
     first, whatever follows, so nothing is admitted after it (`closeFlow`). And the
     loop looks at its window before it asks for an admission (`waiting.mjs`).
   - **Tests:** a probe answered after the window has gone starts no admission and
     loads nothing (`waiting.test.mjs`); after a never-asked close the barrier stands
     and an admission asked for launches nothing; and Codex's whole sequence, with the
     real admission, close and loop over one recorder: the probe held, the close, the
     probe answered "up": nothing is asked of the daemon, no box, nothing loaded.
2. **A second `close()` is not a second request to quit.** Nothing stopped an ordinary
   quit of the app while the second-kind box was up: with no window left it would end
   the process without the operator's answer. The scenario had only repeated the
   close. Codex showed it with a real quit injected under the box.
   - **Fixed:** while a close is under way, the app refuses every quit but the close's
     own final one (`closeFlow.allowsQuit`, and a `before-quit` rule in `main.mjs`).
     What the operating system does to the process is not asked and cannot be.
   - **Tests:** the rule on the module, under each kind of box and after a cancelled
     close; and in the real app the scenario now asks the app to quit while the box is
     up and requires that the quit goes no further (13 checks).
   - **For the operator's eye, and not shown:** what a log-out or a shutdown does while
     that box is up has not been tried. The scenario shows an ordinary request to the
     app to quit, and nothing about how the desktop session ends its programs. If such
     a request reaches the app as an ordinary quit, this rule would refuse it like any
     other, until the box is answered or the system ends the process. That is an
     inference from how the rule is written, not an observation (codex-241), and no
     experiment on the session is proposed here.
- **Reversion proofs for the two fixes,** each in a temporary copy, all failing the test
  named for the rule: the loop without its look at the window; the barrier lifted at
  the never-asked close; every quit allowed; the close's own quit not marked; and, in
  the real app, the `before-quit` rule removed, and the last-window rule removed.
- **Gates again:** 160 unit tests, all eight scenarios, `eslint`, `prettier --check`.

**Not in this slice** (the second): the question and its words; the page's reasons; the
read of FT8's state at a close; H with unsaved Settings; the check that no key reloads;
the wiring to the window; and, with the wiring, the API reference's line that the app
now sends the exit report and the disarm.

No transmission, keyed test or rig command: FT8 is off in every daemon these tests
start, and no request was made to the station's daemon.

### Hosted CI red on `67258f26`: a timing fault in a stream test, not in the commit (2026-11-23; fixed, committed and pushed on its own on the operator's direction, 2026-11-24: "commit and push the CI fix")

Recorded here because this is where the CI state of A3's commits is kept. It is not
part of A3 and is to be committed on its own.

- **What failed.** Hosted run 45199094286, step "go test (race detector, -short)":
  `TestWSStream_ShutdownDuringWriteInterrupts` (`internal/api/stream_ws_test.go`), "after
  Shutdown returned: reader's read ended not observed". The run for `b86f9ca4`
  (45197977972) was cancelled by the push that followed it, so neither of the two
  commits has a green hosted run yet. Neither commit touches the stream or its tests.
- **The cause is in the test's fixture.** It counted the reader's read as ended only when
  it saw a read return after the transport was closed. A reader that has not yet begun
  to read when the transport closes ends without ever reading it, and the fixture then
  reports a failure with the daemon behaving correctly. **Shown, not assumed:** with
  the reader's start delayed by 300 ms in a temporary copy, the test fails every time
  with that exact message. That this is what happened on the runner is an inference:
  the message is the same, and a loaded two-core runner under the race detector is
  where a goroutine is scheduled late. It passed 400 times here.
- **The fix is to the fixture only.** It now counts the reads on the transport, and the
  test requires that none is left blocked when Shutdown has returned. The handler joins
  its reader before it ends, so that is exact, and it does not depend on when the
  reader was scheduled. No product code is changed.
- **Proofs,** in temporary copies: with the delayed reader the fixed test passes; with
  the handler made to end without closing or joining, and the transport left open, it
  fails on the new check ("1 read(s) still blocked on the transport"). A first attempt
  at that fault was caught earlier, by Shutdown itself timing out: the library's
  `CloseNow` already waits for the reader, so a blocked read fails the test there too.
- **Gates:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the stream tests 20 times
  with the race detector, and `go test -race -short ./internal/api` in full, as the
  hosted step runs it.
