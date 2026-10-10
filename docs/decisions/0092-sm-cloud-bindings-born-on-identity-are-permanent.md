---
number: 0092
title: SM Cloud bindings without legacy history are born on identity, permanently
status: Accepted
date: 2026-10-10
---

# 0092 — SM Cloud bindings without legacy history are born on identity, permanently

## Context

Until W-0021 5F.4, SM Cloud may be newly turned on only for Home's default logbook (5F.0), and a
binding reaches the identity wire only by adoption (ADR 0090, ADR 0091): the daemon links an existing
legacy cloud logbook, found by its name, to the archive's and logbook's UUIDs. 5F.4 lifts that gate
for Home's other logbooks and for managed archives. Those bindings have no legacy cloud logbook to
link. The 5F.2 server creates an archive and a logbook on their first identity upload (ADR 0089 S1),
so they need no adoption call. Three questions follow: how the daemon tells such a binding from a
legacy one, what a station account change does to it, and whether it may be turned on while the
server cannot be reached.

The operator ruled F1–F8 on 2026-10-10 (W-0021 dossier, "5F.4 rulings").

## Decision

1. **Born on identity, permanently.** A binding established without legacy history uploads by UUID
   from its first upload and is never adopted. Its legacy Cloud logbook name is neither shown nor
   sent, and it takes no part in the legacy-name collision checks. A new column,
   `logbook_destination.identity_since`, records the decision, written in the same transaction as
   the qualifying enable with the durability that ADR 0091 gives the reservation. Nothing clears it:
   not a disable, an account change or a restart. A downgrade below the schema that adds it is
   refused while any binding holds it, disabled bindings and deleted logbooks included. A binding
   with neither this stamp nor an adoption keeps the name wire.
2. **Only a first transition with clean history stamps it.** Adopted or reserved bindings keep the
   adoption rules. A legacy or unmarked non-default Home binding is converted only when no SM Cloud
   upload record exists for any of its QSOs, deleted QSOs included, whatever the record's status or
   worker name. Unreadable or uncertain evidence counts as history. A binding already born on
   identity is exempt: its own history never blocks re-enabling it.
3. **An account change follows the account.** For a binding born on identity, rotating the token
   within one tenant continues the same backup. Credentials for another tenant make the next
   uploads and reconciliation fill that tenant's backup, keyed by the same UUIDs; the old backup
   stays where it is. This takes effect when the new account does, at the restart. Adopted bindings
   keep their confirmation hold (ADR 0090 T1).
4. **Offline enable is allowed.** At every start with an SM Cloud station account, the daemon checks
   the server's `identity_protocol`, with a bounded timeout, whatever the active archive. The result
   belongs to the server that was checked. An explicit "unsupported" refuses a new identity enable.
   An unreachable server permits it, shown as unverified: uploads stay on the scoped paths and retry
   with no name fallback. A pending check, a malformed answer, or an authentication or configuration
   error is never treated as "unreachable".

## Alternatives considered

### Adopt every new binding under its cloud name

One mechanism for every binding: each new enable calls `POST /v1/archives/adopt` with its Cloud
logbook name as `legacy_name`. Rejected: it invents a legacy name the cloud never had, keeps a
name-keyed mapping alive for bindings that never needed one, and brings the account-change hold
with it.

### Derive the wire from the archive's kind and the logbook's role

Managed archives and non-default Home logbooks would be on identity because of where they are, with
no column. Rejected: Home keeps pre-5F.0 enabled bindings on other logbooks that upload by name, and
a derived rule would move them onto identity silently, without the history check.

### Hold a born-on-identity binding after an account change

Record the account with `identity_since` and hold on a mismatch, as adopted bindings are held.
Rejected: the hold protects a name-to-tenant mapping, and these bindings have none. A token rotation
would stop a working backup for no protective gain.

### Refuse the enable until a start has reached the server

Rejected for this station: it is offline-first, and the scoped paths already keep rows queued and
retrying (Q2), so an unverified server costs a delay, not data.

## Consequences

- One more permanent column on the binding row, with its own downgrade refusal and its own place in
  restart detection.
- A token for another tenant can receive the whole local logbook through reconciliation, not only
  later QSOs. The manual must say so.
- A binding turned on while offline may later meet an old server; its rows then wait, queued, until a
  supporting server answers. A 404 alone does not say why.
- A Home logbook with any SM Cloud upload history needs manual recovery before it can move to
  identity, even where the history is harmless.

## Triggers to revisit

- If a station needs a history-bearing Home logbook converted, design the manual recovery path and
  the evidence that makes conversion safe.
- If tenants come to share archives (device tokens, ADR 0052's roadmap), revisit decision 3.
- If an unverified enable is found sitting behind an old server unnoticed, revisit decision 4 and
  how the row reports unverified support.

## References

- `docs/work/W-0021-qso-archives.md`: "5F.4 design" and "5F.4 rulings" (F1–F8).
- ADR 0088 (scoped identity paths), ADR 0089 (create on first push), ADR 0090 (Home adoption),
  ADR 0091 (durable reservation).
