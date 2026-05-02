# Station Manager — Lessons for v2

**Status:** v1 analysis document, 2026-04-14. The synthesis document. Where `architecture-map.md` / `design-decisions-log.md` / `bug-inventory.md` / `invariants.md` document what v1 *is*, this document captures what v1 *taught us* and how that knowledge should shape v2 (whether v2 is a rewrite or a significant refactor).

**Purpose:** lessons decay fast if they only live in one person's head. Design patterns learned through mistakes are the most expensive kind of knowledge to acquire and the easiest kind to forget. This document makes them explicit and durable.

**How to read this document:**
- If you're making a design decision for v2, read this first. Many of the mistakes here are ones you'd re-make without prompting because they all feel locally reasonable in isolation.
- If you're designing a new package or subsystem, check the "patterns to apply" section against what you're about to do.
- If you find yourself reaching for something that smells like one of the "patterns to avoid," pause and re-read that section.

**How this document relates to others:**
- `invariants.md` — the non-negotiable rules. Lessons here are softer (guidance, not absolutes).
- `design-decisions-log.md` — individual decisions with verdicts. This document explains the reasoning style behind good decisions, not the decisions themselves.
- `bug-inventory.md` — concrete bugs. Many lessons here were learned from those bugs.

---

## Patterns to apply in v2

### 1. Mostly-blob + promoted fields — marshal the whole thing, duplicate, let the authoritative copy win

**The pattern:** when a struct is mostly-serialized-to-a-blob with a small set of promoted/exposed fields (database columns, indexed values, whatever), **do not build a separate "blob view" struct**. Marshal the whole thing into the blob, store the promoted fields twice (once in the blob, once in the promoted columns), and let the authoritative copy (the column) win on read.

**Why:** the duplication cost is trivial (~50 bytes per row for QSO-scale data — 100,000 QSOs is 5 MB extra). The maintenance savings are significant: one source of truth, no hand-maintained parallel struct to keep in sync. And it preserves the property that adding a new field is a one-line change to the canonical struct.

**The antipattern it replaces:** the sqlite adapter's forward direction builds `types.QsoAdditionalData` as a hand-maintained subset of `types.Qso`, marshals that, and stores the result. Every new field on `types.Qso` requires a parallel edit to `QsoAdditionalData` or the field silently gets dropped. 100+ lines of manual field copying. Asymmetric with the reverse direction (which unmarshals straight into `types.Qso`, as it should).

**Concrete code comparison:**

Bad (current v1):
```go
func QsoTypeToModel(qso types.Qso) (models.Qso, error) {
    additionalData := types.QsoAdditionalData{
        SmQsoUploadDate:  qso.SmQsoUploadDate,
        SmQsoUploadStatus: qso.SmQsoUploadStatus,
        // ...80 more lines of manual field mapping...
    }
    jsonData, err := json.Marshal(additionalData)
    // ...
    return models.Qso{
        ID: qso.ID,
        Call: qso.ContactedStation.Call,
        // ...promoted columns...
        AdditionalData: jsonData,
    }, nil
}
```

Good (target):
```go
func QsoTypeToModel(qso types.Qso) (models.Qso, error) {
    blob, err := json.Marshal(qso)
    if err != nil {
        return models.Qso{}, err
    }
    return models.Qso{
        ID:   qso.ID,
        Call: qso.ContactedStation.Call,
        Band: qso.QsoDetails.Band,
        // ...promoted columns, explicit...
        AdditionalData: blob,
    }, nil
}
```

**When to reach for this:** anywhere in v2 that needs "store this complex struct with a few queryable fields promoted to columns." ADIF QSOs are the obvious case. Other potential applications: cached lookup results, event payloads, anything with evolving schema.

**When NOT to use this:** if the promoted field count is large (say, more than half the struct), just store everything as columns. The blob pattern is for "a few queryable, many structured."

### 2. Build specific, not generic

**The pattern:** when a problem affects two or three specific places in the code, solve it specifically in each place, not generically in a shared framework.

**Why:** generic Go frameworks that try to abstract over different contexts almost always end up unmaintainable. The abstractions leak, the edge cases multiply, and the framework code becomes harder to reason about than the specific code it was supposed to replace. The cost of a small amount of duplication between per-context implementations is far less than the cost of a framework.

