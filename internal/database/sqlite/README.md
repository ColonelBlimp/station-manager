# Database — SQLite (Client-side schema)

Purpose
- SQLite is used only on the client side (desktop app). It prioritizes local usability, soft deletes, and storing server linkage material (uid and full API key) alongside logbook metadata.

Key differences vs Postgres (server)
- Deletes
  - SQLite: soft deletes are supported via `deleted_at` on both `logbook` and `qso`.
  - Postgres: hard deletes with cascading from `logbook` to QSOs and API keys.
- API keys
  - SQLite: the full API key (`prefix.secretHex`) may be stored on the `logbook` row for local use; there is no separate `api_keys` table client-side.
  - Postgres: only a hash (or HMAC) of the `secretHex` is stored in `api_keys`; full keys are never persisted.
- Identifiers
  - Both use a sequential internal `id` per table.
  - `uid` is a server-issued opaque identifier on the `logbook`. In SQLite this field is optional (NULL until registration), with a unique index when present.

Schema overview (high level)
- logbook
  - Columns: id, created_at, modified_at, deleted_at, name, callsign, description, uid (nullable), api_key (nullable)
  - Uniqueness: name is unique; uid and api_key are unique when present (partial unique indexes)
  - Notes: `api_key` stores the full key client-side; treat it as sensitive and avoid logging

- qso
  - Columns: id, created_at, modified_at, deleted_at, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, additional_data (JSON), logbook_id
  - Constraints: JSON field must not duplicate core columns; frequency and date/time fields validated by CHECKs
  - Foreign key: `logbook_id` REFERENCES logbook(id) ON DELETE RESTRICT (soft-delete friendly)
  - Indexes: call, band, country, (qso_date, time_on), logbook_id; optional partial indexes for active rows (deleted_at IS NULL)

Soft delete guidance
- Prefer filtering by `deleted_at IS NULL` for active sets.
- Partial indexes are provided to keep these queries efficient.

Migrations
- 0001: creates `logbook` and `qso`, adds partial unique indexes on `uid` and `api_key`, and soft-delete-friendly indexes and triggers.
- 0002: no-op for `api_keys` (client stores full key on `logbook`; no separate table).

Security notes (client-side)
- Full API key is stored locally to authenticate against the server; consider encrypting at rest according to your threat model.
- Never log the full API key; redact in UI and logs.

See also
- Postgres (server) schema and policies: ../postgres/README.md
- API key high-level design: ../../apikey/README.md

