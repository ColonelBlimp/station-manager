# W-0031 — Country lookup: a cached parent prefix claims the entities inside it

**Status:** Opened 2026-11-24 on the operator's direction ("let's also correct the lookup
code - which seems to be incorrect?"). Diagnosed; the 16 wrongly filed contacts found so far
are corrected. **The mechanism is ruled: way A, ask per callsign** (the operator's choice the
same day, of the three ways below). **Built 2026-11-24, tests first; not committed, not
reviewed, not at the station.** The criteria below were a draft when it was built and are
the operator's and Codex's to change.
**Outcome:** a contact with a station in a DXCC entity that shares a prefix block with a
larger country (Christmas Island in `VK`, Crete in `SV`, Madeira in `CT`) is filed under its
own entity, whatever was looked up before it.

## What happens today

Yes, the code is wrong, and by design (ADR 0017, points 3 and 6), not by a slip.

1. The country of a callsign is read from the local `country` table first
   (`readCountry`, `internal/lookup/orchestrator.go`). A hit there is the answer; HamNut is
   not asked.
2. The table is matched by the longest prefix among the rows it ALREADY holds
   (`FetchCountryByCallsignWithContext`, `internal/database/sqlite/api_context.go`).
3. A row is added only when HamNut is asked: when no held prefix matches, or the match is
   older than `country_ttl_days` (365).

So "has this prefix been seen" is judged against a table that cannot know which longer
prefixes exist. Once the row `VK` (Australia) is held, every `VK` call matches it, and HamNut
is never asked about `VK9X`. The answer for a call depends on the order in which other calls
were looked up before it. `MergeStationFromCountry` then writes that country over the
station record's fields, and the DXCC number is taken from the row's primary prefix.

It is the fault the code already records for one-letter blocks (`IsCacheableCountryPrefix`: a
held `U` row filed every Ukrainian call as European Russia), one level down.

## Evidence (2026-11-24; the station's files read-only, HamNut asked through the daemon)

- **The table:** one row beginning `VK`, held since 2026-07-17. Only the parent row for `ZL`,
  `CE`, `PY`, `HK`, `VU`, `ZS`, `CT`, `SV`, `OH`, `HC`, `XE`, `TI`, `YV`, `9M`, `HB` and `EA`
  too (with `PY7` and `EA8` beside two of them).
- **HamNut is right when asked.** The daemon's refresh lookup (`refresh=true`, which bypasses
  the table) answered the specific entity for every call tried: Madeira Islands (`CT3`),
  Dodecanese (`SV5`), Crete (`SV9`), Liechtenstein (`HB0`), Balearic Islands (`EA6`), Ceuta &
  Melilla (`EA9`), Antarctica (a `VK0` call), Christmas Island (`VK9X`). The provider is not
  at fault. It also holds entries for single callsigns: a portable `VK9/` call came back with
  that whole call as its prefix.
- **The log:** against a short and incomplete list of such prefixes, 16 contacts were filed
  under the parent country: CT3 (3), SV9 (4), SV5 (2), HB0 (2), EA9 (2), EA6 (1), VK0 (1),
  VK9 (1). Others with the same prefixes were filed correctly, which is the order-dependence.

## Done on the operator's direction ("Let's check with hamnut and correct")

- **15 contacts corrected** through `PATCH /v1/qso/{uuid}` with HamNut's answers of that day
  and the DXCC number from the daemon's catalogue: country, DXCC, CQ zone, ITU zone and
  continent. Each answered 200 with the values sent, and each edit goes to the forwarders
  as an update. One value is not HamNut's: Antarctica's continent is stored as `AN`
  (HamNut answered `SA`).
