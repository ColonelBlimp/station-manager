---
number: 0083
title: Every distributed build carries the ClubLog application key
status: Accepted
date: 2026-09-26
---

# 0083 — Every distributed build carries the ClubLog application key

## Context

ADR 0054 put the ClubLog application API key into the binary at build time
(`-X …/internal/forwarding/clublog.InjectedAPIKey`), never into source or
configuration, and flagged public pre-built binaries as a trigger to revisit: it
worried about "shipping one operator's key to everyone". The security review's
ST-7 (`docs/reviews/internal-security-trust-boundary-audit.md`) then enforced a
boundary on that worry: the public release path (`scripts/release.sh` →
`scripts/release-rpm.sh`) must be key-free and fails if a key is present, and only
the private dogfood path (`scripts/dev-rpm.sh`) may bake it.

Two facts make that boundary wrong. First, the key identifies the **software**,
not an operator — ClubLog's own guidance: "An API key is specific to your
application" and "API keys are not meant to be secret as such"; what ClubLog
forbids is a key being **published** or **passed around** ("automated scans
detect if keys are published, and they are then automatically deleted without
notice"; "if an API key is passed around it stops us from being able to provide
version controlled APIs for different products") — ClubLog help, "API keys",
https://clublog.freshdesk.com/support/solutions/articles/54910-api-keys. Second,
the key is **required for real-time uploading** (operator, 2026-09-26; the
forwarder sends it as `api` on every real-time upload and delete,
`internal/forwarding/clublog/clublog.go`): a key-free release is a Station Manager
whose ClubLog destination can never upload. The check also contradicted its own
header — it loaded `.env` before checking, so the `.env` every keyed dogfood build
needs made every release build fail (found 2026-09-26 preparing a fresh-install
test).

## Decision

Every distributed Station Manager build carries the ClubLog application key,
injected at build time from `.env` or the build environment and never present in
Git, GitHub or any published text; a release build without the key refuses to
build.

## Alternatives considered

### Keep ST-7: key-free public releases

What `release.sh` enforced since 2026-08-16. Rejected: the key is the
application's identity, so a key-free release is a release whose ClubLog
destination cannot work — every installer outside the operator's own station
would have a dead destination, which is the opposite of what a release is for.

### Per-deployer runtime key (an `EnvironmentFile` the installer fills)

ADR 0054's own alternative for public binaries. Rejected: ClubLog issues one key
per application, so every installer would need the same key and would have to be
handed it — exactly the "passed around" ClubLog asks us to avoid, and a setup step
no visitor could complete unaided.

### Key in source or configuration

Rejected by ADR 0054 and unchanged here: that is publication, which ClubLog
detects and revokes.

## Consequences

- `scripts/release.sh` requires a non-empty `CLUBLOG_API_KEY` (from `.env` or the
  environment), passes it into the release container by name (never on the command
  line), and `scripts/release-rpm.sh` injects it with the same `-X` flag as the dev
  build. A release without it fails before any build step.
- Release artefacts carry the key and are extractable with `strings`. That is
  accepted: embedding in a compiled binary is not publication (ADR 0054). The key
  must still never reach Git, logs, issue trackers or published documents; a
  source-published leak means rotating the key (edit `.env`, rebuild, redeploy).
- `buildinfo.BuildScope` keeps its two values but changes meaning: `public` is a
  distributable release (now keyed), `private` a dogfood build (`dev` tag, the
  stub destination, git-derived version, the `PRIVATE-BUILD-DO-NOT-DISTRIBUTE`
  marker). A keyless build (`go build`, `task build`, CI) is still valid: its
  ClubLog destination constructs and its uploads wait in the queue for a keyed
  build, and Settings says the key is absent.
- ST-7's "public releases are key-free" rule is superseded; its record stays as
  written.

## Triggers to revisit

- ClubLog states that application keys must not be embedded in distributed
  binaries, or revokes this key for that reason — then a key-issuing arrangement
  with ClubLog (or a hosted relay) is needed.
- A third party starts building and distributing Station Manager — they need their
  own application key from ClubLog, not this one.

## References

- ADR 0054 — ClubLog application API key injected at build time (its
  public-binary trigger is resolved here).
- ST-7 — `docs/reviews/internal-security-trust-boundary-audit.md`.
- ClubLog help, "API keys":
  https://clublog.freshdesk.com/support/solutions/articles/54910-api-keys
- `scripts/release.sh`, `scripts/release-rpm.sh`, `scripts/dev-rpm.sh`,
  `internal/buildinfo/buildinfo.go`.
