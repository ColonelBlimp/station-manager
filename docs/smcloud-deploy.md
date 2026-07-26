# SM Cloud — deployment runbook (ADR 0040 S6)

Stand up the operator's own SM Cloud backup service (`cmd/smcloud`) on a VPS,
wire the daemon's `smcloud` forwarder to it, and verify the first backfill.
The service is a single **fully static** Go binary (no CGO, no runtime deps)
in front of a Postgres database, behind a TLS reverse proxy. P1 is
single-tenant: one callsign, one bearer token.

Design + wire contract: [`v2-design/sm-cloud-p1.md`](v2-design/sm-cloud-p1.md)
+ ADR 0040. Deploy artifacts live in [`deploy/smcloud/`](../deploy/smcloud/).

**Two phases (decided 2026-07-17):** first a **LAN staging deploy** on a
separate machine on the shack network — cheap, immediate resilience against
the shack machine's disk/OS failure, and the place to test/soak/fault-drill/
harden — then the **VPS deploy** (the numbered sections below), which adds
the off-site half: a house-level event (lightning, fire, theft, a surge
taking both machines) defeats the LAN copy. Nothing done in phase 1 commits
you to anything — see "Moving to the VPS" below.

---

## Phase 1 — LAN staging deploy

A complete, self-contained walkthrough. **No TLS and no Caddy in this
phase** — Caddy is the VPS deploy's reverse proxy for HTTPS (section 4,
phase 2 only); on your own LAN, smcloud serves plain HTTP directly. Two
machines are involved; every step below says which one it runs on:

- the **dev/shack machine** — builds the RPM, runs the `smd` daemon;
- the **staging box** — runs smcloud + Postgres (an RPM distro, e.g. Fedora).

### 1.1 Build + copy the RPM (dev machine)

```bash
task rpm:smcloud
scp build/release/smcloud.x86_64.rpm <staging-box>:
```

(Non-x86 staging box, e.g. a Pi: `SMCLOUD_ARCH=arm64 task rpm:smcloud` →
`smcloud.aarch64.rpm`; non-RPM OS: `task build:smcloud` for a raw binary.)

### 1.2 Postgres (staging box)

If Postgres isn't installed yet, do section 2's install/initdb lines first.
Then create the service's role + database (invent the DB password here —
it only ever lives on this box, inside the DSN in step 1.4):

```bash
sudo -u postgres psql -c "CREATE ROLE smcloud LOGIN PASSWORD '<pick-a-db-password>'"
sudo -u postgres psql -c "CREATE DATABASE smcloud OWNER smcloud"
```

No schema/migration steps — smcloud applies its embedded schema at boot.

**Fedora/RHEL gotcha (hit on the first staging deploy, 2026-07-18):** the
distro-default `pg_hba.conf` authenticates TCP connections with `ident`,
so the DSN login fails — smcloud restart-loops, and testing the DSN
directly shows `FATAL: Ident authentication failed for user "smcloud"` —
even though the role and password are correct. Fix:

```bash
sudo nano /var/lib/pgsql/data/pg_hba.conf
# (path wrong on your distro? locate it: sudo -u postgres psql -c "SHOW hba_file")
```

Change the METHOD column on the two TCP host lines from `ident` to
`scram-sha-256` — leave the `local … peer` line alone (it's what lets
`sudo -u postgres psql` keep working):

```
host    all    all    127.0.0.1/32    scram-sha-256
host    all    all    ::1/128         scram-sha-256
```

Reload and test the DSN directly (this prints the real error, unlike the
service's exit code):

```bash
sudo systemctl reload postgresql
psql "postgres://smcloud:<db-password>@127.0.0.1:5432/smcloud?sslmode=disable" -c "select 1"
```

If it now fails with `password authentication failed`, the role's stored
hash predates scram — re-store it:
`sudo -u postgres psql -c "ALTER ROLE smcloud PASSWORD '<db-password>'"`
and test again. Once `select 1` returns a row, restart the service.

### 1.3 Generate the bearer token (either machine)

The token is a shared secret you invent — no account or registration
anywhere. It goes in exactly TWO places: smcloud's env file (step 1.4,
"only accept callers presenting this") and the daemon's forwarder
credential (step 1.5, "present this when pushing"). Generate it once and
keep it handy for both:

```bash
openssl rand -base64 32
```

Don't hand-invent a short one: the service refuses to start with the
shipped `CHANGE_ME_TOKEN` placeholder or any token under 32 characters.

If the two ever differ, every push gets 401 and the queue retries until
you fix it — nothing is lost, but nothing backs up either.

