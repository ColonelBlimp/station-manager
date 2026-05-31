# v2 design — release acceptance

**Status:** initial draft 2026-05-16; per-commit CI gate added same day. Scopes both:

1. The **release-tag cadence** — "is `vX.Y.Z` ready to cut?" gate (this document's original scope).
2. The **per-commit CI gate** — "is main always releasable?" rule, shipped 2026-05-16 at `.github/workflows/ci.yml`. The CI workflow runs on every push to main and covers Gates 1–3 + 5a from the list below; Gates 4, 5, 6, 7, 8 remain manual (need real hardware, a clean VM, or real upstream credentials). Local mirror of the CI gate: `task ci:local`.

## Purpose

Enumerate the checks that must pass before an RPM is built and installed on the operator's daily-driver machine. Today these checks are run by hand; this document is the spec a future CD pipeline will mechanise. The spec is the source of truth, not the pipeline.

## Release shape (context)

Single binary `smd` packaged as `station-manager-<ver>.x86_64.rpm` (pre-dogfooding stage 2, shipped 2026-05-14 — see `milestones.md`). Daily-driver install path: `dnf install` over the previous version on the operator's machine. Single operator at present; multi-user shapes deferred per ADR 0016. Build entry points: `scripts/release-rpm.sh <ver>` (tagged release) and `scripts/dev-rpm.sh` (dev iteration, fixed `dev` version).

## Gates

Run in order. Each gate is a hard stop — a failure means the tag does not get cut. Gates are listed fast-to-slow so the cheap signals fail first.

### Gate 1 — Static checks

- `go vet ./...` clean.
- `go build ./...` succeeds.
- SPA from `frontend/logging/`:
  - `npm run lint` — 0 errors (eslint flat config + type-checked TS rules, wired session 47).
  - `npm run format:check` — 0 prettier diffs.
  - `npx svelte-check` — 0 errors across all `.svelte` files.

### Gate 2 — Test suites

- `go test ./...` — full Go suite green (98 test files as of 2026-05-16). Includes:
  - Integration tests against real `&sqlite.Service{}` with in-memory DBs.
  - ADR 0013 boundary tests (bridge ⇄ storage/forwarder package-import isolation).
  - Bridge pipeline tests (37 as of M3a.3, including identity-verification + race-detector hammer).
- `go test -race ./...` — race-detector hammer clean. Particularly important for the bridge subsystem and the `events.Hub` fan-out paths.
- SPA `npm run test` — vitest unit + component (376+ tests as of session 47, 42 test files).

### Gate 3 — Build artifacts

- `scripts/release-rpm.sh <ver>` runs end-to-end:
  - SPA build (`npm run build` → `frontend/logging/dist/`).
  - Static Go build (`CGO_ENABLED=0`, `-trimpath`, `-X main.Version=<ver>`) — the default, CGO-free backend. The opt-in PocketFFT build (`SM_FFT=pocketfft` → `CGO_ENABLED=1 -tags pocketfft`) is dynamically linked instead; the static-link checks below apply only to the default build.
  - `nfpm pack -t build/release/` produces `station-manager-<ver>.x86_64.rpm`.
- Binary smoke: `./build/bin/smd --help` exits 0; for the default build the binary is statically linked (`file build/bin/smd | grep "statically linked"`) — the PocketFFT build is dynamically linked, so skip that grep for it; binary size in the expected range (sanity check against the previous release — large jumps deserve investigation).
- The injected version is observable: `./build/bin/smd` startup log line carries `vX.Y.Z`; `curl http://127.0.0.1:8080/v1/version` returns it.

### Gate 4 — Install smoke (clean install path)

Run on a scratch machine, container, or VM — not the operator's daily-driver. The operator's daily-driver path is exercised by gate 5.

- `dnf install build/release/station-manager-<ver>.x86_64.rpm` succeeds, no scriptlet errors.
- `systemctl --user daemon-reload && systemctl --user enable --now smd` starts the daemon cleanly.
- `loginctl enable-linger "$USER"` is idempotent.
- `journalctl --user -u smd -n 100` shows the daemon bound to its listener, no panic, no startup errors.
- `curl -sf http://127.0.0.1:8080/v1/healthz` returns 200.
- `curl -sf http://127.0.0.1:8080/v1/version` returns the expected version string.

### Gate 5 — Upgrade smoke (dogfood path)

This is the path the operator actually runs at deploy time. The risk this gate exists to catch is "v2-over-v2 upgrade silently corrupts config or DB" — schema migration regressions, config-shape drift, embedded-asset mismatch.

- Backup current `~/.local/share/station-manager/` (the working dir).
- `dnf install` the new RPM over the existing version (no `remove` first — that's the install-day pattern from `docs/install.md` § Update).
- Restart: `systemctl --user restart smd`.
- `journalctl --user -u smd -n 100` shows clean start, **no migration errors**, no "config file shape drift" warnings.
- Browser to `http://127.0.0.1:8080/` — QSO panel renders (NOT the first-run setup card; existing config respected).
- My Station card hydrates with prior values (callsign, gridsquare, default rig, default logbook).
- Recent QSOs visible in SessionPanel (if any were in the session before restart — sessionStorage may not survive, that's fine).
- `sqlite3 ~/.local/share/station-manager/db/smd.sqlite "SELECT count(*) FROM qso;"` matches the pre-upgrade count (no row loss).

#### Gate 5a — Schema migration coverage

The bullet "no migration errors" in gate 5 catches loud failures (a malformed `.up.sql` that the driver rejects). It does not catch silent regressions: a migration that runs cleanly but produces a schema that differs from a fresh-install schema, or a migration that runs cleanly on an empty DB but fails on a DB with realistic row counts. This sub-gate covers those.

- **Forward replay from the oldest shipped schema.** Restore a snapshot of the DB taken at the previous tag (or, lacking one, a freshly-initialised DB from the previous binary). Start the new binary against it. Verify all migrations apply, exit code 0, no errors in the journal.
- **Schema-equivalence check.** `sqlite3 <migrated.db> .schema | sort` matches `sqlite3 <fresh-install.db> .schema | sort` exactly. Catches missed migrations, drifted column types, missing indexes, missing triggers.
- **Row-count survival.** For every table that existed in the previous schema, row count post-migration equals row count pre-migration. Tables added by the new schema may have any count (including zero).
- **Audit-table integrity.** `qso_history` rows from before the upgrade are still present and the append-only triggers still fire (try a manual `UPDATE qso_history SET ...` via sqlite3 — must error with the `qso_history is append-only` message per ADR 0014 prep #2 wiring).
- **Realistic-size replay.** For the first few post-stage-3 tags, the operator's actual daily-driver DB (the QRZ-imported 4233 QSOs + ongoing additions) is the realistic-size sample. Apply the new binary's migrations against a copy of that DB in a scratch dir before installing on the live machine.

**Today's reality (2026-05-16):** only `0001_init.up.sql` exists, and per the carry-over note in session-handoff `qso_history` was appended in-place to that file because v2 is still pre-production. This sub-gate is therefore a near-no-op for the very first tag. **It becomes load-bearing from the second tag onward** — once stage 3 ships and the operator is running v2 on the daily-driver, the in-place edit rule for `0001_init` ends and every schema change must be a new `NNNN_*.up.sql` file. The first such file is the first real exercise of this sub-gate.

### Gate 6 — End-to-end QSO smoke

One QSO logged through the SPA, end-to-end, against the freshly installed binary.

- Type a callsign in the QSO form.
- Tab → enrichment fires (or fails gracefully with no-network — toast appears, form remains usable).
- Fill mode / RST / comment.
- Ctrl+Enter to submit.
- Info-toast confirms stored.
- SessionPanel shows the new row.
- Open QSO edit overlay on the row; verify all fields populated; close with ESC.
- `sqlite3 ~/.local/share/station-manager/db/smd.sqlite "SELECT count(*) FROM qso;"` increases by exactly 1.
- `sqlite3 ~/.local/share/station-manager/db/smd.sqlite "SELECT count(*) FROM qso_upload WHERE status='pending';"` — at least one pending upload row exists (forwarder queue wiring intact).

Session email-out (skip if `smtp.enabled=false`):

- On the Session tab, with ≥1 session QSO, the email controls show next to the tab; the send button enables when the recipient field has an address.
- Click send → "Sending…" then "Sent to …" toast.
- The SessionPanel "Emailed" column flips to a ✓ (with the date) for the sent rows.
- A copy of the emailed ADIF appears under `~/.local/share/station-manager/exports/sent-adif/session-<UTC>.adi`, and it begins with an ADIF header ending in `<EOH>` followed by records — **not** a bare `<CALL...>` first line (the header is what stops importers from swallowing the first QSO).

### Gate 7 — Bridge smoke (skip if no rig attached)

Only runs when a rig is physically connected and `bridge.enabled=true` in the config.

- Daemon journal shows `bridge.Service.Start` succeeded, serial port opened, INIT and READ sent.
- SPA shows the rig name in the My Station header.
- SPA shows live VFO-A / VFO-B / mode from the rig.
- Tune the rig — observe VFO update in the SPA within ~1s.
- Disconnect/reconnect the rig USB cable — `rig-disconnected` event surfaces as a toast; reconnect produces a fresh `rig-state` event and the toast clears.

### Gate 8 — Forwarder smoke (skip if no upstream creds)

Only runs when at least one forwarder is enabled and credentialed.

- After gate 6's QSO is logged, wait for the next forwarder poll (default 120s).
- `curl http://127.0.0.1:8080/v1/qso/{uuid}/uploads` shows the queue row transitioning `pending → ok` (or `pending → failed` with a meaningful error).
- For QRZ specifically: manual eyeball check on `qrz.com/logbook` confirms the QSO appears.

### Gate 9 — Rollback path is intact

Tested every release even if not exercised, because the value of a rollback path is highest when you've not used it recently.

- The gate-5 backup of `~/.local/share/station-manager/` is recoverable (a quick `tar tzf` on the backup confirms it's not corrupt).
- `dnf history undo last` is available and points at the previous RPM.
- Documented in `docs/install.md` § Update.

## Out of scope (do not block a release on these)

- Multi-rig scenarios — single-rig is the only supported shape today.
- WSJT-X / JTDX UDP bridge — `cmd/udp-bridge` deferred to M3b.
- Logbook + config SPAs — separate frontends, not part of the logging SPA release.
- SM Cloud sync, multi-tenant — deferred per ADR 0016.
- Cross-platform (Windows, macOS) — Linux RPM is the only supported package.
- Mobile / responsive layouts — desktop-only.

## Current gaps (spec calls for, not yet fully automated)

**Update 2026-05-16:** Gates 1–3 + 5a are now automated by `.github/workflows/ci.yml` on every push to main. Gates 4 (clean-VM install smoke), 5 (upgrade smoke), 6 (end-to-end QSO), 7 (bridge live test), 8 (forwarder live test) remain manual — each needs real hardware, a scratch VM, or real upstream credentials. The gaps below describe the manual half.


These are deliberate parking lots. None of them block tagging today — the operator runs them by hand. They are listed here so the future CD pipeline knows what to mechanise.

- **No automated install/upgrade harness.** Gates 4 and 5 are manual on a real or scratch machine. A `podman` or `systemd-nspawn` target with `dnf install` scripted would cover them.
- **No headless SPA smoke.** Gate 6 is manual through a browser. A Playwright run against the served SPA would automate it (already mentioned in CLAUDE.md as the expected E2E tool; no specs written yet).
- **Bridge and forwarder smokes stay manual.** Gates 7 and 8 need real hardware and real upstream credentials; a CI virtualisation is not worth building until the failure rate justifies it.
- **No release-notes generation.** Today the operator writes the changelog by hand from `git log`. A `gh release create --generate-notes`-style step is parked.

## Related documents

- `docs/v2-design/milestones.md` — pre-dogfooding stages 1-3 (the path that brought the RPM into existence).
- `docs/install.md` — operator-facing install / upgrade / uninstall procedure. Gates 4, 5, 9 mirror the steps documented there.
- `docs/v1-analysis/invariants.md` — the load-bearing rules that the test suites guard. Any regression in these is a hard release blocker regardless of which gate caught it.
- `docs/decisions/0013-bridge-as-daemon-subsystem.md` — the package-boundary discipline asserted by gate 2's boundary tests.
- `docs/decisions/0016-defer-sm-cloud.md` — the "single operator, single machine" assumption gates 4-8 are written against.
