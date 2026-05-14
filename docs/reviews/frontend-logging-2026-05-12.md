# `frontend/logging` — code review (2026-05-12)

**Status: FULLY RESOLVED ON INTENT (session 53 → 60, 2026-05-12 →
2026-05-14).** All 5 critical (C1–C5), all 17 important (I1–I20 with
the I20 reviewer-numbering quirk; I1/I4/I10 reviewer-deferred per
their own notes), all 4 verification gaps (enrichment /
contact-history / config API outcome tests + `formatFrequency`
utility tests), and 8 of 11 nits closed in session 60. The
remaining 3 nits (N1 / N5 / N11) are explicitly accepted-as-is per
the reviewer's own verdicts or skipped per the project's
"build specific not generic" rule.

**Deferred / pushback:**
- **I1** — derived-via-effect anti-pattern in `QsoPanel`. The
  callback-based fix the reviewer proposes was already considered and
  rejected in session 47 with a documented why-comment + line-disable
  (memory note: `feedback_svelte_empty_script_block`-adjacent; see
  the rationale on `QsoPanel.svelte:46`). The two-effect mirror is the
  load-bearing idiom for two-way bind across two reactive stores
  under ADR 0009's "writes only when editable" gate. Push back rather
  than change; the existing why-comment is the answer.
- **I4** — InfoPanel seed effect re-runs forever. Reviewer's own
  verdict: "Functionally fine; acceptable as-is." Skipped.
- **I10** — `contact-history.ts` `logbook_not_found` 404 collapse.
  Reviewer's own note: "Today the SPA never sends `?logbook=` so
  this is dead." Maintenance-only; revisit when logbook filtering
  ships.
- ~~**I17** — color-only invalid signal.~~ **CLOSED session 59
  (2026-05-14).** All five validators flipped to
  `(v: string) => string | null` (the named three plus `rst.ts` /
  `frequency.ts` / `passthrough.ts` as collateral for the prop
  type change). `ValidatedInput` + `Callsign` now render a paired
  `<p id="{id}-err" role="alert">` with the rendered i18n string
  and wire `aria-describedby`. New `validators.*` namespace in
  `lib/i18n/en.ts` (callsign / maidenhead / cq_zone / itu_zone /
  dxcc / rst / frequency). New `.input-error` utility in
  `styles/app.css`. Consumer-side boolean-semantics fixes at
  `qsoDraft.canSubmit`, `bearing.gridToDecimal`,
  `VfoInput.handleInput`, `app.svelte::putCallsign`. 11 new tests
  (5 in ValidatedInput, 6 in Callsign). Total: 580/580 passing.

**Session 54 (2026-05-13) — I2 + I5 + I6 architectural sweep landed
in a single commit:**
- New `lib/api/_helpers.ts` exports `safeFetch()` (classifies
  abort vs network), `readJsonBody()` (safe `response.json()`),
  `isPlainObject()` (primitive object guard), and `isShape<T>()`
  (object + required-key presence check).
- All six API helpers (`config`, `qso`, `qso-update`, `enrichment`,
  `contact-history`, `session-email`) now accept an optional
  `signal?: AbortSignal` parameter and surface a `kind: 'aborted'`
  outcome arm. Calls without a signal behave exactly as before.
- Body envelope handling refactored through the shared helpers — no
  behavioural change to existing outcomes, but body parsing is now
  consistent across the wrappers (no more blind casts).
- `qsoDraft.svelte.ts`, `qsoDefaults.svelte.ts`, `manual.svelte.ts`
  each capture their `$effect.root` dispose function and export a
  `_disposeForTests()` (matching `bridge.svelte.ts → stopBridge()`).
  Production behaviour unchanged.
- New `_helpers.test.ts` covers the primitives directly; `qso.test.ts`
  gains end-to-end abort wire-through tests. Total: 517/517 passing.

