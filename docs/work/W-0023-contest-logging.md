# W-0023 — Contest logging

**Status:** Design only — no implementation authorised (operator, 2026-10-28)
**Selected:** 2026-10-28 for design (operator: "Open W-0023 as a design-only dossier")
**Outcome:** The operator can log a contest in Station Manager — a declared contest with its exchange,
an explicitly recorded sent serial, in-contest duplicate detection, and a sponsor-ready Cabrillo log —
without leaving the main log. Scoring follows only once the sponsor's rules are settled. The
reference case is the UK/EI DX Contest (SSB and CW).

`W-0023` is an immutable identity. Its status may change, while priority and ranked position live only
in [`docs/backlog.md`](../backlog.md).

## Why this item exists

Contesting is step (5) of the data-model chain ruled on 2026-09-19 (W-0014): contest mode, scoring,
export and different-call operation, designed on the archive and logbook identities that W-0021 built
and in its own ADR. On 2026-10-28 the operator asked to start the design against a real contest, the
UK/EI DX Contest, and logged "cabrillo format session and logbook export" to the inbox the same day.
This dossier holds the design: the reference contest's rules, what already exists, the open
questions, and the proposed acceptance criteria. An ADR follows when the design settles.

## Prior rulings and records this design builds on

- **Contest mode (W-0011, ruled 2026-09-12):** an explicit contest mode with a declared contest id and
  a start/end window, not the tab's session; contacts logged inside the window are stamped with ADIF
  `CONTEST_ID`, so a contest stays queryable and exportable from the main log afterwards; the Logbook
  view gains a contest filter. The id is the ADIF Contest ID enumeration value where one exists and a
  custom string otherwise (W-0011's reading of ADIF 3.1.5, to verify). Still open there: whether auto
  runs skip in-window repeats, and whether the lifetime grey-out stays outside a window (both
  recommended yes).
- **Duplicate semantics ([invariants](../v1-analysis/invariants.md), "Contest dupe check and general
  ingest dedupe are two different things"):** a contest dupe is "per contest rules, is this call
  already worked on this band in this logbook", distinct from ingest idempotency.
- **Dupes inform, never refuse:** today's worked-before check (`GET /v1/contest-dupe`: call + band +
  mode/submode in the logbook, no time window) greys a Band Activity row, and a click proceeds with
  `allow_duplicate` (XE1GM repair, 2026-07-26). W-0011 proposed an optional `since` so that inside a
  declared window the grey-out means "worked in this contest".
- **Different-call contests (W-0014, 2026-09-19):** a contest under another callsign is a different
  logbook, gated by ADR 0056's per-logbook bindings and SM Cloud addressing, and archives (ADR 0071)
  offer a separate file. Out of the first slice: the reference case runs under the operator's own
  call.
- **ADR 0049 (Rejected):** no daemon-owned operating sessions; the contest window, not a session id,
  is the grouping.

## Current state (verified 2026-10-28, tree at `1ac1f85d`)

- `types.QsoDetails` already carries ADIF `CONTEST_ID`, `STX` and `SRX`
  (`internal/types/qso_details.go:18,34-35`). The Phone / CW form exposes none of them, and no
  contest mode, serial counter, exchange field, in-window dupe check or Cabrillo export exists.
- FT8/FT4 contacts get no `CONTEST_ID`; only the FT8 Field Day path sets one (ADR 0037).
- Session export and email produce ADIF only (`POST /v1/session/export`, `POST /v1/session/email`).

## Reference contest — UK/EI DX Contest

**Source:** <https://www.ukeicc.com/dx-contest-rules.php>, page "Version 19 - 2nd May 2026", read
2026-10-28 through a fetch agent. Its report paraphrases except where quoted; the points table was
flattened on the page and its column mapping reconstructed, so **every scoring fact below must be
re-verified against the page before it becomes a test fixture**. Translated PDFs linked from the page
were not read.

- **Events and dates:** separate CW and SSB contests, each "from 1200z on Saturday to 1200z on Sunday".
  2026: CW 25–26 April (past); **SSB 31 October – 1 November**. 2027: CW 24–25 April, SSB 23–24
  October.
- **Bands and modes:** 3.5, 7, 14, 21 and 28 MHz; one mode per contest. Contest-preferred segments
  "must be observed"; operating outside them disallows all QSOs (rule 9.2). Segments given for 80 m and
  20 m only (SSB: 3600–3650, 3700–3800 and 14125–14300 kHz; CW: 3510–3560 and 14000–14060 kHz); none for
  40, 15 or 10 m. News-bulletin frequencies on 80/40 m on Sunday morning are to be avoided.
- **Categories:** UK/EI (EI, G, GD, GI, GJ, GM, GU, GW) versus DX; single-op unassisted or assisted;
  multi-op is 24 h high-power assisted only; 24 h or 12 h (off periods at least 60 minutes); high,
  low (100 W) and QRP (5 W); overlays and teams. Categories and overlays are set at upload (rule
  8.2), **but rule 5 says a Cabrillo log without power information is treated as High Power** — a
  tension between the two rules (the operator's reading of the page, 2026-10-28), recorded here rather
  than resolved: a file may still need its power stated. Each entry uses a different callsign (rule
  11.2).
- **Exchange (rule 6):** "RS(T) (optional) plus a sequential serial number starting from 1"; the
  serial runs across all bands ("do not start at 1 when moving to a new band"); UK/EI entrants add a
  2-letter district code (Appendix 2, "Total = 155 UK/EI District codes"). A missing received serial
  is logged as zero and still scores. The sample logs show `--` in the district slot for DX stations.
- **Scoring (reported; unverified):** points by who works whom, band and continent (for a DX entrant:
  UK/EI 8 on 80/40 m and 4 on 20/15/10 m; Europe 4/2; other DX 2/1); UK/EI entrants score double
  between 0100z and 0459z; multipliers are DXCC entities per band (excluding the UK/EI entities) plus
  district codes per band, **summed across bands** (rule 7.2, per the operator's reading of the page,
  2026-10-28); final score = QSO points × multipliers.
- **Penalties:** a wrong call or serial loses the QSO's points plus twice their value; not-in-log loses
  the QSO's points.
- **Log (rule 8):** electronic Cabrillo only, one log per entry, QSOs in order of serial sent,
  frequencies to the nearest kHz, uploaded through the web robot within 24 hours of the end. The
  robot's emailed acknowledgement can report problems that must be corrected and resubmitted (rule
  8.4), so an acknowledgement alone is not acceptance. Sample
  `QSO:` line for a DX entrant: `QSO: 28024 PH 2014-07-26 1338 DL1AA 599 001 -- ON4SS 599 019 --`
  (field order inferred from the samples, not labelled on the page).

For 7Q5MLV (Malawi) the entry is **DX**: send RS plus serial and `--`; receive RS, serial and, from
UK/EI stations, a district code.

## Unresolved questions

Sponsor rules (to settle from the organisers, the translated PDFs or an established logger's module
before they become fixtures):

1. The **duplicate policy**: once per band? Are duplicates to be logged (and the robot zero-scores
   them) or omitted? **Unanswered by every sponsor document read** — organiser question 3 below.
2. The **Cabrillo header**: version, the `CONTEST:` value, required `CATEGORY-*` and other header
   fields, given that categories are chosen at upload; and whether power must be stated in the file,
   given rule 5's High Power default.
3. The **definition of "Europe"** for the points table. (Whether multipliers sum across bands is
   answered: they do, rule 7.2.)
4. The district codes with no DXCC suffix (the London codes `EC`, `EL`, `NL`, `NW`, `SE`, `SW`, `WC`,
   `WL`) and the dual-entity codes (`CH`, `HR`, `LD`, `SY`, `TD`).
5. The ADIF `CONTEST_ID` value for this contest. **Answered:** ADIF 3.1.6 has no enumeration value;
   a sponsor-defined string is permitted (findings, question 3).

Product decisions (the operator's):

6. **Serial allocation (mechanism unresolved):** a serial is sent over the air before the QSO is
   saved, so the record of what was transmitted cannot be reconstructed from saved QSOs. Cases to
   decide: a serial sent for a contact that was interrupted or never saved; a failed or interrupted
   save; a corrected or deleted QSO; a daemon or browser restart mid-contest; two tabs logging the same
   contest concurrently. Gaps are visible to the sponsor's checker; reuse risks two QSOs claiming one
   transmitted number.
7. **Where the exchange is entered:** dedicated fields on the Phone / CW form (received serial,
   received district) versus a free exchange field parsed per contest.
8. **Received RS:** logged as heard, or defaulted (the rule makes RS optional).
9. **Segment checks:** warn when the rig frequency is outside a published segment? The page gives
   segments for 80 m and 20 m only.
10. **Contest membership scope:** which archive and logbook a declared contest applies to; which modes
    (Phone / CW only for an SSB or CW contest, or FT8/FT4 when a digital contest is the reference);
    whether membership is decided by the QSO's own timestamp or by the time it is submitted (a QSO
    entered late, or imported); how the window boundaries are treated (inclusive or exclusive start and
    end); and what happens to a non-contest QSO logged inside the window — a policy not yet decided.
11. **Duplicate key and local retention:** the sponsor's duplicate key (question 1); every logged
    contact is preserved locally whatever the key, and any exclusion from an export needs an explicit
    rule.

## Sponsor questions 1–5: findings (2026-10-28)

Sources were read in the order the operator asked: sponsor documentation first, then established
logger modules and registries as corroboration. Fetched through web agents on 2026-10-28; the
sponsor PDFs were downloaded and their text extracted locally. Sponsor documents, newest first:

- **S1** — English rules, "Version 19 - 2nd May 2026" (<https://www.ukeicc.com/dx-contest-rules.php>),
  the current rules.
- **S2** — the upload robot, `hfenter.pl` V12.77, read only, nothing submitted:
  <https://logs.ukeicc.com/cgi-bin/hfenter.pl?Contest=DX%20SSB%20Contest> (S2a) and
  <https://logs.ukeicc.com/cgi-bin/hfenter.pl?Contest=DX%20CW%20Contest> (S2b). A category form
  (section DX / UK and EI / checklog; operator; assisted; power; time; overlay; rookie) with no
  instructions about the file. Its linked rules pop-ups
  (<https://logs.ukeicc.com/cgi-bin/hfcontest_rules.pl?contest=ukeidxssb&year=2026> and the
  `ukeidxcw` equivalent) returned empty bodies.
- **S3** — the translated PDFs linked from S1, each "Version 13 – 4 Jan 2023", older than S1:
  French, <https://www.ukeicc.com/DXcontestRules/rulesfrench.pdf> (S3-fr); German,
  <https://www.ukeicc.com/DXcontestRules/rulesgerman.pdf> (S3-de); Polish,
  <https://www.ukeicc.com/DXcontestRules/rulespolish.pdf> (S3-pl).
- **S4** — sponsor-hosted HTML translations, "Version 6.3 January 2020": German,
  <https://www.qsl.net/gm3woj/rulesgerman.htm> (S4-de); Polish,
  <https://www.qsl.net/gm3woj/rulespolish.htm> (S4-pl).

Corroboration:

- **C1** — contestcalendar.com, revised February 2026: the CW contest
  <https://contestcalendar.com/contestdetails.php?ref=597> (C1a) and the Cabrillo-names registry
  <https://www.contestcalendar.com/cabnames.php> (C1b; contests 596 SSB and 597 CW).
- **C2** — N1MM Logger+ documentation, undated: contests list
  <https://n1mmwp.hamdocs.com/manual-supported/contests-list/> (C2a) and HF contest setup
  <https://n1mmwp.hamdocs.com/manual-supported/contests-setup/setup-hf-contests/> (C2b).
- **C3** — WWROF Cabrillo v3: header <https://wwrof.org/cabrillo/cabrillo-v3-header/> (updated
  5 June 2025; C3a) and QSO data <https://wwrof.org/cabrillo/cabrillo-qso-data/> (15 March 2021;
  C3b).
