# `frontend/app` review

**Date:** 2026-08-14  
**Scope:** `frontend/app` only  
**Change policy:** Review and report only; no application-code changes

## Executive summary

`frontend/app` is a substantial, thoughtfully designed operator SPA, not the scaffold its README still describes. The strongest parts are its daemon-authoritative state model, careful whole-block configuration writes, explicit concurrency guards in several async flows, RF confirm-by-push design, SSE revival policy, and unusually broad unit/component coverage.

The review found **nine actionable items**:

- **2 P1 data-integrity risks:** a malformed station-config success can create an empty whole-block save baseline, and QSO re-enrichment is not tied to the callsign that produced it.
- **5 P2 risks/gaps:** incomplete runtime validation at HTTP/SSE boundaries, ambiguous writes reported as failures, incomplete modal keyboard/focus ownership, an empty default Dashboard, and no real-browser test layer.
- **2 P3 resilience/maintenance gaps:** shallow session-storage hydration and stale status/version/build documentation.

The static quality gate is clean: lint, formatting, Svelte/TypeScript checks, all **1,206 tests in 88 files**, the production build, and the offline production-dependency audit passed.

## Scope and method

The review covered the production source, tests, build configuration, package metadata, and generated bundle characteristics under `frontend/app`. It focused on:

- application composition and routing;
- API and SSE boundaries;
- operator and RF state transitions;
- configuration data safety;
- async races and cancellation;
- error and ambiguous-write handling;
- keyboard and modal accessibility;
- persistence, security, performance, documentation, and test strategy.

Backend implementation and runtime behavior were deliberately not reviewed. Where a finding discusses the daemon contract, it relies only on the contracts, comments, and types declared in `frontend/app`.

Priority meanings used below:

- **P1:** can corrupt or erase operator data through an ordinary UI action; address before treating the app as production-complete.
- **P2:** significant correctness, safety communication, accessibility, or release-confidence gap.
- **P3:** resilience or maintenance issue with limited immediate operational impact.

## Findings

### F-01 — P1 — A malformed station-config success can become a destructive whole-block baseline

**Evidence**

- `frontend/app/src/lib/api/config.ts:49` accepts any top-level object as a valid config. A missing, array-valued, or otherwise invalid `logging_station` becomes an empty string map at line 51.
- `frontend/app/src/lib/config/station.svelte.ts:81` applies that result and marks the section loaded. `#apply` then fills every known station field with `''`.
- The callsign is read-only in `frontend/app/src/lib/config/StationSection.svelte`, so the operator cannot repair a missing callsign in this form but can edit another field and enable Save.
- `frontend/app/src/lib/api/config.ts:69` sends the resulting `logging_station` back as a whole-block PUT. The file's own contract says omitted fields in this block are zeroed.
- The same permissive parser is used for the save response, so a 2xx response without `logging_station` is also accepted as an empty authoritative result.

**Impact**

A proxy error, version skew, or daemon regression returning HTTP 2xx with `{}` (or a malformed `logging_station`) renders a blank but "loaded" Station form. Editing one field and saving can replace the station identity block with blanks, including the read-only station callsign and unrendered fields. This defeats the otherwise strong `loaded` precondition because the read was syntactically successful but semantically invalid.

**Recommended action**

Make `logging_station` a required plain-object block for both GET and successful PUT responses. Reject the response unless identity invariants such as a non-empty string `station_callsign` hold once setup is complete. Keep the section unloaded on rejection.

Add boundary tests for `{}`, missing/null/array `logging_station`, invalid member types, and malformed save responses. Include a state-level test proving that an invalid baseline can never make a PUT reachable.

**Acceptance criteria**

- A malformed or missing `logging_station` is surfaced as a load/save error.
- Station Save cannot become enabled from an invalid baseline.
- A malformed successful response cannot clear the live station context or the form.

### F-02 — P1 — QSO re-enrichment results are not bound to the callsign that produced them

**Evidence**

