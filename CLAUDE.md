# Station Manager — project instructions for Claude Code

## Repository structure

- **`main`** is the v2 codebase. v2 is a clean-slate rewrite around a daemon + clients + bridges model (decided 2026-04-14). The structure.md "Migration from main's current state to milestone 1" restructure already ran (session-8 cluster): Wails `apps/` directory removed, `go.work` removed, single `go.mod` at the repo root, daemon (`cmd/smd`) and its new internal packages exist (`internal/api`, `internal/qsoservice`, `internal/events`, `internal/safego`), `internal/forwarding/` reshaped from v1's hardcoded-QRZ into the multi-destination `Forwarder` interface + worker + registry. Milestones 1, 1b, 1c are shipped (audited 2026-05-02).
- **UI toolkit progression on main:** Wails (planned, never built in v2) → Gio (decided 2026-04-21, spike landed, parked) → browser SPA (Svelte 5, decided 2026-04-30 per ADR 0001, scaffold shipped same day, consolidated into the `frontend/app/` shell (ADR 0044), embedded via `//go:embed` and served at `/app/` when `Protocol=tcp && ServeSPA=true` — the original `frontend/logging/` SPA that owned `GET /` was **retired 2026-07-21** (embed + route removed, source kept for reference); `GET /` now 302-redirects to `/app/`).
- **`internal/bridge` subsystem** (serial CAT bridge, ADR 0013 + ADR 0019) — daemon subsystem: `/v1/rig/events` SSE for read-only state display, plus a narrow inbound command path (ADR 0026), a daemon-owned tune-carrier (ADR 0027), and FT8-TX keying (ADR 0030). **Safety-critical invariant:** `tx_on`/`tx_off` are never `exposed`, and tune + FT8-TX share ONE guaranteed-stop / single-flight controller (only that controller keys TX). Enforced narrow scope: `internal/storage` and `internal/forwarder` must not import `internal/bridge` and vice versa (boundary tests + CI). Full design, gotchas, and durable mechanisms (rig-mode→ADIF translation, error i18n, pipeline supervisor, tune-restore snapshot) live in **`internal/bridge/CLAUDE.md`** (auto-loads when working there).
- **FT8 subsystem** (`internal/ft8`) — **operator-initiated**: every session starts with an operator action (the click + arming TX) and the daemon never initiates one. Daemon-initiated sequencing is out of scope — the QEX FT8 spec forbids automatic operation and unattended operation is licence-restricted in many jurisdictions. Note the precise claim: SM does not *require* the operator to remain once a run is started (a Call-CQ run works answerers until Abandon), so it is not "attended-only" in any enforced sense — staying is the operator's responsibility under their licence, not something the software checks. Links **go-ft8** (`github.com/ColonelBlimp/go-ft8`, GPL-3.0-only — the trigger for SM's GPL-3.0-only licence; see the Licence note below). A bare decode is never a QSO; a completed *exchange* is. TX (ADR 0029–0033) reuses the bridge's guaranteed-stop discipline (`tx_on`/`tx_off` never `exposed`); manual sequencing only. **Live FT8 requires a CGO build** (the static CGO-free default leaves the subsystem idle). `internal/ft8` stays narrow by import graph (no `qsoservice`/`config`/`adif` import; assembly + submit live in an injected sink). Full design/decision detail in **`internal/ft8/CLAUDE.md`** (auto-loads when working there); the single FT8 capture point is **`docs/ft8.md`**.
- **CD pipeline (Continuous Delivery) shipped 2026-05-16** at `.github/workflows/ci.yml` — gates every push to main on SPA lint/check/test/build, gofmt drift, go vet, `go test -race`, daemon embed-build, all `cmd/...` builds. Trunk-based workflow (no feature branches); the gate IS the merge protection. Local mirror via `task ci:local` runs the same steps. Releases stay operator-driven (`git tag` + `scripts/release-rpm.sh`); the pipeline only answers "if I tagged this commit right now, would the release artifact be sound?"
- **Build versioning (semver, even for dogfooding) — 2026-05-31.** `scripts/version.sh` derives the build version from `git describe` off the `v2.*` tags (baseline tag **`v2.0.0-alpha.1`**); dogfood/`task` builds get e.g. `2.0.0-alpha.1-7-gabc1234` (`-dirty` when the tree has uncommitted changes), release builds take an explicit `scripts/release-rpm.sh <ver>`. It feeds `cmd/smd` `main.Version` → daemon User-Agent + the ADIF PROGRAMVERSION stamped on forwarded/exported QSOs (so "which build logged this QSO" is answerable — the reason this matters: PROGRAMVERSION is outbound, durable data in QRZ/ClubLog). The RPM `Version:` field can't hold `-`, so it's sanitised (first `-`→`~` for pre-release ordering, rest→`.`); ordering is monotonic (`2.0.0~alpha.1` < `2.0.0~alpha.1.7.gabc1234` < `2.0.0~alpha.2` < `2.0.0`). `dev-rpm.sh` keeps the fixed output **filename** `station-manager-dev.x86_64.rpm` (stable path for `deploy:local:dev`); only the internal version is git-derived.
- **Local-dogfood update task `task deploy:local:dev`** — one command: build dev RPM → stop user-level `smd` → `sudo rpm -Uvh --replacepkgs` → systemd daemon-reload → start → status check. Use after a code change to refresh the dogfooded install without ceremony. Sudo prompts once per session for the package install; systemctl commands run as the user. **Defaults (2026-06-07) to the CGO PocketFFT backend** (set in `scripts/deploy-local-dev.sh` via `SM_FFT="${SM_FFT:-pocketfft}"`) so the dogfood deploy gets live FT8 capture + decode out of the box — the pure-Go static build leaves the FT8 subsystem idle (capture needs CGO). Override with `SM_FFT=gonum task deploy:local:dev` for the static CGO-free build. `task rpm:dev` (called directly, not via this script) is unaffected and stays CGO-free.
- **`v1` branch** (and the `v1.0.0` tag) preserves the last v1 release (a Wails app with no daemon process). Day-to-day ham radio operations run from this branch; v1 bug fixes land here.
- **FT8 preservation tags:** `ft8-snapshot-2026-05-30` is the current preservation point (the clean-room `internal/ft8` + `research/` tree at sandbox strict 129/18, removed from main 2026-05-30 — see the FT8 bullet above). The older `pre-ft8-removal` tag preserves the earlier v1-era Fortran-integration experiment tree. Both are reference points for the out-of-tree FT8 stream.