**The antipattern it replaces:** `internal/adapters/` — a reflection-based struct-to-struct adapter framework with builder API, field converters, tag-based ignores, and `converters/{common,sqlite,postgres}/` subpackages. 30+ test files. Abandoned as "too complicated to maintain and use correctly." Meanwhile `internal/database/sqlite/adapters/` — the per-driver, manual, simple-minded version — is the one that works and will be preserved.

**Rule of thumb:** start with the specific version. Only generalize if you have **two real uses** (not one real use plus an imagined future one). And even then, prefer duplication over a framework until the framework is clearly warranted by three-plus uses.

**Why this is hard to follow:** generic frameworks feel more elegant at design time. A single abstraction that handles all cases "correctly" is more satisfying to contemplate than two-or-three specific implementations that each do one thing. The elegance is imaginary — it evaporates as soon as the framework meets a real edge case the designer didn't anticipate. Every reflection-based Go framework I've seen goes through this arc.

**Corollary:** this applies to error packages, logging abstractions, middleware frameworks, query builders, event dispatchers, and every other class of "wouldn't it be nice if I could handle all of these the same way" code. The specific, boring version is almost always right.

### 3. Asymmetric round-trips are a clue

**The pattern:** when a round-trip converter has one elegant direction and one manual/complicated direction, the elegant direction is usually showing you the right shape and the manual direction is overbuilt.

**Why:** a well-designed round-trip is symmetric by nature. Serialization and deserialization mirror each other. If one side is a 20-line `json.Unmarshal(blob, &qso)` and the other side is a 100-line hand-mapped copy into an intermediate struct, those two halves are not doing equivalent work — one of them is doing extra work the other knows how to avoid.

**Concrete example:** the sqlite adapter's `QsoModelToType` is basically `json.Unmarshal(model.AdditionalData, &qso); overlay columns; return`. About 20 lines of real work. The forward direction is `copy 80 fields into QsoAdditionalData; marshal; build models.Qso with 14 promoted columns; return`. About 100 lines. The asymmetry was the code telling us that the forward direction was doing something the reverse direction already knew how to avoid. Trusting the elegant side and rewriting the manual side produces the correct simplified design (see pattern #1 above).

**Rule of thumb:** whenever you're looking at a converter pair (encode/decode, marshal/unmarshal, to/from), check that the two sides are roughly equal in complexity. If one side is dramatically more complex, interrogate it — it's almost always overbuilt.

### 4. Document intent alongside mechanism

**The pattern:** when a package or struct exists to solve a non-obvious problem or embody a non-obvious design decision, **say so in the code**. A `doc.go` with a short paragraph explaining *why* the package is shaped the way it is, or a leading comment on a key type saying "this struct mirrors the ADIF spec; new ADIF fields go here," saves readers from re-deriving the intent from context.

**Why:** code shows mechanism, not motivation. A reader (including future-you six months from now, including any assistant helping with analysis) can see *what* the code does but not *why* it does it that way. For most code this is fine — obvious mechanism is its own documentation. For load-bearing design decisions, it's actively harmful: the next person to touch the code will either make a mistake the comment would have prevented, or do an archaeology exercise to reconstruct the original intent.

**Concrete example:** the sqlite `adapters` package has no `doc.go` explaining the promoted-columns-plus-blob pattern or why it exists. The ADIF-alignment goal is invisible from reading the code alone. As a result, when I reviewed the package during this analysis, I initially recommended *flattening `types.Qso`*, which would have been backwards given the real design intent. A 3-line comment on `types.Qso` saying "this mirrors the ADIF spec; see `invariants.md` for the pattern" would have prevented the wrong recommendation entirely and saved several hundred words of discussion.

**Rule of thumb:** any package that encodes a deliberate non-obvious design decision deserves a `doc.go` (or at least a package comment) stating the decision. Any struct that is load-bearing for a design pattern deserves a leading comment stating its role. Do not rely on readers remembering design discussions that happened in Slack or in one session with an AI assistant.