- `frontend/app/src/lib/logbook/EditQsoModal.svelte:83` captures the current callsign and starts an async lookup.
- After the await at line 87, the code does not confirm that `form.call` still matches that captured callsign before applying country, name, grid, and hidden DXCC/zone/continent values at lines 93–110.
- `enrichExtras` stores no callsign provenance. `buildPatch` later includes those hidden values whenever they exist, even if the callsign changed after a completed lookup.
- The callsign input remains editable during the lookup, and changing it does not invalidate the prior note or extras.
- The normal logging enrichment flow already demonstrates the required pattern: `frontend/app/src/lib/operate/enrich.svelte.ts:154` uses a generation and AbortController, then line 177 verifies that the draft callsign still matches before applying results.

**Impact**

An operator can start Re-enrich for callsign A, correct the callsign to B while the request is in flight, and then receive A's details into B's QSO. The same mismatch occurs if A's lookup completes and the callsign is changed afterward: the hidden DXCC/zones/continent remain attached to A. Saving persists and may re-forward the misattributed enrichment.

**Recommended action**

Give the modal lookup the same generation/cancellation discipline as the live logging enrichment. Associate hidden extras with the normalized callsign and emit them only while it still matches. On callsign input, invalidate the prior lookup note and hidden extras; if visible enrichment fields are retained for manual review, explicitly mark them stale rather than silently presenting them as B's lookup.

**Acceptance criteria**

- A response for A can never alter a form currently containing B.
- Hidden enrichment fields are never sent for a different callsign.
- Closing/reopening or changing the callsign invalidates or aborts the prior lookup.
- Tests cover a slow A response landing after the form moves to B and a callsign edit after a successful A lookup.

### F-03 — P2 — Runtime wire validation is inconsistent at success and safety-state boundaries

**Evidence**

- `frontend/app/src/lib/api/qso.ts:89` validates only that a successful body has a UUID. Any status other than the literal `duplicate` is classified as `stored` at line 105. A successful `{status: "unexpected", uuid: "..."}` therefore clears the draft and adds a session row; a malformed 200 duplicate response can be reported as a new log.
- `frontend/app/src/lib/api/ft8-sse.ts:171` and `frontend/app/src/lib/api/rig-sse.ts:88` treat every JSON-parsable value as the requested TypeScript type. The tests prove invalid JSON is dropped but do not cover valid JSON with the wrong shape.
- Those payloads feed safety-relevant UI state: tune state, TX/drive alarms, audio levels, rig frequency/mode, FT8 occupancy, and the active QSO ladder. For example, a string `active` value is assigned directly to boolean state, and a non-array FT8 `decodes` value can throw when spread by its consumer.
- `frontend/app/src/lib/api/logbooks.ts:88` and line 141 similarly cast list and QSO-page items without per-row validation; `qso-patch.ts` accepts any non-null object, including arrays, as a QSO.

**Impact**

Version skew or malformed-but-valid JSON can produce false success, corrupt displayed safety state, throw inside an EventSource listener, or inject unusable rows into long-lived stores. The app's central helper says the API layer is the last runtime boundary, but important endpoints do not currently enforce that policy.

**Recommended action**

Introduce small endpoint/event-specific decoders rather than a single permissive cast. Prioritize:

1. exact QSO submit status plus expected HTTP status (`201/stored`, `200/duplicate`);
2. rig/FT8 alarm, tune, meter, audio, frequency, slot, and decode payloads;
3. logbook identifiers and list/page row keys;
4. QSO GET/PATCH success bodies.

Invalid frames should be dropped with rate-limited diagnostics and must leave the last known good state intact. Add valid-JSON/wrong-shape tests alongside the existing invalid-JSON tests.

**Acceptance criteria**

- Unknown submit statuses return `malformed_response` and preserve the draft.
- No malformed SSE value can change a safety-relevant state field or throw in a consumer.
- List/page/edit consumers receive only minimally valid records.

### F-04 — P2 — Most writes flatten an ambiguous transport outcome into a definite failure

**Evidence**