**`/v1/` in the daemon's URL prefix is API versioning (the API's first iteration), unrelated to the project's v1/v2 distinction.**

**Licence: SM is GPL-3.0-only as of 2026-05-31 (was MIT; ADR 0023).** The trigger is go-ft8 — a WSJT-X/jt9-derivative (GPL-3.0-only) that SM links for FT8 decode — pulling the combined work under copyleft. Version-3-**only** (not "or later"), inherited from go-ft8. Practical consequences for new code: new dependencies must be GPL-compatible (MIT/BSD/Apache-2.0/ISC are fine; GPLv2-only/CDDL/proprietary are not); the clean-room "never read WSJT-X source" boundary no longer applies; binary releases carry a source obligation (the public repo satisfies it). Full reasoning: `docs/licensing.md` + ADR 0023. Code published at or before 2026-05-31 (and the `v1` branch / `v1.0.0` tag) remains MIT for anyone already relying on it — relicensing isn't retroactive.

## Where the durable project context lives

**[`docs/README.md`](docs/README.md) is the authoritative documentation map** —
which docs are *live* (Tier 1, kept current and checked against the code) versus
*historical* (Tier 2, a frozen reasoning trail, never edited to reflect current
state). Read it to find the right doc; don't maintain a second index here.

**Session-start orientation is automated.** A `SessionStart` hook
(`.claude/settings.json` → `scripts/session-status.sh`, committed so it reaches
every machine) injects `docs/session-handoff.md`'s Current-state block on every
resume — and prints a **RECONCILE warning when commits exist after the
handoff's "as of" date**, because only `CLAUDE.md` + `MEMORY.md` auto-load and a
stale handoff/backlog once led a resume to re-open finished work (2026-07-05).
So: **at session end, update `session-handoff.md`'s Current state AND bump its
"(as of YYYY-MM-DD)" date** — the hook's staleness check keys off that date, and
a skipped update is exactly what the guard exists to catch. If a resume shows the
RECONCILE warning, check `git log` and confirm an item is still open before
acting on any backlog "open" line.

