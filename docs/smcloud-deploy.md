# SM Cloud — deployment runbook (ADR 0040 S6)

Stand up the operator's own SM Cloud backup service (`cmd/smcloud`) on a VPS,
wire the daemon's `smcloud` forwarder to it, and verify the first backfill.
The service is a single **fully static** Go binary (no CGO, no runtime deps)
in front of a Postgres database, behind a TLS reverse proxy. P1 is
single-tenant: one callsign, one bearer token.

Design + wire contract: [`v2-design/sm-cloud-p1.md`](v2-design/sm-cloud-p1.md)
+ ADR 0040. Deploy artifacts live in [`deploy/smcloud/`](../deploy/smcloud/).

---

## 0. Decisions (make once)

| Decision | Guidance |
|---|---|
| **VPS provider + region** | Any small instance (1 vCPU / 1 GB is plenty — the load is one operator's QSO pushes). Pick a **well-connected region, not Malawi** (ADR 0040): the point is surviving local disaster, and the flaky-link tolerance lives in the daemon's forever-retry queue, not the server. EU (close to the QRZ/ClubLog paths) is a fine default. |
| **Hostname** | A subdomain of the project domain, e.g. `cloud.station-manager.org`, with an A/AAAA record → the VPS. Needed before Caddy can issue TLS. |
| **Postgres** | Self-hosted distro package on the same VPS is the P1 recommendation (zero extra cost, one machine to manage; the DB is small). A managed instance works identically — put its DSN in the env file with `sslmode=require`. |
| **TLS proxy** | Caddy (recommended — automatic Let's Encrypt, 4-line config) or nginx + certbot. smcloud itself listens on loopback only and never terminates TLS. |

## 1. Build the binary (dev machine)

```bash
task build:smcloud          # → build/bin/smcloud (static linux/amd64, version-stamped)
scp build/bin/smcloud vps:/usr/local/bin/smcloud
```

Static = no glibc floor, no container dance — any Linux VPS runs it.

## 2. Postgres (VPS)

```bash
sudo dnf install postgresql-server && sudo postgresql-setup --initdb   # or apt equivalent
sudo systemctl enable --now postgresql
sudo -u postgres psql -c "CREATE ROLE smcloud LOGIN PASSWORD '<db-password>'"
sudo -u postgres psql -c "CREATE DATABASE smcloud OWNER smcloud"
```

No manual migrations: smcloud applies its embedded schema at boot
(`store.Migrate`, the same files/tracking table as the dev CLI).

## 3. The service (VPS)

```bash
sudo mkdir -p /etc/smcloud
sudo cp smcloud.env.example /etc/smcloud/smcloud.env      # from deploy/smcloud/
sudo chmod 0600 /etc/smcloud/smcloud.env
sudoedit /etc/smcloud/smcloud.env                         # DSN password, callsign, token
# Token: openssl rand -base64 32  (the SAME value goes into the daemon's forwarder)
sudo cp smcloud.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now smcloud
curl -s http://127.0.0.1:8091/v1/health                   # → {"status":"ok","db":"ok"}
```

The unit runs as a transient `DynamicUser` with full sandboxing — smcloud
keeps no local state, so nothing needs a real account or a writable path.

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
- **Upgrade:** `task build:smcloud` → scp over the old binary →
  `systemctl restart smcloud`. Schema migrations apply automatically at boot.
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