- `frontend/app/src/lib/api/_helpers.ts:47` documents that a timed-out write may already have committed and says callers should report "outcome unknown" and reconcile. Lines 53–58 explicitly record that only FT8 config currently does so.
- QSO submit and session email also use intentionally cautious operator wording, but most other write APIs return a generic error: Station/Email/Enrichment/Forwarding/Rigs config saves, setup, QSO PATCH, upload enqueue, daemon restart, rig tune/commands, and FT8 arm/QSO commands.
- Several write paths call `safeFetch` without `WRITE_TIMEOUT_MS`, so they use the 15-second read default despite the separate 30-second write constant. Examples include `api/config.ts:69`, `api/rig-command.ts:46`, `api/ft8qso.ts:50`, and `api/uploads.ts:30`.
- Confirm-by-push RF flows can therefore show an error toast while SSE subsequently shows the requested state, and config/setup flows invite a retry after a commit the UI has labelled failed.

**Impact**

Operator guidance can contradict daemon state. Retrying can repeat non-idempotent work or create confusing conflict responses; setup can remain on the welcome surface after it has actually completed; a config save can be persisted while the form remains dirty and says it failed.

**Recommended action**

Define a shared write outcome that distinguishes definite rejection, caller abort, transport failure before confirmation, and outcome unknown. Apply the longer write timeout consistently. Then choose reconciliation by operation:

- config/setup: re-read the authoritative block/context and compare the edited fields;
- rig/FT8 commands: defer the final claim to the existing pushed state, with an "acknowledgement unknown" message if needed;
- QSO PATCH/upload/restart: re-query observable state or give explicit, operation-specific retry guidance;
- genuinely non-reconcilable operations: say that the outcome is unknown rather than failed.

**Acceptance criteria**

- No timed-out write is described as definitely rejected unless an HTTP response says so.
- Every state-mutating request uses an intentional timeout and documented reconciliation policy.
- Tests cover commit-then-timeout behavior for each policy class.

### F-05 — P2 — Modal behavior does not consistently own keyboard and focus

**Evidence**

- `frontend/app/src/lib/operate/ExportDialog.svelte:133` declares `aria-modal="true"` but has no Escape handler, initial-focus behavior, focus trap, background `inert`, or focus restoration.
- Escape for that dialog is implemented indirectly in `LoggingCard.svelte:100`. `LoggingCard` is not mounted in the FT8 branch of `Operate.svelte`, while the shared Session panel can open Export in either mode. Escape therefore does not close Export in FT8 (and depends on an unrelated card in Phone/CW).
- `frontend/app/src/lib/operate/DuplicateDialog.svelte:12` explicitly records that Tab can reach the background.
- `frontend/app/src/lib/logbook/EditQsoModal.svelte:170` owns Escape and initial field focus, but it likewise does not trap focus, inert the background, or restore focus to the opener.

**Impact**

Keyboard behavior changes with the workspace even though the same modal is visible. Focus can remain on or move into background controls behind an element advertised to assistive technology as modal; on close it may be lost rather than returned to the initiating action. This is especially risky in an operator UI with global keyboard shortcuts and RF controls.

**Recommended action**

Create one shared modal behavior/primitive (native `<dialog>` where compatible, or a tested focus-scope action) that owns Escape, initial focus, Tab containment, background inertness, close guards during writes, and focus restoration. The modal itself—not a host card—must own its keys.

**Acceptance criteria**

- Export closes with Escape in Phone/CW and FT8.
- Tab/Shift+Tab remain within each modal; background controls and shortcuts cannot act.
- Focus returns to the opener after every close path.
- Behavior is tested in a real browser, not only jsdom.

### F-06 — P2 — The default route and visible Dashboard navigation lead to an empty placeholder

**Evidence**

- `frontend/app/src/lib/router.svelte.ts:36` maps `/`, unknown paths, and unrecognized deep links to `dashboard`.
- `frontend/app/src/App.svelte:112` renders only a heading and a 60vh dashed placeholder for that view.
- Dashboard is the first visible item in the persistent sidebar, and `/app/` is the canonical base route.

