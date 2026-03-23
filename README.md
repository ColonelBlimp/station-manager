# Station Manager: config package

Service responsible for loading and exposing application configuration.

## Generated config.json

On first run, if no `config.json` is present in the working directory, the service writes a default, pretty-printed file with restrictive permissions (0640).

### Selecting default datastore via environment

You can choose which datastore block gets written into a newly generated `config.json` using the environment variable `SM_DEFAULT_DB`.

Accepted values (case-insensitive):
- `sqlite` (default)
- `postgres`, `postgresql`, `pg`

Example:

```sh
# Use PostgreSQL defaults when generating a fresh config.json
export SM_DEFAULT_DB=pg
```

If the file already exists, the variable is ignored and the existing config is loaded.

### Working directory resolution

The config service resolves its working directory via:
1. The injected `WorkingDir` field if provided
2. The `SM_WORKING_DIR` env var
3. The directory of the running executable

The resolved directory must exist.

## Programmatic overrides (advanced)

If you pre-seed `Service.AppConfig.LoggingConfig` before calling `Initialize()`, the config service preserves that logging section instead of the one loaded from `config.json`. This is intentional to support:
- Unit/integration tests that need to inject invalid levels or specific writer options
- Programmatic bootstraps that want to override logging without modifying files

Notes:
- The sentinel for preserving is a non-empty `LoggingConfig.Level`.
- Only the logging section is preserved; other sections (e.g., datastore) still load from file.
- For typical usage, leave `AppConfig` empty and rely on the file.

## Defaults and tuning guidance

The defaults aim for sensible behavior out of the box and should be tuned per environment and workload.

### SQLite (modernc.org/sqlite)
- MaxOpenConns: 4 (readers benefit; writes are serialized)
- MaxIdleConns: 4
- ConnMaxLifetime: 0 (no forced recycle)
- ConnMaxIdleTime: 5m
- ContextTimeout: 5s
- TransactionContextTimeout: 10s
- Options:
  - `_journal_mode=WAL`
  - `_busy_timeout=5000` (consider 10000 under heavier write contention)
  - `_foreign_keys=on`

Notes:
- WAL enables concurrent readers. Too many connections can increase lock contention; start small and measure.
- Transaction timeout should be longer than typical busy periods to avoid spurious cancellations.

### PostgreSQL
- MaxOpenConns: 10 (consider 20+ for higher concurrency)
- MaxIdleConns: 5–10
- ConnMaxLifetime: 30–60m (keep below server rotation)
- ConnMaxIdleTime: 5–10m
- ContextTimeout: 5–10s
- TransactionContextTimeout: 15–30s
- SSLMode: `disable` for local dev; use `require`/`verify-ca`/`verify-full` in production

Notes:
- Tune pool sizes based on CPU, workload mix, and DB server capacity.
- Longer lifetimes reduce churn; align with server-side settings.

## API

- `Initialize() error` — loads (or creates) `config.json` in the working directory; idempotent.
- `DatastoreConfig() (types.DatastoreConfig, error)` — returns the datastore settings.
- `LoggingConfig() (types.LoggingConfig, error)` — returns the logging settings.

Downstream services (database, logging) validate their respective sections when they initialize.
