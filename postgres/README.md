# Database — PostgreSQL (High‑level schema)

This document summarizes the high‑level schema and design decisions for the PostgreSQL backend. It mirrors the system‑wide API key and UID design and avoids implementation details.

Core entities
- Logbook
  - Internal primary key: `id` (sequential, used for joins)
  - External identifier: `uid` (immutable, opaque; used in client/server protocols)
  - Metadata: `name`, `callsign`, `description`, `created_at`, `modified_at`

- API keys (per logbook)
  - Each logbook can have one or more API keys
  - Key format presented to clients: `prefix.secretHex`
  - prefix: independent random hex string (e.g., 12–16 chars); not derived from `secretHex`
  - secretHex: 64 hex chars from 32 random bytes
  - Stored digest: hash of `secretHex` (e.g., SHA‑512 hex) or HMAC with a server‑side pepper
  - Operational metadata: created/last used, expires/revoked, optional scopes and allowed IPs
  - Lookup pattern: resolve `uid` → `logbook_id`, then `(logbook_id, key_prefix)`

- QSO
  - Each QSO belongs to exactly one logbook
  - Core fields: call, band, mode, freq, qso_date, time_on/off, rst_sent/rcvd, optional country
  - Flexible `additional_data` JSONB for extra fields, with a guard against duplicating core fields

Deletion policy
- No soft deletes in the server‑side schema.
- Deleting a logbook cascades hard deletes to its QSOs and API keys (non‑recoverable).
- Deleting a QSO is non‑recoverable.
- At most one active (non‑revoked) API key per logbook is enforced by a partial unique index; rotate by creating a new key before revoking the old one, or revoke then create.

High‑level flows
- Registration
  - Client registers a logbook; server creates `uid` and issues an API key (`prefix.secretHex`)
  - Client stores `uid` and the full API key locally

- Authentication and authorisation
  - Requests include `Authorization: ApiKey <prefix>.<secretHex>` and `uid`
  - Server resolves `uid` to `logbook_id`, finds an active key by `(logbook_id, key_prefix)`, and validates digest

- Rotation and revocation
  - Keys can be revoked or rotated; policy can allow multiple active keys or restrict to one

Integrity rules (application‑enforced)
- Logging station callsign must match the logbook’s callsign on writes
- Contacted station callsign (`qso.call`) is unconstrained relative to the logbook’s callsign

Notes
- Sequential IDs remain internal; `uid` is the stable external reference
- Prefix is independent random for zero leakage; it is not derived from `secretHex`
- SQL models are generated from the migrations and can be re‑generated after schema changes
