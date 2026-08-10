package config

// This file is the single discoverability point for config defaults (config.md
// §14) — "what's the default (or ceiling) for X, and is it overridable?" answered
// in one place. Defaults reach a loaded Config through FOUR mechanisms; the
// default-fill values live below, and the rest are indexed here:
//
//	(a)  default-fill — applyDefaults() replaces a zero value with a const below.
//	     Every one is operator-overridable in config.json.
//	(a′) first-run seed defaults — set in DefaultConfig(), NOT applyDefaults, for
//	     fields where false/empty is a legitimate operator choice (so they can't be
//	     re-applied every Load): logging.with_timestamp / file_logging /
//	     log_file_compress and smtp.starttls (all default true), plus the
//	     disabled-but-prepopulated hamnut + QRZ provider URL templates.
//	     [§14b DEFERRED 2026-06-13: converting these to *T would fold (a′) into the
//	     single (a) pass, but it churns the shared types.LoggingConfig/SmtpConfig
//	     consumed by the logging + email subsystems (and reddens their tests) for a
//	     cosmetic gain — the DefaultConfig-seed is the standard Go idiom for a
//	     default-true bool that must not be re-stomped each Load. Accepted as-is.]
//	(b)  serve/read-time resolve — types/ft8.go: ResolveFt8Display,
//	     ResolveFt8Frequencies, ResolveFt8CallerAnswerMode, ResolveFt8MaxRepeats
//	     (defaults applied at /v1/config GET, keeping config.json sparse for SPA prefs).
//	(c)  nil-pointer-means-default — Server.ServeSPA, Ft8.EnableOSD,
//	     Ft8.TX.Occupancy.GuardMarginHz (*T distinguishes unset from explicit 0/false).
//
// SAFETY CEILINGS (non-overridable maxima — NOT defaults; clamped where the
// resource is owned, never operator-overridable beyond the limit):
//
//	- tune power ≤ 40 W, duration ≤ 30 s, restore-settle ≤ 2 s — clamped at
//	  bridge.Service construction (internal/bridge).
//	- FT8 TX hard auto-off ft8TxMaxDuration = 18 s — internal/bridge/ft8tx.go.
//	- FT8 sequencer unanswered-rung repeat cap ≤ Ft8MaxRepeatsCeiling = 10 —
//	  clamped in types.ResolveFt8MaxRepeats (ft8.tx.max_repeats; default 6).
//	- band-activity history_max clamp [10, 2000] — types.ResolveFt8Display.
//	- bridge timeout sane range [50 ms, 1 h] — validateBridge (typo guard; rejects).
//
// Config schema versioning + migrations live in migrations.go.

// Default values applied by applyDefaults when the operator leaves a field
// zero-valued. Each is the fallback layer beneath operator config — every one
// of these is overridable in config.json; naming them keeps the magnitudes in
// one place instead of scattered as bare literals across applyDefaults. The
// per-field rationale lives at the assignment site in config.go.
const (
	// HTTP server (seconds unless noted).
	defaultReadHeaderTimeoutSec     = 5
	defaultReadTimeoutSec           = 10
	defaultWriteTimeoutSec          = 30
	defaultIdleTimeoutSec           = 120
	defaultShutdownTimeoutSec       = 10
	defaultMaxBodyBytes             = 1 << 20 // 1 MiB
	defaultPageLimit                = 50
	defaultMaxPageLimit             = 500
	defaultMaxConcurrentRequests    = 128
	defaultMaxEventSubscribers      = 16
	defaultSubmitRatePerSec         = 20
	defaultSubmitRateBurst          = 40
	defaultMaxContactHistoryResults = 100

	// Datastore connection pool.
	defaultMaxOpenConns        = 8
	defaultMaxIdleConns        = 8
	defaultContextTimeoutSec   = 10
	defaultTxContextTimeoutSec = 10

	// Logging rotation.
	defaultLogFileMaxSizeMB  = 100
	defaultLogFileMaxBackups = 5
	defaultLogFileMaxAgeDays = 30

	// Station knobs + default selectors.
	defaultAmpMultiplier = 1.0
	defaultLogbookID     = 1
	defaultRigID         = 1

	// Forwarder cadence.
	defaultForwarderTickIntervalSec = 120
	defaultForwarderBatchSize       = 5

	// Enrichment pipeline (ADR 0017).
	defaultCountryTTLDays     = 365
	defaultStationTTLDays     = 90
	defaultRefreshMaxInFlight = 4

	// Lookup providers + SMTP.
	defaultLookupHTTPTimeoutSec = 10
	defaultSmtpPort             = 587
	defaultSmtpTimeoutSec       = 30

	// Evidence capture (spot-network design §4.1): 500 MiB exactly — an
	// exact byte count, not a rounded unit, to avoid MB/MiB ambiguity
	// (operator, 2026-08-10). The physical cap over evidence.db + WAL + shm.
	defaultEvidenceCapBytes = 524288000
)
