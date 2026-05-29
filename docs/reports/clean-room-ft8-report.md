# Clean-Room FT8 Go Port Report

## Purpose

This report transfers behavioral findings from a parity investigation into a separate FT8 Go project without transferring implementation code, pseudocode, copied tables, or source-derived structure. It is intended for an MIT-licensed clean-room implementation based on public FT8 specifications and black-box oracle tests.

The target behavior is strict parity with default `jt9 -8` decoding for the supplied WAV fixtures.

## Clean-Room Boundary

The implementation team should not inspect:

- WSJT-X source code.
- The existing Go implementation in this research project.
- Instrumentation files created during this investigation.
- Any generated source, tables, or line-by-line notes derived from WSJT-X.

The implementation team may use:

- Public FT8 protocol/specification material, including QEX/public technical descriptions.
- Public mathematical references for DSP, LDPC belief propagation, CRC, Gray coding, GFSK, FFTs, and message packing.
- The supplied WAV files as black-box inputs.
- Captured `jt9 -8` output as oracle behavior.
- This report.

## License Posture

This report is not legal advice. The practical rule is: transfer facts, behavior, measurements, and public-spec requirements; do not transfer implementation expression.

Copyright generally does not protect facts, ideas, systems, methods, procedures, or processes, but it can protect source-code expression. WSJT-X source is GPLv3. A clean MIT implementation should avoid copying GPL code, tables, comments, structure, naming, or translation artifacts unless that material is independently available from a compatible public source.

Relevant public references:

- U.S. Copyright Office: copyright protects expression, not ideas, methods, systems, or processes.
- Open Source Initiative MIT License text.
- WSJT-X public licensing notice: WSJT-X source is GPLv3.

## Oracle Test Set

Use the six WAV files in `testdata/` as the initial acceptance corpus:

- `20m_slot1.wav`
- `20m_slot2.wav`
- `20m_slot3.wav`
- `live_slot1.wav`
- `live_slot2.wav`
- `live_slot3.wav`

The oracle is the installed command-line decoder in default FT8 mode:

- `jt9 -8`

For this corpus, default oracle output contains:

- Total oracle decodes: 144
- Expected clean-room strict target: 144 decoded messages
- Required exact message matches: 144
- Allowed missing messages: 0
- Allowed extra messages: 0

Normalize comparison output by treating trailing annotations such as `a1` as decoder metadata, not message text.

## Behavioral Findings

Strict default parity is not the same goal as maximum decode count. Some additional messages found by deeper settings are plausible real decodes, but they are not part of default `jt9 -8` parity.

The implementation should distinguish at least two modes:

- Strict default parity mode: match `jt9 -8` default output.
- Deep/experimental mode: may intentionally recover additional messages, but must not be used for strict parity scoring.

Observed strict-parity requirements:

- Use the same effective default frequency range as the oracle fixture run.
- Maintain state for hashed callsign resolution across sequential WAV files in a single run.
- Resolve hashed callsigns when earlier decoded messages provide the needed call history.
- Treat unresolved hash placeholders and resolved hashed calls as different message text for strict comparison.
- Do not admit deeper-search decodes into strict default output.
- Avoid broad candidate offset probing in strict mode; it produced valid-looking but non-default extra decodes.
- Use a sync gate calibrated to this oracle set. A threshold near `1.8` matched the six-file oracle corpus during investigation.
- Signal cancellation/subtraction is behaviorally important. Weak later decodes depended on prior decoded signals being removed with the correct amplitude normalization.
- Cancellation that is too strong or too weak changes later decode results.

## Regression Cases

The following cases are important acceptance checks:

- `20m_slot1.wav`: weak messages recovered only after correct signal cancellation must still appear.
- `20m_slot3.wav`: `<DG6JW/T> SV0TPN +01` should resolve when decoded after the prior WAV in sequence; a single-file run may produce `<...> SV0TPN +01`.
- `20m_slot2.wav`: `JY5IB EA3DYI JN11` is a deeper-supported decode, but it is not present in strict default oracle output and should not appear in strict parity mode.

## Acceptance Procedure

Run the clean-room decoder over the six WAV files in the listed order. Capture decoded message text, frequency, and time if available, but score strict parity on normalized message text per file.

Separately capture `jt9 -8` output over the same files in the same order. Compare per-file message sets after normalizing metadata annotations.

Pass criteria:

- 144 total clean-room messages.
- 144 exact normalized oracle matches.
- No missing oracle messages.
- No clean-room-only extras.

## Recommended Project Hygiene

Keep the clean-room repository audit-friendly:

- Add a document listing all public references used by implementers.
- Keep oracle WAVs and oracle text outputs in test fixtures.
- Keep this report separate from implementation files.
- Do not include WSJT-X source excerpts.
- Do not include generated tables copied from WSJT-X.
- Require contributors to state whether they have inspected WSJT-X source or this research implementation.
- Review all new tables/constants for independent public-spec provenance.

## Non-Transferable Material

Do not transfer:

- Source code from WSJT-X.
- Source code from this research Go implementation.
- Fortran instrumentation source.
- Local probe binaries or generated intermediate data.
- Function names, variable names, or control-flow structure copied from inspected source.
- Any table whose only provenance is inspected GPL source.

## Summary

A clean-room FT8 Go implementation targeting default `jt9 -8` parity is feasible. The minimum transfer needed is the public FT8 spec, the six WAV fixtures, oracle outputs, and the behavioral constraints in this report. The clean-room team should implement independently and use the oracle comparison as the authority.