Before a non-trivial design choice, the load-bearing reads are still the
v1-analysis baseline — **`docs/v1-analysis/invariants.md`** (rules that must
carry forward; check proposals against it) and **`docs/v1-analysis/lessons-for-v2.md`**
(patterns to apply/avoid — the single most important pre-design read). For
decisions with genuinely-weighed alternatives, record an ADR in
**`docs/decisions/`** (append-only; format in `decisions/README.md`).

When docs and code disagree, **the code wins** — and only Tier 1 is expected to
track it. The canonical current-state references are
**`docs/v2-design/api-endpoints.md`** (every HTTP route — update in the same
commit as any route change) and **`docs/v2-design/config.md`** (config.json
shape/validation); the other `docs/v2-design/` files are historical design
briefs, not current references.

## Load-bearing invariants (headlines)

Full list in `docs/v1-analysis/invariants.md`. Never violate these without explicit discussion.

- **Enrichment never blocks logging.** External lookups (hamnut, QRZ) degrade the QSO draft's completeness when they fail; they never prevent the operator from logging. The only thing that should stop logging is a broken local DB.
- **One-fails-all-fail for QSO writes.** The QSO row and its upload-queue row(s) are atomic in a single transaction; cache/enrichment writes live outside the transaction as best-effort.
- **Narrow daemon scope (package-boundary, per ADR 0013).** The daemon's log/forward subsystems are narrow — ingest, validate, store, forward, emit status, serve queries. Rig control / CAT / PTT / audio / capture UX must not couple with those subsystems. Under ADR 0013 the bridge runs as a daemon subsystem (`internal/bridge`) in the default deployment. Enforced at the **package-import graph**: `internal/storage` and `internal/forwarder` must not import `internal/bridge`, and vice versa. The split-host opt-in (separately-built `cmd/bridge`) preserves the original process-boundary enforcement for that topology. A proposal that wires log/forward code through the bridge package (or vice versa) contradicts a decided constraint. Boundary tests in `internal/bridge/boundary_test.go` defend this; CI catches violations. (The parallel rule for the FT8 subsystem under ADR 0021 is parked — FT8 was moved out of the SM tree 2026-05-30; if it returns as an in-tree subsystem, the sibling-isolation rule with `internal/bridge` returns with it.)
- **`types.Qso` follows the ADIF specification.** The data model mirrors ADIF; adding a new ADIF field is a one-line change. The shape is load-bearing — do not propose flattening or reshaping without checking the invariants doc first.

## Lessons to apply (headlines)

Full list in `docs/v1-analysis/lessons-for-v2.md`. These patterns were explicitly extracted from v1's mistakes; each one has a cautionary tale attached.

- **Build specific, not generic.** Three specific implementations are easier to read, debug, and evolve than one clever abstraction. A small amount of duplication is cheaper than a framework. Don't DRY things up prematurely. Cautionary tale: v1 `internal/adapters/` — 30+ test files of reflection-based adapter framework, abandoned as "too complicated to maintain and use correctly."
- **Integration tests over mocks.** Real `&sqlite.Service{}` with in-memory databases is the pattern v1 uses correctly. If an interface's only consumer is a mock, delete the interface and the mock. Cautionary tale: v1 `DatabaseServiceInterface` drifted out of sync with the concrete type because nothing real ever used it.
- **Document intent, not mechanism.** Code shows what; doc comments should explain why. Non-obvious design decisions deserve a `doc.go`. Cautionary tale: the `additional_data` blob pattern had no `doc.go`, so the ADIF-alignment intent had to be rediscovered during analysis.
- **Asymmetric round-trips are a clue.** If a converter pair has one elegant side and one manual/complicated side, the complicated side is overbuilt. Cautionary tale: the `QsoAdditionalData` intermediate struct had a 100-line hand-mapped forward direction next to a 20-line `json.Unmarshal` reverse direction; the asymmetry was the code telling us the forward direction was wrong.
- **Explicit fallbacks for every external dependency.** The first test to write for enrichment code is "what happens when the service is down," not the second. Cautionary tale: hamnut-blocks-logging — a failed hamnut lookup propagated as fatal and prevented QSO entry entirely.
- **Transaction boundaries are conscious design.** Multi-table writes need an explicit decision about what's atomic and what's best-effort. Cautionary tale: v1 `LogQso` ran four sequential non-transactional writes and could leave a QSO stored without its upload-queue row.
- **Characterization tests before refactoring.** Write tests that freeze current observable behavior first; then refactor against them. Don't trust "CI passes" as evidence the refactor is correct unless CI was exercising the behavior you touched.
- **Enumerate all API consumers before designing any endpoints.** The three client apps (logging, logbook, config — now Svelte SPAs per ADR 0001, originally planned as Wails apps) have different needs; a logging-centric API sketch will miss the logbook-management surface entirely.

