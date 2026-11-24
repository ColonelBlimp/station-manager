---
number: 0104
title: Resolve a callsign's country by asking for that callsign, and remember it per callsign
status: Accepted
date: 2026-11-24
---

# 0104 — Resolve a callsign's country by asking for that callsign, and remember it per callsign

**Accepted 2026-11-24 (the operator chose it of three ways, W-0031). It amends ADR 0017's
points 3 and 6 for the country layer only; the station layer and everything else in ADR
0017 stand.**

## Context

ADR 0017 made the `country` table, keyed on prefix, the cache of HamNut's answers, and its
read policy treated a row in that table as a hit. The table was matched by the longest prefix
it already held. That cannot be sound: the table is filled one answer at a time and has no
way of knowing which longer prefixes exist. Once the row `VK` (Australia) was held, every
`VK` call hit it, and HamNut was never asked about `VK9X` (Christmas Island). Which entity a
call was given depended on which other calls had been looked up before it.

At the station on 2026-11-24 this had filed sixteen contacts under a parent country (CT3,
SV5, SV9, HB0, EA6, EA9, VK0, VK9), found after a Christmas Island contact read as
Australia. Asked directly, HamNut answered the right entity for every one of them, so the
provider was not at fault. HamNut also holds entries for single callsigns: a portable call
one letter short matched the block and the whole call matched its own entry, with a
different entity. The table had been storing such entries as if they were prefixes.

The same fault had been met once before for one-letter blocks and patched there
(`IsCacheableCountryPrefix`).

## Decision

The country of a callsign is HamNut's answer for that callsign. The first time a callsign is
looked up HamNut is asked; the answer is remembered against that exact callsign, in a table
of its own (`callsign_country`), and later lookups of the same callsign are local until the
answer is stale. The `country` table's longest held prefix is never a hit. It is what is
left when HamNut cannot be asked, and such an answer is named as that
(`country_source: "country_prefix"`) and is not remembered for the callsign.

## Alternatives considered

### Always ask HamNut, with the prefix table as the fallback only

No new table and no migration. Rejected because every lookup, repeats included, would then
wait for a request to HamNut, and because nothing would be answerable locally for a callsign
already resolved when HamNut is down except by the very prefix guess that caused the fault.

### Hold a complete table

Sound only if every prefix and every single-callsign entry HamNut knows were held locally
and kept current, so that the longest held prefix was always the longest that exists. Not
taken: whether HamNut can supply such a list is not known, the single-callsign entries
change as operations are announced, and a table that is believed complete and is not fails
in exactly the way this one did, silently.

### Keep the prefix cache and refuse more kinds of prefix

The patch made for one-letter blocks, extended: refuse two-letter prefixes, or a list of
blocks known to hold several entities. Rejected because the list of such blocks is the
knowledge the table lacks, and any list is another thing to be incomplete.

### Remember per callsign in the `country` table itself

A row whose prefix is the whole callsign needs no new table. Rejected because that table is
matched by prefix: such a row answers for every longer call that begins with it. The
station's table already held two of these.

## Consequences

- **One request to HamNut for each new callsign,** where it was one for each new prefix.
  Lookups come from two places only, the operator leaving the callsign field and the
  start of an FT8 or FT4 contact, so this follows contacts, not decodes.
- **A reference-database migration (0004)** adds `callsign_country`, one row for each
  callsign ever resolved, each carrying the entity's fields so that it stands alone. The
  provider's single-callsign entries and its one-letter blocks have no row in `country`.
- **Calls in one-letter blocks are now remembered too.** They were never cached at all, and
  each lookup of the same call asked HamNut again.
- **A new value of `country_source`,** `country_prefix`, for the answer given when HamNut
  could not be asked. The page does not yet show it differently.
- **HamNut's explicit "not recognised" is no longer overruled by a held prefix.** It was,
  whenever any shorter prefix of the call happened to be held. The same verdict, given for a
  callsign whose answer was remembered (at a forced refresh, or at the refresh of a stale
  answer), forgets that answer. A failure to reach HamNut forgets nothing.
- **Rows already in `country` that are really single-callsign entries stay there,** and can
  still mislead the fallback. They are not told apart from prefixes by anything stored.
- **The first lookup of every callsign after the upgrade asks HamNut,** since nothing is
  remembered yet. Contacts already logged are not changed by this.

## Triggers to revisit

- HamNut offers its whole table, kept current: a complete local table becomes possible and
  would answer offline for callsigns never seen.
- The number of requests to HamNut becomes a cost or a courtesy problem: batch, or prime
  the memory from the log.
- The fallback is seen to mislead in practice: show `country_prefix` to the operator as
  unconfirmed, or stop offering a country at all without HamNut.
- `callsign_country` grows large enough to matter: expire rows not read for a long time.

## References

- ADR 0017, points 3 and 6 (amended here), 7, 9 and 13 (unchanged).
- `docs/work/W-0031-country-lookup-parent-prefix.md`: the diagnosis, the evidence at the
  station and the criteria.
- `internal/lookup/orchestrator.go` (`readCountry`, `heldPrefix`, `rememberCountry`),
  `internal/database/sqlite/callsign_country.go`, reference migration 0004.
