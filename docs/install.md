# Station Manager — install and first-run setup

This guide walks a new operator from a fresh Linux machine to a
working Station Manager install with the first QSO loggable. The
target distribution is Fedora / RHEL family (RPM + systemd); Debian
support is on the roadmap but not yet packaged.

---

## 1. Prerequisites

- A modern Linux distribution with `dnf` and `systemd` (Fedora 40+,
  RHEL 9+ or equivalent).
- Permission to run `sudo` for the install step. Everything after
  install runs as your normal user.
- A web browser. The UI is the embedded Svelte SPA served by the
  daemon — no separate desktop application to install.

Station Manager is a single-user, single-machine daemon by design.
It listens on `127.0.0.1:8080` (loopback only) and stores all state
under your home directory. There is no system-wide service, no
shared database, and no network exposure.

---

## 2. Install the RPM

```
sudo dnf install /path/to/station-manager-<version>.x86_64.rpm
```

The package installs three files:

- `/usr/bin/smd` — the daemon binary (the browser SPA is embedded
  inside it). The default build is a single statically-linked,
  CGO-free executable. A CGO build (built with `SM_FFT=pocketfft`) is
  dynamically linked against the C runtime and adds two things the
  static build lacks: ~2× faster FT8 decode (PocketFFT) **and live FT8
  audio capture** — the static default can decode WAV files but cannot
  capture from a sound device. See the "Live FT8 decode" section below
  if you want live FT8.
- `/usr/bin/smctl` — the start/stop control wrapper (see §3).
- `/usr/lib/systemd/user/smd.service` — the systemd user unit.

The post-install scriptlet prints the next steps. The OpenPGP
warning on an unsigned local-build RPM is expected; signed builds
will land when the CD pipeline does.

---

## 3. Enable and start the daemon

Run as your normal user, not root:

```
systemctl --user daemon-reload
systemctl --user enable --now smd
```

To start the daemon at boot without needing an active login session
(recommended for a personal logging machine):

```
loginctl enable-linger "$USER"
```

Verify it's running:

```
systemctl --user status smd
```

The service should report `active (running)`. If it doesn't, check
`journalctl --user -u smd` for the cause — the most common failure
on first run is a stale `config.json` from a prior install pointing
at unreadable paths.

For day-to-day start/stop, the package installs a small wrapper,
`smctl`, alongside the binary:

```
smctl start     # → "SM Started."   (once confirmed active)
smctl stop      # → "SM Stopped."   (once confirmed down)
smctl restart
smctl status
```

It wraps `systemctl --user … smd` and prints a confirmed result line
that bare `systemctl` cannot — `systemctl` is silent on success and
the daemon's own output goes to the journal, not your terminal. Use
plain `systemctl --user enable/disable smd` for the boot-time
enablement above; `smctl` only handles the running state.

---

## 4. First-run setup (REQUIRED)

Open `http://127.0.0.1:8080` in your browser.

On a fresh install the daemon's `setup_complete` flag is `false`,
and the SPA renders only a "Welcome to Station Manager" page with a
single input field: **your callsign**. The main logging interface
is gated behind setup until this is complete.

Enter your callsign (the one you'll log under most of the time —
typically your personal callsign) and click **Save**. This single
action:

- Seeds the default logbook row in the database.
- Flips `setup_complete=true` in `config.json`.
- Copies your callsign into ADIF `OPERATOR` and `OWNER_CALLSIGN`
  fields used on every QSO export.
- Unlocks the main QSO logging UI.

After that, the welcome page disappears and you can log a QSO.

Optional but recommended next steps once you're past setup, all
accessible from the My Station tab in the SPA:

- **Gridsquare** (Maidenhead locator). The daemon derives latitude
  and longitude from this; bearing and distance calculations on the
  Country panel depend on it.
- **Name** (operator name) and other ADIF MY_\* fields.
- **Rig / CAT settings** if you want the daemon to read frequency
  and mode directly from a connected transceiver (Yaesu FT-710 and
  FTdx10 are the two tested drivers).
- **Forwarders** (QRZ Logbook, etc.) if you want QSOs uploaded to
  online services. No forwarders are configured by default — add an
  entry to the `forwarders` array in `config.json` for each destination
  you want, then restart the daemon. A first-run setup affordance for
  this in the SPA is future work; see ADR 0022.

---

## 5. Import historical QSOs (recommended next step)

If you have an existing ADIF logbook from another tool (QRZ Logbook,
HRD, etc.), import it now — **before logging any live QSOs**. This
isn't a sidebar; the SPA's per-contact context features lean on
your operating history:

- **Worked panel** shows prior contacts with the station you're
  logging right now — Date, Time, Band, Mode, RST exchanged. With
  no history imported, every QSO looks like a first contact.
- **Country panel** flags DXCC entities you've never worked before
  with a `*` marker ("new one"). With no history imported, every
  country reads as new.
- **Forwarder upload status** carries through. The importer stamps
  the QRZ `qso_upload` row pre-success for any record that came
  out of QRZ Logbook (recognised via `app_qrzlog_logid`), so the
  daemon won't try to re-upload your history when forwarding is
  later enabled.

Import:

```
smd import /path/to/your-export.adi
```