**Impact**

A returning operator who opens the app root lands on an apparently unfinished empty page and must know to choose Operate. Unknown links are also silently normalized into the same blank Dashboard, obscuring routing mistakes instead of showing a useful not-found state.

**Recommended action**

Until Dashboard has real content, make the base route redirect to the last-used Operate mode and remove or label the Dashboard entry. Treat unknown routes separately with an explicit not-found/redirect decision rather than folding them into Dashboard.

**Acceptance criteria**

- `/app/` lands on a functional surface.
- The sidebar contains no navigation item whose destination is only a placeholder.
- Unknown paths have deliberate, tested behavior distinct from the home route.

### F-07 — P2 — Release confidence depends entirely on jsdom; browser-only behavior is unverified

**Evidence**

- `frontend/app/package.json:18` exposes only Vitest scripts; there is no Playwright/Cypress or accessibility-audit dependency/configuration.
- `frontend/app/vite.config.ts:47` runs the whole suite in jsdom.
- Eleven test comments explicitly acknowledge browser gaps including no layout, rendering, inert support, real EventSource, or suspended-tab behavior. These overlap the app's highest-risk features: fixed rails/drawers, focus/modal behavior, history navigation, SSE revival, maps, and RF keyboard shortcuts.
- The unit/component suite is excellent in breadth—1,206 passing tests—but cannot establish geometry, focus containment, actual History/EventSource integration, or accessibility-tree behavior.

**Impact**

Regressions can pass the full gate while breaking the operator's real browser workflow. F-05 is an example: component structure and jsdom checks do not prove that a modal owns focus and Escape in both workspaces.

**Recommended action**

Add a deliberately small browser suite around critical journeys rather than duplicating unit coverage:

- base/deep-link routing and Back/Forward with unsaved Settings;
- Phone/CW and FT8 keyboard ownership;
- all modal focus/Escape/restore paths, with an automated accessibility scan;
- rail/drawer/modal layout at the supported width floor and in both themes;
- EventSource drop/reconnect/catch-up against a controllable fixture server;
- setup to first log, QSO duplicate handling, and one settings whole-block save.

**Acceptance criteria**

- A real-browser smoke suite runs in the normal frontend gate/CI.
- Accessibility checks cover every modal and the primary shell routes.
- The suite catches the workspace-specific Export/Escape failure described in F-05.

### F-08 — P3 — Session persistence claims fail-soft hydration but validates only the outer array

**Evidence**

- `frontend/app/src/lib/operate/session.svelte.ts:32` says malformed storage should fail soft.
- Hydration checks only that `qsos` is an array, then casts every member to `SessionQso` at line 45.
- Consumers assume member types; for example `SessionPanel.svelte:29` calls `r.callsign.toUpperCase()`, and keyed rendering assumes usable IDs.

**Impact**

Partially written, old-version, or manually altered session storage can hydrate successfully and later throw or create duplicate/undefined keys. The failure is avoidable because this data is a convenience mirror of durable daemon data.

**Recommended action**

Decode each stored record, keep only entries with a finite unique numeric ID and required string fields, validate optional UUID/boolean fields, and compute `nextId` from the validated set. Add migration/versioning if the stored shape is expected to evolve.

**Acceptance criteria**

- Any malformed member is dropped without preventing app boot or valid members from loading.
- Hydrated IDs are finite and unique.
- Search, render, edit, and export tolerate corrupt/old storage fixtures.

### F-09 — P3 — Status, build, and version documentation no longer describe one coherent product state

**Evidence**

- `frontend/app/README.md:8` still says the app is a scaffold, despite implemented Operate, FT8, Logbook, Settings, Map, setup, and 1,206 tests.
- `frontend/app/vite.config.ts:8` says the logging SPA still owns root and line 33 calls the committed `dist/index.html` future work, while `frontend/app/dist/index.html` is already tracked.
- The sidebar hard-codes `v2.0.0-alpha.1` at `src/lib/ui/Sidebar.svelte:223`, while `package.json:4` says `0.0.0`; neither is connected to build metadata.
- Several source headers still describe completed functions as follow-up increments.

