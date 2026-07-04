# Developing Station Manager (v2)

This is the developer setup for the v2 daemon (`main` branch). It covers a
**fresh Linux workstation** — what to install, how to build, run, and test.

The instructions are written for **Fedora** (the dogfood/development host),
with the equivalent package names noted for Debian/Ubuntu where they differ.
For project conventions, invariants, and the documentation map, read
[`CLAUDE.md`](CLAUDE.md) and [`docs/README.md`](docs/README.md) first.

> Developing v1 instead? v1 (the last released Wails app, still used for
> day-to-day operating) lives on its own branch: `git checkout v1 && cat
> DEVELOPING.md`.

---

## 1. The toolchain at a glance

| Tool | Version | Why it's needed |
|------|---------|-----------------|
| **Go** | 1.26.2 (see `go.mod`) — install **upstream**; Fedora 41's packaged 1.24.10 is too old | The daemon and all `cmd/...` tools. |
| **gcc** + C toolchain | any recent | CGO builds — live FT8 audio capture (miniaudio) and the optional PocketFFT FFT backend. The default dev build (`task build`) is **CGO-on**. |
| **Node.js + npm** | Node ≥ 22 | The three Svelte SPAs (`frontend/logging`, `frontend/config`, `frontend/logbook`). |
| **Hugo (extended)** | 0.126+ (Fedora 41's repo build is sufficient) | The operator manual (`manual/`), embedded into the daemon via `go:embed`. The daemon build compiles against it, so it must be built first. |
| **Task** (go-task) | v3 | The task runner — every build/run/test entrypoint is a `task` target (`Taskfile.yml`). |
| **nfpm** | latest | Builds the dev/release RPMs without `rpmbuild`. Only needed for `task rpm:dev` / `deploy:local:dev`. |
| **podman** | any | Release-only — the shipped RPM is built in an AlmaLinux 8 container (`scripts/release.sh`). Not needed for day-to-day dev. |
| **actionlint** | latest | Lints `.github/workflows/*.yml` (`task ci:lint`). Optional. |

---

## 2. Fedora: install everything

> **Reproducible shortcut:** every non-interactive step in this section is
> scripted in [`scripts/dev-bootstrap.sh`](scripts/dev-bootstrap.sh). Read this
> section for the *why*; run the script for the *how*. It was walked
> command-by-command against a clean **Fedora 41** install on 2026-07-03.

### First: update the base system

A freshly-installed Fedora ships the anaconda base packages, which lag the
`updates` repo. **Update before installing anything else** — skip it and
Fedora's `nodejs` breaks on first run:

```bash
sudo dnf upgrade -y
```

> ⚠️ **This upgrades your *entire* system, not just SM's dependencies.** It's
> written for a **clean install**, where a full upgrade is exactly what you want.
> On an *existing* machine where you don't want everything bumped, run only the
> one package that actually blocks the build instead:
> `sudo dnf upgrade -y sqlite-libs`. (`scripts/dev-bootstrap.sh` does the full
> `dnf upgrade` — it assumes a fresh box, so run it knowingly on a machine that
> isn't one.)

Why it matters: the clean-install base `sqlite-libs` (`3.46.1-1.fc41`) does not
export `sqlite3session_attach`, yet Fedora's `libnode` links the SQLite session
extension against it. Without the upgrade, `node`/`npm` die immediately with:

```
node: symbol lookup error: /lib64/libnode.so.127: undefined symbol: sqlite3session_attach
```

The fix is `sqlite-libs-3.46.1-5.fc41` from `updates` — a full `dnf upgrade`
pulls it (or `sudo dnf upgrade -y sqlite-libs` for just that).

### System packages (dnf)

```bash
sudo dnf install -y \
  git \
  gcc \
  nodejs npm \
  hugo \
  pipewire-libs
```

(`golang` is intentionally omitted — Fedora's is too old; install upstream Go in
§2.1. It still arrives transitively as a `hugo` dependency, which is harmless.)

Notes:

- **`gcc`** is required even though most of the code is pure Go — the default
  dev build enables CGO for live FT8 audio capture. Without it,
  `task build` fails with `cgo: C compiler "gcc" not found`. (You can still
  build the CGO-free static shape with `task build:smd:static`.)
- **Go is deliberately *not* in the list.** Fedora 41's `golang` is 1.24.10 —
  older than the `go.mod` line (`go 1.26.2`), so it cannot build the daemon.
  Install the upstream toolchain in §2.1 (**required**, not optional). Fedora's
  `golang` still comes in transitively via `hugo`; that's harmless as long as
  upstream Go is first on `PATH` (§2.1) so it shadows it. (`go version` will then
  report `go1.26.2`; if it reports `1.24.x` your `PATH` isn't picking up the
  upstream install.)
- **`hugo`** in Fedora's repos is the *extended* build, which is what's
  needed. Fedora 41's is **0.126.2** and builds `manual/` fine (verified
  2026-07-03) — the repo version is sufficient. Only if a future template
  breaks against it, grab a pinned release the way CI does:
  ```bash
  HUGO_VERSION=0.162.1
  curl -fsSL "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-amd64.tar.gz" \
    | sudo tar -xz -C /usr/local/bin hugo
  ```
- **`pipewire-libs`** is the *runtime* audio backend. miniaudio `dlopen`s an
  audio backend at run time for live FT8 capture; on modern Fedora that's PipeWire.
  It's almost always already installed as part of the desktop — listed here for
  a truly minimal install. No `-devel` headers are needed (miniaudio bundles its
  own C).

### 2.1 Go toolchain (upstream — required on Fedora 41)

Fedora 41's packaged Go (1.24.10) is older than `go.mod`'s `go 1.26.2`, so the
upstream toolchain is **required**, not a fallback:

```bash
GO_VERSION=1.26.2
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
  | sudo tar -xz -C /usr/local
# Add to your shell profile (~/.bashrc):
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
```

`$HOME/go/bin` on PATH is also where `go install`-ed tools (Task, nfpm,
actionlint) land — make sure it's there.

### 2.2 Go-installed tools

```bash
# Task runner — the primary entrypoint for everything below.
go install github.com/go-task/task/v3/cmd/task@latest

# Only if you'll build RPMs (task rpm:dev / deploy:local:dev):
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# Optional — lints the GitHub Actions workflow (task ci:lint):
go install github.com/rhysd/actionlint/cmd/actionlint@latest
```

> Task is also packaged as `go-task` in some distros, but `go install` is the
> version-agnostic route and guarantees v3.

### 2.3 Serial port access (rig CAT control)

Rig CAT control talks to a USB serial device (`/dev/ttyUSB*` or
`/dev/ttyACM*`), which is owned by the **`dialout`** group. Add yourself once:

```bash
sudo usermod -aG dialout $USER
# Log out and back in (or `newgrp dialout`) for it to take effect.
```

Without this, the daemon can't open the serial port and the bridge/CAT
subsystem stays disconnected.

### 2.4 SM Cloud (smcloud) — optional, only if working on the cloud service

The `internal/cloud/store` + `cmd/smcloud` subsystem (SM Cloud P1, ADR 0040) uses
**Postgres**. The daemon itself is SQLite-only and needs **none** of this — install
it only to run smcloud's codegen or integration tests.

```bash
# Container runtime for the throwaway dev Postgres (Fedora-native, rootless). The
# Taskfile defaults to podman; override with PG_RUNTIME=docker if you use Docker.
sudo dnf install -y podman

# sqlboiler + its Postgres driver — regenerates the models (task models:cloud).
go install github.com/aarondl/sqlboiler/v4@latest
go install github.com/aarondl/sqlboiler/v4/drivers/sqlboiler-psql@latest

# golang-migrate CLI — applies the Postgres migrations (task migrate:cloud:up).
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

Dev loop, from the repo root:

```bash
task db:pg:up            # start a throwaway Postgres container (user/pass/db = smcloud)
task migrate:cloud:up    # apply internal/cloud/store/migrations
task models:cloud        # regenerate the sqlboiler models (read-only artifacts)
# ... smcloud integration tests connect to the same DB ...
task db:pg:down          # tear it down
```

> No container runtime? A native `sudo dnf install postgresql-server` + `initdb`
> works too — just point the Taskfile DSN at it. Podman is preferred: throwaway,
> isolated, no host state, and it's what CI would use.

### Debian/Ubuntu equivalents

`gcc` → `build-essential`; `nodejs npm` (use NodeSource for Node ≥ 22);
`hugo` is often too old — use the pinned-release `curl` above; audio runtime
is `libpipewire-0.3-0`; serial group is also `dialout`.

---

## 3. Project setup

```bash
git clone https://github.com/ColonelBlimp/station-manager.git
cd station-manager

# Install SPA dependencies (logging is the main one; config + logbook
# have their own install tasks if you work on them).
task frontend:install

# Sanity check the full toolchain — vet + build + test, the CI-equivalent
# local smoke check.
task
```

### Git identity (fresh install)

A brand-new install has no git identity, so the first `git commit` fails with
`Author identity unknown`. Set yours once — use the name and address your
commits should be attributed to (e.g. your GitHub username and its noreply
address, so pushes attribute correctly):

```bash
git config --global user.name  "Your Name"
git config --global user.email "you@example.com"
```

### The `.env` file

The Taskfile loads `.env` from the repo root (`dotenv: ['.env']`). Create one:

```bash
# .env  (gitignored — never commit it)
SM_WORKING_DIR=/home/you/.local/share/station-manager
```

`SM_WORKING_DIR` is the runtime data directory (SQLite DB, `config.json`,
logs) used by `task run` / `task run:smd`. For the live-QRZ integration tests
you'd also add `QRZ_TEST_API_KEY` and `QRZ_TEST_CALLSIGN` (a throwaway test
logbook), but those are optional.

---

## 4. Everyday workflow

All entrypoints are `task` targets — run `task --list` for the full set. The
ones you'll use most:

### Build

```bash
task build          # CGO-on dev build of the whole module + build/bin/smd (live FT8)
task build:smd      # daemon with freshly embedded SPAs + manual (CGO, gonum FFT)
task build:smd:pocketfft   # ~2x faster FT8 decode (CGO PocketFFT) — needs gcc
task build:smd:static      # CGO-free static build — no FT8 capture (release shape)
```

### Run

```bash
task run            # build + run the daemon on :8080 (stops the systemd smd first)
```

For SPA development with hot-reload, run two terminals:

```bash
task run:smd        # terminal 1: daemon via `go run` (CGO-on, live FT8)
task frontend:dev   # terminal 2: Vite on :5173, proxies /v1/* to the daemon
```

Both `run` targets automatically stop a running user-level `smd` systemd unit
first so they don't fight over `:8080` and the audio/serial ports. Restart it
afterwards with `systemctl --user start smd`.

### Test

```bash
task test           # go test -race -short ./...
task ci:local       # full local mirror of the CI gate (SPA lint/check/test/build
                    # + Go gofmt/vet/race-test + daemon embed-build)
task ci             # actionlint + ci:local — run before pushing
```

### Dogfood deploy (install over your running copy)

```bash
task deploy:local:dev    # build dev RPM → stop smd → rpm -Uvh → reload → start
```

One command to refresh the dogfooded systemd install after a code change.
Defaults to the PocketFFT (CGO) backend for live FT8; prompts for sudo once
for the package install. Override the backend with `SM_FFT=gonum task
deploy:local:dev` for the static build.

---

## 5. CGO, FFT backends, and FT8 in one paragraph

The default dev builds (`task build`, `run`, `run:smd`) are **CGO-on** so live
FT8 audio capture works without a full deploy. There are two FFT backends:
**gonum** (pure Go, the CGO-on default) and **PocketFFT** (`SM_FFT=pocketfft`
or the `:pocketfft` task variants — CGO C code, ~2× faster decode, dynamically
linked). The **CGO-free static build** (`build:smd:static`, `CGO_ENABLED=0`)
drops audio capture entirely — the FT8 subsystem logs "capture unavailable;
subsystem idle" — and is the shipped-release shape plus the right build for a
headless/aggregator node. If you only touch logging/forwarding/bridge code and
don't care about FT8, the static build needs no gcc and no audio libraries.

---

## 6. Where the durable context lives

- [`CLAUDE.md`](CLAUDE.md) — conventions, idioms, load-bearing invariants.
  Read first.
- [`docs/README.md`](docs/README.md) — the authoritative documentation map
  (which docs are live vs. historical).
- [`docs/install.md`](docs/install.md) — end-user (operator) install, as
  opposed to this developer setup.
- [`RELEASING.md`](RELEASING.md) — tagging and building the shipped RPM.
- [`docs/session-handoff.md`](docs/session-handoff.md) — rolling
  cross-session state: what's done, what's next.
