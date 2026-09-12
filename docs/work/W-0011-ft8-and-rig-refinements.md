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
  plus a decision on which rung carries it.
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
