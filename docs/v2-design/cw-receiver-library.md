# CW receiver library proposal

**Status:** Draft for design review; implementation is not selected.
**Opened:** 2026-11-17.

Build a CW/Morse receiver that copies a selected signal, estimates its sending speed, and
automatically follows frequency drift through automatic frequency control (AFC). The longer-term
goal is to supply a CW operating subsystem in Station Manager that can continue an
operator-started run automatically.

The operator confirmed reception, speed estimation and AFC as the desired capabilities. A
standalone Go library, the receive-first order and the boundaries below are proposals for review.
Rig retuning and transmit-frequency matching remain separate design choices.

## Observable outcomes

The receiver should follow the selected signal through the agreed operating conditions, preserve
its Morse timing, and report copied text, estimated speed and tracked audio frequency. When it
cannot distinguish the target reliably, it should expose uncertainty or lost lock.

The nearest confusable outcome is convincing text from a neighbouring station after an unnoticed
tracking switch. Other failures to distinguish explicitly are noise decoded as a callsign, a
fade interpreted as a character boundary, and AFC changes that distort dot/dash timing.

Following a tone does not establish the sender's callsign. A track represents a signal hypothesis;
when signals overlap or continuity is ambiguous, the receiver cannot promise station identity.

## Library boundary

Following the separate modem approach described for [go-rtty](../work/W-0025-rtty.md), the proposed
library accepts streaming mono PCM with an explicit sample rate and sample-position or timing
information. The first executable consumer would replay WAV files for evaluation.

The receive interface should express:

- A selected initial frequency and permitted search window, in audio Hz.
- Tracked frequency, acquisition/lock state and signal-quality observations.
- Character-speed and spacing estimates, with their validity state.
- Decoded characters and prosigns, their sample positions or timestamps, and ambiguities.
- Explicit discontinuities, resets and target changes.

Exact sample formats, supported rates, buffer ownership, latency and API shapes remain open.
Processing should give equivalent results across supported input chunk sizes. A capture gap must
be explicit so the receiver can invalidate partial observations rather than join unrelated
elements across missing samples.

The host owns audio devices, CAT, rig retuning, transmit authorization, keying, presentation and
logging. The library's AFC adjusts digital receive tuning/filtering. Its frequency estimate can
later inform a separate SM frequency-control decision.

## Proposed receive processing

Use three cooperating components with separately observable state:

1. **Signal acquisition and tracking** finds the selected tone and estimates its frequency and
   quality. Acquisition and subsequent tracking may use different search widths or update rules.
2. **Timing estimation** infers element lengths and spacing from the detected signal. Character
   speed and spacing need separate estimates so wider spacing does not force a slower character
   model.
3. **Morse decoding** interprets the timing observations as characters and prosigns. Ambiguous
   sequences remain visible; callsigns, reports and serials must not be silently rewritten into
   plausible text.