- **The sixteenth, the portable `VK9/` call of 2025-09-21, corrected to Christmas Island on
  the operator's word:** "Yes, correct it to Christmas Island - I've checked on QRZ.com - the
  operator lists the callsign to Christmas Island in 2025". It showed what a prefix cannot
  do. Asked for that call one letter short, HamNut matched `VK9` and answered Norfolk Island,
  its default for the block; asked for the whole call, it matched an entry for that one
  callsign and answered Christmas Island. The operator had seen the same himself ("add the H
  and it give Christmas Island???"). The longest entry wins, and some entries are whole
  callsigns.
- **A side effect of asking:** each refresh stored the specific row (`CT3`, `SV5`, `SV9`,
  `HB0`, `EA6`, `EA9`, `VK0`, `VK9X`), so those prefixes now resolve correctly from the
  table. That repairs these cases only, and `VK0` (Antarctica) now claims the other `VK0`
  entities in the same way.
- **Not done:** a sweep of the whole log. It would ask HamNut, and the callsign providers,
  about every distinct call.

## A second defect found on the way (not fixed)

The DXCC number comes from an embedded catalogue of primary prefixes
(`internal/enums/dxcc/dxcc-entities.json`), which holds 166 entities. It has `VK9C` and
`VK9L` and not `VK9X` or `VK9N`, so even with the right country, a Christmas Island contact
gets no DXCC number: the lookup answered Christmas Island with the number empty. The
contact entered by hand carries 35, from the ARRL DXCC list.

## Acceptance criteria (DRAFT, not ruled)

1. **The entity of a callsign does not depend on what was looked up before it.** With any
   set of rows already held, a call in a separate entity inside a larger block is answered
   with its own entity whenever HamNut can be asked.
2. **A callsign HamNut has resolved is answered locally afterwards,** with no request, until
   it is stale.
3. **When HamNut cannot be asked,** the answer is the best the local data gives and says so:
   it is distinguishable from an answer HamNut gave for that call. Enrichment still never
   blocks logging, and the route still always answers 200.
4. **An entry HamNut holds for one callsign** (a portable or special call) is honoured for
   that call and never applied to another.
5. **Nearest confusable outcome:** the lookup looks right because the specific prefix
   happens to be held already (as at this station after today's refreshes), while a fresh
   install, or the next unseen entity, is still wrong. The tests must start from a table
   that holds only the parent row.
6. **Evidence:** tests first, with a fake provider that knows more prefixes than the table:
   parent held, then a sub-entity call; the same call again (no second request); the
   provider down (fallback, marked); a single-callsign entry; a stale row. A reversion to
   the present order must fail the first.

## Ways to do it (for the operator; ADR 0017 is amended whichever is chosen)

- **A. Resolve per callsign, remember per callsign (recommended).** HamNut is asked the
  first time a callsign is seen; the answer is remembered against that callsign (a small
  table in `reference.db`, a migration), and the `country` rows are kept as the entity data
  and as the fallback when HamNut cannot be reached. Cost: one HamNut request for each new
  callsign, where today it is one for each new prefix.
- **B. Always ask HamNut, table as fallback only.** No migration. Cost: a request on every
  lookup, repeats included, and every answer waits for HamNut.
- **C. Hold a complete table.** Sound only if every prefix and every single-callsign entry
  HamNut knows is held locally and kept current. Whether HamNut offers such a list is not
  known.

Separately, the DXCC catalogue wants completing (the second defect), whichever is chosen.

## Questions for the operator

1. A, B or C? **Ruled 2026-11-24: A, ask per callsign.**
2. The portable `VK9/` contact of 2025-09-21: Norfolk Island or Christmas Island? **Answered
   2026-11-24: Christmas Island; corrected.**
3. Sweep the whole log for other contacts filed under a parent country, after the fix? Open.

## Already wrong the other way round (found while designing A)

The table holds HamNut's whole-callsign entries as if they were prefixes. This station's
table has `KH6M` and `KH2SR`, each answering United States: entries HamNut holds for one
callsign each. Under the present longest-prefix rule the first also claims any longer call
that begins `KH6M`, which would otherwise read as Hawaii. Way A must therefore keep what is
remembered for a callsign apart from what is held for a prefix (criterion 4).

## Built (2026-11-24; ADR 0104)

**What the operator will see after a deploy.** A callsign's country, zones and DXCC are
HamNut's answer for that callsign, whatever was looked up before. The first lookup of each
callsign asks HamNut, so the country appears a moment later than it did when a prefix was
already held; the same callsign again is immediate. Contacts already in the log are not
changed. With HamNut unreachable the country is still offered from the held prefix, as
before, and may be a parent block.

**What changed.**
- **A table of its own** (`callsign_country`, reference migration 0004;
  `internal/database/sqlite/callsign_country.go`): the provider's answer against the exact
  callsign, never matched as a prefix, with the entity's fields in the row.
- **The read** (`readCountry`, `internal/lookup/orchestrator.go`): what was remembered for
  the callsign, fresh or stale; else HamNut is asked; else, when it cannot be asked, the
  longest held prefix, named `country_prefix` and not remembered (`heldPrefix`). HamNut's
  explicit "not recognised" stands against a held prefix. A forced refresh that fails
  gives nothing, as before.
- **The write** (`rememberCountry`): every provider answer is remembered for its callsign;
  the prefix table is written only when the answer is a prefix it may hold, never for an
  entry HamNut keeps for one callsign (its prefix IS the callsign), nor for a one-letter
  block. The stale refresh writes the same way.
- **The station record's country** at a station refresh comes from what is remembered for
  the callsign (`localCountry`), not from the held prefix.
- **The API:** `country_source` gains `country_prefix`. The page's type knows it and shows
  the country as it shows any other.

**Evidence.**
- `internal/database/sqlite/callsign_country_test.go`, 7 tests: a miss; the whole answer
  kept; exact and never a prefix (a `KH6M` answer does not answer `KH6MB`); case and
  spaces; a later answer replacing the earlier whole; refusals; no prefix row written.
- `internal/lookup/percallsign_test.go`, 13 tests, **every one starting from a table that
  holds only the parent row** (criterion 5): the held parent does not answer for the
  entity inside it; the answer is remembered and the second lookup asks nothing; another
  callsign is asked in its own right; HamNut unreachable in four ways falls back and says
  so, and remembers nothing; the fallback is not sticky; nothing held is `none`; "not
  recognised" stands; a whole-callsign entry is never applied to another call; a prefix
  answer still feeds the fallback table; a stale answer is served and refreshed; a forced
  refresh asks and does not fall back; the station record follows the callsign's answer.
- Nine older tests seeded a prefix row and expected it to be a cache hit. That is the rule
  replaced, so each now seeds what a provider answer leaves (remembered for the callsign,
  and the prefix row), through one helper in each package. The one-letter-block test also
  shows the same call asked again is local.
- **Reversion proofs, each in a temporary copy:** the old order (the first test and six
  others fail); a whole-callsign entry written as a prefix (its test fails); a fallback
  guess remembered as an answer (two tests fail); the station refresh reading the held
  prefix (its test fails).
- **One fixture strengthened on Codex's review (codex-252).** The station-refresh test
  resolved the callsign first, which wrote the same country to both stores, so it would
  have passed with the refresh reading the prefix table. The two stores are now made to
  disagree before the refresh, and the test shows the refresh ran.
- **Gates:** `gofmt`, `go vet ./...`, `golangci-lint` on the three packages (0 issues),
  `go test ./...` whole (67 packages ok), maintainability (no regressions), the page's
  format, `svelte-check` and API tests. `task ci:local` not yet run on it.

**Not done, and not shown.**
- **The DXCC catalogue is still incomplete** (166 entities; no `VK9X`, no `VK9N`): a
  Christmas Island contact will carry the right country and zones and an empty DXCC
  number until that is completed. A separate change.
- **Nothing was tried against the real HamNut from this code.** The tests use a provider
  of their own. That the real one answers per callsign was seen through the daemon now at
  the station.
- **The page shows `country_prefix` like any other country.** Whether to mark it as
  unconfirmed is the operator's.
- **Rows already in `country` that are single-callsign entries stay there** and can
  mislead the fallback.
- **What an older build does with a reference database at version 4** is not established:
  migrations go up only.
- **No sweep of the log.**

## The clean-room review of the commit (`a09f4adc`), two findings

1. **P2, accepted, with the reason in the code** (`heldPrefix`). The rows already in the
   country table that are really entries for single callsigns are still there after an
   upgrade, and with HamNut unreachable a call never looked up before that begins with one
   is given that row's entity. Nothing stored tells such a row from a prefix, so neither a
   migration nor the fallback can pick them out. It is confined to the fallback, which
   already says its answer is a prefix guess, and no new row of the kind is written.
   **Emptying or pruning a station's country table would close it and is the operator's
   decision:** the rows are a cache of HamNut's answers and would be learnt again.
2. **P2, fixed, tests first.** An explicit "not recognised" from HamNut for a callsign whose
   answer was remembered left that answer in place: the stale refresh treated the verdict
   like a network failure, so the old country was served for ever and a refresh scheduled at
   every lookup, and a forced refresh reported `not_found` and the next ordinary lookup
   returned the rejected country again. The verdict now forgets what was remembered for that
   callsign, at both places (`forgetCountry`), and the next lookup asks and reports
   `not_found`. A failure that is not a verdict (no answer, a bare 404) forgets nothing.
   Three tests in `percallsign_test.go` and one in `callsign_country_test.go`
   (`DeleteCallsignCountryWithContext`: exact, and forgetting twice is not an error). This
   is the policy Codex said would need its own decision (codex-252, point 3): the choice
   made here is that the provider's explicit verdict outlives a remembered answer, and it is
   the operator's to reverse.

## The DXCC catalogue completed (2026-11-24; the second defect above)

The operator: "fix the DXCC catalogue next", then "go ahead with cty.csv".

**Why it was incomplete.** `scripts/gen-dxcc-entities.py` built it from one log: an entity
the operator had worked, and a HamNut request for a callsign of that entity. So it held the
166 entities worked by then, and a contact with any other got the right country and no DXCC
number. At the station 38 primary prefixes HamNut had already returned had no number, among
them Christmas Island, Norfolk Island, Jersey, Guernsey, Fiji, Pakistan and the Republic of
the Congo (the inbox item of 2026-10-31, a contact saved without a DXCC entity).

**The source now:** the Amateur Radio Country Files' `cty.csv` (version 20261114, 346
entities), each with its primary prefix and its ADIF number. That it speaks HamNut's
vocabulary was checked, not assumed:
- of the 166 old entries, 164 name a prefix that is in `cty.csv`, and all 164 carry the
  number `cty.csv` gives; none differs;
- every one of the 174 primary prefixes HamNut had returned at the station is an entity of
  `cty.csv`.

**Two differences of spelling,** seen through the daemon's own lookup and handled in the
generator alone:
- `cty.csv` stars an entity that is not a DXCC entity of its own (Sicily, Shetland, African
  Italy, Bear Island, European Turkey, the Vienna International Centre); HamNut writes the
  prefix plain. Such an entity carries its parent's number.
- `cty.csv` writes `3Y/b`; HamNut writes `3Y…B`, with a horizontal ellipsis and in
  capitals (seen for Bouvet, South Cook Islands, Glorioso Islands, Clipperton Island and
  Shetland). The old catalogue's one such entry, Bouvet, was already in that form.

**What changed.** The catalogue has 346 entries: every old prefix is still there with its
number unchanged, and 180 are new. The generator reads `cty.csv` and no longer reads a log
or asks HamNut. The site's permission notice (MIT in form) is in `NOTICE`, and
`docs/licensing.md` names the source.

**Evidence.** Three tests in `internal/enums/dxcc/dxcc_test.go`: eight entities the old
catalogue lacked, by number; the keys as HamNut writes them, with seven of the two special
forms; the whole catalogue well formed and not smaller than the source. They failed
against the old catalogue for those reasons.

**What it does not do.** A contact already stored without a DXCC number is not changed.
The operator's override file (`dxcc-entities.json` in the data directory) still wins over
the embedded entries. The catalogue is as current as the `cty.csv` it was generated from;
a new entity needs a regeneration.

## Two more clean-room findings, both fixed, tests first (2026-11-24; not committed)

1. **Of `557cd26a` (P2): an older rejection could delete a newer answer.** A forced refresh
   that got "not recognised" waits for its station lookup before it acts; meanwhile another
   refresh of the same callsign can succeed and be remembered; the rejection then deleted
   that newer answer, because it forgot by callsign alone. The forget now carries the moment
   HamNut was asked and removes only what was remembered at or before it
   (`DeleteCallsignCountryWithContext`, `forgetCountry`). Shown in the database and at both
   places in the pipeline, with a provider that remembers another answer before it rejects.
   With the delete unconditional again, all three tests fail.
2. **Of `1952d50a` (P2): a newly numbered entity could look new though it had been
   worked.** Before the catalogue was completed, a prefix with no number sent the new-entity
   check to the country name, which found a contact stored with that country. With the
   number known, the check goes by number, and a contact stored earlier with its country
   and NO number (the very contacts the old catalogue left without one) is not found by it.
   The check now also counts a contact with that country that carries no number. Where a
   number was stored, the number still rules: a contact with the same country name and
   another entity's number does not count. Three pipeline tests and one database test.
   (First built as a second query, `HasQsoForCountryLackingDxccWithContext`; replaced the
   same day by one statement, below.)

**The contacts stored without a DXCC number, filled in on the operator's direction**
("Let's fill in the missing DXCC numbers", 2026-11-24). There were 31, with 26 callsigns.
HamNut was asked about each callsign through the daemon's refresh lookup and its primary
prefix turned into a number by the new catalogue. Where HamNut's country was the country
already stored, the number alone was added: 29 contacts. The other two had no country at
all (`NON-DXCC` and `Unknown`) and were given country, number, zones and continent from
HamNut's answers, on a second direction ("Yes, correct both from HamNut's answers"). Each
went through `PATCH /v1/qso/{uuid}` and answered 200 with the values sent; the log file,
read afterwards, holds no contact without a number. So at this station the fallback of
point 2 has nothing left to find; it stays for any log that still has such contacts.

## The clean-room review of those two fixes (`fbab155f`), two findings

1. **P2, accepted, with the reason in the code** (`forgetCountry`). Two lookups of one
   callsign that are out at once, one answered with a country and the other with a
   rejection, are not put in the order they were asked: if the country was asked for first
   and written last, it stays. Ordering them needs a record of the rejection to outlast it,
   which is negative caching, and ADR 0017 declined that. It takes HamNut contradicting
   itself about one callsign within the seconds two lookups overlap; what stays is an answer
   it gave moments earlier, and the next refresh of that callsign removes it.
2. **P2, fixed.** The new-entity check asked the log twice, by number and then by country
   without a number. An edit that gave such a contact its number between the two was seen
   by neither, and the entity read as new with the contact in the log throughout. It is now
   one statement (`HasQsoForEntityWithContext`): a contact carrying the number, or one with
   the country and no number. The database test covers ten cases, among them a stored
   number ruling over a matching name and a deleted contact not counting. That an edit
   between two reads can no longer hide a contact follows from there being one read; it is
   not shown by a test of its own.

**Where this stands.** The last three reviews have each found a narrower case than the one
before: first a defect any operator would meet, then a race needing two refreshes of one
callsign at once, then one needing HamNut to contradict itself within seconds. The first
kind is fixed. The last is written down and accepted.
