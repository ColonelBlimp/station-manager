# W-0030 — FT8: the DXpedition combined message

**Status:** Selected and built 2026-11-24 (operator: "go with recommendations, fix it now").
Reviewed by Codex in three rounds (codex-242, codex-243, codex-245), with no runtime finding
open; committed and pushed the same day on the operator's direction. It is not at the station
until it is deployed there. Closing the dossier is the operator's decision.
**Outcome:** a contact that a station working several callers at once has rogered is logged
here, as it is in that station's log.

`W-0030` is an immutable identity. Priority and ranked position live only in
[`docs/backlog.md`](../backlog.md); this item was built on the day it was reported and is not
ranked there (the backlog had no room for an entry, and adding one is the operator's edit).

## The fault (operator's report, 2026-11-24)

The operator worked VK9XY on 20 m and is in their log; nothing was logged here. Read from the
station's own decode log and daemon log for those minutes (read-only; no request to the daemon).

- **What the DX station sent.** It was working several callers at once and sent the combined message, which the decoder renders as `<caller A> RR73; <caller B> <DX> <report>`: a roger to A and a report to B in one transmission, with its own call hashed in angle brackets. At 05:05:00 UTC the report to us arrived that way (as caller B); at 05:06:00 the roger to us arrived that way (as caller A), decoded here at -12 dB.
- **What this daemon did.** `parseMessage` (`internal/ft8/sequence.go`) takes a directed message as `<to> <from> [token]` and requires the second word to be a callsign. In the combined message the second word is `RR73;`, so the whole line parses to nothing, and neither half is seen. The report was taken 30 seconds late, when the DX station repeated it in the plain form. The roger was never taken: the daemon sent its rogered report three times and at 05:07:15 ended the session, "no answer after max repeats", logging nothing. Fifteen seconds later the DX station sent the roger again in the plain form, to a session that no longer existed.
- **The fault is here, not on the air:** the roger was received and decoded; it was not understood.
- **Also seen, a separate question:** twice in those ten minutes the DX station answered after this daemon had already given up (three calls, then abandon). A station working a pile-up through a queue answers minutes later. Whether the repeat limit, or what a late answer from the station just given up on should do, wants a rule is the operator's to decide; it is not part of the fault above.
- **Not established:** whether the contact can be put into the log other than by hand. Its facts, for a manual entry: 2026-11-24, 05:05 to 05:06 UTC, FT8, 14.090 MHz dial (their log: 14090.50), report sent -12, report received -06, their grid OH29.

## Acceptance criteria (Claude, 2026-11-24; revised after Codex's review, codex-242; ruled)

1. From the station being worked, `<us> RR73; <other> <DX> <report>` is its roger to us, exactly as `<us> <DX> RR73` is.
2. From the station being worked, `<other> RR73; <us> <DX> <report>` is its report to us, exactly as `<us> <DX> <report>` is.
3. **The sender is the call in angle brackets, and only when the decoder resolved it.** `<...>` is nobody: no roger, no report, no spot. A resolved call is the decoder's attribution (a short hash looked up among calls it has heard), which this rule accepts; it is not described as proof of identity.
4. **The whole shape is checked before either half is taken:** two valid recipients, the literal `RR73;`, a resolved sender, a valid report. Anything else is not this message. Angle brackets are not stripped from any other kind of line. *A recipient is a plain call, or a hashed token as the decoder prints one: `<...>`, or a call in angle brackets (added while building, supported by Codex, codex-243). A hashed recipient is never taken to be us; a bracketed word that is neither is not a recipient.*
5. **One line, two meanings, one reader.** Only the half addressed to us may act, and only when the sender is the station being worked. The same selection answers every place that asks "what did they say to us" (the ladder's advance, the diagnostic line, the hold after a roger). The decode itself stays one line in the list, the decode log and the stream.
6. Our call in both halves is ambiguous and is taken as neither (our rule; the decoder does not forbid it). Our call in neither half, or another sender, changes nothing.
7. **It never makes a caller:** such a line adds nothing to the answerer list, the queue or an automatic choice.
8. A combined line whose resolved sender is our own call is dropped like any transmission of our own.
9. Field Day and the type-4 ladder are unchanged, and that is pinned: Field Day gains no report exchange through the shared parser; type 4 keeps its own parser and its own attribution rules.
10. After such a roger the ladder is as it is today: our 73 once, and the contact logs whether or not it reaches the air.
11. **Evidence:** tests first, synthetic calls, fake keyer and player only. Both halves; the wrong sender; neither recipient; both recipients; a malformed line; an unresolved sender; a combined roger repeated; the closing 73 refused, an abandon and a disarm after the roger is earned. Exactly one QSO row and one `ft8-logged`, at the frequency the session was pinned to. The side that calls CQ keeps its own completion timing.

## Ruling

**Operator, 2026-11-24: "go with recommendations, fix it now".** So: it is fixed before the next slice of the app's close; the ladder after such a roger is unchanged (our 73 once, and the contact logs whether or not it reaches the air); a combined line may be the source of a reception spot (the resolved sender, no grid, once per decode); and for the page this fix only pins that neither recipient is presented as the sender, with actions on such a line left unsupported for now. **Not ruled, and not part of the fix:** what should happen when the DX station answers after the repeat limit; no recommendation was given, and it stays the operator's question.

## Record

### The fix (2026-11-24; committed and pushed on the operator's direction, "commit and push"; not at the station)

- **What changed.** `internal/ft8/sequence.go`: `parseCombined` (the whole shape or nothing; a resolved sender only), `directedTo` (the one reader for "what did this line say to us": the half addressed to us, never the first by default) and `transmitterOf`. The answer ladder's and the calling side's `Advance`, their diagnostic lines and the hold after a roger read through `directedTo`. `SpotFrom` and the own-transmission filter take the resolved sender. `parseMessage` is deliberately not taught the line, so the answerer collectors and Field Day cannot read it; the type-4 ladder has its own parser and is untouched. `docs/ft8.md` carries the rule.
- **Tests, first and seen to fail** (`internal/ft8/combined_test.go`, synthetic calls, fake transmitter): the shape and sixteen lines that are not it; the reader; each half against its plain form on every rung of both ladders; this morning's order of slots replayed (one contact, logged once, at the session's frequency; a repeated roger logs nothing more); the contact earned though the 73 is refused; only the station being worked and only when resolved; never a caller; Field Day unchanged; the spot and the own-transmission filter; the three diagnostic lines; the hold after a bare roger. On the page, `ft8Message.test.ts` pins that such a line offers no sender, no addressee, no CQ and no caller.
- **Reversion proofs,** fourteen, each in a temporary copy, each failing the test named for the rule. One did not bite at first: in the hold after a roger, a line that goes unread looks like silence, which also releases the hold; the test now uses a partner who is still asking, where reading the line is what sends our RR73 again.
- **Gates:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the maintainability metrics (0 regressions), `go test -race ./internal/ft8` in full, the page's test file and its lint. The whole local gate (`task ci:local`) over the finished fix: exit 0; the one test file added after it (`cmd/smd/ft8qsolog_logged_test.go`) was run on its own with the race detector, `go vet` and lint.
- **It does nothing for the station until it is deployed there.** Nothing was transmitted and no rig command was sent for any of this.

### Codex's review of the fix (codex-243): two follow-ups, both fixed (2026-11-24; committed with the fix)

Codex found the reader, the two ladders, their diagnostic lines and the hold in agreement,
and supports a hashed OTHER recipient. Two things remained.

1. **A bracketed word passed for a recipient.** `G0XYZ RR73; <HELLO> <K1ABC> -08` was accepted,
   and its roger half could advance an exchange. Right: the recipient rule looked only for
   the brackets.
   - **Fixed:** a hashed recipient must be `<...>` or a call in angle brackets
     (`combinedRecipient`). Tests: four malformed bracketed recipients refused, in both
     positions; four hashed recipients accepted, in both positions; a hashed token that spells
     our call is still not us.
2. **The evidence stopped at the completion callback.** Criterion 11 asks for one QSO row and
   one `ft8-logged`; the tests showed one completion at the pinned frequency, through the
   sequencer's recorder. Right.
   - **What is added.**
     - The existing earned-contact fixtures gain the answer ladder with its report and its
       roger combined (`groupALadders`, `earnedcontact_test.go`). Every rule those tests hold
       for a plain roger now runs for a combined one: a stop of TX and the operator's Abandon
       after the roger is earned each log the contact once; a stop before the roger logs
       nothing; a deferred closing rung is still sent once and logged.
     - **Through the Service's own transmission and completion,** with the fake keyer and
       player: a combined roger puts our 73 on the air and hands the logging sink one
       contact, on the frequency and in the logbook of the exchange; a repeated roger hands it
       nothing more.
     - **What reaches the sink is the same value** as for a plain exchange, field for field,
       for each mix of plain and combined report and roger.
     - The type-4 ladder is pinned: its parser and its exchange read nothing from such a line.
   - **What is still not one test, and why.** No single test goes from a decoded line to a
     stored row. The sink that stores the row and publishes `ft8-logged` lives in `cmd/smd`;
     nothing exported feeds a decode to a Service, and a method added to `internal/ft8` only
     for a test is what the operator declined on 2026-11-22. So the chain is shown in two
     halves joined by a value: here, exactly one completed contact reaches the sink, identical
     to a plain exchange's; in `cmd/smd`, the sink stores one row and its upload row for a
     completed contact (the W-0028 tests) and publishes one frame naming that row (below).
     **Criterion 11's "one QSO row and one `ft8-logged`" is met in that sense and not in one
     test.** That is a change of the criterion's wording. The operator was told so, with
     "say so if you object", and then directed "commit and push" without objecting; Codex
     supports the two-halves form (codex-245). It stands unless he says otherwise.
   - **The frame was not observed by any test, and now is (codex-245).** The sink's tests read
     the row and its upload row and never the `ft8-logged` frame, so "publishes the frame"
     above rested on reading the code. `cmd/smd/ft8qsolog_logged_test.go` reads the frame as
     the app does, from the FT8 event stream opened before the contact is completed: one
     contact gives one row and one frame carrying that row's UUID, its mode and submode (FT8,
     and FT4 as MFSK/FT4), 20m and the exchange's dial; a second contact's frame is the very
     next one, so the first was published exactly once. The stream is opened only because FT8
     is off in that daemon (a subscriber starts capture only on an enabled service); the test
     refuses to run otherwise. Reversion proofs in temporary copies: the frame never
     published, and published twice, each failing it. No production code is changed.
- **Reversion proofs,** each in a temporary copy: a bracketed word accepted as a recipient;
  the answer ladder reading plain lines only (eleven earned-contact cases fail); the reader
  never returning the roger half (the Service test fails).
- **Gates:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the maintainability metrics (0
  regressions), `go test -race ./internal/ft8` in full.