**What to document (and what not):**
- **Document:** design intent, non-obvious constraints, invariants the code relies on, the reason a specific approach was chosen over alternatives.
- **Don't document:** what the code obviously does, the mechanism of standard library calls, anything a reader can see at a glance.

### 5. Explicit fallbacks for every external dependency

**The pattern:** every call to an external service (HTTP lookup, database, remote API) must have an **explicit fallback** defined at design time, not discovered at runtime when the service is down.

**Why:** "what does this do when the external service fails" is the test case that catches the most subtle bugs, because the happy-path code gets exercised constantly and the failure path never does unless something breaks in production. If the fallback isn't designed in, the default behavior is usually "propagate the error to the caller," which is almost always wrong — it turns transient outages into user-visible failures that shouldn't be.

**Concrete example:** the hamnut-blocks-logging bug (fixed 2026-04-14, see `bug-inventory.md`). A failed hamnut.com lookup propagated as a fatal error through `initializeQso` and blocked QSO entry entirely, even when the local country cache had usable data. The fix was to define an explicit fallback: "if hamnut is down and we have a cached row, return the cached row; if we have no cached row, synthesize `Country{Name: 'Unknown'}` and continue." Writing *that* test first, before the happy path, would have caught the bug at design time.

**Rule of thumb:** for every function that calls an external service, the first test written should be "service returns an error." Not the second test, not a "we'll add that later" test — the first test. The happy path is easy to add after. If you can't articulate what the function should do when the service fails, you haven't finished designing it.

**Related invariant:** "Enrichment never blocks logging" (see `invariants.md`). That invariant is the general form; this pattern is how you enforce it at the code level.

### 6. Transaction boundaries are conscious design, not accidents

**The pattern:** when a function writes to multiple tables, **explicitly draw the transaction boundary** at design time. Decide which writes are atomic (inside the transaction) and which are best-effort (outside the transaction), and be prepared to defend that decision.

**Why:** sequential non-transactional writes are a subtle data-corruption hazard. If any write after the first succeeds-then-fails, the caller sees "operation failed" and retries, producing duplicates or inconsistent state. Most callers don't think about this at the call site — they just retry on error. The only place it can be handled correctly is inside the function, by grouping the writes into a proper transaction.

**Concrete example:** the `LogQso` atomicity gap (fixed 2026-04-14, see `bug-inventory.md`). Four sequential DB writes (`InsertQso` → contacted station → country → `InsertQsoUpload`). A failure on the fourth write left the QSO durably stored, returned an error to the caller, and a retry produced a duplicate. The fix wrapped QSO insert + upload insert in a proper `BeginTxContext` transaction, and moved the cache writes (contacted station, country) outside the transaction as best-effort because they're enrichment, not authoritative.

**Rule of thumb:** when designing a function that writes to multiple tables, write down the transaction boundary explicitly: "these two writes are atomic; these other two are best-effort after commit." If you can't articulate which is which, keep thinking. The answer depends on the authoritative-vs-cache distinction (authoritative data goes in the transaction; caches go outside).

**Related invariant:** "One-fails-all-fail for QSO writes" and "Authoritative vs cache" (see `invariants.md`).

### 7. Characterization tests before refactoring

**The pattern:** before refactoring code that lacks test coverage, write **characterization tests** — tests that describe what the code *currently does*, not what it *should do*. You're not trying to prove correctness of the old code; you're trying to freeze its observable behavior so you can tell when the new code diverges.

**Why:** "this passes CI after my refactor" means nothing if CI isn't exercising the behavior you changed. Most behavioral regressions during refactoring are silent — the code still compiles, still passes the existing tests (which were testing something else), and something subtly different happens at runtime. Only a test that was specifically written to describe the old behavior will catch the drift.

**What to test:** the golden paths and the specific invariants you care about. For Station Manager specifically:
- End-to-end QSO lifecycle: `NewQso("G0XYZ") → populated draft → LogQso → retrieve → assert round-trip matches`.
- Each failure mode of `enrichment never blocks logging`: hamnut down, QRZ lookup down, both down, DB broken (this one *does* fail).
- The atomicity fix: stub the upload insert to fail, assert the QSO row is not persisted.
- ADIF round-trip: parse known-good ADIF, serialize, parse again, assert equality.
- SQLite adapter round-trip: `types.Qso → model → types.Qso` asserts identity (or catches the specific lossiness that exists today).

