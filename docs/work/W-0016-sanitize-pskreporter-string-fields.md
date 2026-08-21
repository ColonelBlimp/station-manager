# W-0016 — Sanitize outbound PSK Reporter string fields

**Status:** Deferred hardening — trigger on adjacent PSK Reporter work or demonstrated consumer harm
**Selected:** Not selected
**Outcome:** Control bytes cannot leak through IPFIX string fields into downstream text exports,
while visible content and wire framing remain otherwise unchanged.

## Scope

Sanitize at `internal/pskreporter/ipfix.go`'s `appendVarStr` chokepoint so sender and receiver string
fields receive one policy. Cover C0 controls, tab/newline, and DEL. This is client hygiene, not an
attribution of any past collector failure.

## Decision required

The operator must choose **strip** or **replace with space** before implementation. Strip is the
smaller transformation; replacement preserves word boundaries.

## Operator-observable acceptance criteria

1. A Spot and Receiver containing tab, newline, `0x01`, and `0x7f` encode to a datagram whose
   corresponding field bytes contain none of those bytes.
2. Visible non-control content and field boundaries are unchanged.
3. The test asserts on the emitted datagram rather than only the helper's return value.

TDD and a reversion proof are required. No network, hardware, audio, credential, or RF action is
part of verification.

## References

- Expanded incident-era rationale: `d0391ed7:docs/backlog.md`.