- **C4** — ADIF 3.1.6 (2025-09-15), <https://adif.org/316/ADIF_316.htm>.
- **C5** — Writelog guide, January 2016, <https://www.qsl.net/gm3woj/writelog1.htm>.

### 1. Cabrillo version, `CONTEST:`, headers and power — partly answered, one conflict

- **Version:** no sponsor document names a Cabrillo version. C3a makes `START-OF-LOG: 3.0` and
  `END-OF-LOG:` the only **unconditionally** required tags of the generic specification; it also
  requires `CATEGORY-TRANSMITTER` for multi-operator entries (irrelevant to this single-op slice).
  `CONTEST:` is a common tag whose value "Contest sponsors may define".
- **`CONTEST:` value — conflict, unresolved.** S1 (current) states none. The sponsor's translations
  state `CONTEST: UKEIDXCW or UKEIDXSSB` (S3-fr; S4-de and S4-pl; S3-de and S3-pl state no header lines). C1b's registry
  lists **`UKEI-DX`** for both the CW and SSB contests (registry intro: names taken "from the contest
  sponsor's web site or an e-mail exchange with the contest manager", source for this entry not
  given), with alias `UKEIDX-CW` on the CW page (C1a). C2a and C2b use log types `UKEIDXCW` / `UKEIDXSSB` but does
  not say what it writes to `CONTEST:`.