**What NOT to test:** every function (low ROI), arbitrary coverage targets (distraction), mocked versions of things that should be tested against the real implementation (the `DatabaseServiceInterface` mocks in v1 are an example — they passed while the real behavior diverged).

**Scope:** 25-40 integration-style tests is about the right order of magnitude for v1's lifecycle. Not hundreds. Not ten. Enough to catch the kinds of regressions the restructure is likely to introduce.

**Rule of thumb:** if you're about to refactor a package that has no real test coverage, **write the characterization tests first** and get them passing against current behavior. Then refactor. The tests become the regression guard for the redesign work.

### 8. Enumerate all API surfaces before designing any of them

**The pattern:** when designing an API that will serve multiple consumers, enumerate **every consumer's needs** before committing to the shape of any endpoint. Don't design the "main" consumer's API and then try to bolt the others onto it later.

**Why:** API shapes have gravity. Once the "main" API exists, extending it to cover other consumers tends to produce awkward extensions, cross-cutting concerns, or duplicated endpoints that should have been unified. You get a better result if you enumerate all the slices up front and find the shared patterns.

**Concrete example:** during session discussions, the earliest daemon API sketches were logging-centric (QSO draft init, LogQso, contest dupe check, contact history — all the things `apps/logging` needs). The logbook-management surface (`apps/logbook` — create/delete/rename/export logbooks, batch-edit QSOs, list all QSOs in a logbook with paging) was implicit and largely missing from the sketches. That's a gap of ~10+ endpoints that would have been discovered late and bolted on awkwardly. Realizing the three-concerns split is deliberate (see `invariants.md`) and explicitly enumerating all three (logging, logbook, config) before designing any of them produces a cleaner daemon API.

**Rule of thumb:** when the daemon API design session happens in v2, the first deliverable is **a table of all consumers and their required operations**, not a list of endpoints. Endpoints come second.

---

## Patterns to avoid in v2

These are the shapes that caused v1's worst pain. Knowing them explicitly means you skip past them without making the mistakes first.

### 1. Intermediate "view" structs that duplicate a source-of-truth struct

If a second struct exists to be a "filtered view" or "subset" of a canonical struct, and it has to be kept in sync by hand, it will be kept out of sync and you won't notice until data is silently dropped. Replace it with field tags, field exclusion functions, or — best — just marshal the canonical struct and accept a small duplication cost. See pattern #1 above.

### 2. Generic reflection frameworks for problems solved by specific code

Reflection-based struct manipulation is a red flag. It's usually reached for because "I have this pattern in three places and I want to DRY it up." Don't. Three specific implementations are easier to read, easier to debug, and easier to evolve than one clever generic. See pattern #2 above and the `internal/adapters/` cautionary tale.

### 3. Asymmetric round-trip converters

If your forward converter is 100 lines and your reverse converter is 20 lines, something is wrong with the forward converter. Investigate the asymmetry before accepting it. See pattern #3 above.

### 4. Aspirational interfaces with no real consumer

`DatabaseServiceInterface` in v1 exists only to be mocked in tests that nobody writes. The mock was implemented, the interface was written, and then all the actual tests used real `&sqlite.Service{}` instances anyway. The interface sat unused and drifted out of sync with the concrete type. **If an interface's only consumer is a mock, delete the interface and the mock.** The integration-test pattern is better anyway.

### 5. Sequential non-transactional writes to multiple tables

If a function writes to more than one table and you don't have an explicit transaction boundary, you have a silent data corruption bug waiting to happen. See pattern #6 above. The specific v1 example (`LogQso`) is now fixed, but the principle is general: **every multi-table write is a transaction-boundary design decision**, not an accident.

### 6. Enrichment errors that escape as fatal errors

External service failures must never propagate as fatal errors to user-facing operations. See pattern #5 above. The specific v1 example (hamnut-blocks-logging) is now fixed, but the principle applies to every enrichment path you'll ever write.

### 7. Deferred documentation of design intent