## Code style

Standard Go idioms (`gofmt`, `goimports`, Effective Go). Project-specific notes that override generic advice:

- **Short single-letter names are encouraged** for tight scopes and method receivers — consistent with Go convention (`r Reader`, `s *Service`, `t *T`). Avoid generic names like `data`, `item`, or `value` unless the context makes purpose obvious.
- **Use `internal/errors` for operation-tagged errors.** The canonical idiom is a package-scoped `const op errors.Op = "pkg.Func"` followed by `errors.New(op).WithErr(err).WithMsg("context")` (or `.WithMsgf(...)` for formatted messages). All builder methods are nil-safe and chainable. Prefer this over `fmt.Errorf` in code that belongs to the tagged-error paths. There is no `Errorf` convenience method — if you need to wrap a formatted cause, use `.WithErr(fmt.Errorf("...: %w", err))` explicitly. See `internal/errors/doc.go` for the full pattern and `docs/reviews/internal-errors.md` for the review that shaped it.
- **Use `internal/iocdi` for service lifecycle and dependency wiring.** The `di.inject:"..."` struct tag convention on Service fields is how dependencies are resolved. Don't hand-wire services in new code; register them with the container.
- **Comments explain *why*, not *what*.** Well-named code documents its own mechanism. Document non-obvious constraints, invariants the code relies on, and the reason a specific approach was chosen over alternatives. Don't narrate what a function obviously does; don't reference specific callers or tickets (that belongs in PR descriptions).
- **Keep functions short and focused** on a single responsibility. Avoid deep nesting and long parameter lists.
- **Line length 100–120 characters** is a reasonable ceiling; `gofmt` doesn't enforce it but readability suffers past that.
- **For Svelte frontends:** prefer Svelte 5 runes (`$state`, `$derived`, `$props`) over Svelte 4 syntax. Use snippets for reusable UI chunks within components when logic is tightly coupled; reach for separate files when it isn't.
- **SPA lint + format are wired up.** From `frontend/logging/`: `npm run lint` (eslint with type-checked TS rules), `npm run lint:fix`, `npm run format` (prettier across `src/`), `npm run format:check`. ESLint config is the modern flat format (`eslint.config.js`); prettier owns formatting via `eslint-config-prettier`. When a lint disable is genuinely warranted (e.g. svelte-eslint-parser inference gaps, intentionally non-reactive plumbing inside a `.svelte.ts` module, two-way `bind:value` patterns that look like passive `$state` mirrors but aren't), write a why-comment immediately above the `eslint-disable-next-line`. Never a bare disable.

## Project idioms

Conventions that carry forward from v1 and that new v2 code should follow. These are real project rules, not general Go advice.

- **`internal/types` only imports the Go standard library.** It is deliberately dependency-free to prevent cyclic dependencies — the whole project imports from `types`, so anything `types` imports is transitively imported everywhere. Adding a non-stdlib import to `types` is almost certainly a mistake; push the dependency outward into the consumer package instead.
- **Reuse `types.X` rather than building parallel structs.** When a config block, request body, or response payload describes the same shape as an existing `types.X` struct (e.g. `types.Qso`, `types.LoggingStation`), embed or alias the canonical type — don't define a parallel `XConfig`, `XRequest`, or `XPatch` that duplicates fields. Parallel structs drift, double the maintenance cost, and bury the ADIF/JSON tag conventions that already live on `types.X`. Use `json.Unmarshal` overlay + stash-restore for read-only fields when only a subset is settable; build a parallel struct only when fields fundamentally don't overlap.
- **Service lifecycle pattern.** Services follow `Initialize()` → `Open()` / `Start(ctx)` → `Close()` / `Stop()`, with all lifecycle methods idempotent. `Initialize()` validates config and dependencies; `Start` / `Open` begins operation; `Stop` / `Close` does graceful shutdown. New services should match this shape for composability with existing DI wiring.
- **DI `ServiceName` constants.** Each service exposes a `ServiceName` constant used as its bean ID in the `iocdi` container. Shared service names live in `internal/types/services.go` (e.g. `types.SqliteServiceName`); app-specific services define their own locally (e.g. `facade.ServiceName = "logging-app-facade"`). This convention keeps DI wiring discoverable by grep.
- **sqlboiler-generated models are not hand-edited.** Files under `internal/database/sqlite/models/` are regenerated from the schema; hand edits get clobbered on the next run. The ORM choice itself is undecided for v2 (see `docs/v1-analysis/design-decisions-log.md` → "ORM / query generator choice"), but while sqlboiler is still in use, treat generated output as read-only.
- **Runtime data path resolution via `utils.WorkingDir()`.** Anything that needs the on-disk data directory (SQLite DB, logs, config) calls this helper. Resolution order: explicit argument → `SM_WORKING_DIR` env var → the executable's own directory. Don't hand-roll path resolution elsewhere.

## Error handling

- **Handle errors explicitly.** No silent failures. Log errors with operation context using the `errors.Op` pattern.
- **Enrichment errors are never fatal.** External service failures log a warning and fall through to a cached value or a default. See the "Enrichment never blocks logging" invariant.
- **Wrap errors with meaningful context** as they propagate; don't return naked errors from deep call stacks.

## Claims about external systems (operator directive, 2026-07-29)

Anything asserted about a system outside this tree — the rig, CAT, the OS,
PipeWire, a third-party API — must be **cited**. A claim carrying no quote, no
source line and no measurement is a guess, and must be labelled one out loud.

- **Cheapest source first: grep → doc → passive observation → keyed
  transmission.** Each step costs more than the last, and the final one spends
  the operator's time and a transmission from a licensed station. Never reach
  past a cheaper source that would settle the question.
- **Grep our own tree before theorising about the rig's behaviour.** What *we*
  send it is usually the entire explanation. Cautionary tale: four rounds spent
  theorising about why the FTdx10 pushes `RM` frames unsolicited, when the
  answer was `{"name": "INIT", "cmd": "AI1;"}` in our own rigdef — one grep,
  available every round, missed every round, at the price of two wasted on-air
  transmissions.
- **Read the whole page, not the line you went looking for.** Terminating the
  search on first confirmation is the actual failure mode, and it is invisible
  from the inside because the check *succeeded*. Cautionary tale: the `RM` page
  was opened to correct the `RM4`=ALC/`RM5`=PO mapping, and the `P1=0` legend
  directly above it — which documents the pushed frame, the whole question at
  issue — went unread; the write-up then asserted "the manual is silent here"
  about a page that had been read.
- **Never let an unverified inference become a premise.** Conversation summaries
  keep conclusions and drop the evidence under them, so a guess that survives
  one compaction returns looking like established ground. Write a verified
  external fact into a code comment or doc **with its citation, at the moment it
  is verified** — worked example: the quoted AI and `RM P1=0` clauses in
  `internal/bridge/meters.go`.
- **An anomaly dismissed twice is a finding, not noise.** Cautionary tale: three
  blank transmissions *were* the drive-collapse signature under investigation,
  and were written off as instrument failure twice before a controlled test
  forced the issue.
- **Don't blame the source for your own inference.** Check it clause by clause
  before concluding a manual or spec is wrong. Misattribution hides the real
  defect — a reading habit — and teaches "distrust the documentation, probe the
  hardware", which is the most expensive path available.

## Acceptance criteria (ATDD) (operator directive, 2026-07-29)

TDD says: state the behaviour, write the tests, then the code. ATDD puts one
step in front of that — **state what the OPERATOR will observe when the feature
works, before choosing any mechanism.** Package-level tests can be entirely
sound and still prove nothing about whether the feature answers the question it
exists for. Cautionary tale: `internal/bridge/meters_test.go` rules R1–R15c were
written test-first and were correct; not one of them asked whether the log could
tell "no RF left the rig" apart from "the instrument is broken". That
distinction was the entire point of the feature, and establishing it empirically
cost two days and 24 on-air transmissions.

**Shape of a criterion** — one or two plain sentences:

```
When <situation>, <observable outcome>, and I can tell it apart from
<the nearest confusable state>.
```

The third clause is load-bearing and is the one usually missing. The nearest
confusable state is where the defects live: no-RF vs dead instrument, alarm
stuck vs rig genuinely still keyed, dial moved vs noise.

**Who writes it.** Claude drafts, the operator checks — the operator will
normally delegate the drafting. That delegation only works if the check stays
cheap, so:

- **State it in operator-observable terms** — a logged QSO row, RF keyed or not,
  an SSE frame, a banner, a spot emitted. If checking it requires reading the
  code, it is written at the wrong level.
- **Present it BEFORE the mechanism is chosen**, as its own short artefact,
  while changing it is still cheap. Never back-fill a criterion from an
  implementation already sketched: a criterion that describes what you were
  going to build anyway is the ATDD equivalent of a test that passes before the
  fix exists.
- **Mark the judgement calls; do not fill them in.** Where a criterion needs a
  tolerance, a timeout, or a definition of "collapsed", flag it as an open
  question for the operator. Every threshold invented without asking has been
  wrong.

**Three layers, no framework.**

1. **The criterion**, recorded in the acceptance test's file header and in the
   ADR where one exists. Worked example of reasoning-in-the-header:
   `internal/ft8/dialguard_test.go`.
2. **Automated acceptance at the daemon boundary** — real service, assertions on
   HTTP responses, SSE frames and log output, never package internals. Already
   house style: real `&sqlite.Service{}` with in-memory DBs. Playwright for
   anything the operator sees in the SPA.
3. **On-hardware acceptance procedure for rig/TX-touching work**, written down
   BEFORE the build, with the expected observation for each step. **Passive
   first:** `cmd/catcli` and the logs answer most rig questions for free; spend a
   keyed transmission only where passive observation cannot settle it. The two
   wasted on-air transmissions of 2026-07-29 were an acceptance test run at the
   most expensive layer available, because nothing cheaper had been written.

**No BDD framework** — no godog, no Cucumber, no Gherkin runner. Plain Go tests
named after the criterion get essentially all of the value. A BDD runner is the
exact shape of `internal/adapters/`: 30+ test files of framework, abandoned as
too complicated to maintain and use correctly.

**The loop.** Outer acceptance test RED → inner TDD cycles until it goes green.
The TDD rules below apply unchanged to the inner cycles — including the
reversion proof, which the outer test also owes: it must fail for the right
reason before the feature exists.

## Testing

- **TDD IS THE ROUTE, NOT AN OPTION (operator directive, 2026-07-27).** Determine the
  behaviour required → write the tests → then write code against the tests. If the
  behaviour needs to grow, that is fine: expand the tests, then the code. Never the
  other way round.
  - **State the behaviour before choosing a mechanism.** Where a rule involves a
    judgement the code cannot settle — a tolerance, a timeout, what counts as
    "moved" — that is the operator's call. Ask; do not infer a plausible number.
    Every threshold invented without asking has been wrong.
  - **Run the tests RED before implementing**, and make the red informative: they
    must compile and fail on an assertion, not fail to build. A test that passes
    before the fix exists proves nothing (a "preservation" test that merely counted
    callbacks passed against the un-fixed code, 2026-07-27).
  - **Keep the reversion proof.** After going green, revert the implementation and
    confirm the test fails for the RIGHT reason. That is what distinguishes a test
    that pins behaviour from one that describes whatever the code happens to do.
    Two ways a proof lies, both seen on 2026-07-29:
    - **It never applied.** A scripted revert whose pattern does not match leaves
      the code untouched and the test "passes" — certifying the implementation it
      was meant to challenge. Once from a `\t` pattern run against a
      space-indented file, once from a `grep -c` guard that counted a
      pre-existing identical line elsewhere in the file. **Assert the pattern
      matched before running the test.**
    - **It went red somewhere else.** Routing the drive alarm through
      `raiseTxAlarm` turned its tests red on *"no drive alarm to test against"* —
      they died in setup and never reached the assertions they exist to make.
      Read the failure message: if it is not the rule's own assertion, the proof
      is worthless. And if red→green straddled an edit to the TEST, revert only
      the implementation half and re-prove.
  - **TDD orders the WORK, not the commits — ship tests and implementation as ONE
    commit.** A commit holding only the RED tests leaves `main` failing, and CI
    gates every push to `main` (`.github/workflows/ci.yml`, `on: push`), so the
    split turns the gate red on a commit that was never a release candidate and
    puts a known-broken revision in the path of `git bisect`. Cautionary tale:
    `638b3198` (drive-alarm tests) and `1be5ae65` (the detector) went up as two
    commits on 2026-07-29; the first was red on main and the clean-room review
    correctly filed it P1. Nothing real is lost by combining them — the RED step
    is a process artefact, and what makes it durable is the test-file header
    reasoning plus the reversion proof, neither of which git history holds.
  - **A failing test is one of two things, and they are not the same:** the code
    does not meet the spec (a bug — fix the code), or the spec is wrong/incomplete
    (a design question — settle it with the operator, then expand the tests). Being
    unable to tell these apart is what turned one FT8 dial-attribution issue into a
    ten-round arc of plausible fixes that each undid the last.
  - **Assert on what an operator or another subsystem can OBSERVE** — a logged QSO
    row, RF keyed or not, a published SSE frame, a spot emitted — not on fields the
    current mechanism happens to carry. Field-level assertions from that arc were
    all deleted within a round or two; the behavioural ones caught real defects.
  - **Feed inputs where right and wrong actually differ.** A test whose fixture makes
    both paths agree proves nothing, however behavioural its name. **The check: for
    each rule, ask whether THIS fixture would produce a different value under the
    implementation you are guarding against.** If not, the fixture is decoration.
    This rule already existed and still caught none of three instances in one day
    (2026-07-30), so learn the shapes rather than the maxim:
    - **The fixture never exercises the interval.** A rule that post-unkey silence
      must not be counted, tested with a fixture that fed no frames after the
      unkey — so the sealed and unsealed paths agreed, and the defect shipped.
    - **The fixture never writes the state under test.** A rule that a running
      maximum does not leak between transmissions, tested with a *silent* first
      transmission — whose silence is computed at flush as a local and never
      touches the field that leaks. It passed against code that never reset it.
    - **The fixture asserts the defect as the intent.** A map test proved "prefer
      coordinates over grid" by pairing London coordinates with a Malawi grid.
      That contradiction WAS the bug; the test pinned it as correct behaviour for
      as long as it existed. A precedence rule whose fixture is a contradiction
      cannot demonstrate precedence.
  - **The test must be as strong as the RULE it claims to pin.** Three rounds
    running (2026-07-27) the finding was "your test proved a weaker statement than
    your rule": a rule about the cancellation path tested only the synchronous one;
    a rule that EVERY transmitter is stopped tested only that A hook existed; a rule
    about where the guard is wired entered at the seam it was wired to. Read the
    rule and the assertion side by side and ask what an implementation could get
    wrong while still passing. If the answer is "quite a lot", the test is weaker
    than the rule.
  - **Enumerate the states a change CREATES.** Adding a binding creates unbound,
    stale and mismatched states; adding a wait creates an ordering. Each is a rule
    to write before implementing. Every finding in the 2026-07-27 dial-guard arc was
    a state the previous round's fix had just introduced.
  - **Enumerate the STEPS too, and name which one a rule means.** The same
    discipline applied to time rather than state. Where a rule refers to a moment —
    "at unkey", "when the request completes", "on save" — an operation is usually a
    SEQUENCE, and the steps can be seconds apart. List them, pick one, and write the
    choice into the test header; if two candidate steps differ by more than the
    quantity being measured, the choice is load-bearing, not a detail. Cautionary
    tale: "the window ends at unkey" took FIVE review rounds and four real defects
    (2026-07-30), every one from treating `releaseFt8TxChecked`'s
    issue → ACK → confirm → settle → restore as a single instant — measuring to the
    end of the sequence, freezing one field but not another, taking the instant after
    the write returned (CI-V waits for the ACK), then sealing after the write so
    frames arriving mid-write still counted. Each fix was correct about the step it
    named and silent about the next one. Related: an instant chosen inside a sequence
    creates a rollback state — if a later step FAILS, does the earlier decision still
    hold? (Here a failed `tx_off` had to unseal, because the transmission was not
    over.)
  - **A risk you NOTICE and dismiss must be dismissed on evidence, not on estimated
    cost.** The words to catch yourself on are "narrow race", "not worth testing",
    "can't be tested deterministically", "acceptable in practice". Each is a
    decision to ship a known defect, so before making it, check whether the system
    ALREADY CARRIES the fact that would settle it — a value already computed, an
    existing measurement, one grep. State the evidence in the dismissal, or do not
    dismiss. Two cautionary tales from the same afternoon (2026-07-30): the drive
    recovery rested on whether an alarm timer had run, a race spotted at build time
    and waved off as untestable, while `meterGapAtUnkey` was returning the frozen
    measurement that answered it and the call site discarded it with `_` — the fix
    was to stop ignoring a value already in hand, and it REMOVED state. And a code
    comment asserted the SPA drops its drive banner on disconnect, citing
    `resetCatLink`, which is a test seam with no production caller; the real handler
    only sets `rig.cat = 'lost'`, so the daemon was discarding a report the operator
    could still see a banner waiting for. Both were found by review, not by the
    reasoning that had already been over the ground.
  - **If a behaviour test cannot be written without inventing a fact the system does
    not carry, the SYSTEM is missing that fact.** Do not substitute a threshold or a
    heuristic. Worked example: `internal/ft8/dialguard_test.go`, written before its
    implementation, with the reasoning for each rule in the file header.
- **Integration tests are the default** for anything touching storage, services, or cross-package flows. Use real `&sqlite.Service{}` with in-memory SQLite databases. Do not introduce mock interfaces for internal services.
- **Unit tests for pure logic** — parsing, math, format conversions, adapter round-trips. No mocks needed.
- **Test the error path first for enrichment code.** Per the invariant: "what does this do when hamnut/QRZ is down" is the test that should exist before the happy-path test is written.
- **Characterization tests before refactoring** untested code. Freeze the current behavior with tests, then refactor.
- **Cover edge cases and error paths,** not just the happy path.
- **Go:** standard `testing` package. `github.com/stretchr/testify` is fine when it makes assertions cleaner.
- **TypeScript/Svelte:** Vitest for unit and component tests; Playwright for E2E.

## Dependencies

- **Minimize external dependencies.** Prefer the standard library. Document why a non-obvious dependency is needed.
- **Home-grown choices are deliberate.** `internal/iocdi` (DI), `internal/serial` and `internal/cat` (rig control), the custom ADIF parser — all chosen after considering alternatives and judged lighter or better-fit than the popular options. Don't suggest replacing them with `uber-go/fx`, `google/wire`, hamlib, etc. without a concrete reason specific to this project's needs.
- **Keep dependencies current** but don't upgrade prophylactically during unrelated work — version bumps get their own commit.

## Commits and version control

- **Atomic commits with meaningful messages.** One logical change per commit. Commit message explains the *why*, not just the *what*.
- **Don't commit secrets.** `.env`, credentials, configuration with real tokens are gitignored; don't add them.
- **Don't touch the `v1` branch** unless fixing a v1 bug the user surfaced while running the software. Main is where v2 work happens; v1 is frozen except for user-reported fixes.
