# W-0011 — Refine operator-initiated FT8 and rig behavior

**Status:** Deferred — choose a recognized operator problem, not the whole inventory
**Selected:** Not selected
**Outcome:** Selected FT8 or rig refinements improve a concrete operating workflow while preserving
single-flight keying, guaranteed stop, and operator-initiated session boundaries.

## Current inventory

- **FT8 sequencing/UI:** type-4 free text; reachable type-4 work-a-caller after the hashed-callsign
  decision; callsign ignore list; unanswered Call-CQ feedback; a layer-2 recent-answerer pool; clean
  next-slot work opening; attempt-limit Settings control; and the still-open arbitrary-click offset
  snap decision.
- **CQ modifier (operator design point, 2026-09-12; not selected):** "CQ DX 7Q5MLV KH78" is a
  standard type-1 message carrying a CQ token, not free text. Verified in go-ft8 v0.9.0
  (`ft8/pack.go`, `pack28`): the modifier is three digits or one to four letters packed into the
  first 28-bit field; the caller must be a standard callsign (/P rides the portable flag; a compound
  call cannot carry a modifier or a grid); free text is a separate 13-character type the encoder
  rejects. The protocol attaches no meaning to it, and our parser already skips leading modifiers
  on incoming CQs (`sequence.go`, `parseMessage`). Plumbing when selected: the Call-CQ sequencer
  composes the one CQ string and validates it with the encoder before the session commits
  (`caller_sequencer.go`, `StartCallCq`) — the modifier goes between CQ and the call, and a bad one
  is refused before any RF with the existing bad-message error; an optional `cq_modifier` on
  `POST /v1/ft8/cq/start` (strict body — add and document in `api-endpoints.md`); a selector on the
  TX control bar beside parity and answer mode, presets plus a custom entry validated by the same
  rule, previewed from the status frame's `next_message`. Logging, answerer detection, PSK spots and
  slot timing do not read the CQ text; FT4 shares the sequencer. Open operator decisions: where the
  value lives (per session, browser-remembered — recommended — or an `ft8.tx` default like
  `caller_answer_mode`); presets versus free entry; a mid-run change means restarting the run.
  Free-form messages such as "TNX 73 GL" are a separate, larger design: a go-ft8 free-text encoder
  plus a decision on which rung carries it. Use case named 2026-09-12: "CQ AF 7Q5MLV KH78" to thin a
  rest-of-the-world pile-up in the Africa FT4 DX Contest — a convention other operators may or may not
  honour, pairing with the continent-preference entry below; not available for the 2026-09-12 contest.
  UI proposed 2026-09-12 (operator's placement, not yet ruled): the modifier is typed INTO the ladder's CQ
  rung — `buildLadder` composes that rung client-side from the operator's call and grid
  (`ft8Ladder.ts`), so the idle rung renders "CQ [ ] 7Q5MLV KH78" with a four-character token field
  between CQ and the call: uppercase, letters (one to four) or three digits, validated live by the
  protocol rule with the daemon the authority, a datalist of presets (DX, AF, EU, NA, SA, AS, OC, TEST,
  POTA), blank = plain CQ, remembered in localStorage, and inert to the FT view's shortcuts while
  focused. Read-only while a session is active: the sequencer repeats one string, so a change applies
  at the next Call CQ (v1); a live change mid-run needs a daemon setter that swaps the CQ text under
  the sequencer lock at the next slot evaluation after re-validating the encode (v2, only if the
  operator wants it without losing the run's answerer list). Once a run is calling, the rung shows the
  daemon's `next_message` rather than the client-composed text, so the ladder reads exactly what goes
  on air. The control bar keeps only the CQ-slot parity beside Call CQ. Nearest confusable outcomes: a
  rung that shows a modifier the running CQ is not sending; a field that swallows the view's keyboard
  shortcuts; a lowercase or five-character token accepted client-side and refused by the daemon at
  start. Refined by the operator 2026-09-12: editing is behind an explicit enable on the rung (a
  "custom CQ" toggle); disabled means the standard format, "CQ <call> <grid>", with no field shown,
  and the rung's text is the message either way. Ruled 2026-09-12: the enable is per
  session — a reload or a new tab returns the rung to the standard format, the token itself remembered
  so re-enabling restores it — so a contest-day "CQ AF" never leaks into an ordinary session
  unnoticed; a remembered enable can be revisited if there is pressure for it later.
  Validation (asked 2026-09-12): only the CQ rung has operator-editable content, and only its one token;
  every other rung — grid answer, report, R-report, RR73, 73 — is a type-1 message whose fields the
  protocol fixes (two callsigns, then a 4-character grid, a report in −30…+49 with the R flag, or one of
  RRR/RR73/73), and free text (type 0.0, 13 characters from a 42-symbol alphabet) is neither in the
  ladder nor in the encoder. The CQ rung's grammar: `CQ` [token] <standard call>[/P|/R] [grid4];
  the token is exactly three digits (000–999) or one to four letters A–Z, uppercase, nothing mixed, no
  punctuation (`pack28` in go-ft8 v0.9.0 `ft8/pack.go`; client mirror `^([A-Z]{1,4}|[0-9]{3})$` after
  trim and uppercase); `DE` and `QRZ` are the other legal first tokens and could be offered later; a
  compound or nonstandard own call (type 4) can carry neither a token nor a grid. The daemon is the
  authority through the existing `EncodeStandardMessage` round trip in `StartCallCq`, which rejects
  anything the packer refuses before the session commits. Sources: the QEX July/August 2020 paper
  (Franke, Somerville, Taylor, "The FT4 and FT8 Communication Protocols") named as the spec source in
  `docs/research-pipeline.md` and ADR 0021, not checked into the repository; the WSJT-X User Guide's
  message-format section for the operator-facing rules; go-ft8's README type table and packer source
  for what we ship. Gap: `docs/ft8.md` has no message-format section — add one in the same change as
  this feature. **BUILT and on air 2026-09-12:** `bf472ba6` (feature, tests, api-endpoints.md, the
  new `docs/ft8.md` message-format section, the manual's FT8 chapter) + `77d5453c` (codex P2: the
  run's CQ text rides every caller frame as `cq_message`, so the rung is truthful in a tab that did
  not start the run); deployed as `2.0.0-alpha.2-64-g77d5453c`. Evidence (`smd.log`, local +02:00):
  14:00:15 `CQ AS 7Q5MLV KH78` transmitted on 17 m FT8, 14:01:15 BA4IAW answered and was reported,
  14:02:28 QSO stored, 14:02:45 the run resumed `CQ AS …` — the token rides every CQ of the run and
  the exchange is unaffected. 14:04:29 second contact stored, RA6OY (European Russia, not Asia —
  the token is a convention, not a filter, as this entry says; operator 2026-09-12: for the contest,
  thinning is enough — the continent preference stays unselected). The dev-server check (FT4 and
  FT8 views) preceded it.
- **Contest continent preference for answerer selection (operator idea, 2026-09-12; not selected):**
  the operator's concrete problem: under an auto mode a rest-of-the-world pile-up is worked first-come or
  strongest-first and the 6-point African callers starve behind it. Raised for the Africa FT4 DX Contest
  (2026 SARL Contest Manual v1.0 pp. 45–46). Item 1.4 aims the
  contest at African and near-African stations, but 6.1 scores an African contestant 6 points per
  African-entity QSO, 4 within the own entity and 1 per non-African QSO capped at one third of the log,
  with 6.2 defining "African" as the ARRL DXCC list's Continent = AF. A hard exclusion of non-African
  callers therefore forfeits points whenever only non-African stations answer, and would also drop
  callers whose continent is not yet known — so the shape is a preference, never a filter. What the
  daemon knows: nothing locally. `internal/enums/dxcc` maps hamnut's primary prefix to the entity
  number (166 entities, no continent, no callsign-to-prefix resolution); continent comes only from the
  enrichment chain (`types.Country.Continent`, hamnut), cached by ≥ 2-character prefix with a
  longest-prefix read and a 365-day TTL, warmed in practice by Band Activity's first-sight lookups
  before a station answers. Answerer selection is `pickAnswererLocked` (auto modes) and
  `collectAnswerersLocked` (pick listing) in `caller_sequencer.go`; ADR 0067 makes the Answer mode the
  ONE run input, and the pileup drawer lists call + SNR only. Path available today, no code: run
  `operator_pick`, read the continent off Band Activity's country column, bag African answerers first
  and non-African ones as filler under the cap. Smallest coherent change if selected: annotate, do not
  filter — the `answerers`/`queue` frame items carry `continent`/`country` from the country cache via
  an injected resolver at the FT8 boundary (cache read only, never an external call in the slot path;
  unknown stays empty), and the drawer shows it and can sort AF first while the operator still picks.
  A further step, a session-scoped "prefer continent" ordering inside `auto_first`/`auto_strongest`
  (preferred callers first, then the mode's own rule, never an exclusion), is a second run input and
  needs an ADR 0067 amendment weighed on its own. Nearest confusable outcome: a strict filter silently
  skipping a 6-point caller whose prefix the cache has not seen. Hamnut's continent versus the ARRL
  AF list is an unverified equivalence for the islands 6.2 names.
- **Contest dupes (operator design question, 2026-09-12; not selected):** rule 6.4 of the Africa FT4
  DX Contest scores each station once per band; a repeat is zero points, not a penalty, but it costs
  a run several 7.5 s slots. What the system does today: Band Activity greys a row on two signals —
  the daemon's worked-before check (`GET /v1/contest-dupe`: call + band + mode/submode in the logbook,
  no time window, so a station worked on 20 m FT4 the day before shows grey during the contest though
  it is a valid contest QSO) and the tab's own session evidence (`session.qsos` and the engaged set,
  call + band); a click on a grey row informs ("already worked this session — working again") and
  proceeds with `allow_duplicate`, never refuses (XE1GM repair, 2026-07-26). Runs are blind: the auto
  modes' `pickAnswererLocked` skips only stalled, cooling-off and unencodable answerers, so a station
  that calls again is worked again and logged again; the pick listing and the pile-up drawer carry no
  worked mark at all. FT8/FT4 contacts get no `CONTEST_ID` (only Field Day sets one), so the ADIF the
  SARL converter receives cannot be filtered to the contest and carries every repeat as a row. Three
  layers if selected: (1) visibility — the dupe check gains an optional `since` (date and time) so the
  grey-out inside a declared window means "worked in this contest", the lifetime answer staying the
  default outside one; the drawer marks listed answerers from the same client-side evidence, no daemon
  change; (2) runs — the sequencer keeps a worked-this-session set (call + band, filled by its own
  completed contacts) and, when the window is declared, the auto modes skip those answerers with a
  logged reason as they do for stalled ones, and the `answerers` frame carries `worked` so the drawer
  can show it; pick stays the manual override, so a repair for a partner who never copied the RR73 is
  still one click — an exclusion rule like stall cool-off, not a second answer mode under ADR 0067;
  (3) logging — contacts logged inside the window are stamped with the declared `CONTEST_ID` (ADIF
  field already on `types.QsoDetails`) so the session export and email can be filtered to the contest
  and the Session panel can count in-window repeats. Nearest confusable outcomes: a window that greys
  a pre-contest contact as a dupe; an auto run that skips a partner asking for the repair; a repeat
  refused rather than informed; a stamp applied to a non-contest QSO logged during the window (the
  operator declares the window, so that is by their choice). Ruled 2026-09-12: an explicit contest mode
  with an id and a start/end window, not the tab's session start — the id gives the log a historical
  perspective (contests stay queryable by `CONTEST_ID` afterwards, as ADR 0049 anticipated), and the
  Logbook view gains a contest filter when this ships; the id is the ADIF Contest ID enumeration value
  where one exists and a custom string otherwise (the enumeration is recommended, not exclusive — my
  reading of ADIF 3.1.5, to verify at build time). Still open: whether auto runs skip in-window repeats
  (recommended: yes, only inside a declared window); whether the lifetime grey-out stays outside a
  window (recommended: yes). Sibling: the continent preference
  entry above shares the answerer-annotation plumbing.
- **Band Activity defect:** stale decode fading must be visually distinct from the worked mute. The
  2026-08-10 report was diagnosed as a visual collision, not a false worked state; implementation
  awaits an operator-chosen presentation.
- **Power/occupancy:** per-band generated-waveform attenuation, tune-carrier occupancy treatment
  after passive/hardware evidence, and an optional paired ALC-ceiling/PO-collapse overdrive signal
  after thresholds and presentation are chosen.
- **Rig state:** one owner for commanded-but-not-yet-reported rig position; no timeout substitute for
  per-field report sequence. Characterize every frequency/mode/VFO path before restructuring.
- **Bridge-open TX alarm (alpha.2 dogfood Finding #7):** at the 2026-09-05 20:34:45 start the bridge
  raised `TX ALARM` three seconds after opening the FTdx10 CAT port and cleared it within the same
  second as idle; the operator was not transmitting. Acceptance outcome: opening the CAT port with an
  idle rig never raises a TX alarm, while a rig genuinely keyed at open still raises one within the
  existing latency. No settle duration or re-read mechanism is chosen yet. Passive reproduction at
  port open (receive only, operator agreement for that occasion) promotes this to backlog P1 #2.
- **Safety-adjacent deferred evidence:** rig TOT surfacing/clamp, FT-710 meter-selector verification,
  meter-tail semantics, output-sink logging, playback reopen after a reproduced collapse, and
  persistent TX-state escalation only after an operator duration threshold.
- **Later operating aids:** auto band-hop, semi-auto watch list, occupancy waterfall, CAT poll mode,
  `MY_RIG` from the connected rig, and one source for frequency-to-band data distinct from future
  regional band-plan policy.

## Exclusions and gates

- [W-0002](W-0002-ft8-type4-on-air-validation.md) owns reduced type-4 on-air validation.
- Field Day UI remains blocked until the relevant contest; daemon-initiated sequencing remains out
  of scope.
- No keyed, tune, rig-command, or hardware test occurs without operator agreement for that occasion.
- The six-item FT8 cluster was parked by the operator on 2026-07-31; do not present it as a target
  list unless the operator names a specific pain point.

## References

- [`docs/ft8.md`](../ft8.md) — canonical current FT8 behavior.
- [`ADR 0027`](../decisions/0027-tune-carrier-control.md)
- [`ADR 0057`](../decisions/0057-tx-safety-scope-cat-confirmation-is-detection-not-guarantee.md)
- Expanded historical evidence: `d0391ed7:docs/backlog.md` and
  `d0391ed7:docs/dogfood-inbox.md`.