If you're making a non-obvious design decision right now, document it right now. Do not say "I'll add a doc.go later." Later never happens, and the undocumented intent becomes archaeology for anyone who touches the code six months from now — including you. See pattern #4 above.

### 8. Hardcoded configuration at the call site

If an ingest site, dispatch site, or fan-out site hardcodes a specific destination/mode/service, it will stay hardcoded and block every future extension. V1's hardcoded `upload.OnlineServiceQRZ` is the example: it blocks multi-destination forwarding entirely and requires editing code to add ClubLog. **Configuration belongs in configuration, not in call sites.**

---

## Code-level vs architecture-level problems

This is a classification lens that emerged during the analysis and is worth naming explicitly because it affects whether v2 is actually justified.

**Code-level problems** can be fixed in v1 as cleanup commits without restructuring anything:

- The `QsoAdditionalData` intermediate struct (delete, simplify adapter).
- The `DatabaseServiceInterface` mismatch (resolve or delete).
- The hamnut-blocks-logging bug (fixed).
- The LogQso atomicity gap (fixed).
- Dead code removal: `internal/adapters/`, `internal/ft8*`, `cmd/ft8*`, `internal/listeners/handlers/wsjtx/`, FT8-related docs.
- Missing characterization tests (add them).

**Architecture-level problems** require significant restructuring and are the real case for v2:

- Three-concerns-as-three-Wails-apps vs. daemon-and-clients topology. The current monolith-per-app shape doesn't support the daemon scope we want; a restructure is needed either way, and it's a lot of churn whether you do it in-place or greenfield.
- Serial/CAT bridge as a separate process with two frontends (rigctld-compat + SM-native). Doesn't exist in v1 at all; entirely new subsystem.
- Hardcoded forwarder fan-out redesign to support multi-destination forwarding. Touches the config model, the ingest site, and the upload queue design; not a one-commit fix.
- Multi-rig as a first-class assumption. Not supported by any v1 code; entirely new ground for the bridge and possibly for the log schema.

**Why the distinction matters:** the "rewrite vs refactor" decision is essentially "how much of the problem list is architecture-level?" If most issues are code-level, v1 is salvageable with focused cleanup. If most are architecture-level, v2 is genuinely justified and trying to evolve v1 into v2 incrementally is more work than a clean build.

**Current read:** roughly half the issues are code-level (the six fixable items above) and half are architecture-level (the daemon split, the bridge, the forwarder fan-out, multi-rig). That's enough architectural churn to justify v2 *if* the other factors (personal project, single user, learning-oriented, willing to run v1 from a tag while v2 is built) align — which they do for this specific situation. For a shipping product with users to protect, the answer would lean toward refactoring. For this project, v2 is defensible.

**Decision (2026-04-14):** v2 rewrite chosen. v1 tagged at `v1.0.0` (commit `0e158ec`), `v1` branch created for daily operational use and bug fixes, main is where v2 construction begins. See `design-decisions-log.md` → "v2 rewrite vs. v1 incremental refactor" for the full reasoning.

---

## Meta-lessons about the work itself

Things learned about how the analysis and refactoring process should go, not about the code specifically.

### Analyze before rewriting

Jumping straight to v2 without the v1 analysis would miss most of what we've surfaced in this session: the ADIF-alignment design intent, the non-functional WSJT-X listener, the deliberate three-concerns split, the specific shape of the `QsoAdditionalData` mistake, the two fixable bugs, the dead code inventory. Without the analysis, v2 would have been designed on vibes, and we'd discover some of these things *while building v2*, which is the expensive time to discover them.

**Rule of thumb:** whenever you're tempted to rewrite something significant, invest in understanding *why* the current version looks the way it does before committing to a new shape. "The old thing is bad because X" is a better foundation than "the new thing will be better because Y."

### The "oh, I see what happened there" moment is a signal of useful mode

When you can look at a past mistake and think "oh, I see what happened there, that's kind of funny" rather than feeling frustrated — you're in the headspace where good design decisions get made. Analysis sessions grind when the reviewer is angry at the code; they move when the reviewer can identify with the original author's reasoning and see the specific wrong turn without judgment.