### 1.4 Install + configure smcloud (staging box)

```bash
sudo dnf install -y ./smcloud.x86_64.rpm   # binary + systemd unit + env-file skeleton
sudoedit /etc/smcloud/smcloud.env
```

Set all four values in the env file:

| Variable | Value for LAN staging |
|---|---|
| `SMCLOUD_LISTEN` | `0.0.0.0:8091` (LAN posture — MUST be set explicitly; the binary and the skeleton both default to loopback, the VPS posture) |
| `SMCLOUD_DSN` | as shipped, with `CHANGE_ME_DB_PASSWORD` → the password from step 1.2 |
| `SMCLOUD_CALLSIGN` | your callsign (tenant 1 — owns the backed-up log) |
| `SMCLOUD_TOKEN` | the token from step 1.3 |

**Additional tenants (multi-tenancy, ADR 0052 milestone 1):** add a numbered
pair per tenant to the same env file, then restart —

```bash
# in /etc/smcloud/smcloud.env (N in 2..32; indices need not be contiguous):
#   SMCLOUD_CALLSIGN_2=7Q8AC
#   SMCLOUD_TOKEN_2=<openssl rand -base64 32 — that tenant's OWN token>
sudo systemctl restart smcloud
journalctl -u smcloud -n 5   # one "tenant provisioned" line per tenant
```

Each tenant's token goes into **that operator's** daemon forwarder
credentials (their config SPA → Forwarding → SM Cloud backup). Boot refuses
half-pairs, unparseable `SMCLOUD_CALLSIGN_*`/`SMCLOUD_TOKEN_*` variables,
duplicate tokens, and duplicate callsigns — a malformed env file fails loudly
rather than silently dropping or merging a tenant. Rotating one tenant's
token touches only its line + a restart; removing a pair revokes that
tenant's access (their data stays — it's a backup service).

Then start it, check health, and open the firewall port:

```bash
sudo smcloudctl enable      # start on boot (Restart=on-failure is already in the unit)
sudo smcloudctl start       # confirmed "smcloud Started." once it's actually up
curl -s http://127.0.0.1:8091/v1/health    # → {"status":"ok","db":"ok"}
sudo firewall-cmd --add-port=8091/tcp --permanent && sudo firewall-cmd --reload
```

`smcloudctl` (shipped in the RPM, the smcloud counterpart of `smctl`) wraps
`systemctl` with a verified result line: `start`/`stop`/`restart`/`enable`/
`disable`/`status`, and its `start`/`restart` only report success once the
process is confirmed running — a boot-validation failure (bad token/DSN)
shows a red error pointing at `journalctl`, not a false "started". Plain
`sudo systemctl … smcloud` works too.

> **Updating the binary:** after installing a new RPM (`sudo dnf install`),
> run **`sudo smcloudctl restart`** — a plain `systemctl enable --now` does
> NOT restart an already-running unit, so the old binary would keep serving
> (the same trap the Caddy step calls out in §4). `smcloudctl restart` does a
> real restart and confirms the new process is up; check the live version
> with `curl -s http://127.0.0.1:8091/v1/version`.

Plain-HTTP-with-bearer-token is acceptable on your own network — it is
NOT the internet posture. **Never port-forward 8091 through the router.**
Also give the box a stable address (static IP or DHCP reservation) so the
forwarder URL doesn't strand on lease churn.

### 1.5 Wire the daemon (shack machine)

Unlike QRZ/ClubLog, the smcloud forwarder is **not pre-seeded** into
`config.json` (it has no canonical URL — yours is the only instance), so
the entry must be added, either way below. Then restart `smd`.

**Via the config SPA** — Forwarding → **Add → SM Cloud backup**:

- **Service URL** — `http://<staging-box-ip>:8091`
- **Bearer token** — the step 1.3 value
- **Cloud logbook name** — leave empty (`main`)

…and enable it.

**Or by hand** — add this entry to the `forwarders` array in
`$SM_WORKING_DIR/config.json` (stop `smd` first; it writes the file):

```json
{
  "name": "smcloud",
  "type": "smcloud",
  "enabled": true,
  "credentials": {
    "url": "http://<staging-box-ip>:8091",
    "token": "<the step 1.3 token>",
    "logbook": "main"
  }
}
```

`logbook` may be omitted (defaults to `main`). Everything else —
`action_filter` (all three actions), `tick_interval_sec` (120),
`batch_size` (5), `retry` — is filled with defaults at startup; set them
only to override.

