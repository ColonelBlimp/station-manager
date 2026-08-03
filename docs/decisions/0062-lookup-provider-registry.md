---
number: 0062
title: Lookup providers self-register; adding one is a package, not a code sweep
status: Accepted
date: 2026-08-03
---

# 0062 — Lookup providers self-register

## Context

Forwarding destinations (ADR 0039) are **config-driven and self-describing**. A
forwarder package calls `forwarding.RegisterForwarderType(typeName, displayName,
actions, creds)` in its `init()`; `applyDefaults` then seeds a disabled config
entry for it, `GET /v1/forwarder-types` serves the descriptor, and the Settings
→ Forwarding section renders it with **no SPA change at all**. Adding a
destination is: write the package, register it.

Enrichment providers (ADR 0017) never got that treatment, and it was only
noticed while porting the Enrichment tab into Settings on 2026-08-03. There is
no registry in `internal/lookup` at all (`grep -rln Register internal/lookup/`
is empty). Adding one provider today means editing **five places besides the
provider package**, four of which are hand-written name checks:

| Site | What is hardcoded |
| --- | --- |
| `cmd/smd/main.go` `buildEnrichment` | a `case types.QRZLookupServiceName:` constructing the provider |
| `internal/config/config.go` `DefaultConfig` | the seed chain entry |
| `internal/config/config.go` `normalizeLookupURLs` | default URL / view URL by name |
| `internal/types/lookup.go` `LookupProviderNeedsCredentials` | which providers require a login, plus QRZ's length limits |
| `frontend/app/.../enrichment.svelte.ts` `PROVIDERS` | display name, blurb, whether credentialed |

The last two were added the same day, by the port itself. The gap was *observed*
during that work — "there is no `/v1/lookup-types` descriptor endpoint the way
Forwarding has" was written into the port's own reasoning — and used as an
argument for hardcoding the presentation rather than raised as the defect. The
operator caught it immediately on seeing the result: **adding a service should
be a config addition, not a code change.**

The forwarder shape is reachable here, but **not** in the position first
assumed. Drafting this ADR checked only that `internal/lookup` does not import
`internal/config` directly and concluded the registry could live there. It
cannot: `internal/lookup` imports `internal/database/sqlite`, which imports
`internal/config`, so `config` → `lookup` is a cycle **transitively**. The build
found it immediately. The registry therefore splits in two — see the Decision.

## Decision

Give enrichment providers the same self-registration model as forwarders, split
across **two** registries because the import graph forces it:

- **`internal/lookupdef`** — a true leaf (imports only `internal/types`) holding
  the per-provider DESCRIPTOR: display name, help text, credential requirements,
  and default URL / view URL / timeout. `internal/config` and `internal/api` both
  read it.
- **`internal/lookup`** — the CONSTRUCTOR registry, where the provider
  interfaces already live. `internal/config` cannot import this package (it
  reaches `internal/config` transitively via `internal/database/sqlite`), which
  is precisely why the descriptor half is elsewhere.

Each provider package's `init()` registers into both, and `cmd/smd` blank-imports
the packages to trigger it — the same mechanism as forwarder types.
`buildEnrichment` iterates the constructor registry instead of switching on
names; `config` reads defaults and credential rules from the descriptors; a new
`GET /v1/lookup-types` serves them so the Settings → Enrichment section renders
any provider the daemon knows without an SPA change.

## Alternatives considered

### Leave it; hardcode each provider as it arrives

The status quo. It has survived because there has only ever been **one** chain
provider — the cost is entirely latent, and a second provider is what makes it
real. Rejected because the cost is not merely repetitive editing: the five sites
are in four packages and two languages, so a missed one fails in a different way
each time (no default URL → validation refuses an enabled provider; missing
credential rule → the daemon starts and then cannot authenticate; missing SPA
entry → a nameless row). Nothing links them, and nothing fails at compile time
when one is forgotten.

### Move only the SPA presentation onto the wire

Serve `label` / blurb / `credentialed` per provider from `GET /v1/config` and
delete the `PROVIDERS` map. This was the narrow reading of the complaint and it
does remove the most visible hardcoding. Rejected because it fixes the two sites
added on 2026-08-03 and leaves the three older ones, so "add a provider" remains
a Go code change in `buildEnrichment` and `config` — the operator's actual
objection. It also puts presentation strings in the config block rather than a
descriptor endpoint, diverging from the forwarder precedent for no gain.

### A generic provider-chain editor in the SPA, no descriptors