International Morse uses a nominal dash length of three dots, with gaps of one dot between
elements, three between letters and seven between words. These supply an initial timing model
([ITU-R M.1677-1, Annex 1 section 2](https://www.itu.int/dms_pubrec/itu-r/rec/m/R-REC-M.1677-1-200910-I!!PDF-E.pdf)).
Farnsworth sending can combine a faster character rate with wider spacing
([Fldigi CW manual](https://www.w1hkj.org/FldigiHelp/cw_page.html)). Hand sending and changing
weighting require evaluation beyond ideal generated timing.

Digital baseband conversion, filtering and adaptive signal detection are established CW receiver
approaches ([Fldigi CW configuration](https://www.w1hkj.org/FldigiHelp/cw_configuration_page.html)).
A candidate implementation could combine spectral acquisition, local frequency estimation,
digital tuning, filtering and envelope detection before timing estimation. The particular
estimators and decoder are not selected; acceptance conditions should guide that choice.

## AFC behaviour

The AFC should follow the chosen signal through drift while retaining a clear distinction between
measured frequency, a held estimate and loss of tracking.

| State | Proposed behaviour |
|---|---|
| Acquire | Search within the configured window around the selected frequency; report acquisition uncertainty. |
| Track | Update digital tuning from usable signal evidence and report the current estimate. |
| Hold | Preserve the estimate through normal key-up gaps or insufficient evidence; expose declining certainty during a fade. |
| Reacquire | Search within the permitted bounds for continuity with the selected track. |
| Lost | Report lost lock when the agreed conditions are exceeded; require an explicit transition before committing text to a new track. |

Normal Morse gaps must not let noise steer AFC. A stronger adjacent signal must not silently
replace the target. Recovery after a fade may remain ambiguous; reporting that ambiguity is a
valid outcome. Search bounds, drift limits, state transitions and loss criteria require explicit
definitions before implementation. No numerical values are selected here.

AFC and timing estimation should cooperate without conflating their estimates. Tests must show
that following frequency drift does not itself change the recovered sending speed or element
timing. A quality score should not be presented as an error probability until that interpretation
has been calibrated against labelled recordings.

## First milestone and evaluation

The proposed first milestone is an offline receiver for one selected signal. Replay known PCM/WAV
input and display text, WPM, frequency and lock/uncertainty state over time. Automatic operation
and transmission follow only after receiver performance is understood.

Use generated samples for controlled parameter coverage and independently annotated receive
recordings for realistic evaluation. Keep recordings outside the tuning/training set for the
final evaluation. A transmitter/receiver round trip alone can conceal shared assumptions.

The central AFC fixture is a drifting target beside a stronger neighbour, with keying gaps and
fades. It must either retain the target through the declared conditions or report uncertainty/loss;
it must not silently adopt the neighbour. Include fixed tones, noise without CW, speed and spacing
changes, hand sending, amplitude changes, interference, stream gaps and explicit target changes.

| Measurement | What it establishes |
|---|---|
| Character errors | Substitutions, insertions and deletions against the annotated message. |
| Callsign and exchange errors | Accuracy of fields that a later operating sequencer would depend on. |
| False decodes | Text or actionable fields produced from noise and unrelated interference. |
| Frequency error and target switches | AFC accuracy and retention of the selected signal. |
| Speed error | Character-speed and spacing estimates against the fixture's defined reference. |
| Acquisition and recovery delay | Time needed to establish or recover a useful track. |
| Streaming behaviour | Results across chunk boundaries, discontinuities and declared latency limits. |

The operator must set the required speed range, signal conditions, drift range/rate, maximum
delays and acceptable errors. Define sample rates and the SNR measurement convention for
reproducibility. Machine-generated and hand-sent cases should have separately reported results.

## Future Station Manager integration

A CW operating scope would consume receiver observations and own exchange state, reply decisions,
operating frequency policy and QSO completion. A decode remains an observation; logging requires
a completed exchange under the operating rules chosen for that scope.

The integration must follow the current [scope contract](../../internal/api/app_admission.go):
the current app lifetime owns operation, its CW view supplies presence, and loss or replacement
stops operation through the guaranteed-stop path. An open view is a connection signal, not proof
that a person remains at the desk.

CW transmission needs a separate choice between a keyline, an external keyer and generated audio,
with reviewed cancellation and guaranteed-stop behaviour. That integration must preserve the
single-flight transmit ownership and existing logging invariants. The receiver library should
not create a second hardware-control path.

The existing [deferred product inventory](../work/W-0014-deferred-product-workstreams.md) owns
the phone/CW copilot idea; [the backlog](../backlog.md) alone owns priority. This proposal records
the receiving-library direction without selecting an SM implementation workstream.

## Questions for design review

- Is a standalone Go receiver for one selected audio signal the right initial boundary?
- What speed, drift, interference and sending-style conditions must the first version cover?
- How should ambiguities, tentative characters and prosigns be represented for both a human
  display and a future operating sequencer?
- What evidence permits reacquisition to continue a track, and when must it become a new track?
- Which labelled recordings and comparison receiver should establish the evaluation baseline?
- What receive contract will the future SM CW scope need before transmitter design begins?