**Impact**

Reviewers and operators cannot reliably tell what is supported, where the app is served, or which build is running. Hard-coded release text will drift and makes incident reports less useful.

**Recommended action**

Update the README to list current routes, supported workflows, deliberate limitations, quality gates, and deployment/base-path behavior. Remove historical/future-tense comments that no longer explain current constraints. Inject one build version/commit identifier from the release pipeline and use it for the sidebar chip.

**Acceptance criteria**

- README, Vite comments, routing/deployment docs, and visible version agree.
- The visible build identifier changes automatically with a release and is traceable to an artifact/commit.

## Positive observations

- Configuration state modules repeatedly treat a successful authoritative baseline as a save precondition and preserve whole-block fields. The refresh-and-merge logic in Rigs and the timed-out FT8 config reconciliation are particularly careful.
- QSO submit and session email correctly tell the operator that transport failures are ambiguous and warn against blind retries.
- Async generations/cancellation are already used well in live enrichment, worked-before lookups, map refreshes, session editing, and logbook pagination. F-02 can reuse a proven local pattern.
- `openReviving` has conservative revival policy and removes global listeners during teardown.
- RF changes are generally confirm-by-push, capability-gated, and owned by the daemon rather than optimistically claimed by the UI.
- No raw HTML injection or dynamic code execution was found. External new-tab links use `noopener` (and the QRZ link also uses `noreferrer`).
- The map and large country dataset are code-split. The production build's initial JavaScript is about 342 kB raw / 104 kB gzip, with the map and country data deferred.
- Error toasts have live-region roles and non-color severity labels; global alarms use appropriate alert/status roles.

## Verification results

| Check | Result |
|---|---|
| `npm run lint` | Pass |
| `npm run format:check` | Pass |
| `npm run check` | Pass — 0 errors, 0 warnings |
| `npm run test` | Pass — 88 files, 1,206 tests |
| `npm run build -- --outDir /tmp/station-manager-frontend-app-review-dist --emptyOutDir` | Pass |
| `npm audit --offline --omit=dev` | Pass — 0 vulnerabilities in the available offline advisory data |

Production build snapshot:

| Artifact | Raw | Gzip |
|---|---:|---:|
| `assets/index.js` | 342.09 kB | 104.47 kB |
| `assets/index.css` | 55.17 kB | 10.59 kB |
| Map view chunk | 150.17 kB | 57.47 kB |
| Country-data chunk | 756.51 kB | 242.86 kB |

The build was directed to `/tmp`; it did not update the tracked frontend distribution. No file under `frontend/app` was modified during this review.

## Recommended action order

1. **Protect stored data:** fix F-01 and F-02, with regression tests.
2. **Harden trust boundaries:** implement F-03 decoders, starting with submit and RF/SSE safety state.
3. **Make writes truthful:** define and apply the F-04 ambiguous-write policies.
4. **Fix modal ownership:** address F-05 and prove it with the first browser tests from F-07.
5. **Remove the empty landing:** decide and implement the F-06 route behavior.
6. **Raise release confidence:** complete the remaining F-07 browser/a11y smoke suite.
7. **Clean resilience and product metadata:** address F-08 and F-09.

## Suggested review decisions

- Confirm whether a missing `logging_station` can ever be legitimate after setup. If not, F-01 can fail closed unconditionally; if it can, the frontend needs an explicit setup-state-aware contract rather than treating absence as an empty editable block.
- Decide whether Dashboard should ship in the current release. If not, redirect/remove it now; if yes, define its minimum useful content before calling the consolidated app complete.
- Choose whether endpoint decoding will use hand-written guards (consistent with the current code) or a schema library. The important decision is one runtime source of truth, not the library.
- Choose the supported browser matrix before selecting the modal implementation and browser-test targets.
