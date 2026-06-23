# Station Manager — install and first-run setup

This guide walks a new operator from a fresh Linux machine to a
working Station Manager install with the first QSO loggable. The
target distribution is Fedora / RHEL family (RPM + systemd); Debian
support is on the roadmap but not yet packaged.

---

## 1. Prerequisites

- An RPM-based Linux with `dnf`/`zypper` and `systemd`. The release
  binary's glibc floor is 2.28, so it runs on **Fedora 34+, RHEL/Rocky/
  AlmaLinux 8+**, and recent openSUSE. (Debian/Ubuntu `.deb` is on the
  roadmap but not yet packaged — see §9.)
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
  inside it). The shipped RPM is a **CGO + PocketFFT** build, so **live
  FT8 capture + decode work out of the box** (faster PocketFFT decode,
  plus the audio capture the CGO-free build can't do). It is dynamically
  linked against glibc and built on an old-glibc baseline (AlmaLinux 8 →
  glibc 2.28), so it runs on Fedora and other RPM distros back to RHEL 8.
  The only runtime need beyond a base system is an audio backend —
  PipeWire, PulseAudio, or ALSA — which any desktop already has. See §8
  for FT8 setup, and §9 for how the release is built.
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

`enable --now` starts smd immediately **and** auto-starts it whenever you log
in. For an always-on logging machine you also want it to start at **boot, with
no login session** — enable lingering for your user:

```
loginctl enable-linger "$USER"
```

Without linger, smd runs only while you have a session open; with it, your
user's systemd instance (and smd) starts at boot and survives logout. It's a
`--user` service throughout — there is no `sudo systemctl enable smd`.

Verify it's running:

```
systemctl --user status smd
```

The service should report `active (running)`. If it doesn't, check
`journalctl --user -u smd` for the cause — the most common failure on
first run is a bad `config.json` (malformed JSON, an invalid value, or
a stale path from a prior install). A config/startup error that aborts
the daemon before logging starts is **also written to `smd.log`** (see
§6 for its location), so either place shows it — look for the
`"smd startup failed (fatal) before logging was initialised"` line.

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
- **Rig / CAT** — to have the daemon read frequency/mode from a connected
  transceiver (and key PTT for the tune carrier and FT8 transmit), see
  **"Connecting a rig (CAT)"** just below. Skip it for a logging-only station —
  the bridge stays disabled and you type frequency/mode by hand.
- **Forwarders** (QRZ Logbook, etc.) if you want QSOs uploaded to
  online services. No forwarders are configured by default — add an
  entry to the `forwarders` array in `config.json` for each destination
  you want, then restart the daemon. A first-run setup affordance for
  this in the SPA is future work; see ADR 0022.

### Connecting a rig (CAT)

CAT lets the daemon read the rig's frequency and mode (shown live in the SPA)
and key PTT for the tune carrier and FT8 transmit. It's optional — a
logging-only station leaves the bridge disabled and enters frequency/mode by
hand. There is no rig editor in the SPA yet (that's the future config app), so
this is a `config.json` edit.

**1. List the host's devices.** With the daemon running, ask it what hardware is
present — no extra tools needed (the `ft8-capture-probe` / `catcli` dev tools
are not in the RPM):

```
curl -s http://127.0.0.1:8080/v1/hardware | jq
```

`serial_ports[].id` are the CAT port paths (each with a friendly `label` and
VID:PID); `audio.capture` / `audio.playback` list the sound devices by `name`
(needed only for FT8). Prefer the stable `/dev/serial/by-id/...` path over a
bare `/dev/ttyUSBn` — it survives reboots and re-plugging. A rig like the FTdx10
presents **two** serial ports (a dual USB UART); the CAT one is the "Enhanced"
port — if unsure, try one and check the log (step 4), then the other.

**2. Grant serial-port access.** The daemon runs as your user, so your user must
be able to open the port. Read which group owns it and add yourself — the group
name varies by distro, so take it from `stat`, don't assume `dialout`:

```
stat -c '%U %G %a' /dev/ttyUSB0      # → e.g. "root <group> 660"
sudo usermod -aG <group> "$USER"     # use the group printed above
```

Then **log out and back in (or reboot)**: the systemd `--user` instance only
picks up the new group on a fresh session (`newgrp` won't fix the service).
Confirm with `id`.

> On Fedora, **ModemManager** probes USB serial ports on plug-in and can briefly
> hold the CAT port, which looks like an open failure. If CAT connects flakily,
> check `systemctl status ModemManager`; a udev rule tagging the rig's VID:PID
> with `ENV{ID_MM_DEVICE_IGNORE}="1"` tells it to leave the port alone.

**3. Configure the rig.** Stop the daemon first — it rewrites `config.json` on
any change, so a hand-edit while it's running gets clobbered:

```
smctl stop
```

Add a rig to the `rigs` array, point `default_rig_id` at it, and enable the
bridge:

```json
"bridge": { "enabled": true },
"rigs": [
  {
    "id": 1,
    "model": "yaesu-ftdx10",
    "port": "/dev/serial/by-id/usb-..._-if00",
    "audio": { "rx": "<capture name>", "tx": "<playback name>" }
  }
],
"default_rig_id": 1
```

- `model` is the CAT **driver id**. Tested rigs: **`yaesu-ftdx10`** and
  **`yaesu-ft710`** (Kenwood-style CAT), **`icom-ic7300`** (CI-V — also set the
  radio-menu items below; ADR 0034).
- `port` is a `serial_ports[].id` from step 1.
- `audio.rx` / `audio.tx` are `audio.*[].name` values from step 1 — needed only
  for FT8 (capture / playback). Omit them for CAT display only.

> **Icom IC-7300 — set these in the radio's menu first.** Set **USB SEND = OFF**
> (Menu » SET » Connectors » USB SEND). The IC-7300 can map PTT to a serial
> control line (RTS/DTR); on Linux/macOS the OS may briefly assert that line when
> the daemon opens the port, which would key the transmitter. USB SEND = OFF
> removes PTT from the control line and is the dependable fix (the daemon
> de-asserts the lines, but the OS can't guarantee a pulse-free open — you'll see
> a one-line warning at connect). Also set **CI-V Transceive = ON** (so the rig
> pushes frequency/mode changes), **CI-V USB Port = Link to [REMOTE]**, and match
> the daemon's baud to the rig's **CI-V Baud Rate** (the default rigdef uses
> 19200 — that's the CI-V baud, not the USB baud). See ADR 0034.

**4. Restart and verify.**

```
smctl restart
```

The SPA's frequency/mode readout should go live within a second or two. If it
doesn't, `journalctl --user -u smd` (or `smd.log`, §6) shows the bridge
connecting — look for the decoded `rigIdentity`, or a permission / port / serial
error.

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
- **Nothing is uploaded.** Import seeds your *local* logbook only — it
  never re-sends your history to QRZ, ClubLog, or any forwarder, even
  when those are already configured and running. Your past QSOs already
  live on whatever service you logged them to; SM just needs its own
  copy for the panels above. (If a record came from a QRZ export, its
  QRZ logid is preserved on the QSO so a later edit can still find the
  right QRZ record — but import itself sends nothing.)

Import with `smctl import` — it stops the daemon, imports, and restarts
it for you (the daemon and importer can't share the database at once):

```
smctl import /path/to/your-export.adi
```

**Want the imported log pushed to a service? Use `--forward`.** This is the
rare case — e.g. you're seeding a log that genuinely *isn't* on QRZ yet and you
want it sent. Name the forwarder(s) to queue the whole batch for upload:

```
smctl import --forward qrz /path/to/log.adi          # one forwarder (by name)
smctl import --forward qrz,clublog /path/to/log.adi  # several
```

The names are your `forwarders[].name` values (matched case-insensitively); an
unknown name fails the import up front. Without `--forward`, the summary reports
`uploads: none`. If you only later subscribe to a service (say ClubLog) and want
your existing log sent then, that's a one-click bulk action in the logbook app —
not something you redo the import for.

The importer drives the same QSO submission path the live SPA uses
(field validation + atomic write + audit table all inherited). Throughput
is around 900 records per second on typical hardware — a few
thousand QSOs import in seconds. **One deliberate difference:** import
does *not* enforce that each record's `STATION_CALLSIGN` matches the
target logbook's callsign — a historical or mixed log can legitimately
carry QSOs made under a different callsign — though it still requires the
target logbook to exist. The live submit path (SPA + FT8) *does* enforce
the callsign match, so QSOs logged on-air always line up with the logbook
the forwarders upload to.

The importer prints a summary on exit: `{stored, duplicate,
errors}`. Errors are usually source-data problems (e.g. an `rst_rcvd`
field exceeding three characters) and are listed line-by-line so you
can patch the source ADIF and re-import. Re-imports are idempotent —
already-stored QSOs return as `duplicate`, not as errors.

The importer is the same binary as the daemon — there's no separate
package. `smctl import` handles the database hand-off for you: it stops
the daemon (if running), runs the import, and restarts it only if it was
running before. If you call the raw `smd import` instead, stop the daemon
yourself first (`smctl stop`) so the two aren't racing on the same SQLite
file. The import itself takes seconds.

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

1. **A CGO build of the daemon.** The shipped RPM is already a CGO +
   PocketFFT build, so live capture works out of the box — no separate
   build step. (Only the CGO-free fallback build can't open a sound
   device; if you somehow run that one it logs "capture unavailable;
   subsystem idle" and stays up.) For a local dogfood build the
   equivalent is `SM_FFT=pocketfft task deploy:local:dev`. **PocketFFT
   (not the plain gonum CGO build) is the supported flavour for live FT8
   *transmit*:**
   answering a CQ must finish decoding within ~1.7 s of the slot boundary
   to reply in time, and PocketFFT's faster decode stays comfortably inside
   that on typical hardware (gonum is borderline with OSD on — a missed slot
   simply retries the message a cycle later, never a lost QSO). See
   `docs/ft8.md` for the timing detail. Nothing else is needed to keep the
   timing healthy beyond not pinning the CPU during a QSO.
2. **Receive audio routed to a capture device** — typically the rig's
   USB audio codec, set on the rig profile (see "Connecting a rig (CAT)").

List the available capture devices from the running daemon — no dev tools
needed (`ft8-capture-probe` isn't in the RPM):

```
curl -s http://127.0.0.1:8080/v1/hardware | jq '.audio.capture'
```

Take the rig codec's `name`, set it as the active rig's `audio.rx`, and turn
FT8 on in `config.json`:

```json
"ft8": { "enabled": true, "enable_osd": true },
"rigs": [
  { "id": 1, "model": "yaesu-ftdx10", "port": "...",
    "audio": { "rx": "<capture name>", "tx": "<playback name>" } }
]
```

The daemon resolves the device **by name** (ADR 0028), so it survives index
reordering across reboots; an empty/absent `audio.rx` falls back to the system
default capture device. `enable_osd` (default **true**, so you can omit it)
turns on go-ft8's deeper OSD fallback decode — it recovers weak signals that
basic decoding misses, at a small CPU cost well within the 15-second budget;
set it to `false` only if you want the faster, shallower decode.

Enabling FT8 does **not** make the daemon hold the capture device the
whole time it runs. Capture is acquired only while you have the FT8
view open in the SPA, and released a few seconds after you leave it —
so an idle daemon with FT8 enabled leaves the sound device free for
other software. (The first decode after you open the view can take up
to one 15-second slot to appear.)

Restart the daemon (`smctl restart`), then open the **FT8 view** in the SPA —
capture starts when you do, and decodes appear in Band Activity. Per-decode log
lines are **debug**-level (a 12–16×/slot firehose), so they don't show at the
default log level. To capture the decode stream durably, enable the **decode
log** (`ft8.decode_log` — a JTDX `ALL.TXT`-style file; see `docs/ft8.md`); for a
one-off look, raise the daemon to debug level and:

```
journalctl --user -u smd -f | grep "ft8 decode"
```

To smoke-test capture without the daemon — **from a source checkout** (the probe
isn't shipped in the RPM) — run the live slot scheduler and print decodes per
UTC slot:

```
CGO_ENABLED=1 go run ./cmd/ft8-capture-probe -scheduler -slots=4 -device=1
```

Healthy output is one slot every 15 s on the UTC :00/:15/:30/:45
boundary, a non-trivial peak amplitude, and decoded messages. A
near-zero peak means the input is muted, the wrong device, or
unconnected.

---

## 9. Building the release RPM (maintainers)

The shipped RPM is the **CGO + PocketFFT** (live-FT8) build. Because that
build is dynamically linked to glibc — live capture `dlopen`s the audio
backend, which rules out a fully static binary — it must be built on the
**oldest glibc** you intend to support, or it won't start on older distros
(`version 'GLIBC_2.xx' not found`). `scripts/release.sh` does exactly that in
an **AlmaLinux 8** container (glibc 2.28 → runs on RHEL 8+, Fedora incl. 43,
recent openSUSE):

```
scripts/release.sh 2.0.0-alpha.2
```

It builds the SPA on the host, then compiles the binary and packages the RPM
inside the container (via nfpm — no `rpmbuild`). Output lands in
`build/release/*.rpm`. Needs `podman` (or `SM_CONTAINER_ENGINE=docker`) and
network on the **first** run to build the cached builder image.

Do **not** build a release with a bare `SM_FFT=pocketfft scripts/release-rpm.sh`
on your own (newer) Fedora — that binary is pinned to your machine's glibc and
may fail to start on the target. `release-rpm.sh` deliberately stays CGO-free by
default so an accidental local build can't ship a glibc-broken binary; the
container is what supplies the old-glibc baseline.

Only an RPM is built today (RPM distros — decided 2026-06-20, first external
deploy). A portable tarball / `.deb` for Debian/Ubuntu isn't built yet; nfpm can
emit them from the same `nfpm.yaml` when a non-RPM target appears.

---

## 10. Uninstall

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
