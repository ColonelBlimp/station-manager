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

Same binary, same unit, same Postgres steps as the VPS sections below, with
these deltas:

- **Bind the LAN interface, skip TLS.** There is no reverse proxy in this
  phase: set `SMCLOUD_LISTEN=0.0.0.0:8091` (or the box's specific IP) in the
  env file, and skip section 4 entirely. Bearer-token-over-plain-HTTP is
  acceptable on your own network — it is NOT the internet posture; do not
  port-forward this listener through the router. Open 8091 in the box's own
  firewall (`firewall-cmd --add-port=8091/tcp --permanent` or ufw equivalent).
- **Give the box a stable address** — a static IP or DHCP reservation /
  LAN hostname — so the daemon's forwarder URL (`http://192.168.x.y:8091`)
  doesn't strand on lease churn.
- **Non-x86 box (e.g. a Pi):** `SMCLOUD_ARCH=arm64 task rpm:smcloud`
  (→ `smcloud.aarch64.rpm`), or `SMCLOUD_ARCH=arm64 task build:smcloud` for
  a raw binary on a non-RPM OS.
- Everything else is identical: Postgres (section 2), env file + unit
  (section 3), daemon wiring (section 5 — with the `http://…:8091` URL),
  verify (section 6), operations (section 7 — the `pg_dump` cron matters
  MORE here, since the LAN box is likely older hardware).

**What to exercise while staging** (the point of the phase — fault drills
beat throughput numbers, which a LAN inflates anyway):

- Pull the LAN cable mid-backfill → the queue must hold (ADR 0038
  unreachable-retries-forever), then drain on reconnect.
- Stop Postgres mid-push → transient classification, worker retries.
- Restart smcloud during a large backfill → the idempotent UUID upsert
  absorbs the replay; `POST /v1/smcloud/reconcile` converges to `in_sync`.
- Edit + delete QSOs with the LAN box powered off → power it on → the hourly
  reconcile (or an on-demand pass) self-heals the drift.
- A restore drill against a scratch `SM_WORKING_DIR` (section 7).

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
create the role + database, and check `pg_hba.conf` allows password logins
(`scram-sha-256`/`md5`) for `127.0.0.1` — a pre-existing install may be
`ident`/`peer`-only, which rejects the DSN login.

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

## 4. TLS (VPS)

```bash
sudo dnf install caddy                                     # or apt install caddy
# append deploy/smcloud/Caddyfile.example (with your hostname) to /etc/caddy/Caddyfile
sudo systemctl enable --now caddy
curl -s https://cloud.station-manager.org/v1/health        # end-to-end through TLS
```

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

- **Logs:** `journalctl -u smcloud -f` (slog to stderr). Health: `/v1/health`
  (unauthenticated; checks the DB ping).
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
