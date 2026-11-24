# W-0030 — FT8: the DXpedition combined message

**Status:** Selected and built 2026-11-24 (operator: "go with recommendations, fix it now").
Reviewed by Codex in three rounds (codex-242, codex-243, codex-245), with no runtime finding
open; committed and pushed the same day on the operator's direction, and **at the station
since 2026-11-24** (the operator's deploy: both packages at `2.0.0~alpha.5.315.gb8ad8dba`, the
daemon active, no error in its log since the start; checked locally and read-only). It has not
yet met a combined message on the air. **On FT4 it acts from `go-ft8` v0.10.0,** whose FT4
decoder keeps the calls it has heard (the last section below); with v0.9.0 an FT4 sender was
never resolved. Closing the dossier is the operator's decision.
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

### Clean-room reviews of the three commits (2026-11-24; fixes not committed)

`94e176d6` (the close-flow fixes): no findings. `9acd5ce2` (this fix): two. `b8ad8dba` (the
banner): two, recorded with the banner's note in the dogfood inbox.

1. **P2, kept as a known limit, with the reason in the code:** a combined message that
   addresses us by a HASHED call (`<OURCALL> RR73; …`) is not read as ours. The decoder prints
   a recipient in angle brackets only when the sender hashed it, which a sender does for a
   call that does not fit the message's 28-bit field: a compound call or one with a suffix. A
   plain call such as this station's is carried in full. So this does not touch the reported
   fault or this station as it operates today; it would touch an operator signing portable or
   with a compound call. Reading that half means accepting the decoder's resolution of a
   recipient hash as "that is us", and settling which ladder such a station is on (the type-4
   ladder does not read this line at all). **Not covered by W-0030, and the operator's to
   decide.**
2. **P3, fixed (test only):** the logged-frame test looked at an empty channel to show that
   the last contact was published once, which shows nothing about a frame still on its way. A
   third contact is now the sentinel: the frame after the second contact's must be the
   third's, and nothing is claimed about the third.

### At the station: the fix does not act on FT4 (found 2026-11-24, an hour after the deploy; not fixed)

The operator, on FT4 on 17 m after the deploy, showed the Band Activity panel saying only
"1 unidentifiable decode hidden (hide hashed calls)" ("Erk!"). Read from the station's decode
log for those minutes (read-only):

- **What was hidden.** A combined message from a station he was calling, with its sender
  unresolved: `<call> RR73; <call> <...> <report>`. Two arrived in three minutes, from a
  station whose plain messages (`<call> <DX> RR73`) were being decoded in the slots around
  them. So the decoder had heard that station's call and still printed `<...>`.
- **Why.** The decoder library's FT4 side keeps no table of heard calls. Its own source says
  so ("The initial FT4 decoder is stateless", `go-ft8` v0.9.0, `ft4/decode.go`), and so does
  this repository's (`internal/ft8/decode.go`, `slotDecoder`: "the v0.9.0 FT4 core carries no
  such state yet"). On FT8 the table is what turned `<...>` into `<VK9XY>` this morning. On
  FT4 a hashed call is never resolved.
- **What follows.** Criterion 3 reads a combined message only when its sender is resolved. On
  FT4 it never is. **So on FT4 this fix changes nothing:** a roger that comes only in the
  combined form is still not read, and the contact is logged only if the station also sends a
  plain roger before the repeat limit ends the session. The fault the operator reported was on
  FT8, and the fix holds there.
- **Claude's miss.** Before the deploy the operator said he would try the fix on FT4. Claude
  looked at what the DX station had sent on FT4 that morning and did not look at whether the
  decoder resolves a hashed sender on FT4 at all, though this repository's own comment says it
  does not. The limit should have been in the criteria.
- **Two ways to close it, neither chosen.**
  1. **Here:** when the sender is `<...>`, compare the message's own 10-bit sender hash with
     the hash of the station being worked. The decoded message carries its 77 payload bits
     (`DecodedMessage.Payload`), so the hash is there to read; the function that hashes a
     callsign is not exported by the library and would have to be written here, or exported
     there. It would act on FT4 and on FT8 alike. It is the same evidence the decoder uses to
     resolve a hash, applied to the one call that matters; it changes criterion 3, so it is
     the operator's ruling.
  2. **In the decoder library:** give its FT4 side the table of heard calls. That is a
     release of the library and a dependency upgrade here, which is a change of its own.
- **Meanwhile, for the operator:** such lines are shown, not hidden, with "hide hashed calls"
  off (Settings, FT8, Band Activity display); and a higher repeat cap gives a plain roger more
  time to arrive.

### A recommendation for the decoder library (2026-11-24; written at the operator's request, for him to pass on)

The operator: "Give a recommendation for the go-ft8 library and I'll pass it on to them for
consideration...". It is a page of its own, with the same text ready to copy as Markdown:
<https://claude.ai/artifact/HiadEqvoq9WcScuGj6vU4E> (private to the operator until he shares
it). It names no station and uses placeholder callsigns. Against `go-ft8` v0.9.0 it recommends,
in this order:

1. **The FT4 decoder keeps the callsign hash history,** with the FT8 decoder's semantics (one
   table per decoder instance, empty when new). This is the fix: the FT4 side renders every
   payload through `ft8.ParsePayload77`, which has no history.
2. **The caller can seed the history** with calls it already knows (its own, the station being
   worked), since the table is empty at the start of a session and after a band change.
3. **The hash function and the fields of the combined message are exported,** so a caller can
   check an unresolved sender against the one call it cares about, with no history at all.
4. **The documentation says what each decoder remembers.**

What this means here: with item 1 or item 3 in a released library, criterion 3 could be met on
FT4 after a dependency upgrade, which is a change of its own. Nothing in this repository is
changed by the recommendation, and whether to close the gap here first (the first of the two
ways above) is still the operator's to decide.

**Ruled (operator, 2026-11-24): "Let's wait for the ft8 lib as they are working on it...".** The
gap on FT4 is closed in the decoder library, not here: nothing is built in this repository for
it, and the hash check of the first way is not written. Until a library release with FT4 hash
history is taken here (a dependency upgrade, a change of its own), a combined message on FT4
is not read, and a contact on FT4 is logged only when the station also sends a plain roger in
time. When that upgrade is made, criterion 3 is to be shown on FT4 by a test of its own.

**The gap seen again the same morning, and the two contacts entered by hand.** At 07:03:45 and
07:05:00 UTC on 17 m FT4 the station being worked rogered the operator twice in the combined
form, sender `<...>`, and sent no plain roger. The daemon re-sent its report to the repeat cap
of 6 and abandoned the session as `no_answer`; nothing was logged. The operator then asked for
this contact and the 20 m FT8 contact that began this work item to be added by hand. Both were
submitted on 2026-11-24 through `POST /v1/qso`, one record each, with times taken from the
station's logs (the session's start, and the end of the slot that carried the first roger), and
both were stored and forwarded. Nothing of FT8, the rig or TX was touched.

### The FT4 gap closed by the decoder library (2026-11-24; `go-ft8` v0.10.0 taken, a change of its own)

The operator, the same day: "new FT8 lib has landed 0.10". The release does what the
recommendation asked (its `CHANGELOG.md` and `docs/CALLSIGN_HASH_HISTORY.md`): an `ft4.Decoder`
keeps callsign hash history across the slots of its stream, with FT8's rules; both decoders can
be seeded (`RememberCallsigns`); the hash and the fields of the combined message are exported
(`ft8.CallsignHash`, `Payload77.DXpedition()`).

**What the upgrade is here.** The version line in `go.mod`, with `go.sum`. No production code:
`slotDecoder` already keeps one decoder per capture session in one goroutine, a skipped slot
decodes silence (which the library documents as keeping the history) and a dial move builds a
fresh decoder. Three comments in `internal/ft8/decode.go` that called the FT4 core stateless
are brought up to date.

**Shown by tests of its own** (`internal/ft8/decoder_profile_test.go`), with FT8's existing
fixture, a call heard in full and then a message naming it by its 12-bit hash:
- on FT4 the hash resolves in the slot after the call was heard, where a fresh decoder (the
  control, asserted first) prints `<...>`;
- a skipped slot, our own transmission, keeps the history;
- a dial move clears it, so a call heard on one band never resolves a hash on another.
The first two fail on v0.9.0 (`<...> PJ4/K1ABC RR73`) and pass on v0.10.0; the third fails
when `reset()` no longer replaces the decoder.

**What that does and does not show for criterion 3.** The missing fact was a resolved hashed
call on FT4, and that is shown through this repository's own decoder wrapper. The readers of
the combined message work on its text and do not know the mode. A combined message decoded
from audio on FT4 is not made here: neither encoder in this repository can make one. The
library's own tests cover it (`ft4/callsign_history_test.go`).

**What the operator can expect on FT4.** A combined message is read once the station's call
has been heard in full on that stream since the session began or the dial last moved, which
is so for a station answered from its CQ or seen working others. In the first slots of a
session, before any plain message from it has been decoded, its hashed sender is still
`<...>`.

**Not done, and for the operator to decide.** Seeding the history with our own call and the
station being worked would cover those first slots; reading a combined message that addresses
us by a hashed call is now checkable with `CallsignHash`. Each changes attribution and wants
its criteria first.

**The clean-room review of the upgrade (`20d00a5d`), one finding, accepted with its reason in
the code.** P2: on FT4 a combined message's sender is now printed as the most recently heard
call with that 10-bit hash, so a message from another station whose call collides with the
station being worked (one call in 1,024), and which rogers us, would be taken as our
partner's and the contact logged. That is criterion 3's accepted cost ("a resolved call is
the decoder's attribution … which this rule accepts"), true on FT8 since the fix and now on
FT4. Nothing in the message tells the two stations apart, and a guess such as the audio
offset is not attribution. `internal/ft8/sequence.go` now states the case at the field it
concerns. **It is the operator's to change:** the stricter rule would be to read no combined
message at all, which is where this work item started.
