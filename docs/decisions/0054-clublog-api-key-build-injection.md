---
number: 0054
title: ClubLog application API key injected at build time, not stored in source or config
status: Accepted
date: 2026-07-20
---

# 0054 — ClubLog application API key injected at build time, not stored in source or config

## Context

The ClubLog forwarder (`internal/forwarding/clublog`) needs four values to
upload a QSO. Three are the OPERATOR'S own account details — `email`,
`password` (a scoped ClubLog "Application Password"), and `callsign` — and
legitimately live in `config.json`. The fourth, the **application API key**,
is different in kind: it identifies *Station Manager the software*, is the
SAME for every SM install, carries no operator identity, and is issued
confidentially by ClubLog.

ClubLog's terms (relayed verbatim by the operator on 2026-07-20 when the key
was granted): *"Please keep this key confidential and do not publish it in
source code."* ClubLog actively **auto-deletes any key it finds published in
a public repository**. SM's source is public (GPL-3.0-only, ADR 0023), so the
key can never be committed.

The original implementation put the app key in the forwarder's `credentials`
blob (config.json), operator-entered via the config SPA. That satisfies "not
in source" but the operator ruled it insufficient: `config.json` is plaintext
on disk, is pasted when debugging, and is exactly the kind of artifact that
leaks. The key must live in **neither source nor config**.

## Decision

The ClubLog application API key is **injected into the binary at build time**
via linker flags, sourced from the operator's gitignored `.env`:

```
-ldflags "-X github.com/ColonelBlimp/station-manager/internal/forwarding/clublog.InjectedAPIKey=$CLUBLOG_API_KEY"
```

This is the SAME `-X` channel already used for `main.Version` (and
`internal/buildinfo.Env`), so it is established plumbing, not a new mechanism.
Concretely:

- **`clublog.InjectedAPIKey`** is a package `var` (empty by default), set only
  by the linker. `cmd/smd` does not touch it (unlike `clublog.UserAgent`, which
  is set from resolved config at runtime).
- **The key source is `.env`** (`CLUBLOG_API_KEY`), which is already gitignored.
  `Taskfile.yml` auto-loads `.env` via `dotenv:`; the build scripts source it
  explicitly (`scripts/dev-rpm.sh`, `scripts/release.sh`) so the dogfood and
  release paths pick it up; `scripts/release.sh` passes it into the AlmaLinux
  release container via `-e CLUBLOG_API_KEY` (the `.env` is never copied into
  the image).
- **Config carries only the three operator fields.** `clublog.New` sources the
  API key from `InjectedAPIKey` and ignores any stale `api` left in config (the
  `credentials` struct no longer has that field). The config-SPA add-forwarder
  descriptor drops the `api` field, so the operator is never prompted for it.
- **Fail-loud when the key is absent.** A binary built without `CLUBLOG_API_KEY`
  (CI, a fresh clone, a musl/static fallback build) has an empty `InjectedAPIKey`;
  `clublog.New` then **refuses to construct the forwarder**, leaving ClubLog inert
  rather than firing keyless requests that 403 and trip the circuit breaker. The
  CGO-free releasability gate and the test suite stay green with no key present.

**A compiled binary is not source code**, so baking the key satisfies ClubLog's
"do not publish it in source code" rule — the same posture WSJT-X and Log4OM take
with their own app keys.

## Alternatives considered

### Key in `config.json` credentials (the original implementation)

Operator-entered, stored in the forwarder's `credentials` blob. **Rejected** by
the operator: config.json is plaintext, shared when debugging, and an easy leak
vector. It technically kept the key out of *source*, but not out of an artifact
that circulates.

### Runtime environment variable (systemd `EnvironmentFile`)

The key read from `os.Getenv` at daemon start and injected into the forwarder,
supplied via a chmod-600 `EnvironmentFile` (the pattern SMC tokens use). Keeps
the key out of the binary too. **Not chosen** — the operator preferred build-time
baking: fewer moving parts at deploy time (no per-host secrets file to manage),
and the key is confidential-not-secret (it identifies the app, not an account),
so having it inside the operator's own private binary is an acceptable trade for
the operational simplicity. Revisit if SM ever ships **public pre-built
binaries**, where a per-deployer runtime key would avoid shipping one operator's
key to everyone (see Triggers).

### Baked as a source constant / obfuscated in source

**Rejected** outright — this is precisely what ClubLog forbids and auto-deletes.

## Consequences

- **The key is present in the compiled binary** and is extractable with
  `strings`. This is fine for SM's current private-build dogfood model (the
  operator builds and deploys to their own host). It is NOT source publication,
  so it is ClubLog-compliant. The distributed *artifact* now carries a
  confidential value — see Triggers for when that matters.
- **Config no longer holds the key.** An older `config.json` with an `api` field
  is not an error — the field is silently ignored (unknown fields are dropped on
  unmarshal). No migration needed.
- **Build reproducibility depends on `.env`.** Two builds of the same commit
  differ only in the baked key; a build with no `.env` is valid and ships an
  inert ClubLog forwarder. This is intentional (CI must build without the key).
- **New build-time input to remember.** Rotating the key = edit `.env` + rebuild
  + redeploy. Documented in `docs/v2-design/forwarding.md` and the clublog
  package doc.

## Triggers to revisit

- **If SM ships public pre-built binaries** (RPMs/tarballs to third parties), a
  single baked key would ship one operator's confidential key to everyone.
  Switch to the runtime-env approach so each deployer supplies their own key, or
  require each deployer to build with their own `.env`.
- **If ClubLog changes its stance** on keys embedded in distributed binaries
  (not just source), reassess — a runtime-supplied key becomes mandatory.
- **If a second build-time secret appears**, factor the `.env` → `-ldflags`
  plumbing into a shared helper rather than duplicating it across the scripts.

## References

- ClubLog API-key terms (operator relay, 2026-07-20): "keep this key
  confidential and do not publish it in source code"; ClubLog auto-deletes keys
  found in public repositories.
- `internal/forwarding/clublog/clublog.go` — `InjectedAPIKey`, `New` (fail-loud),
  the three-field `credentials`.
- Build plumbing: `Taskfile.yml` (`dotenv` + `-X` on the five `cmd/smd` builds),
  `scripts/dev-rpm.sh`, `scripts/release-rpm.sh`, `scripts/release.sh`.
- Precedent `-X` injection: `main.Version` (`scripts/version.sh`),
  `internal/buildinfo.Env`.
- Licensing context: ADR 0023 (GPL-3.0-only), `docs/v2-design/forwarding.md`.