**The `QsoAdditionalData` mistake is the archetypal example.** Past-you (or past-me, in a different project) reasoned "I shouldn't marshal the whole thing into the blob because some fields are promoted columns" and built the intermediate struct. That reasoning feels right in the moment. It's only wrong if you notice three separate things (duplication cost is trivial; column overlay makes the blob copy safely ignorable; maintaining two structs in sync by hand is a bigger tax than 50 bytes per row), and those observations don't arrive together. The fact that this mistake was easy to make and hard to notice is *exactly* why it ended up in the codebase. Being able to see it that way, and laugh about it, is the mode that produces the fix without rancor.

**Rule of thumb:** if an analysis session is making you angry at the code, stop. Come back when you can find the mistakes interesting rather than infuriating. Good refactors don't happen in an angry headspace.

### Documentation first, refactoring second

The discipline of writing down invariants, decisions, and lessons *before* refactoring has value even if some of the refactoring turns out to be deferred. The documents are durable; the session context isn't. Writing them first means the refactoring happens against a clear spec, and anyone (including future-you) picking up the work months later has something concrete to start from.

**Rule of thumb:** when a session produces substantive design insight, capture it in repo docs before doing anything else. Memory files help across sessions; repo docs help across months.

### Code-level fixes can land on v1 without committing to v2

Some of the issues surfaced in this session (the hamnut bug, the atomicity gap, the `QsoAdditionalData` simplification, the `DatabaseServiceInterface` cleanup, the dead code removal) are valuable fixes regardless of the v2 decision. They improve v1 and they simplify v2 if it happens.

**Rule of thumb:** don't defer code-level fixes to "wait for v2." Land them now, on v1, and make v1 a cleaner reference point for whatever comes next. The cost of fixing them now is low; the benefit of having a cleaner v1 (whether as the basis for refactoring or as the archived reference for v2) is real.

---

## What v1 got right that v2 must preserve

This section is the explicit counterweight to the "patterns to avoid" list. It's easy in a rewrite to throw out good decisions along with bad ones, so this list documents the things v1 got right.

1. **The ADIF-alignment of `types.Qso`** and the `additional_data` JSON blob pattern. Load-bearing design decision, correctly motivated, should be preserved verbatim in v2.
2. **The three-concerns split across logging / logbook / config apps.** Deliberate, clean, should carry into v2 as three clients of the daemon.
3. **The per-database adapter pattern scoped to each driver** (not a generic framework). The right tradeoff between code duplication and maintenance burden.
4. **The `internal/errors` operation-tagging pattern (`errors.Op`).** Works well as internal error context; keep the pattern and revisit only the HTTP serialization question when the daemon's API handlers are being designed.
5. **The `internal/iocdi` home-grown DI container.** Lightweight, flexible, suits the single-developer project; keep unless a better alternative surfaces.
6. **The `internal/enums/*` per-concept subpackage split.** Unusual for Go but works; don't fix what isn't broken.
7. **The Facade design pattern for each Wails app's backend.** The Wails-binding layer is cleanly separated from the domain logic. In v2 this shape continues, though the "domain logic" moves out of the facade and into the daemon API calls — the facade becomes thinner, serving only as the Wails binding surface and calling the daemon over HTTP.
8. **The transactional `BeginTxContext` primitive on the sqlite service.** It was there before the atomicity fix; the fix just built on it. Keep this primitive for v2 — it's the right shape for "caller-owns-transaction" patterns.
9. **Using real `&sqlite.Service{}` in integration tests instead of mocks.** The tests that exist today follow this pattern, which is the right pattern. Characterization tests added in v2 should extend this, not introduce mocking.
10. **The commit to writing the serial and CAT libraries from scratch instead of depending on hamlib.** The user's own libraries are more robust and better-integrated. Preserve this decision; v2 keeps the hand-written serial/CAT stack as the foundation.

---

## Concrete v2 scope (provisional, subject to revision)

This section is more speculative than the rest of the document — it's a first sketch of what v2 looks like informed by everything above. Revise as the analysis deepens.