**Backfill drain speed:** the worker defaults (one batch of 5 every 120 s)
are tuned for a slow, flaky internet link — a first backfill of a few
thousand QSOs would take *days* at that pace. On the LAN, add
`"tick_interval_sec": 10, "batch_size": 200` to the entry above
(≈1,200 rows/min — a 5,500-QSO logbook drains in under 5 minutes; validated
on the first staging deploy, 2026-07-18). Leave the faster settings for the
LAN phase, or revert to defaults once the backfill is done — steady-state
traffic is a row or two per QSO, where the defaults are fine either way.
While a drain is in progress, don't hammer `POST /v1/smcloud/reconcile`:
each call re-enqueues everything not yet in the cloud, so mid-drain calls
pile duplicate (idempotent, but wasteful) work onto the queue.

On startup the daemon spawns the forwarder worker AND the reconciler;
**the reconciler's first pass (≈2 min after startup) backfills the entire
logbook automatically** — no manual export/import.

### 1.6 Verify (shack machine)

```bash
curl -s -X POST http://127.0.0.1:8080/v1/smcloud/reconcile | jq
# First run: in_sync:false + enqueued_upserts:N (the backfill being queued).
# When the worker has drained it, re-run and expect:
#   {"in_sync": true, "local_count": N, "cloud_count": N, ...}
```

From here every logged/edited/deleted QSO pushes within one worker tick.
Section 7's operations notes apply as-is — the `pg_dump` cron matters MORE
here, since the LAN box is likely older hardware.

**What to exercise while staging** (the point of the phase — fault drills
beat throughput numbers, which a LAN inflates anyway). *First full campaign
run 2026-07-18, all passed, live on-air throughout (zero QSOs lost; final
audit CLEAN at 5,590/5,590):*

- Pull the LAN cable mid-backfill → the queue must hold (ADR 0038
  unreachable-retries-forever), then drain on reconnect.
- Stop Postgres mid-push → transient classification, worker retries.
- Restart smcloud during a large backfill → the idempotent UUID upsert
  absorbs the replay; `POST /v1/smcloud/reconcile` converges to `in_sync`.
- Edit + delete QSOs with the LAN box powered off → power it on → the hourly
  reconcile (or an on-demand pass) self-heals the drift.
- A restore drill against a scratch `SM_WORKING_DIR` (section 7).
- **Field-fidelity audit:** `scripts/smcloud-audit.py` (dev machine) fetches
  the cloud export and deep-compares every record against the local DB —
  UUID parity, core columns, the full `additional_data` blob, and the
  reconcile `modified_at` contract. Expect `RESULT: CLEAN`; exit 1 on any
  mismatch. First run 2026-07-18: 5,545/5,545 clean.