- **Category headers:** the sponsor's translations list `CATEGORY-OPERATOR` (SINGLE-OP / MULTI-OP),
  `CATEGORY-POWER` (HIGH / LOW / QRP), `CATEGORY-ASSISTED` (ASSISTED / UNASSISTED — the translations'
  wording; **conflicts with C3a**, whose value is `NON-ASSISTED` per the operator's reading of the
  WWROF header page; neither spelling is verified against the robot), `CATEGORY-TIME`
  (24-HOURS / 12-HOURS) and `CATEGORY-OVERLAY` (SINGLE-ELEMENT ANTENNA). S1 rule 8.2 says categories
  and overlays "are created automatically when you upload your logfile and select the categories on
  the log submission robot", and S2 is that selection form.
- **Power (finding):** S1 rule 5 — "Any station not indicating their transmitter power on their
  submitted Cabrillo logfile will be classified as High Power" (the same in S3-fr, S3-de, S3-pl, S4-de and S4-pl). The rule
  establishes the consequence of omitting power; it does not itself specify the header line. Which
  source prevails when the file and the robot selection disagree is unresolved.
  *Proposed product policy (not a sponsor finding):* always write `CATEGORY-POWER`, so a low-power
  or QRP entry is never reclassified by omission.
- **QSO lines:** S1 rule 8.3's samples, including a DX entrant's: `QSO: 7165 PH 2014-07-26 1343 DL1AA
  59 003 -- EI7CC 599 136 DU` — frequency in kHz (to 1 kHz for awards), mode, `yyyy-mm-dd`, `hhmm`,
  sent call, RS(T), serial, district or `--`, then the received call, RS(T), serial, district or
  `--`; QSOs in order of serial sent. Samples pad serials to three digits; width beyond 999 is not
  stated. S1's samples all use `PH`; S3-fr, S3-de, S3-pl, S4-de and S4-pl use `CW` for CW contacts. C3b defines the mode codes
  (`CW`, `PH`, …).