The importer drives the same QSO submission path the live SPA uses
(validation + atomic write + audit table all inherited). Throughput
is around 900 records per second on typical hardware — a few
thousand QSOs import in seconds.

The importer prints a summary on exit: `{stored, duplicate,
errors}`. Errors are usually source-data problems (e.g. an `rst_rcvd`
field exceeding three characters) and are listed line-by-line so you
can patch the source ADIF and re-import. Re-imports are idempotent —
already-stored QSOs return as `duplicate`, not as errors.

The importer is the same binary as the daemon — there's no separate
package. **Stop the daemon first** (`smctl stop`) so the two processes
aren't racing on the same SQLite file, then run the import, then
`smctl start`. The import itself takes seconds.

You don't need to set `SM_WORKING_DIR` in your shell. When run from
`/usr/bin/smd` (the installed location), the binary resolves its
working directory via the same XDG fallback the daemon uses, so it
finds your real `config.json` and database automatically.

By default, imported QSOs land in your **default logbook** — the
one first-run setup seeded from your callsign. To target a
different existing logbook, pass `--logbook <id>`. The importer
will not create a logbook for you; if `--logbook` points at a
missing row, or no default is set and no flag is given, it fails
loudly without writing anything. The logging SPA currently has no
UI for creating additional logbooks (that lives in the future
logbook app); if you need one, hit `POST /v1/logbook` directly with
`curl`.

---

## 6. Where things live

| What | Path |
|---|---|
| Binary | `/usr/bin/smd` |
| Control wrapper | `/usr/bin/smctl` |
| Systemd unit | `/usr/lib/systemd/user/smd.service` |
| Data directory | `~/.local/share/station-manager/` |
| Config | `~/.local/share/station-manager/config.json` |
| Database | `~/.local/share/station-manager/db/station-manager.db` |
| Logs | `~/.local/share/station-manager/log/` |
| Emailed-session ADIF archive | `~/.local/share/station-manager/exports/sent-adif/` |

Each time you email a session's QSOs from the logging app, a copy of
the exact ADIF that was sent is archived under `exports/sent-adif/`
(filename `session-<UTC timestamp>.adi`). The copy is written before
the email is dispatched, so a failed or flaky send still leaves a
usable local export.

The data directory is set by the systemd unit via the
`SM_WORKING_DIR` environment variable. When running `smd` directly
(e.g. `smd import`) without that env var, the binary falls back to
`$XDG_DATA_HOME/station-manager` (default `~/.local/share/station-manager/`)
as long as it's installed under a system path — so the daemon and
the importer always see the same files. The daemon will create the
directory and seed a default `config.json` on first start if
neither exists. That default `config.json` is a complete template:
every operator-editable field is present, with sensible empty
defaults or canonical public URLs (QRZ XML endpoint, hamnut endpoint,
QRZ web-profile prefix) prefilled. Edit the fields you care about
and restart the daemon.

---

## 7. Update

```
sudo dnf install /path/to/station-manager-<newer-version>.x86_64.rpm
systemctl --user daemon-reload
systemctl --user restart smd
```

Your data directory survives the upgrade — `dnf` only manages the
files it installed (the binary and the unit). Database schema
migrations are applied automatically on daemon startup.

---

## 8. Live FT8 decode (optional)

FT8 decode is off by default and opt-in. The daemon decodes live
receive audio into "heard this" log lines — **a decode is not a QSO**;
nothing is written to your logbook or upload queue. It's a monitoring
aid, not a logger.

Two prerequisites:

1. **A CGO build of the daemon.** Live audio capture requires CGO; the
   static default build cannot open a sound device (it logs "capture
   unavailable; subsystem idle" and stays up). Build/install the CGO
   flavour with `SM_FFT=pocketfft` — e.g. for a local dogfood install,
   `SM_FFT=pocketfft task deploy:local:dev`.
2. **Receive audio routed to a capture device** — typically the rig's
   USB audio codec.

Find your capture device index with the probe tool (CGO build):

```
CGO_ENABLED=1 go run ./cmd/ft8-capture-probe -list
```

It prints each device and its index. Pick the rig's codec, then set
the FT8 block in `config.json`:

```json
"ft8": {
  "enabled": true,
  "device": "1"
}
```

`device` is the integer index from `-list` as a string; an empty
string means the system default capture device. Restart the daemon
(`smctl restart`) and watch the log for decodes:

```
journalctl --user -u smd -f | grep "ft8 decode"
```

To smoke-test capture directly without the daemon, the probe can run
the live slot scheduler and print decodes per UTC slot:

```
CGO_ENABLED=1 go run ./cmd/ft8-capture-probe -scheduler -slots=4 -device=1
```

Healthy output is one slot every 15 s on the UTC :00/:15/:30/:45
boundary, a non-trivial peak amplitude, and decoded messages. A
near-zero peak means the input is muted, the wrong device, or
unconnected.

---

## 9. Uninstall

```
systemctl --user disable --now smd
sudo dnf remove station-manager
```

The data directory at `~/.local/share/station-manager/` is left in
place. Remove it manually if you want a fully clean uninstall:

```
rm -rf ~/.local/share/station-manager/
```

Back it up first if you have any QSOs you want to keep — that
directory contains your entire logbook.