Render the chain purely from the uniform `LookupProviderInfo` shape — every
provider gets identical username/password/URL/timeout fields and no friendly
name. Requires no daemon work at all. Rejected because it loses the affordances
that make the section usable (hamnut is anonymous by design and must not show
credential boxes; QRZ needs its "subscription with XML/API access" note), and it
was already the fallback path chosen for *unrecognised* providers — this
alternative is just "make every provider look unrecognised".

### Descriptors in config.json rather than in Go

Let the operator declare a provider's display name and credential requirements
in config.json, so a new provider needs no build at all. Rejected because the
*constructor* still has to exist in the binary — a provider SM cannot construct
is not addable by configuration, whatever the config file says. This would make
config.json capable of describing providers that cannot run. The `label` field
(added 2026-08-03) already covers the legitimate operator-side half: renaming a
source without a build.

## Consequences

- Adding an enrichment provider becomes: write the package, register it in
  `init()`. Same shape as adding a forwarder, so there is one extension model to
  learn instead of two.
- `types.LookupProviderNeedsCredentials` and the `QRZMin*Len` constants added on
  2026-08-03 move into the QRZ descriptor and out of `internal/types` — better
  placed, since they are QRZ facts that only ended up in `types` to dodge an
  import cycle.
- The Settings → Enrichment section reworks to read descriptors instead of its
  `PROVIDERS` map. The parts that carry the real safety — per-source
  disclosures, the three-state password contract, whole-chain preservation on
  save — are unaffected; only the source of labels, blurbs and credential rules
  changes.
- A new endpoint (`GET /v1/lookup-types`) to document and keep current.
- **Accepted cost:** this is refactoring with one provider in the tree, so it
  buys nothing observable today. It is justified by the extension model, not by
  a present defect — the present defects (a missing default URL, a missing
  credential rule) are already fixed. If a second provider never arrives, this
  work never pays for itself.
- **Accepted cost — and a difference from forwarding found during the build:**
  forwarder type packages are LEAVES (none imports `internal/config`), but the
  lookup providers do (`hamnut/service.go`, `qrz/service.go` both take a
  `config.Service`). Either way `internal/config` cannot import them, so the
  registry is populated only by whatever imports the provider packages — in
  practice `cmd/smd`. **`internal/config`'s own unit tests therefore see an
  EMPTY registry.** Forwarding accepts the same thing, but harmlessly: its
  registry-driven behaviour is config SEEDING, and a no-op seed breaks nothing.
  Lookup's registry-driven behaviour includes URL defaults and the
  credential-validation rules, which existing config tests DO depend on. Those
  tests must register a descriptor explicitly — which is an improvement, since
  it makes each test state the providers it assumes instead of inheriting them
  from a global.
- **`applyDefaults` seeds a DISABLED config entry per registered provider** (the
  forwarder non-sparse behaviour), so every known provider appears in config.json
  and as a disabled row in Enrichment. Written here first as an optional
  "accepted cost" and deliberately left out of the initial build — which was
  wrong: a clean-room review (83d595f88838) filed it P1, correctly, because
  without it a newly registered provider appears in `/v1/lookup-types` and in no
  config block, so the section has no row for it and it cannot be enabled. The
  decision's own goal — adding a provider is a package plus an import — was
  false until seeding landed. It also removed the LAST hardcoded provider name
  (`DefaultConfig` seeded QRZ by name), which the first build had missed while
  claiming all five sites were gone.

## Triggers to revisit

- **If a second chain provider is never added**, this decision bought nothing;
  it should be judged by whether HamQTH/QRZCQ ever land, not by how the code
  reads.
- If a provider needs configuration the uniform `LookupConfig` shape cannot
  express (an API key that is not username+password, an OAuth flow), the
  descriptor needs a credential-field list like the forwarder registry's, and
  this ADR's "uniform shape" assumption is what breaks.
- If the country side ever fans out beyond a single provider, `EnrichmentConfig`'s
  hamnut-is-one-block shape has to change too, and the registry should cover both
  legs rather than just the chain.

## References

- ADR 0017 — enrichment pipeline (the providers this covers).
- ADR 0039 — forwarder `enabled` gating and the non-sparse, config-driven
  forwarder model this mirrors.
- ADR 0044 — the app Settings view the Enrichment section lives in.
- `internal/forwarding/registry.go` — the pattern being copied.
- `docs/v2-design/config.md` §(a‴) — lookup `label`, TTL semantics, and the
  enabled-needs-usable-credentials gate added 2026-08-03.