**Carry forward verbatim or near-verbatim from v1:**
- `internal/types` (with `QsoAdditionalData` deleted, adapter simplified)
- `internal/database/sqlite/adapters` (simplified per pattern #1)
- `internal/errors` (internal usage; HTTP serialization TBD)
- `internal/iocdi`
- `internal/enums/*`
- `internal/serial`, `internal/cat`, `internal/ptt`
- `internal/maidenhead`
- `internal/adif`
- `internal/lookup/hamnut`, `internal/lookup/qrz`
- `internal/forwarding/qrz` (the QRZ forwarder itself; the fan-out mechanism around it needs redesign)
- `internal/logging`, `internal/utils`, `internal/config`, `internal/email`
- `internal/database/sqlite` (core package, with the simplified adapter)

**Delete, don't carry forward** (status after v1.0.0 cleanup on 2026-04-14):
- ~~`internal/ft8`, `internal/ft8x`~~ — ✅ deleted 2026-04-14 (commit `0e158ec`)
- ~~`cmd/ft8`, `cmd/ft8test`~~ — ✅ deleted 2026-04-14 (commit `0e158ec`)
- ~~FT8-related docs in `docs/`~~ — ✅ deleted 2026-04-14 (commit `0e158ec`)
- `internal/listeners/handlers/wsjtx` (and probably `internal/listeners` framework itself) — still pending
- `internal/audio` (if no non-FT8 consumer — verify) — still pending, reverse-dependency check needed now that FT8 is gone
- `internal/adapters` (the generic reflection framework) — RECLASSIFIED as "relocate with server-side DB cluster," not delete. See `bug-inventory.md` → "internal/adapters generic framework."
- `internal/database/postgres` and top-level `internal/database/` — relocate to future server repo, not delete.

**New in v2 (status as of 2026-05-02):**
- ✅ The daemon binary itself (`cmd/smd`). Owns SQLite, HTTP+JSON/ADIF server over Unix socket OR TCP, SSE event stream. Shipped milestone 1 / 1b / 1c.
- ✅ Internal `qsoservice` package — the daemon's service layer that HTTP handlers are thin wrappers over. Replaces v1's `apps/logging/backend/facade`. Shipped milestone 1.
- ✅ Redesigned forwarder configuration model supporting multi-destination fan-out (`internal/forwarding/`). Shipped milestone 1c.
- ✅ Proper upload-queue API exposed to clients (`GET /v1/qso/{id}/uploads` + SSE `forward.*` events). Shipped milestone 1c.
- ✅ Logbook-management API surface (`GET/POST/PATCH/DELETE /v1/logbook`). Shipped milestone 1b.
- 🚧 `frontend/logging/` — Svelte 5 SPA, embedded into the daemon. Replaces the original "smclient + three Wails apps" plan per ADR 0001 (2026-04-30). The SPA talks directly to the daemon from the browser via `fetch()` / `EventSource`, so a Go HTTP client (`smclient`) is no longer needed and was never created.
- ⏳ `internal/bridge` package per ADR 0013 — daemon subsystem providing rig SSE, rigctld-compat TCP, AUTO-mode CAT, PTT arbitration. Replaces the originally-planned standalone bridge binary; default deployment is single-binary. Split-host shape (`cmd/bridge` as a separate binary) is opt-in.
- ⏳ A `wsjtx-bridge` client binary that translates WSJT-X UDP → daemon HTTP (separate from the serial/CAT bridge; they run alongside each other). Milestone 3.

**Changed substantially in v2 (revised per ADR 0001, 2026-04-30):**
- The original "thin Wails clients + `smclient` HTTP + facade pattern" plan was replaced by a single-page browser SPA. The SPA *is* the thin client; there's no Wails binding layer to adapt for. Validation, draft state, and presentation live in the SPA; the daemon owns persistence, orchestration, and shared state. See `docs/decisions/0004-daemon-vs-spa-feature-split.md` and the `project_sm_daemon_vs_spa_split` memory.

**Undecided until v2 design starts in earnest:**
- ORM/generator choice (sqlboiler / Bob / sqlc / hand-rolled)
- `internal/apikey` placement (client, server, or split)
- `internal/errors` HTTP serialization shape
- Specific forwarder configuration model (needs its own design pass)
- Whether `cmd/server` gets populated as the SM-Online server in the same repo or moved to a separate one
