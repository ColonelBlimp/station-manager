---
number: 0090
title: Home adopts its SM Cloud identity in the background, on local and cloud evidence
status: Accepted
date: 2026-10-07
---

# 0090 — Home adopts its SM Cloud identity in the background, on local and cloud evidence

## Context

The SM Cloud server now carries archive identity: schema 8, explicit adoption, scoped paths, and the
`identity_protocol` flag (W-0021 5F.2, ADRs 0088 and 0089). That server is deployed. 5F.3 is the
first client step: the daemon must adopt Home's legacy cloud logbook and move Home's default SM
Cloud binding onto the identity paths.

Three facts of the daemon constrain how:
- Forwarder workers are built once, at start, from the binding rows, so a binding's wire is fixed
  for the run.
- 404 is a terminal upload failure today.
- The worker's `OutcomeUnreachable` means that no response came back at all.

v5 sent every Home logbook to one cloud logbook name, so a legacy cloud logbook may hold QSOs from
more than one local logbook. ADR 0082 part 7 requires that such a mapping is not adopted, and
reported as `legacy_logbook_ambiguous` for manual recovery.

The operator ruled on T1–T7 of the W-0021 5F.3 design (2026-10-07).

## Decision

1. **Background adoption, switch at the next start.** A lifecycle-tracked background task attempts
   adoption for Home's ENABLED default SM Cloud binding, only while Home is active. On success it
   records `remote_adopted_at`. That durable marker joins the restart fingerprint. The binding
   becomes an identity forwarder at the next start; until then, the legacy worker and reconciler
   continue.
2. **Bounded requests, hourly retries of the whole attempt.** Each HTTP request has a 10 s timeout.
   A temporary failure anywhere in the attempt (the check, the manifest, the adoption) retries
   hourly. A server without identity support waits for the next start. Authentication failures,
   malformed responses and adoption conflicts get their own diagnostics.
3. **Adopt only on local AND cloud evidence.** All of these must hold:
   - The default binding is the only Home SM Cloud binding with its normalized cloud name; disabled
     bindings count.
   - No other Home binding has queued uploads under that name.
   - Every QSO UUID the cloud holds under that name, tombstones included, is a local QSO of the
     default logbook.
   Cloud-only UUIDs block adoption. A failed or incomplete read never counts as evidence of safety.
4. **Status that means what it says.** "Adopted" appears only after confirmed remote success and a
   durable local record. Every other state — checking, unsupported, unreachable, refused token,
   unreadable answer, conflict, unsafe match, failed local record — is named, and each transition is
   logged once.
5. **An honest outcome for an identity endpoint the server lacks.** A 404 on an identity path
   keeps the upload pending indefinitely, with capped backoff and no name fallback, under its own
   diagnostic. It is not classed as unreachable, because the server answered.
6. **The adoption key is fixed.** Once a binding is adopted, its cloud logbook name is hidden and
   refused by the API.

## Alternatives considered

### Adopt synchronously before the workers start

The wire would switch in the same run. Rejected: every start would wait up to the timeout whenever
the server is unreachable — a station that boots offline would boot slowly — to save one restart
that the bindings banner already asks for.

### Rebuild the forwarder live after adoption

There would be no restart. Rejected: it is a lifecycle change under ADR 0070, with its own drain and
handover semantics, for a one-time transition.

### Decide ambiguity by counting bindings alone

This was ADR 0082's wording. Rejected as the ONLY test: a binding count cannot see a deleted
logbook's past uploads, a v5-era merge, or rows that exist only in the cloud. It is kept as a
necessary condition.

### Decide ambiguity by the cloud manifest alone

Rejected as the only test too. The manifest is a snapshot: another binding with the same name may
have uploads still queued, or may upload more after the read. The local conditions (one binding per
name, nothing queued elsewhere) cover the writers the snapshot cannot see.

### Treat cloud-only UUIDs as harmless

Rejected: they prove nothing either way. They may come from an earlier install, a lost local
database or another logbook. Blocking is the conservative reading; the status says manual recovery
is needed, not that another logbook owns them.

### Classify an identity-path 404 as unreachable, or as transient

`OutcomeUnreachable` would report an outage that is not happening: the server answered. Transient
would fail the upload after five attempts. A named outcome retries indefinitely without
misreporting the link.

### Adopt a disabled default binding too

Rejected: adoption changes what the cloud records and how the binding uploads. A disabled binding
uploads nothing, and enabling it is the operator's act. Disabled bindings still count for ambiguity.

## Consequences

- **A one-time restart.** The first adoption needs one restart before Home uploads by UUID; the
  banner says so.
- **Pending by design.** An ambiguous Home stays on the name-only wire indefinitely, and its card
  says manual recovery is required. No automatic recovery is offered.
- **Snapshot limit.** The manifest check proves the cloud's contents at the moment of reading.
  Combined with the local conditions and the server's single-writer rule (ADR 0052), that is what
  makes adoption safe. A writer outside this daemon using the same token and name — a second
  station — is not detected; adoption's archive-UUID conflict (ADR 0089) is the backstop for a
  second archive.
- **A new worker outcome.** The forwarding layer gains one outcome for "the endpoint this upload
  needs is not available", retried indefinitely with its own diagnostic.

## Triggers to revisit

- Stations are found stuck as ambiguous in practice: design an operator-driven recovery (choose the
  owning logbook, or split by membership).
- A second station sharing a token and cloud name appears: add a server-side writer identity.
- Live forwarder rebuilds arrive under ADR 0070: drop the restart from the switch.

## References

- ADR 0052 — SM Cloud backup identity and single writer.
- ADR 0070 — lifecycle.
- ADR 0082 part 7.
- ADRs 0088 and 0089.
- W-0021 dossier: the 5F.3 design and its rulings (2026-10-07).