### 2. Duplicates — unanswered by the sponsor

No duplicate rule appears in S1, S3-fr, S3-de, S3-pl, S4-de or S4-pl, and S2a/S2b say nothing. C1a, C1b, C2a and C2b are silent; only the
multiplier rule says "worked on each band" (S1 7.2). The sponsor's duplicate key and whether
duplicates stay in the submitted log are **unresolved** and go to the organiser (below). Locally
every contact is kept regardless (question 11).

### 3. ADIF contest ID — answered: no enumeration value

C4 has no UK/EI entry (the nearest are `UK-DX-RTTY` and `UK-DX-BPSK63`, different contests).
`CONTEST_ID` is a String with "use enumeration values for interoperability", so a sponsor-defined
string is permitted. Proposal: store the sponsor's `CONTEST:` value once question 1 settles it. C4
also defines `STX`/`SRX` (integer serials ≥ 0) and `STX_STRING`/`SRX_STRING` ("contest QSO
transmitted/received information", in Cabrillo format where no dedicated field exists).

### 4. District codes — partly answered

- S1 lists 155 codes; the London codes (`EC`, `EL`, `NL`, `NW`, `SE`, `SW`, `WC`, `WL`) carry no
  DXCC suffix in S1, S3-fr, S3-de, S3-pl, S4-de and S4-pl alike.
- **Conflict on dual-entity codes:** S1 (2026) gives `CH`, `HR`, `LD`, `SY` as "G / GW" and `TD` as
  "G / GM", and its notes tell UK entrants to "pick one District Code and use the same code in all
  UK/EI DX Contests". S3-fr, S3-de, S3-pl (2023) and S4-de, S4-pl (2020) give `CH`, `HR`, `SY` as G, `LD` as GW and `TD` as
  G/GM. S1, the newest, prevails.
- For a DX entrant the code is received exchange only, and the code-to-entity mapping matters only
  to scoring (deferred). *Proposed product policy (not a sponsor finding):* store the two letters as
  received and check them against S1's list, reporting an unknown code without refusing the QSO.

### 5. "Europe" — deferred with scoring

Not defined in any sponsor document read. Deferred until scoring is designed.

### Other conflicts found (recorded, not product questions)

- **Not-in-log penalty:** S1 9.1 and S3-fr — "no additional penalty"; S3-de, S4-de and S4-pl — a
  further penalty (S3-pl's wording was not located in its extracted text). S1, current, prevails.
- **Multi-op:** S1 has one multi-op category (24 h, high power, assisted); S4-de and S4-pl limit a
  single-transmitter multi-op to UK/EI. S1 prevails; irrelevant to a single-op DX entry.
- **Deadline:** C5 (2016) says two hours; S1 says 24 hours.

### Provisional export contract (pending the organiser's answers)

```
START-OF-LOG: 3.0
CONTEST: <UKEIDXSSB | UKEIDXCW per the sponsor's translations; UKEI-DX per C1b — unresolved>
CALLSIGN: 7Q5MLV
CATEGORY-OPERATOR: SINGLE-OP
CATEGORY-ASSISTED: <ASSISTED | NON-ASSISTED per C3a; UNASSISTED per the translations — unresolved>
CATEGORY-POWER: <HIGH | LOW | QRP>   (always written: proposed policy, see 1)
CATEGORY-TIME: <24-HOURS | 12-HOURS>
QSO: <kHz> PH <yyyy-mm-dd> <hhmm> 7Q5MLV <rs> <nnn> -- <call> <rs> <nnn> <district | -->
END-OF-LOG:
```

The `CALLSIGN:` tag and any `CREATED-BY:`/`OPERATORS:` lines are C3a common tags, not sponsor
requirements; the sponsor's need for them is unconfirmed.

### Draft questions for the organiser (prepared, not sent)

1. Which `CONTEST:` value does the robot expect for the SSB and CW contests — `UKEIDXSSB` /
   `UKEIDXCW` (the translated rules) or `UKEI-DX` (contestcalendar.com)? Which Cabrillo version?
2. Does the robot read the `CATEGORY-*` lines, or only the categories selected on upload? If they
   differ, which prevails? Given rule 5, should `CATEGORY-POWER` always be in the file? For
   `CATEGORY-ASSISTED`, does the robot expect `NON-ASSISTED` (the Cabrillo v3 value) or `UNASSISTED`
   (the translated rules)?
3. Duplicates: is a station counted once per band? Should duplicate contacts be left in the submitted
   log (and zero-scored by the robot) or removed?
4. Serial numbers in `QSO:` lines: is a fixed width expected (three digits in the samples), and how
   should serials above 999 be written?
5. Which DXCC entity do the London district codes belong to, and is the 2026 list's G / GW and G / GM
   treatment of CH, HR, LD, SY and TD authoritative over the 2023 translations?
6. Is there a way to validate a log before the contest — a test upload or a checker — without it
   counting as an entry? (The robot offers a "checklog" section, which is an entry category, not a
   test facility.)
7. (For scoring, later) Which entities count as "Europe" for the points table?

## Proposed acceptance criteria (first slice; proposal, not ruled)

Scoring is **out of the first slice** until questions 1–4 are settled.

1. **Declared contest:** the operator declares a contest with an id, a start and end (UTC), a mode and
   the sent exchange (here: RS and serial; `--` for the district as a DX entrant). Which QSOs are
   members — archive and logbook, applicable mode, QSO timestamp or submission time, and the window
   boundaries — follows question 10; members carry `CONTEST_ID`.
2. **Explicit sent serial:** every contest QSO stores the serial actually sent, in ADIF `STX`, written
   at logging time; it is never derived later from a count of rows. How the next serial is allocated,
   and what happens to a serial sent for a QSO that is never saved, is unresolved (question 6).
3. **Received exchange stored:** the received serial in `SRX` (0 when none was sent, per the rules) and
   the received district (or `--`) in a stored field (ADIF `SRX_STRING` is the candidate), both
   visible in the Session panel and the Logbook.
4. **Duplicate detection, kept apart from the sponsor's policy:** inside a declared window the form and
   Band Activity show "already worked in this contest" by the sponsor-defined duplicate key, pending
   verification (question 1), and logging still proceeds — detection informs, it never refuses. Every
   logged contact is preserved locally. Whether a duplicate is logged, omitted or zero-scored is the
   sponsor's policy, applied at export only under an explicit rule (question 11).
5. **Cabrillo export:** the declared contest's QSOs export to a Cabrillo file in serial order, with
   frequency in kHz, the mode code (`PH` for SSB) and the sent and received exchanges in the sponsor's
   order, plus the header fields the sponsor requires (question 2). The export refuses, naming the QSOs,
   when a contest QSO lacks a stored sent serial or received serial; it never invents one.
6. **Proof against the reference contest:** fixtures built from the UK/EI rules (exchange, serial
   continuity across bands, district codes, `--` for DX, the sample `QSO:` lines) produce a file that
   passes validation: an acknowledgement reporting no problems requiring correction (rule 8.4), not
   the acknowledgement alone. A rehearsal submission is made only through a test facility the sponsor
   supports or with the organiser's agreement.

Nearest confusable outcomes: a serial reconstructed from a row count after a deletion; a pre-contest
QSO greyed as a contest duplicate; a duplicate refused at entry; a Cabrillo file in time order instead
of serial order; a header the robot rejects; an acknowledgement taken as acceptance although it
reports problems; a non-contest QSO stamped because it fell inside the window (its policy is
undecided, question 10).

## Interim: operating the 2026 SSB event on alpha.5 (not authorised)

If the operator enters the 31 October – 1 November 2026 SSB event before the slice exists, the interim
proposal is to log on alpha.5 and convert afterwards. Operator corrections, 2026-10-28:

- **Record the sent serial explicitly for every QSO.** A session or logbook count is not a reliable
  record of what was transmitted after corrections, deletions or interrupted contacts. A comment
  convention such as `S 001 R 036 D OX` (sent serial, received serial, received district) carries all
  three per QSO.
- **Prove the export path before operating for a submitted entry.** Validate representative ADIF
  records, the required Cabrillo headers, district handling and serial ordering in advance; finding a
  gap after the contest would fall inside the 24-hour upload deadline.

Any converter for this would be an out-of-product tool and is **not authorised**; it needs the
operator's go-ahead, and question 2 answered, before it is written.

## Verification boundary

Design only. No code, migration, API or SPA change is authorised by this dossier. When implementation
is authorised: test-first against fixtures from the reference contest, an ADR for the contest model,
and acceptance through a new candidate's gates — never a live contest on an unaccepted build. No RF,
rig command or keyed test is part of this item.

## References

- [W-0011](W-0011-ft8-and-rig-refinements.md) — contest mode ruling and dupe-layer proposal (2026-09-12).
- [W-0014](W-0014-deferred-product-workstreams.md) — the contesting design thread and the data-model
  chain (2026-09-19).
- [W-0021](W-0021-qso-archives.md) and [ADR 0071](../decisions/0071-first-class-qso-archives.md) —
  archives.
- [ADR 0037](../decisions/0037-ft8-field-day.md) — FT8 Field Day, the one path that sets `CONTEST_ID`.
- [Dogfood inbox](../dogfood-inbox.md), 2026-10-28 — Cabrillo session and logbook export.