**Session 55 (2026-05-14) — `api/enrichment.ts` outcome tests
landed.** New `enrichment.test.ts` (10 cases) closes the
highest-priority verification gap. Covers: GET URL with
`encodeURIComponent` (portable suffix `/P` proves the encoder fires),
`kind=ok` happy path with full payload, `kind=ok` always-200 contract
(country_source/station_source `none`, no country/station — ADR 0017
#12), `kind=server unparseable_response` when 200 body isn't a JSON
object, `kind=validation` on 400 with daemon envelope, `kind=server`
on 5xx with daemon envelope, synthesised `unknown_error` fallback for
unparseable error bodies, `kind=network` on fetch reject, `kind=aborted`
on AbortError, and AbortSignal passthrough to `fetch`. Total:
527/527 passing (+10), svelte-check 0/0, lint clean.

**Session 60 (2026-05-14) — 8 nits closed; review fully resolved.**
N2 (i18n placeholder regex comment), N3 (`submitQso` gains
`force?: boolean` + options shape — daemon support verified at
`internal/api/handler_qso.go:236`, two new wire-seam tests),
N4 (`formatFrequency` clamps negative/fractional via
`Math.max(0, Math.floor(hz))` + two guard tests), N6
(`DaemonQsoForEdit` moved from `lib/states/qsoEdit.svelte.ts` to
`lib/api/qso-update.ts` — API layer no longer drags reactive-state
dependencies; re-export keeps existing call sites working),
N7 (zone `DIGITS_ONLY` strict-vs-daemon-ParseInt drift documented),
N8 (long-path km + miles clamped at 0 for antipodal grids),
N9 (LoggingCard spacer divs collapsed to `ml-auto`),
N10 (InfoPanel icon picker via `Record<TabId, Snippet>` lookup).
Skipped: N1 (reviewer-acceptable), N5 (take-or-leave, premature
abstraction), N11 (future-cost note only). Total: 584/584 passing
(+4 from 579), svelte-check 0/0/249, lint clean.

**Session 59 (2026-05-14) — I17 closed.** Validators flipped from
`boolean` to `string | null` (null = valid, i18n key = malformed);
last architectural item from the original review. All 5 validator
modules updated (named three plus rst/frequency/passthrough as
collateral for the prop type change). `ValidatedInput` + `Callsign`
render the resolved i18n string in a paired `<p id="{id}-err"
role="alert">` and wire `aria-describedby` so the invalid signal is
no longer color-only. New `validators.*` namespace in `lib/i18n/en.ts`,
new `.input-error` utility in `styles/app.css`. Consumer-side
boolean-semantics fixes at `qsoDraft.canSubmit` (`isValidX(x)` →
`isValidX(x) === null`), `bearing.gridToDecimal`,
`VfoInput.handleInput`, and `app.svelte::putCallsign`. 11 new tests
(5 in ValidatedInput, 6 in Callsign) covering the error-rendering +
aria-describedby contracts. Total: 580/580 passing (+11),
svelte-check 0/0/249, lint clean.

**Session 58 (2026-05-14) — `utils/frequency.formatFrequency` tests
landed.** Appended a `describe('formatFrequency', ...)` block (10
cases) to the existing `frequency.test.ts` rather than splitting.
Pins the doc-comment example (`14_250_000 → "14.250.000"`),
zero-padding correctness via the smallest non-zero case
(`5 → "0.000.005"`), HF watering-hole spot-checks, the 1 MHz
boundary in both directions, mid-value kHz/Hz tens padding, and the
microwave case (`1_296_000_000 → "1296.000.000"`) which guards
against a future "always pad MHz to 4 digits" refactor that would
break HF rendering. Also a structural invariant across five
magnitudes: output always splits to exactly three dot-separated
groups. Total: 569/569 passing (+10), svelte-check 0/0/249, lint
clean. **All four verification gaps now closed.**

**Session 57 (2026-05-14) — `api/config.ts` outcome tests landed.**
New `config.test.ts` (18 cases) covers both `fetchConfig` (GET) and
`putConfig` (PUT) end-to-end. `putConfig` gets its own parseOutcome
coverage rather than relying on the shared internal helper — the
Save-button path is louder when it regresses. Highlights: the I7
parse-failure guard is pinned via two unparseable-200 shapes
(non-JSON body and a JSON array — the array case proves
`isPlainObject` excludes arrays even though JSON.parse succeeds),
`putConfig` body is bit-for-bit `JSON.stringify(payload)` so a future
"helpful preprocessing" can't drift the wire format silently,
`kind=validation` covers daemon `invalid_field_value` (session 46
zone/callsign validation), `kind=server` covers `db_error` /
`config_write_failed`, plus `kind=network` / `kind=aborted` / signal
passthrough on both functions. Total: 559/559 passing (+18),
svelte-check 0/0/249, lint clean.

**Session 56 (2026-05-14) — `api/contact-history.ts` outcome tests
landed.** New `contact-history.test.ts` (14 cases). Endpoint-specific
divergence from enrichment: contact-history's wrapper deliberately
downgrades a malformed 200 body to `kind=ok, items=[]` rather than
synthesising a `server` error, so three of the cases pin the
structural-fallback contract (non-JSON 200, 200 without `items`,
200 with non-array `items`). Other coverage: GET URL `encodeURIComponent`
via `M0XYZ/P`, happy path with two rows, `items: []` "never worked them",
`kind=validation` for 400 `missing_required_param` / 400
`invalid_field_value` / 404 `logbook_not_found` (404 < 500 routes to
validation; SPA call path is dead per I10 but daemon emits the status),
`kind=server` on 5xx, synthesised `unknown_error` + `HTTP 502`
fallback, `kind=network` on fetch reject, `kind=aborted` on
AbortError, and AbortSignal passthrough. Total: 541/541 passing (+14),
svelte-check 0/0/248, lint clean.

**Remaining:**
- **All 11 Nit findings** (N1–N11) — polish, batched whenever an
  adjacent piece of work surfaces one.
Conducted by three parallel agents covering (a) Svelte 5 rune
discipline with emphasis on `$effect`, (b) the pure-TypeScript layer
(`lib/{api,utils,validators,i18n}`), and (c) the UI layer
(`lib/ui/` + top-level `app.svelte`/`main.ts`).

## Scope

The full logging SPA at `frontend/logging/src/`:

- `app.svelte`, `main.ts`
- `lib/states/` — 13 `.svelte.ts` reactive modules
- `lib/ui/` — 1 card, 7 panels, 14 form components, `Toasts.svelte`
- `lib/api/` — 6 fetch wrappers (3 with co-located tests, 3 without)
- `lib/utils/` — 5 modules (all tested)
- `lib/validators/` — 6 modules (5 tested, `passthrough` is trivial)
- `lib/i18n/` — `en.ts` baseline + `t()` helper

Tests were spot-checked for coverage gaps; this is not a test-suite
review per se.

## Headline verdict

The structural architecture is sound. The four-object decomposition
(`catState` / `manualState` / `configState` / `displayedState`)
described in ADR 0009 is upheld throughout: `displayedState` is pure
`$derived`, never written; components consume it everywhere; reactive
vs plain-field discipline is genuinely well-applied. Of **9 total
`$effect` call sites** across 5 files, 6 are OK, 2 are Suspect, 1 is
Wrong. `$effect.pre` appears only once — at the broken site. No
`untrack()` is used anywhere in the codebase; both of the
problematic effects are exactly the cases that need it.

Counts: **5 Critical**, **17 Important**, **11 Nit**.

The Critical findings cluster into three buckets: one latent rune
bug (mode-mapping clobber), one wire-protocol bug (ADIF length
prefix), and three modal/keyboard-flow bugs in `QsoEditOverlay` and
the date/time/mode inputs.

---

## Critical findings

### C1. `$effect.pre` clobbers in-progress mode-mapping edits

**File:** `lib/ui/panels/MyStationPanel.svelte:210-214`

```ts
$effect.pre(() => {
    if (activeSection === 'modes') {
        snapModeMappings();
    }
});
```

`snapModeMappings()` reads `configState.bridge.rigModes` and
`configState.bridge.modeMappings` inside the effect, so those reads
become tracked dependencies. While the operator sits on the Mode
Mappings tab, **any** update to either reactive field — a future
periodic `/v1/config` refresh, a bridge subsystem reporting new rig
identity, a second browser tab triggering a PUT, an SSE-driven
applyResponse — re-fires the effect and replaces `editingModes` with
a fresh snap from the daemon, wiping every unsaved edit.

The doc comment immediately above (lines 180-186) explicitly states
the design intent is to avoid this: *"that way an external config
refresh doesn't stomp in-progress edits while the operator is
mid-change"*. Implementation does not match intent.

Latent today (no actor mutates those fields after the initial
`applyResponse`) but exactly the kind of fragility the rune audit
exists to catch.

**Fix:** track only `activeSection`, detect the transition *into* the
modes tab, and `untrack()` the snap call:

```ts
import { untrack } from 'svelte';
let lastSection: SectionId | undefined;
$effect.pre(() => {
    const section = activeSection;
    if (section === 'modes' && lastSection !== 'modes') {
        untrack(() => snapModeMappings());
    }
    lastSection = section;
});
```

`lastSection` is plain `let` — no reactive consumer.

**Verification gap:** add a vitest case asserting that after opening
the Mode Mappings tab, mutating `editingModes`, and then calling
`configState.applyResponse(...)` with a different `mode_mappings`
payload, the operator's edits survive. With the current code this
test fails.

### C2. ADIF length prefix uses UTF-16 code units, not UTF-8 bytes

**File:** `lib/utils/adif.ts:162-164`

```ts
function adifTag(name: string, value: string): string {
    return `<${name}:${value.length}>${value}`;
}
```

The ADIF spec is byte-counted, and the daemon parser at
`internal/adif/parse.go:130-131` slices by byte. JS `string.length`
returns UTF-16 code units, not UTF-8 byte length. Any non-ASCII
character in `name`, `qth`, `comment`, `notes`, `myName`, `myCity`,
`myCountry`, `rig`, etc. produces a wrong length prefix.

**Concrete failure:** operator types "José" in NAME → JS reports
length 4, UTF-8 is 5 bytes (`J o s é = 0xC3 0xA9`). Daemon reads 4
bytes (`Jos\xC3`), leaving the `\xA9` byte as the next thing the
parser sees — either truncating the value silently or breaking the
next tag boundary outright. Same risk for QTH, COMMENT, MY_CITY,
MY_COUNTRY ("Côte d'Ivoire"), MY_NAME, NOTES, RIG.

**Fix:**

```ts
const utf8 = new TextEncoder();
function adifTag(name: string, value: string): string {
    return `<${name}:${utf8.encode(value).byteLength}>${value}`;
}
```

Add a UTF-8 round-trip case to `adif.test.ts` covering `name: 'José'`
and `myCountry: "Côte d'Ivoire"`.

### C3. `QsoEditOverlay` does not trap focus inside the modal

**File:** `lib/ui/components/QsoEditOverlay.svelte:204-393`

The modal opens with `aria-modal="true"` and `role="dialog"` but
nothing scopes Tab navigation to the dialog. With the overlay open,
Tab past the last field (Notes) walks straight through to the live
QsoPanel's Callsign input behind the backdrop — the backdrop has
`tabindex="-1"` but the underlying form fields remain in the DOM
tab order.

**Concrete failure:** operator opens the overlay, Tabs past the end,
types into what looks like the overlay but is actually the live
form, silently mutating `qsoDraft.callsign` while believing they're
editing the historical QSO.

**Fix:** bind the dialog element, focus the first input on open,
loop Tab/Shift+Tab inside the dialog, restore focus to the
originating element on close. Skeleton:

```ts
let dialogEl: HTMLDivElement;
let previouslyFocused: HTMLElement | null = null;

$effect(() => {
    if (qsoEditState.open && !qsoEditState.loading) {
        previouslyFocused = document.activeElement as HTMLElement | null;
        dialogEl?.querySelector<HTMLElement>(
            'input,textarea,button,select'
        )?.focus();
    } else if (!qsoEditState.open) {
        previouslyFocused?.focus();
        previouslyFocused = null;
    }
});

function trapTab(e: KeyboardEvent) {
    if (e.key !== 'Tab' || !dialogEl) return;
    const f = dialogEl.querySelectorAll<HTMLElement>(
        'a[href],button:not([disabled]),input:not([disabled]),' +
        'select:not([disabled]),textarea:not([disabled]),' +
        '[tabindex]:not([tabindex="-1"])'
    );
    if (f.length === 0) return;
    const first = f[0], last = f[f.length - 1];
    if (e.shiftKey && document.activeElement === first) {
        e.preventDefault(); last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault(); first.focus();
    }
}
```

### C4. Overlay ESC propagation contract is fragile

**Files:** `lib/ui/panels/QsoPanel.svelte:431-444` and
`lib/ui/components/QsoEditOverlay.svelte:98-102,202`

Both register a `svelte:window` `onkeydown`. `QsoPanel.handleKeydown`
early-returns when `qsoEditState.open` is true, so the current
behavior is correct — but the overlay's handler does not
`stopPropagation()`, so the contract relies on the QsoPanel
remembering to guard. Any future regression in that guard makes ESC
both close the overlay *and* clear the underlying QSO draft.

**Fix:** the overlay's keydown should call `e.stopPropagation()`
after `qsoEditState.close()`. Keep the QsoPanel guard as
belt-and-braces; cross-link the two files in code comments.

### C5. Date / Time / Mode inputs are keyboard-unreachable

**Files:** `lib/ui/components/DateInput.svelte:36`,
`TimeInput.svelte:37`, `Mode.svelte:28`

All three carry `tabindex={-1}` on the native form element. The
result is that the operator typing fast cannot Tab from
Comment → Date → Time On → Time Off → Submit; the entire row is
mouse-only.

This contradicts the project's documented keyboard-first invariant
(`feedback_keyboard_first_logging_flow` memory). If the intent was
"Tab should fly over these for the common case where they auto-tick
from `qsoDraft`," it's still wrong because the operator cannot reach
the field to back-date a manual entry without the mouse.

**Fix:** drop `tabindex={-1}` from all three.

---

## Important findings

### Rune discipline

**I1. `QsoPanel.svelte:67-74` — derived-value-via-effect plus
writeback.** Two effects together implement "compute a value from
other state, assign to `$state`, then write back" — the documented
Svelte 5 anti-pattern. The eslint-disable on line 65 acknowledges
the `prefer-writable-derived` rule was tripped. The rejection
rationale (needing `bind:value` on `<Mode>`) is solvable by dropping
`bind:` and passing an explicit callback:

```ts
const mode = $derived(displayedState.subMode || displayedState.mode);
function setMode(v: string): void {
    if (displayedState.editable) manualState.mode = v;
}
// <Mode value={mode} onchange={setMode} list={modes}
//   disabled={!displayedState.editable} />
```

Currently quiet only because `resolveModeAndSubmode` round-trips
identically. The shape silently requires every future resolver edit
to preserve that round-trip invariant; the `$derived` + callback
shape is impossible to break this way.

**I2. Module-level `$effect.root` exposes no test-time disposer** in
`lib/states/qsoDraft.svelte.ts:244`, `qsoDefaults.svelte.ts:79`, and
`manual.svelte.ts:94`. In production this is fine (module lifetime =
page lifetime), but in vitest the singleton state and effects
persist across cases, forcing brittle workarounds like the one in
`Vfos.test.ts:344-362`. Compare `bridge.svelte.ts` which correctly
exports `stopBridge()`. Export a `_disposeForTests()` from each:

```ts
let rootDispose: (() => void) | null = $effect.root(() => { ... });
export function _disposeForTests(): void {
    rootDispose?.();
    rootDispose = null;
}
```

**I3. `qsoDefaults` and `manual` write hydrated values back on
initial load.** When each module loads, every per-field `$effect`
fires once and writes the just-hydrated localStorage value back to
localStorage. Pointless I/O on page load, noisy in unit tests. Gate
with an `initialized` flag set after the first pass.

**I4. `InfoPanel.svelte:77-83` seed effect re-runs forever.** Once
`recipientSeeded` flips true, every subsequent change to
`configState.mailer.defaultRecipient` re-fires the effect to do
nothing. Functionally fine; idiomatically prefer an `untrack()`
inside the body. Acceptable as-is.

### API layer

**I5. All 6 API helpers lack `AbortController` / timeout / stale-
response handling.** A grep of `lib/api/` for `AbortController`,
`signal`, or `timeout` returns nothing. On the operator's documented
slow / flaky network this matters:

- Rapid keystrokes in Callsign fire multiple `enrichCallsign` calls;
  the older response can overwrite the newer one's results
  (last-write-wins by arrival time, not request order).
- A hung `submitQso` invites double-submission.
- No upper bound on `fetchContactHistory` or `enrichCallsign`.

Add an optional `signal: AbortSignal` parameter on at least the
call-bound helpers (`enrichCallsign`, `fetchContactHistory`,
`fetchQso`). For long-running endpoints consider a default timeout
via `AbortSignal.timeout(...)`. Surface `AbortError` as a distinct
`kind: 'aborted'` outcome.

**I6. `qso-update.ts:69,108` blind-cast response bodies.** `body`
was typed `unknown` then `as DaemonQsoForEdit` with no runtime
guard. A malformed daemon response (or proxy interference) lands in
the QSO edit overlay as `undefined` field reads and either crashes
or renders blank. Minimum fix: a single guard helper that checks
`body && typeof body === 'object' && 'uuid' in body` before the
cast.

**I7. `config.ts:211` returns a fake `kind: 'ok'` outcome with a
null config on JSON parse failure.** The `try/catch` at lines
204-208 sets `body = null` on parse error, and the `response.ok`
branch immediately casts that to `ConfigResponse`. Caller treats
null as the config object and dereferences `config.logging_station`
→ crash. Same pattern needs checking in `contact-history.ts:71`
and `qso-update.ts:68,107`.

**I8. `qso.ts:74` silently collapses missing `uuid` to `''`.** A
201 with a body lacking `uuid` (proxy mangling, daemon-side
regression) produces `{ kind: 'stored', uuid: '' }`. Callers key off
`uuid` for everything downstream — phantom empty IDs propagate.
Downgrade to `{ kind: 'server', code: 'malformed_response' }` when
`response.ok` but `!ok?.uuid`. Same check belongs in
`enrichment.ts` (handles `result === null` but not
`result.callsign === ''`).

**I9. `enrichment.ts:65` index signature defeats type checking.**
`EnrichmentStation` lists 21 fields then adds `[extra: string]:
unknown`. Typos like `station.gridsqure` compile silently as
`unknown`. Daemon-side, the response already carries fields the SPA
hasn't listed (`iota_island_id`, `sig`, `sig_info`, `wwff_ref`).
Drop the index signature; add the missing fields explicitly.
Forward-compat for unknown new daemon fields is already covered by
structural typing — extra JSON keys are silently ignored on reads.

**I10. `contact-history.ts:78` collapses `logbook_not_found` 404
into generic `validation`.** The daemon handler emits a distinct
404 code for a missing `?logbook=<id>`. The SPA wrapper has no
`not_found` arm and falls through to `validation`. Today the SPA
never sends `?logbook=` so this is dead — but the wrapper's own
comment plans for it. When logbook filtering ships the wrapper will
hide a distinct condition under a generic error. Add a
`kind: 'not_found'` arm before the `validation` fallback.

**I11. `validators/callsign.ts` MAX=20 is stricter than the
daemon's 32.** Daemon handler at `internal/api/handler_qso.go:184`
accepts 3-32 chars. Operator pastes a long special-event call →
SPA shows red border, daemon would accept. Bump SPA MAX to 32; add
a 30-char test row. This is exactly the validator/daemon drift the
`zone.ts` doc comment explicitly warns about.

### UI / accessibility

**I12. `QsoEditOverlay.handleSave` flips `saving=true` outside the
try.** `lib/ui/components/QsoEditOverlay.svelte:127-184`. The
assignment at line 129 happens before `qsoEditState.toPatchBody()`
on line ~131. A throw in `toPatchBody()` leaves `saving=true` with
no `finally` to clear it; Save button is permanently disabled until
the overlay is dismissed and reopened. Move the flip inside the
try, or build `body` first.

**I13. `Toasts.svelte:91` aria-label overrides the operator
message.** The toast `<button>` contains the operator-readable
message, but `aria-label="Dismiss ${level} notification"`
*overrides* that content for assistive tech. Screen-reader users
hear "Dismiss error notification" — never the actual message —
contradicting the comment at line 39 stating the level is in the
spoken stream to convey severity. Drop the aria-label and rely on
the button text, or move dismiss to a child element with its own
label.

**I14. Toast container has interactive children inside an
`aria-live` region.** `Toasts.svelte:80-95` — each toast is a
`<button>` inside `aria-live="polite" role="status"`. Buttons in
live regions are a documented anti-pattern; NVDA/VoiceOver behavior
is unspecified. Make each toast a non-interactive
`<div role="alert">` with a child `<button aria-label="Dismiss">`.

**I15. `InfoPanel` and `MyStationPanel` tablists have no keyboard
nav.** `InfoPanel.svelte:279-305` and `MyStationPanel.svelte:236-247`
both declare `role="tablist"` but ArrowLeft/Right/Home/End are not
wired; no roving tabindex. Standard WAI-ARIA tabs pattern is missing.
Tabpanels also lack `aria-labelledby` back to the tab buttons —
neither tab has an `id` for that to reference.

**I16. `SessionPanel` rows use `<tr role="button">` — table
semantics broken.** `lib/ui/panels/SessionPanel.svelte:126-137`. The
implicit `role="row"` is overridden; cells lose their gridcell
relationship; screen readers stop announcing cell-by-cell. Move the
edit affordance into a trailing cell with an inline button instead
of role-on-row.

**I17. `ValidatedInput` / `Callsign` show invalid via red border
only.** ~~`ValidatedInput.svelte:62,82` and `Callsign.svelte:46`
toggle `invalid-input` and `aria-invalid` but render no inline
error text and no `aria-describedby`. Color-only signal. Validators
return `boolean`, so there's no message to render even if a slot
existed. Refactor path: validators return `string | null`
(i18n key + details); add an error slot to `ValidatedInput` that
renders `<p id="{id}-err">` and points `aria-describedby` at it.~~
**CLOSED session 59 (2026-05-14)** along the recommended refactor
path. See the session 59 entry near the top of this document for
the full work record.

**I18. `Callsign.validateAndFocus` refocuses on every blur**
including Shift+Tab and direction-reverse exits. An operator who
realizes mid-typo they want to abandon the QSO entirely cannot leave
the field by keyboard. Gate the refocus on `e.relatedTarget` so a
deliberate exit is not fought.

**I19. `app.svelte` setup form has no Enter-to-submit.** The
callsign input on the first-run setup card sits in a bare div, not a
`<form>`. Enter does nothing; operator must mouse to Save. Wrap in
`<form onsubmit={...}>` and let the Save button default to submit.

**I20. Native `readonly` inputs in `DetailsPanel.svelte:131-153`
also carry `tabindex={-1}`.** `readonly` is the right attribute for
"non-editable but focusable"; `tabindex={-1}` makes the value
unreachable. Operator cannot keyboard-select to copy a station's
email or other read-only enrichment field. Drop `tabindex={-1}`.

**I21. `DetailsPanel` and `QsoEditOverlay` re-implement digit-strip
inline.** `DetailsPanel.svelte:75-94` and
`QsoEditOverlay.svelte:192-199` use raw `<input type="text">` with
inline `replace(/[^0-9]/g, '')` handlers. The `ValidatedInput`
component already supports `transform` for exactly this (see
`Rst.svelte`). Two divergent inline implementations vs the canonical
primitive. Swap both to `<ValidatedInput transform={stripNonDigits}>`.

---

## Nit findings

**N1.** `qsoDraft.svelte.ts:244-250` — module-level `$effect.root`
overwrites RST on mode-flip with no operator-typed override. Comment
acknowledges the tradeoff; flagged only as a candidate for `$derived`
if the operator-override policy ever changes. **Accepted as-is per
reviewer verdict (skipped session 60).**

**N2.** ~~`i18n/index.ts:70` — substitution regex `\{(\w+)\}` rejects
hyphenated keys.~~ **CLOSED session 60.** Added a 6-line comment
noting the `\w+` constraint vs hyphenated names; all daemon `details`
keys are snake_case today.

**N3.** ~~`api/qso.ts:48` — `submitQso` doc comment promises "caller
decides whether to offer `?force=1` retry" but the wrapper has no
parameter to pass it.~~ **CLOSED session 60.** Signature changed to
`submitQso(adif, logbookID, options: SubmitOptions = {})` with
`{ force?, signal? }`. Daemon support verified at
`internal/api/handler_qso.go:236`. Existing call sites unaffected
(empty options default); existing signal-passthrough tests updated;
two new tests pin the `?force=1` wire seam.

**N4.** ~~`utils/frequency.ts:58-63` — `formatFrequency` mishandles
negative or fractional Hz.~~ **CLOSED session 60.** Coerce via
`Math.max(0, Math.floor(hz))`; two new guard tests in
`frequency.test.ts`.

**N5.** `utils/adif.ts` operator-station section is a 60-line
if-cascade of `if (f.X && f.X.length > 0) lines.push(...)` repeated
25+ times. A small table-driven loop would halve the file. Squarely
in "build specific not generic" territory — take or leave. **Skipped
session 60** per the project's "build specific not generic" rule —
collapsing the specific repetition into a table-driven generic is
exactly the kind of premature abstraction
[feedback_design_patterns] warns against.

**N6.** ~~`api/qso-update.ts:29` — cross-package type import from a
`.svelte.ts` state module.~~ **CLOSED session 60.**
`DaemonQsoForEdit` interface moved from
`lib/states/qsoEdit.svelte.ts` to `lib/api/qso-update.ts`;
`qsoEdit.svelte.ts` re-exports via `export type { ... } from`. API
layer no longer drags a reactive-state dependency.

**N7.** ~~`validators/zone.ts:35` — strict regex rejects leading `+`;
daemon's `strconv.ParseInt` accepts it.~~ **CLOSED session 60.** Kept
the strict SPA regex (a leading `+` is almost certainly a paste
artefact); added a 5-line comment noting the deliberate stricter
behaviour vs daemon parity. Daemon stays compatible because
ParseInt accepts both forms.

**N8.** ~~`utils/bearing.ts:144` — long-path distance can produce a
small negative for antipodal grids.~~ **CLOSED session 60.** Wrapped
both `longPathDistanceKm` and `longPathDistanceMiles` with
`Math.max(0, …)`.

**N9.** ~~`LoggingCard.svelte:14-32` — three empty spacer divs for
layout.~~ **CLOSED session 60.** Replaced with `ml-auto` on the
Session Time block; 6 lines deleted, same visual layout.

**N10.** ~~`InfoPanel.svelte:280-304` — per-tab icon picker uses a
4-arm `{#if/else if}` chain inside an each.~~ **CLOSED session 60.**
Replaced with `{@const tabIcons: Record<TabId, Snippet> = {...}}`
inside the `{#each}` + `{@render tabIcons[tab.id]()}`. Added
`import type { Snippet } from 'svelte'`.

**N11.** `SessionTimer.svelte` — `setInterval` runs from script
body (module lifetime) rather than inside `$effect` (component
lifetime). `onDestroy` clears it correctly. Pattern is consistent
with QsoPanel's ticker; future component tests will need to mock
timers. **Accepted as-is per reviewer verdict (skipped session 60)
— future test-mock cost, not a defect.**

---

## Verification gaps

These tests should exist but don't. Highest priority first.

1. **MyStationPanel mode-mappings clobber test (C1).** Open the
   tab, mutate `editingModes`, call `configState.applyResponse(...)`
   with different `mode_mappings`, assert edits survive. Fails on
   the current code.

2. **ADIF UTF-8 round-trip test (C2).** `NAME='José'`,
   `MY_COUNTRY="Côte d'Ivoire"`. Decode the produced ADIF with the
   same byte-counted slicing the daemon uses.

3. **`api/enrichment.ts` outcome tests.** ~~Per the project rule
   *"test error path first for enrichment code"* this is the
   highest-priority API test gap.~~ **CLOSED session 55 (2026-05-14)
   — `enrichment.test.ts` ships with 10 cases covering all five
   outcome arms, ADR 0017 #12 always-200 contract, `unparseable_response`
   server downgrade, synthesised `unknown_error` body fallback, and
   AbortSignal passthrough.**

4. **`api/config.ts` outcome tests.** ~~Both `fetchConfig` and
   `putConfig`; cover the parse-failure path (I7) and the daemon's
   `invalid_field_value` 400 shape.~~ **CLOSED session 57 (2026-05-14)
   — `config.test.ts` ships with 18 cases. Both functions covered
   end-to-end; the I7 parse-failure guard pinned via two
   unparseable-200 shapes (non-JSON body and a JSON array, the latter
   proving `isPlainObject` excludes arrays); `putConfig` body
   serialisation pinned bit-for-bit; `kind=validation` for
   `invalid_field_value`; `kind=server` for `db_error` /
   `config_write_failed`; transport arms + AbortSignal passthrough on
   both functions.**

5. **`api/contact-history.ts` outcome tests.** ~~Empty `items`
   preservation; special chars in callsign URL-encoded
   (`call=K1ABC%2FP`).~~ **CLOSED session 56 (2026-05-14) —
   `contact-history.test.ts` ships with 14 cases. Covers
   `encodeURIComponent` via `M0XYZ/P`, `kind=ok, items=[]` for
   "never worked them", the three structural-fallback contracts
   (non-JSON 200 / no `items` field / non-array `items` all
   downgrade to `kind=ok, items=[]` per source intent),
   `kind=validation` for `missing_required_param` /
   `invalid_field_value` / `logbook_not_found`, `kind=server` on
   5xx, synthesised `unknown_error` + `HTTP 502` fallback,
   `kind=network` on fetch reject, `kind=aborted` on AbortError,
   AbortSignal passthrough.**

6. **QsoPanel mode round-trip stability** (I1). Pin the
   round-trip invariant the eslint-disable rationale assumes. If
   `resolveModeAndSubmode` ever changes, this test must catch the
   loop becoming non-idempotent.

7. **Bridge effect lifecycle.** Already partially covered, but
   missing: rapid true→false→true on `configState.station.enabled`
   produces exactly one fresh EventSource; prior listeners no
   longer fire.

8. **Module `$effect.root` disposers** (I2). Tests that need a
   clean baseline can't currently get one for qsoDraft / qsoDefaults
   / manual. Either expose disposers or document the workaround
   pattern from `Vfos.test.ts:343-363`.

9. **Callsign max-length boundary** (I11). 30-char input — fails
   on the current MAX=20.

10. **`utils/frequency.formatFrequency`** ~~has no tests at all.~~
    **CLOSED session 58 (2026-05-14) — appended a 10-case
    `formatFrequency` describe block to the existing
    `frequency.test.ts`. Pins the doc-comment example, zero-padding
    correctness, HF watering-hole spot-checks, the 1 MHz boundary,
    mid-value kHz/Hz tens padding, microwave (1296 MHz) MHz-field
    width invariance, single-Hz precision, and the three-group
    structural invariant across five magnitudes.**

---

## Cross-cutting observations

### What's working

- **Four-object decomposition holds.** `displayedState` is pure
  `$derived`, never written. Components consume `displayedState`
  everywhere (verified in `Vfos.svelte`, `QsoPanel.svelte`,
  `MyStationPanel.svelte`). `catState`, `manualState`, `configState`
  have clear writers. The architecture from ADR 0009 is intact.
- **`$state` vs plain-field discipline is good.** Plain `let`
  is correctly chosen for non-reactively-consumed flags
  (`qsoDraft.qsoStarted`, `enrichment.recipientSeeded`,
  `LoggingStationView` identity strings). The comment on
  `LoggingStationView` lines 100-121 documents the rationale.
- **Error-shape consistency across API helpers.** All six follow
  the same `{kind, code?, message?, details?}` discriminated-union
  pattern. The shape is consistent enough that boilerplate could be
  collapsed into a shared `parseDaemonError(response)` helper.
- **Documentation discipline.** All six API files carry top-of-file
  rationale comments referencing api.md, ADRs, and counterpart
  daemon paths. `mode.ts`, `bearing.ts`, `adif.ts`, `time.ts`
  carry detailed rationale. This is some of the cleanest
  "document intent" adherence in the codebase.
- **Component composition is clean.** Form primitives take
  explicit `$props<T>()` interfaces with `$bindable`. Panels reach
  into `lib/states/*.svelte.ts` directly. Boundary is consistent:
  components are dumb visuals, panels orchestrate.
- **Most form/timer patterns are correct.** `QsoPanel`'s
  `setInterval` lives in script body with `onDestroy` cleanup —
  not wrapped in `$effect` — which is the right choice for a
  fixed-period timer that doesn't need reactivity.

### What's systemic

- **No `untrack()` anywhere.** The two problematic effects (C1, I1)
  are both cases where `untrack()` would be the right tool.
- **No `AbortController` anywhere.** Six API helpers, zero
  abort-signal support. Adding it once to a shared helper would
  cover all of them.
- **JSON-body parsing trusts the wire boundary.** Five of six
  wrappers cast response bodies without runtime checks. A single
  `isShape<T>()` guard helper at the boundary would catch I6, I7,
  I8 collectively.
- **Validator/daemon parity has no tracking mechanism.**
  `zone.ts` documents daemon parity in its doc comment;
  `callsign.ts`, `maidenhead.ts`, `frequency.ts` do not — and
  `callsign.ts` has already drifted (I11). Suggest a one-line
  `// daemon parity: <file>:<func>` comment in every validator.
- **`InfoPanel` (~340 lines) and `MyStationPanel` (~640 lines)
  are too large.** Both fold multiple concerns into a single file.
  `InfoPanel` owns the session-email send flow that belongs next to
  `SessionPanel`. `MyStationPanel` is six sub-sections that could
  follow the same panel-split pattern InfoPanel already uses.
- **No error/loading-state UI primitive.** `WorkedPanel`,
  `SessionPanel`, `CountryPanel`, `DetailsPanel` each handle
  not-fetched / empty / error differently. At this small scale
  three patterns are diverging. A shared `<EmptyState />` snippet
  would unify them as new panels appear.

---

## Suggested fix order

If a single follow-up session takes this on, the recommended order:

1. **C2 (ADIF byte length)** — smallest fix, real data-corruption
   risk on every non-ASCII QSO. ~5 lines + one test.
2. **C5 (drop `tabindex={-1}`)** — three one-line removals; the
   keyboard-first invariant is currently broken.
3. **C1 (mode-mapping clobber)** — small fix, prevents a latent
   bug from biting once any actor mutates bridge config post-load.
4. **C3 + C4 (overlay focus trap + ESC propagation)** — one
   coherent overlay-hardening commit.
5. **I7 (config.ts parse-failure crash)** — one of the more
   load-bearing wrappers; cheap fix.
6. **I5 (AbortController) + verification gap 3 (enrichment
   tests)** — together, since the abort-signal API is most useful
   in enrichment.

The Important UI/a11y findings (I12-I21) cluster naturally with
follow-up work on the overlay and the toast/tablist patterns.