**Moving to the VPS later:** local is authoritative, so there is no data
migration — stand up the VPS per the sections below, change the forwarder's
Service URL (+ token), and the reconciler's first pass rebuilds the cloud
copy from scratch. (`pg_dump`/restore across instead if you want the LAN
box's tombstone history carried over.) Decommission the LAN box or keep it
as a second destination later — P1 wires one smcloud forwarder, so it's
either/or for now.

---

## 0. Decisions (make once)

| Decision | Guidance |
|---|---|
| **VPS provider + region** | Any small instance (1 vCPU / 1 GB is plenty — the load is one operator's QSO pushes). Pick a **well-connected region, not Malawi** (ADR 0040): the point is surviving local disaster, and the flaky-link tolerance lives in the daemon's forever-retry queue, not the server. EU (close to the QRZ/ClubLog paths) is a fine default. |
| **Hostname** | A subdomain of the project domain, e.g. `cloud.station-manager.org`, with an A/AAAA record → the VPS. Needed before Caddy can issue TLS. |
| **Postgres** | Self-hosted distro package on the same VPS is the P1 recommendation (zero extra cost, one machine to manage; the DB is small). A managed instance works identically — put its DSN in the env file with `sslmode=require`. |
| **TLS proxy** | Caddy (recommended — automatic Let's Encrypt, 4-line config) or nginx + certbot. smcloud itself listens on loopback only and never terminates TLS. |

## 1. Build the artifact (dev machine)

```bash
task rpm:smcloud            # → build/release/smcloud.x86_64.rpm (version-stamped)
scp build/release/smcloud.x86_64.rpm box:
```

The RPM carries the static binary (`/usr/bin/smcloud`), the system unit
(`/usr/lib/systemd/system/smcloud.service`), and an `/etc/smcloud/smcloud.env`
skeleton (0600, `noreplace` — upgrades never clobber the edited file). Built
by `scripts/smcloud-rpm.sh` + `nfpm-smcloud.yaml`; `SMCLOUD_ARCH=arm64` for
an aarch64 box. The binary is pure Go and fully static — no glibc floor, so
it builds on the dev box with no container dance.

Non-RPM target: `task build:smcloud` → scp `build/bin/smcloud` to
`/usr/bin/smcloud` and hand-copy the unit + env file from
[`deploy/smcloud/`](../deploy/smcloud/) (see section 3).

## 2. Postgres (VPS)

```bash
sudo dnf install postgresql-server && sudo postgresql-setup --initdb   # or apt equivalent
sudo systemctl enable --now postgresql
sudo -u postgres psql -c "CREATE ROLE smcloud LOGIN PASSWORD '<db-password>'"
sudo -u postgres psql -c "CREATE DATABASE smcloud OWNER smcloud"
```

No manual migrations: smcloud applies its embedded schema at boot
(`store.Migrate`, the same files/tracking table as the dev CLI).

Already have Postgres installed and running? Skip the first two lines — just
create the role + database. Either way, check `pg_hba.conf` allows password
logins for `127.0.0.1` — the distro default is `ident`-only, which rejects
the DSN login; the exact fix is in Phase 1 step 1.2.

## 3. The service (VPS)

```bash
sudo dnf install -y ./smcloud.x86_64.rpm     # binary + unit + env-file skeleton
sudoedit /etc/smcloud/smcloud.env            # DSN password, callsign, token
# Token: openssl rand -base64 32  (the SAME value goes into the daemon's forwarder)
sudo systemctl enable --now smcloud
curl -s http://127.0.0.1:8091/v1/health      # → {"status":"ok","db":"ok"}
```

The unit runs as a transient `DynamicUser` with full sandboxing — smcloud
keeps no local state, so nothing needs a real account or a writable path.

Non-RPM box (from `deploy/smcloud/` in the checkout): copy
`smcloud.env.example` → `/etc/smcloud/smcloud.env` (`chmod 0600`) and
`smcloud.service` → `/etc/systemd/system/`, `systemctl daemon-reload`, then
the same edit + enable.

## 4. TLS + per-IP rate limiting (VPS)

**Rate limiting is two layers (decided 2026-07-18):** smcloud's own
in-process bound ships in the binary (accept-time connection cap + request
cap; over-limit → 503 + Retry-After; tune with `SMCLOUD_MAX_CONCURRENT`) —
nothing to install. The **per-IP** layer runs in Caddy — and the stock distro
package does NOT include a rate-limit handler, and `xcaddy build` only drops
a `caddy` binary in the current directory (it does not touch the
systemd-managed one). So the order matters: install the custom binary and
verify the module BEFORE enabling the public listener.

```bash
# 1. Distro package first — it provides the systemd unit, user, and paths:
sudo dnf install caddy                                     # or apt install caddy

# 2. Build a Caddy with the rate-limit plugin (any machine with Go — build on
#    the dev box and scp if the VPS has no toolchain; same-arch binary), or
#    download one from caddyserver.com/download with http.handlers.rate_limit
#    ticked:
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
xcaddy build --with github.com/mholt/caddy-ratelimit       # → ./caddy in CWD

# 3. Install it over the packaged binary, then PROVE the module is there:
sudo install -m 0755 ./caddy /usr/bin/caddy
caddy list-modules | grep rate_limit                       # → http.handlers.rate_limit

# 4. Config: append deploy/smcloud/Caddyfile.example (your hostname) to
#    /etc/caddy/Caddyfile, UNCOMMENT its rate_limit block (the handler now
#    exists), and validate:
caddy validate --config /etc/caddy/Caddyfile

# 4b. The access-log directory must exist and be writable by the caddy user
#     BEFORE reload — Caddy fails to provision a file logger it cannot open,
#     and a Caddy that won't start takes the whole service offline. The distro
#     package normally creates it; verify rather than assume, and check the
#     packaged unit does not confine /var/log (ProtectSystem=strict would):
sudo install -d -o caddy -g caddy -m 0750 /var/log/caddy
systemctl show caddy -p ProtectSystem -p ReadWritePaths

# 5. Only now expose the listener. RESTART, not `enable --now`: the
#    Debian/Ubuntu package auto-starts stock Caddy during install, and
#    `enable --now` does not restart a running unit — the old binary would
#    keep serving. restart covers both families (starts it where it wasn't
#    running, swaps the binary where it was):
sudo systemctl enable caddy
sudo systemctl restart caddy
curl -s https://cloud.station-manager.org/v1/health        # end-to-end through TLS
```

**A package upgrade clobbers the custom binary** (`dnf upgrade caddy`
reinstalls the stock one, and Caddy then refuses to start on the unknown
`rate_limit` directive — fail-loud, not silently unlimited). After any caddy
package upgrade, repeat step 3 and restart (step 5). `dnf versionlock caddy`
avoids the surprise if upgrades are automated.

Deliberately skipping the plugin (e.g. LAN staging behind a firewall): leave
the `rate_limit` block commented — config validates against stock Caddy and
the in-process caps are then the only limiter. Safe for the process, but on
the open internet abuse then eats the shared cap instead of being turned
away per-IP, so the plugin is the Phase-2 posture.

## 5. Wire the daemon (shack machine)

Config SPA → **Forwarding → Add → SM Cloud backup**:

- **Service URL** — `https://cloud.station-manager.org`
- **Bearer token** — the `SMCLOUD_TOKEN` value
- **Cloud logbook name** — leave empty (`main`)

Enable it and restart `smd`. On startup the daemon spawns the forwarder worker
AND the reconciler (hourly + 2-min startup delay). **The reconciler's first
pass backfills the entire logbook automatically** — no manual export/import.

## 6. Verify

```bash
# Shack machine — run one reconcile pass now instead of waiting for the tick:
curl -s -X POST http://127.0.0.1:8080/v1/smcloud/reconcile | jq
# First run: in_sync:false + enqueued_upserts:N (the backfill). The worker
# drains N rows at its tick/batch pace; when done, re-run and expect:
#   {"in_sync": true, "local_count": N, "cloud_count": N, ...}
```

`in_sync: true` = every live QSO's (uuid, modified_at) hash-matches the cloud.
From here every logged/edited/deleted QSO pushes within one worker tick, and
the hourly reconcile self-heals anything a flaky link drops.

## 7. Operations

- **Logs — two places, deliberately.**
  - **Application:** `journalctl -u smcloud -f` (slog to stderr; journald owns
    rotation). smcloud writes NO files — `DynamicUser=yes` +
    `ProtectSystem=strict` leave it no writable path, which is why journald is
    the sink rather than an app-side rotating file like `smd` uses.
  - **Access:** `/var/log/caddy/smcloud-access.log` (JSON, 10 MiB × 5, 30 days
    — Caddy's own roll settings). This is the ONLY request log in the stack:
    smcloud has no request-logging middleware, so without it nothing records
    method, path, status, latency or client IP, and the per-IP rate limiting
    in §4 is unobservable. Caddy is the right layer — it sees the real client
    IP, which smcloud never does behind the proxy.
    ```bash
    # recent non-2xx, newest last
    sudo jq -c 'select(.status >= 400) | {ts,status,uri:.request.uri,ip:.request.remote_ip}' \
      /var/log/caddy/smcloud-access.log | tail -20
    ```
    The `Authorization` header is **deleted** from the log by an explicit
    filter, not left to Caddy's default credential redaction — smcloud's auth
    is a long-lived bearer token and an access log is a long-lived file.
- **Health:** `/v1/health` (unauthenticated; checks the DB ping).
- **Back up the backup:** the local log DB remains the authority, but a VPS
  loss shouldn't cost the history either —
  `sudo -u postgres pg_dump smcloud | gzip > /var/backups/smcloud-$(date +%F).sql.gz`
  on a daily cron/timer, ideally synced off-box.
- **Upgrade:** `task rpm:smcloud` → scp → `sudo dnf upgrade -y ./smcloud.x86_64.rpm`
  → `sudo systemctl restart smcloud` (no scriptlets — the restart is yours; the
  env file is `noreplace`, so the edited token/DSN survives). Rebuilding the
  same dirty commit keeps the same NVR — install that with
  `sudo rpm -Uvh --replacepkgs`. Schema migrations apply automatically at boot.
- **Restore drill** (worth one rehearsal — see `smd restore` in
  sm-cloud-p1.md S5): on the shack machine with the daemon stopped,
  `smd restore -dry-run` fetches the export and reports counts without
  writing; drop `-dry-run` to restore into the default logbook. Existing rows
  skip, so it is safe to run against a live database too.
- **Token rotation:** new `openssl rand -base64 32` → `/etc/smcloud/smcloud.env`
  → `systemctl restart smcloud` → update the daemon's forwarder credential →
  restart `smd`.

## Not in P1

Multi-tenant onboarding (7Q8AC) waits for the security assessment (ADR 0040 §
Identity — trust-on-provisioning, per-tenant tokens; the server's token→tenant
map already makes it a data change). The pile-up "am I being heard?" status
site is a separate P3/P4 feature, not this service.
