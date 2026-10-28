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

6. **Serial allocation (ruled 2026-10-28; see *Serial allocation* below):** a serial is sent over the air before the QSO is
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
10. **Contest membership scope:** ruled 2026-10-28 — see *Contest membership* below.
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

## Contest membership — ruled 2026-10-28 (operator)

Membership is designed before serial allocation because the serial sequence needs a defined owner
and scope. The proposals M1–M5 and decisions D1–D4 below were ruled by the operator on 2026-10-28
with corrections, recorded here. The weighed alternatives go into the contest ADR before
implementation. Serial allocation was ruled separately (see *Serial allocation*); no implementation
is authorised.

**Facts it rests on (tree at `81c670f8`):** a QSO's time is its own ADIF `QSO_DATE` + `TIME_ON`,
stamped by the Phone / CW form's clock when the contact starts and editable before submit ("only
blank fields get 'now'", `frontend/app/src/lib/operate/qso.svelte.ts`). QSOs are created by `POST
/v1/qso` (the form, `curl`, import tooling), the FT8 logging path and `smd import`
(`qsoservice.SubmitImport` / `SubmitImportBatch`), and edited by `PATCH /v1/qso/{uuid}`. Logbooks
live inside an archive (W-0021); ADR 0056 places per-logbook and relational data in the database
rather than `config.json` — it supports this placement but does not prescribe a contest schema.

- **M1 — ownership (approved, with a correction).** A declared contest *occurrence* (one edition:
  the 2026 SSB event, say) is a record in the archive file, owned by one logbook. It carries a
  **stable identity of its own, distinct from ADIF `CONTEST_ID` and from the sponsor's Cabrillo
  name**: annual editions can share those strings but never share membership or a serial sequence.
  One serial sequence per occurrence. *Alternative weighed:* station-global in `config.json` —
  contrary to ADR 0056's placement rule, and it would follow the operator into an archive it does not
  belong to.
- **M2 — eligibility by the QSO's own time (approved).** A QSO is *eligible* when its
  `QSO_DATE`/`TIME_ON` falls inside the window, whenever it was submitted. **Eligibility and
  membership are separate:** membership is explicit (M5), and a member can become ineligible after an
  edit (D2). *Alternative weighed:* submission time — a late-entered or corrected QSO would land on
  the wrong side of the boundary.
- **M3 — window `[start, end)` (recommended product boundary).** 12:00:00 on the Saturday is inside;
  12:00:00 on the Sunday is outside; an `HHMM` time of `1200` on the end date is outside (ADR 0041
  allows `HHMM` or `HHMMSS`). This is the **proposed product boundary, not a confirmed reading of
  the sponsor's "1200z on Saturday to 1200z on Sunday"**.
- **M4 — mode is part of eligibility (approved).** A UK/EI SSB occurrence takes **SSB and its
  applicable submodes** (`USB`, `LSB`); CW, FT8/FT4 and other modes inside the window are not
  eligible.
- **M5 — explicit membership, persisted as an association (approved, with a correction).**
  Membership is an **association between the QSO and the declared occurrence, written atomically
  with the QSO**; `CONTEST_ID` alone is insufficient. Nothing becomes a member by a query over the
  window. An **imported `CONTEST_ID` stays intact on the QSO without creating an association**; a QSO
  from another logbook, or from before the declaration, stays a non-member unless explicitly added
  (D3). *Alternative weighed:* derived membership (every QSO in the logbook, mode and window) — it
  sweeps in non-contest contacts and cannot carry a sent serial that was never recorded.

| Decision | Ruling (operator, 2026-10-28) |
|---|---|
| D1 — a non-contest QSO inside the window | An **explicit, visible contest mode** on the form, **selecting the declared occurrence**. Ordinary logging remains ordinary. |
| D2 — editing a member's time or mode out of eligibility | **Allow the correction; retain membership and serial.** Show the inconsistency immediately and report it in export validation. Never silently omit the QSO or remove membership. |
| D3 — membership changes after the fact | **Allowed, explicit and recorded.** The QSO and its transmitted serial are preserved. A serial gap is diagnostic, not automatically an invalid log; duplicate-serial handling remains unresolved. |
| D4 — overlapping declarations | **Refused within one logbook** for the first slice. Adjacent half-open windows do not overlap. Other logbooks remain independent. |

**Concurrency and intent:** every request that logs into a contest **explicitly identifies the
intended occurrence**. The daemon validates that the occurrence belongs to the request's archive and
logbook and writes the membership atomically with the QSO; it **never infers intent from whichever
contest is currently active**. So two tabs agree on membership because each states its occurrence,
not because the daemon guesses. How serials are allocated across tabs, restarts and unsaved contacts
is ruled in *Serial allocation*.

### Export-validation rulings (2026-10-28, operator)

Design rulings only; no implementation authorised. They govern the **submission export** — the file
offered as ready for the sponsor. **None of them ever refuses saving a contact**; logging is never
blocked by an export rule. The sponsor questions that remain open (*Unresolved questions* 1–4, 11, and
the organiser questions) are separate and unchanged by these rulings.

| # | Case | Ruling |
|---|---|---|
| 1 | A member that has become **ineligible** (D2) | The submission export is **blocked**, naming each inconsistency. A member is never silently omitted. |
| 2 | **Duplicate sent serials** (from D3 or from allocation) | The actual duplicate sent serials are **preserved** and **every affected contact is reported**. Whether the sponsor accepts a duplicate serial is unverified. |
| 3 | **Serial gaps** | **Diagnostic only**; no gap pattern blocks the export. Serials are **never renumbered**. |
| 4a | A member **missing its received serial** | An explicit operator record of **"no serial received"** may export as `0` (rule 6: a missing received serial is logged as zero). An absent value with no such record is not exported as `0`. |
| 4b | A member **missing a required received district** | **Unresolved.** Rule 6 does not establish `--` for a missing UK/EI district; the samples establish `--` only for DX stations. Until resolved, the submission export is **blocked** for such a member. |
| 5 | An **unknown district code** | For a **syntactically representable** code: the unknown two-letter code is **preserved** and exported with a **warning**. **Invalid field syntax** is reported; it is never silently altered and never allowed to corrupt the Cabrillo file. |
| 6 | An **imported `CONTEST_ID` with no association** (M5) | Relevant unassociated imports are **reported separately**; they are never exported as occurrence members. |
| 7 | The **sponsor's duplicate policy** in the file (question 1) | **Unresolved.** All contacts are preserved locally. An **all-contact diagnostic export** is acceptable, but it is not called sponsor-ready, and **no provisional duplicate key is invented**. |
| 8 | A member flagged with **exchange uncertainty** (T3: both *H* and *Y* sent, carried serial unclear) | The submission export is **blocked** until the carried serial is **explicitly resolved**. Both transmitted numbers and the resolution history are preserved. |

Source for 4: [UK/EI DX Contest rules](https://www.ukeicc.com/dx-contest-rules.php) (S1), rule 6 and
the sample logs.

## Serial allocation — option B ruled 2026-10-28 (operator)

Answers question 6. Drafted as a proposal and revised through several operator reviews the same day.
**Ruled 2026-10-28 (operator), as design rulings — design approval only, no implementation
authorised:**

- **Option B:** durable reservations per contest occurrence, protected from the moment a number is
  displayed.
- **No timeout and no automatic retirement** of a hold.
- **The reuse policy:** a number is reused only after an explicit "not sent", only when it has no
  current hold and no permanent protection, and only under a new allocation identity.
- **Atomic save, membership and binding; retry-safe operations; stale-write checks** — as in the
  transition table.
- **Preservation of the serial actually transmitted** through explicit reconciliation.
- **Typed serials (T3, ruled 2026-10-28):** an explicit override with the held number resolved in the
  same save — see *Typed serials during normal operation*.

The candidate mechanisms named below (such as the request record's placement) remain provisional
design, not rulings. Options A1 and A2 stay recorded as the alternatives weighed; the contest ADR
captures the accepted design and the alternatives before implementation.

### Three distinct events

1. **Offered** — the form displays a number for the next contact.
2. **Transmitted** — the operator sends it over the air. On SSB and CW only the operator knows this.
3. **Saved** — the QSO is written with that number in `STX` and its membership (M5).

Saving cannot establish whether a number was transmitted, and not saving cannot establish that it
was not. The conservative rule needs no transmission proxy: **a number is possibly transmitted from
the moment it is displayed**, because the operator can read it out before any other signal changes.

### The reuse constraint (ruled 2026-10-28)

**Never automatically reuse a number that may have been transmitted.** The number actually
transmitted is preserved on the QSO even when that creates a gap or a duplicate; the anomaly is
reported (export validation), never repaired by renumbering history. A number returns to the pool
only through the operator's explicit statement that it was not sent.

### Option A1 — automatic `highest + 1`, recorded on each QSO

The form proposes `next = highest stored STX in the occurrence + 1`, editable; the saved QSO stores
what was sent. No allocation state exists outside saved QSOs.

| Case | What happens |
|---|---|
| Cancelled before transmission | The form is cleared; the same number is proposed next — but it was displayed, so under the rule above it may have been sent. Reuse depends on the operator. |
| Interrupted after transmission (never saved) | Nothing records the number; the next proposal is the same number, **reused automatically**. Breaks the constraint. |
| Failed save, retried | The form still holds the number; the retry resends it. Correct if the save is idempotent. |
| Restart (daemon or browser) | Recomputed from saved QSOs; a transmitted-but-unsaved number is forgotten and **reused**. Breaks the constraint. |
| Two tabs | Both compute the same `highest + 1` and **display the same number**. Detected only as a duplicate after saving. Breaks the constraint. |
| Correction of a saved serial | The operator edits `STX`; any gap or duplicate is reported. |
| Deletion | Unless soft-deleted QSOs count, a deleted QSO's highest number is **reused**. |
| Membership removed or reassigned | If the QSO held the highest number, that number is **reused** next. |

A1 satisfies the constraint only through the operator's memory.

### Option A2 — explicit operator-entered serials, no automatic proposal

The operator types every number sent; the software never proposes one. It records what was typed,
warns at save when the number is already bound in the occurrence, and reports gaps and duplicates at
export. Because it never proposes a number, the software cannot *automatically* reuse one — but the
operator can still enter a previously transmitted number, so the constraint is not guaranteed: the
software can only warn when the typed number is already bound. The burden of tracking the sequence
(across interruptions, restarts and two windows) rests entirely on the operator, and two windows can
still type the same number. It is the
natural fallback when no reservation is available (below).

### Option B — daemon-reserved serials per occurrence, with an allocation ledger (leading option)

The archive holds, per occurrence (M1), a durable **allocation ledger**
separate from saved QSOs. Each entry records the serial, an allocation id, its state, the holder (the
form instance that took it), timestamps, and — once bound — the QSO's UUID. The ledger keeps the full
history of every number; no entry is ever deleted.

**States and transitions** (each transition is one transaction; at most one can leave **held**):

- **held** — displayed to one form. Leaves only by save or release. **No timeout and no automatic
  retirement** in the first slice (operator recommendation): an unanswered hold stays held.
- held → **bound** — the save. QSO creation, the membership association (M5) and the binding commit in
  one transaction, or none of them does.
- held → **retired** — the operator releases the number as *possibly sent*. Never offered again.
- held → **returned** — the operator releases the number as *not sent*. This **revokes the allocation
  id**: the old holder can no longer save against it. The number may be offered again — only under a
  **new allocation id**, and only if no other current hold and no permanent protection exists (the
  protected-number rule below).
- **externally bound** — a number that entered the occurrence without a reservation (a correction, a
  reassignment, an override (T3), an operator-entered number while reservations were unavailable). Recorded before any
  further allocation, so it is never offered (see the offer rule below).

Numbers are offered only by the offer rule in the transition table below: from the **returned**
pool, or a number with no ledger entry at all.

| Case | What happens |
|---|---|
| Cancelled before transmission | The number was displayed, so it stays **held** until the operator releases it: "not sent" → returned (old allocation revoked); "possibly sent" → retired (a gap, reported). |
| Interrupted after transmission | The form keeps holding the number until the contact is saved or released. Never reused automatically. |
| Failed save, retried | See *Save, release and retries* below: a retry consumes no new number. |
| Restart | The ledger is durable in the archive file; holds survive a daemon restart. A form whose draft is restored reattaches to its allocation id. A form that never returns leaves its number **held**, listed for the operator to release as sent or not sent. |
| Two tabs | Each form holds its own number; the daemon never offers a held number. Tab 2 sees the next free number with a note such as "045 is held by another window (since 12:03)". Serial order and time order can interleave; the Cabrillo export orders by serial, as the sponsor asks. |
| Correction of a saved serial | An explicit, recorded edit (the number actually sent differs). The original binding stays in the history as *corrected*; the new number is **accounted for in the ledger before commit** — externally bound, so protected — and the offer rule never offers it afterwards. A collision with a held or bound number is recorded as a duplicate and reported. |
| Deletion | The QSO is soft-deleted; its number stays bound to the deleted QSO and is never reused; the export reports the gap. |
| Membership removed | The QSO keeps its `STX`; the number stays bound in the history (*membership removed*) and is never reused. |
| Membership reassigned to another occurrence | The source ledger keeps the history (*moved out*); the QSO's number is **accounted for in the target ledger before the reassignment commits** (externally bound, so protected in the target), so the target can never allocate it; any collision is reported. Nothing is renumbered. |

### Save, release and retries

- **Save and release are mutually exclusive.** Whichever commits first decides the entry; the other
  receives a definite refusal ("allocation no longer held") and changes nothing.
- **A save that loses** — to a "not sent" release, or after the number was revoked and re-offered —
  never loses the contact. The refusal leads to an **explicit reconciliation path**: the operator
  saves the QSO with the number actually transmitted, recorded as externally bound and flagged
  (a duplicate if the number has since been bound elsewhere). What was transmitted is preserved.
- **Identities.** The *allocation id* identifies one number held for one contact; the *request id*
  identifies one operation (reserve, save, release). The daemon records each committed operation's
  request id, a digest of its content and its result **in the same transaction as its effect**.
  - A retry with the same request id and the same content returns the recorded result — for example
    a save whose response was lost returns the stored QSO and consumes no new number.
  - The same request id with **different content is rejected**.
  - No record means no committed effect — **within the proposed atomicity and retention contract**
    (the record commits in the same transaction as the effect and is kept for the occurrence's life).
    It does not prove that the original request has finished: it may still be running.
  - **Required outcome for concurrent attempts with the same request id:** at most one committed
    effect, whatever happens — lock contention, a cancelled request, a client that gives up after
    exhausting its retries. Every such outcome leaves the operation **safely retryable with the same
    request id**: the retry either returns the one recorded result or executes the operation once.
    (A candidate mechanism: the request record is the transaction's first write under a unique key.
    The mechanism is for design; the outcome is the requirement.)
  - **A retry returns the recorded result and the current state separately.** For example, a retried
    reserve returns the allocation it recorded *and* that allocation's present state; if it has since
    been revoked, the form must not treat it as usable.
- **Lost response to a release:** the retry returns the recorded outcome, so the form learns whether
  its number was returned or retired.

### Transition table (proposed, for review)

**Protected-number rule (takes precedence over the returned pool).** Two kinds of protection:

- **Current hold** — an entry that is **held** and not revoked protects the number while it stays
  valid. The explicit "not sent" (return) revokes that hold, and with it this protection.
- **Permanent protection** — any entry in the number's history, under any allocation, that is
  **bound**, **externally bound**, **retired**, **corrected**, **moved out** or **membership
  removed**. It survives a return.

A number is **protected** while it has a current hold or any permanent protection, and a protected
number is never offered.

**Offer rule.** A number is *offerable* only if it has **no ledger entry at all** (never issued), or
it is **returned** and has **no current hold and no permanent protection**. Reserve takes the lowest
such returned number first, otherwise the lowest never-issued number above the highest ever issued.
Advancing a counter alone protects neither a number already held by another form nor one with a
returned entry; checking current holds and the permanent history protects both.

**Serialisation.** All ledger writes for one occurrence run in one write transaction at a time (the
archive file's single writer). Each operation is recorded under its request id with the outcome
required above (at most one committed effect, safely retryable); writing that request record first
under a unique key is a **provisional candidate mechanism**, as in *Save, release and retries*. Every operation names its occurrence explicitly (M1, intent rule)
and is refused if the occurrence does not belong to the request's archive and logbook.

| Operation | Preconditions | Atomic effects (one transaction) | Retry (same request id) | Conflicting operations |
|---|---|---|---|---|
| **Reserve** | Occurrence exists and belongs to the request's archive and logbook; contest mode on the form. | Choose a number by the offer rule; create a **held** entry with a **new allocation id** and the holder, which protects the number (including one that came from a returned entry); record the request. | Same content: returns the recorded allocation and, separately, its current state — a since-revoked allocation is reported as revoked and must not be used. Different content: rejected. | Two reserves serialise and receive different numbers. A reserve never selects a protected number. |
| **Save** | The allocation is **held** and not revoked, and the request comes from its holder; the QSO is valid. | Insert the QSO with `STX` = the held number; write the membership association (M5); move the entry **held → bound** with the QSO UUID; record the request. | Same content: returns the stored QSO; no second QSO, no new number. Different content: rejected. | A save that finds the allocation not held is refused, commits nothing, and **says which case applies**: **already saved** — the allocation is bound to a QSO, or carries an override's **completion link** (for example an earlier save or override under another request id); the refusal names that QSO and the form shows it rather than inviting a second contact; or **revoked before save** — the allocation was returned or retired, no QSO is bound to it and it has no completion link; only then, once it is established that this contact has not already been persisted, is **reconcile** offered. Racing **return** or **retire**: whichever commits first wins. A save rejected for validation commits nothing and leaves the hold intact. |
| **Return** ("not sent") | The allocation is **held**. | **held → returned**; revoke the allocation id; record the request. Revoking the hold removes this allocation's current-hold protection; the number becomes offerable again **only if no other current hold and no permanent protection exists** — for example a reconcile that bound another contact to it keeps it permanently protected. | Recorded outcome and the allocation's current state returned (the form learns it was returned). | Save or retire committed first → return refused, nothing changes. After return, the number can be re-offered only under a new allocation id. |
| **Retire** ("possibly sent") | The allocation is **held**. | **held → retired**; the number is never offerable again; record the request. | Recorded outcome returned. | Save or return committed first → retire refused, nothing changes. |
| **Reconcile** (a losing save; an unreserved number; a late save against a revoked allocation) | Explicit operator action naming the occurrence and the number *X* actually transmitted; the QSO is valid. | Insert the QSO with `STX` = *X* and its membership; add an **externally bound** entry for *X* (with the reason), which protects *X* even if it has returned entries; if *X* is **held** by another form or already **bound**, record a **duplicate anomaly** (both contacts may genuinely have sent *X*) without touching the other entry; record the request. | Same content: returns the stored QSO. Different content: rejected. | A reserve that committed first and holds *X*: the reconcile still commits and records the duplicate; the other hold stays valid. A reserve after the reconcile never offers *X* (protected). |
| **Correct** (the number actually sent differs) | Explicit operator action naming the QSO, the new number *Y* and the state it was decided against: the **expected QSO revision**, the **expected occurrence membership** and the **expected allocation identity** (the entry binding *W*). | Mark *W*'s entry **corrected** (history kept; *W* stays protected); account *Y* exactly as reconcile accounts *X* (externally bound, so protected; duplicate anomaly if held or bound elsewhere); set the QSO's `STX` = *Y*; record the request. | Same content: recorded result and current state. Different content: rejected. | Any mismatch with the expected revision, membership or allocation — a concurrent correction, edit, reassignment or removal committed first — refuses the correction as **stale, for the operator's review**; serialising writes alone does not preserve intent. A reserve holding *Y* first → the correction commits with a duplicate anomaly. |
| **Reassign** (to another occurrence *P*) | Explicit operator action naming the QSO, the target *P* (same logbook) and the **expected QSO revision**, **expected membership** (occurrence *O*) and **expected allocation identity** (the entry binding *X* in *O*). | Move the membership *O → P*; mark *O*'s entry **moved out** (history kept; *X* stays protected in *O*); account *X* in *P* as reconcile does (externally bound, so protected in *P*; duplicate anomaly if held or bound in *P*); record the request. | Same content: recorded result and current state. Different content: rejected. | Any mismatch with the expected revision, membership or allocation refuses the reassignment as **stale, for review**. A reserve in *P* holding *X* first → the reassignment commits with a duplicate anomaly in *P*. |

**Removing membership** without a target (D3) has the same preconditions — the expected QSO revision,
membership and allocation identity, refused as stale on any mismatch — and the same first two effects:
the association is removed and the entry is marked **membership removed**, so the number stays
protected.

**A reconciled number in the two cases the offer rule exists for:**

- **Held by another tab.** Both contacts may have sent the number. The reconcile commits, the
  duplicate is recorded, and neither contact or hold is invalidated: the other tab may still save, and
  that second binding is a second recorded duplicate. The operator sees both in export validation.
- **In the returned pool.** The operator once said "not sent", but a contact did carry it. The
  reconcile's externally bound entry protects it in the same transaction, so it is never offered
  again, whatever its returned entries say. If it had already been re-offered and is now **held**
  under a new allocation id, the first case applies.
- **Returned after a reconcile.** Tab A holds 045; a reconcile binds another contact to 045; then A
  answers "not sent". The return revokes A's allocation, but 045 stays protected by the reconcile's
  entry and is never offered again.

### Daemon unavailable before a reservation

If the form cannot reach the daemon, it cannot be given a number. The operator may still transmit
one (from memory, a paper sheet, or A2 style), and the contact must not be lost: **allocation
availability never becomes a new barrier to preserving a contact**. The form accepts an
operator-entered serial for that contact, marked *unreserved*. When the contact is saved, the number
goes through the same accounting as a correction: externally bound in the ledger before commit, so
the offer rule never offers it, and any collision reported as a duplicate. (Saving itself still needs the
daemon, as for every QSO today; reservation adds no further condition.)

### What the operator sees

- In contest mode the form shows the number it holds ("Serial 045, held"), not a computed guess.
- A number another window holds is shown, not silently skipped ("045 held by another window").
- Clearing a contact asks whether the number was sent; an unanswered hold stays held and stays
  visible.
- Held numbers, gaps, duplicates, corrections and unreserved numbers appear in the occurrence's status
  and in export validation as reports, not as errors that stop logging.

### Typed serials during normal operation — T3 ruled 2026-10-28 (operator)

The case: a number *H* is held and displayed, and the operator sent (or will log) a different number
*Y* — a misread or misspoken number, a number from a paper sheet after an interruption, or a number
sent again when the other station asked for a repeat. **Ruled (design approval only, no
implementation authorised): T3, an explicit override, with *H* resolved as part of the successful
save.**

- The form shows *H* read-only by default. An explicit **"I sent a different number"** action opens a
  field for *Y*, which the operator confirms.
- The override completes only with an answer for *H*: **not sent**, **possibly sent**, or **both
  sent**. **No answer means no completed override:** the draft is kept and *H* stays held. There is
  no default answer; in particular "not sent" is never assumed.
- Cancelling the override, or a save refused for validation, changes nothing: *H* stays held, the
  draft is kept, no QSO is written.
- **Y equals H:** the ordinary save — *H* is bound; it is neither returned nor retired.
- **Both sent:** *H* is retired and the fact that *H* was also transmitted for this contact is
  preserved alongside *Y*. If it is unclear which serial the contact should carry, that **exchange
  uncertainty is flagged** on the contact rather than one serial being silently selected. Export
  handling of a flagged uncertainty is ruled in *Export-validation rulings* (8).
- **A repeated exchange is not automatically another QSO.** When the other station asks for a repeat
  within the same contact, it is the same contact: one QSO, and if a different number went out on the
  repeat, that is the *both sent* case. A genuinely separate contact (a later re-work, a new
  exchange) takes its own reservation and its own QSO, and duplicate detection informs as for any
  other contact. The override is never the route to a second QSO.

**Override operation** (added to the transition table; all effects in one transaction):

| Operation | Preconditions | Atomic effects (one transaction) | Retry (same request id) | Conflicting operations |
|---|---|---|---|---|
| **Override** ("I sent a different number") | Explicit operator action naming the occurrence, the **expected allocation identity** of *H* (held by this form, not revoked), the confirmed *Y* ≠ *H*, and the answer for *H* (not sent / possibly sent / both sent); the QSO is valid. *Y* = *H* is not an override: the request is the ordinary **save**. | Validate that *H*'s allocation is still held by this form; insert the QSO with `STX` = *Y* and its membership (M5); account *Y* exactly as reconcile accounts *X* — **externally bound** (reason: override), so protected even if it has returned entries, with a **duplicate anomaly** if *Y* is held by another form or already bound, without touching the other entry; resolve *H* under the existing protection rules — *not sent*: **held → returned**, allocation revoked, offerable again only with no current hold and no permanent protection; *possibly sent*: **held → retired**; *both sent*: **held → retired**, recorded as also transmitted for this QSO, and the exchange-uncertainty flag set when the operator says the carried serial is unclear; whatever the answer, link *H*'s allocation to this QSO as the override that **completed** it (the completion link) — *H*'s returned or retired state alone cannot distinguish a completed override from a release, and the link is not protection (a *not sent* *H* stays offerable under the protected-number rule); record the request. | Same content: returns the stored QSO, the recorded resolution of *H* and, separately, the current state; no second QSO, no new number. Different content: rejected. | **Save or override of *H* committed first** (for example a double submit under another request id): the binding or completion link identifies that QSO; refused as **already saved**, naming it; the form shows it, and a differing number is then a **correct** of that QSO, not a second contact. **Return or retire of *H* committed first** (from another window or the held-numbers list), with no binding or completion link: override refused, nothing committed; once it is established that this contact has not already been saved, the form offers **reconcile for *Y* carrying the operator's statement about *H*** (below). ***H* revoked and re-offered** to another form under a new allocation id: the stale allocation identity refuses the override; the other hold is untouched. **Another form holds *Y*** (reserved first): the override commits with a duplicate anomaly; that hold stays valid. **A reserve after the override** never offers *Y* (externally bound) and offers *H* only if it was returned and is unprotected. |

**Reconcile carrying a statement about *H*.** When *H* was resolved elsewhere before the override
committed, the operator's answer for *H* is not discarded. The reconcile of *Y* records it in the
same transaction, keeping the operator's distinction:

- *not sent* adds nothing for *H*;
- *possibly sent* records **uncertainty** — *H* possibly transmitted for this contact, linked to the
  QSO — and **permanently protects** *H*;
- *both sent* records **confirmed transmission** of *H* for this contact, linked to the QSO, and
  **permanently protects** *H*.

Either protection holds even when *H* was returned and has since been reissued. A form now holding
*H* under a new allocation id keeps a valid hold, and the overlap is recorded as a **possible serial
collision**. Transmission certainty is recorded **per contact**: *both sent* confirms that *H* was
transmitted for this contact, but another form merely holding *H* does not prove a second
transmission. The collision remains possible until both contacts establish that they carried *H*. The
exchange-uncertainty flag carries over unchanged. (The entries recording *H* are a candidate
representation; the distinction between uncertainty and confirmed transmission, and the permanent
protection, are the requirement.)

**Alternatives weighed (for the contest ADR):**

- **T1 — no typing:** *H* read-only; a different number only through correct, reconcile or the
  unreserved path. Simplest, but a contact that carried *Y* is first saved with a wrong `STX` and then
  corrected — two steps and a transient wrong record.
- **T2 — free typing:** the operator overwrites *H* before saving. *Y* still needs accounting, and *H*
  — displayed, so possibly transmitted — is left held with no answer, or tempts an automatic return,
  which the reuse rule forbids.

### Still to decide

- Nothing in allocation; duplicate serials (2) and exchange uncertainty (8) at export are ruled in
  *Export-validation rulings*.

## Proposed acceptance criteria (first slice; proposal, not ruled)

Scoring is **out of the first slice** until questions 1–4 are settled.

1. **Declared contest:** the operator declares a contest with an id, a start and end (UTC), a mode and
   the sent exchange (here: RS and serial; `--` for the district as a DX entrant), as an occurrence
   with its own identity owned by one logbook (M1). Membership is an explicit association written
   with the QSO in contest mode (M5, D1); eligibility follows the QSO's own time and mode (M2–M4).
2. **Explicit sent serial:** every contest QSO stores the serial actually sent, in ADIF `STX`, written
   at logging time; it is never derived later from a count of rows. Allocation, holds and the
   reconciliation of a serial sent for a QSO that is never saved follow *Serial allocation* (option B,
   ruled 2026-10-28).
3. **Received exchange stored:** the received serial in `SRX` (0 when none was sent, per the rules) and
   the received district (`--` for a DX station, per the samples) in a stored field (ADIF `SRX_STRING` is the candidate), both
   visible in the Session panel and the Logbook.
4. **Duplicate detection, kept apart from the sponsor's policy:** inside a declared window the form and
   Band Activity show "already worked in this contest" by the sponsor-defined duplicate key, pending
   verification (question 1), and logging still proceeds — detection informs, it never refuses. Every
   logged contact is preserved locally. Whether a duplicate is logged, omitted or zero-scored is the
   sponsor's policy, applied at export only under an explicit rule (question 11).
5. **Cabrillo export:** the declared contest's QSOs export to a Cabrillo file in serial order, with
   frequency in kHz, the mode code (`PH` for SSB) and the sent and received exchanges in the sponsor's
   order, plus the header fields the sponsor requires (question 2). The submission export follows
   *Export-validation rulings*: it is blocked, naming the QSOs, when a member lacks a stored sent
   serial, a received serial without an explicit "no serial received" record, or a required received
   district (4b), and it never invents a value. Export rules never refuse saving a contact.
6. **Proof against the reference contest:** fixtures built from the UK/EI rules (exchange, serial
   continuity across bands, district codes, `--` for DX, the sample `QSO:` lines) produce a file that
   passes validation: an acknowledgement reporting no problems requiring correction (rule 8.4), not
   the acknowledgement alone. A rehearsal submission is made only through a test facility the sponsor
   supports or with the organiser's agreement.

Nearest confusable outcomes: a serial reconstructed from a row count after a deletion; a pre-contest
QSO greyed as a contest duplicate; a duplicate refused at entry; a Cabrillo file in time order instead
of serial order; a header the robot rejects; an acknowledgement taken as acceptance although it
reports problems; a non-contest QSO made a member because it fell inside the window (D1: membership
needs the explicit contest mode); a member silently dropped from the export after an edit (D2); two
annual editions sharing a serial sequence because they share a `CONTEST_ID` (M1); a QSO filed under
whichever contest the daemon thinks is active rather than the occurrence the request named.

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
