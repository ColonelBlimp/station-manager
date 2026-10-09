---
number: 0091
title: SM Cloud adoption reserves its cloud name durably before sending
status: Accepted
date: 2026-10-08
---

# 0091 — SM Cloud adoption reserves its cloud name durably before sending

## Context

ADR 0090 has Home adopt its legacy SM Cloud logbook in the background, after an evidence check (T3)
that no other binding uses the default binding's cloud name. That check judges one moment. The 5F.2
server still files a name-only push into the adopted legacy logbook. So, after the check, a save
that gives another binding the same name would let a second local logbook write into the adopted
one, and checking `remote_adopted_at` alone would not stop it.

A reply can be lost, and the daemon can stop between the request and the local record. The server
may therefore have adopted the mapping while the daemon has no record that it did. The server's
adopt is idempotent for the same mapping, commits nothing on a 409, and never clears a mapping once
set.

On the local side:
- Binding rows are never hard-deleted. The bindings PUT upserts, and deleting a logbook is a soft
  delete, so `ON DELETE CASCADE` never fires.
- The default logbook can be moved, and the old one then deleted.
- The station account (URL and token) is saved through the config service's own mutex, not
  `bindingsMu`.

The operator ruled on the acceptance criterion and on R1–R4 (W-0021 dossier, 2026-10-08):
once remote adoption may have occurred, no other binding can acquire that name through a save.

## Decision

1. **An archive-local reservation, distinct from confirmation.** A new column,
   `logbook_destination.adoption_reserved_at`, marks a binding whose cloud name is reserved.
   - The binding fixes the owner. Its credentials fix the normalized name (`smcloud.CloudName`),
     and the T6 lock keeps them fixed from the reservation onward.
   - The reservation says only that adoption may have been requested from this archive. It is not
     proof that any server accepted adoption.
   - It survives station account changes.
   - On its own, it neither selects the identity wire nor claims "adopted, restart required"; only
     `remote_adopted_at` does either.
2. **Reserved before sending, never released automatically.**
   - The reservation is written and committed before the adoption request is sent. A failed or
     uncertain write sends nothing.
   - The commit is durable across a power loss. The archive's connections run WAL with
     `synchronous=NORMAL`, under which SQLite allows a committed transaction to roll back after a
     power failure or an OS crash (sqlite.org, `PRAGMA synchronous`). The reservation therefore
     commits on one pinned connection set to `synchronous=FULL` before its transaction begins, with
     the setting read back inside the transaction. Any failure there reserves nothing, and nothing
     is sent.
   - Nothing releases it: not a conflict, a cancellation, a failed marker write or an uncertain
     outcome. Manual recovery owns any release.
   - A downgrade below schema 16 (migration 0016 adds the column) is refused while any binding holds
     a reservation or a recorded adoption. The check runs before any down step: a refusal raised
     inside a step would leave the schema marked dirty.
3. **Every save is checked against every protected name.**
   - Protected names come from every SM Cloud binding that is reserved or adopted, whether enabled
     or disabled and whether its logbook is live or deleted. They are read on every save, so a
     stored reservation is enforced from the first save after start.
   - The bindings PUT checks the whole merged candidate: no other SM Cloud binding, disabled ones
     included, may normalize to a protected name. A cleared name normalizes to `main`.
   - A name that cannot be read is refused.
   - A refusal is 409 `binding_name_reserved`, and nothing is written.
4. **The judgement and the reservation are one step.** The cloud manifest is read outside any
   lock, because it is a network read.
   - The attempt pins its identity before that read: the archive UUID, the binding and logbook,
     the normalized name, and the account.
   - Under `bindingsMu`, the attempt checks that the pinned identity still matches, reads the local
     evidence, judges it and writes the reservation.
   - A mismatch, such as a rename or a moved default, abandons the attempt. A moved default never
     redirects an attempt, or its status, to another row.
5. **Retries judge again.** A retry with a reservation already held runs the whole check again. If
   it now refuses, nothing is sent and the reservation stays. Because an earlier request may have
   succeeded, the status says "adoption confirmation blocked", not "not adopted".
6. **A completion from another account is discarded.**
   - The account comparison and the marker write are coordinated with account saves, so a save
     cannot land between them.
   - A changed account needs a fresh version check, manifest read and adoption under the new
     account.
   - The reservation stays.

## Which account a confirmation belongs to (ruled 2026-10-08, for 4b)

`remote_adopted_at` does not say which account confirmed it. A replacement account must not inherit
account A's confirmation: if it did, the next start would build the identity forwarder, which would
create a managed archive under B and bypass adopting B's legacy data.
- **A local fingerprint of the confirming account.** HMAC-SHA256 keyed by the token (RFC 2104),
  over an unambiguous, versioned encoding of the archive UUID and the normalized URL. The archive
  UUID is public, so it is not used as the key.
  - It is stored with `remote_adopted_at`, in the same write. That write is coordinated with account
    saves, so a save cannot land between the account comparison and the record.
  - A missing or mismatched fingerprint needs a fresh confirmation under the current account.
  - The value is a verifier derived from the token, and could be used to check offline guesses of
    it. That storage cost is accepted.
- **No name-only fallback after adoption.** A binding adopted under one account whose account then
  changes keeps its uploads queued until a fresh confirmation, preserving ADR 0088's rule that a
  binding on the identity wire never falls back to the name. A rotated token for the same tenant
  confirms through an adoption that the server accepts again without change.
- **Tests (4b):** an account replacement, a token rotation, an offline `config.json` edit, and an
  account save racing with confirmation.

Adjustments ruled on the 4b design (2026-10-08):
- **Encoding.** The key is the token. The message is the prefix `sm-adoption-account/v1`, then the
  archive UUID (lower case) and the URL (trimmed, trailing `/` removed), each length-prefixed.
  The fingerprint is stored as hex in `logbook_destination.remote_adopted_account` (migration
  0017).
- **The confirmation is durable too.** It authorizes a permanent wire transition, so it commits
  like the reservation: a pinned connection at `synchronous=FULL`, read back inside the
  transaction.
- **Downgrade.** A downgrade below 17 is refused while any binding holds a confirmation, disabled
  bindings and deleted logbooks included. A schema-16 build would upload by name without the
  account-confirmation hold, and a later upgrade cannot undo those uploads.
- **Lock order.** `bindingsMu`, then the config read lock. The callback uses only the config it is
  given, stays read-only, and makes no nested config call and no network request. The pinned
  archive, default, binding, name and account are rechecked before the reservation and before the
  confirmation.
- **The account race has two orderings.** If the save wins, the old completion is discarded. If
  the confirmation wins, the save waits, the confirmation commits for the old account, and the new
  account then leaves that fingerprint unmatched.
- **Status belongs to its subject.** A moved default or a changed account never inherits an earlier
  attempt's status. A remote success followed by a failed local write says so and retries; a lost
  response is an uncertain outcome, never "not adopted". An adoption whose fingerprint does not
  match says "Adoption needs confirmation for the current station account."
- **Retrying after a refusal.** After 401, a malformed answer or a 409, the hourly check retries
  only once a relevant saved input changes (the account fingerprint, or the binding, the default,
  the name or the eligibility). The check keeps running after a success and while nothing is
  eligible.

Adjustments ruled for the adopter (2026-10-09, 4b2):
- **(A1) The first check runs at once** when the lifecycle dependencies are ready, then hourly.
  Elapsed time does not guarantee a drained queue. Safety comes from the judgement and the
  durable reservation, and the judgement already counts waiting, in-flight and failed uploads.
- **(A2) An old server is rearmed like a refusal.** A version answer without `identity_protocol`
  waits for a relevant input change, replacing ADR 0090 T2's restart-only rule. A server upgraded
  at the same URL with unchanged inputs still needs a daemon restart.
- **(A3) A status the protocol does not define is a refusal.** An unexpected 400, 403 or 404 is
  terminal for unchanged inputs and says "Adoption could not be confirmed: the server returned
  HTTP {status}." It is not reported as an unreadable answer. Transport failures and 5xx stay
  retryable hourly.
- **(A4) Texts and kept uncertainty.** No answer to the adoption request: "Adoption outcome
  uncertain: no confirmation was received from the server. Retrying." The client cannot know that
  a reply was lost. Judged unsafe while reserved: "Adoption confirmation blocked: the legacy cloud
  logbook can no longer be matched safely to Home's default logbook; manual recovery is required."
  Local evidence unreadable: "Not yet: the local archive could not be read (retrying)." While an
  earlier outcome for the same subject is uncertain, a later "Not yet" keeps the uncertainty.
- **(A5) A later refusal does not resolve an uncertain outcome.** A 401 proves only that the
  latest request failed authentication. For the same subject the status keeps the uncertainty and
  names the current blocker: "Adoption outcome remains uncertain. Confirmation is blocked: the
  server rejected authentication (HTTP 401)." A cloud success whose local record failed is kept
  the same way. Only evidence clears it: a recorded confirmation, or a 409 to the same adoption
  request carrying one of the three adoption conflict codes, since the store never clears a
  mapping and a replay of one in effect answers 200. Any other 409 resolves nothing. It stops
  showing when its subject no longer matches. An unreadable answer to the adoption request is
  uncertain too (the server may have committed), and is still suppressed as terminal.

Options weighed:
- **Server tenant identity.** A tenant UUID served on an authenticated endpoint would be exact and
  survive token rotation. Not chosen: it needs a server change and a redeploy of the 5F.2 server.
- **HMAC keyed by the archive UUID.** Rejected: the UUID is public, so it gives no secrecy as a key.
- **Clear the confirmation on an account save.** Rejected: an offline edit of `config.json`
  bypasses it.

## Alternatives considered

### Protect only adopted bindings (`remote_adopted_at`)

Rejected (operator ruling): a lost response or a failed marker write leaves a remote adoption with
no local record, and a save in that window could give the name to another binding.

### A separate reservation table keyed by the cloud name

It would survive any row deletion, but binding rows are never hard-deleted, and the name is already
fixed by the locked credentials. A second store of the name could disagree with the credentials.
Rejected as duplication.

### Release the reservation on a 409

A 409 does prove that our mapping is not in effect, since mappings are never cleared, so releasing
would be sound. Rejected: it adds a release path for a rare case that already needs manual recovery.

### Hold `bindingsMu` across the manifest read

The judgement would then cover the manifest too, but a slow server would block every bindings save
for up to the request timeout. Pinning the attempt's identity and re-checking it under the lock
gives the same guarantee for local writers. The manifest is a snapshot either way (ADR 0090).

### Reuse the evidence verdict without judging again on retry

It would send sooner after a restart. Rejected: the evidence can change while the outcome is
unresolved, and the name-only wire keeps working while the retry waits.

## Consequences

- **A permanent local fact.** A reserved name stays reserved in that archive even if adoption never
  succeeds. Freeing it is manual recovery.
- **A one-way migration once used.** A downgrade below 16 is refused while a reservation or an
  adoption exists, and below 17 while a confirmation exists.
- **A new refusal on saves.** `binding_name_reserved` (409) joins `binding_field_locked`. The SPA
  shows the API's message.
- **Two statuses for "not confirmed".** "Not adopted" means no request could have succeeded.
  "Adoption confirmation blocked" means one may have.
- **Lock ordering.** The marker write coordinates with the config mutex: `bindingsMu` first, then
  the config read lock. A config save waits for that local write.

## Triggers to revisit

- An operator needs a reserved name released in practice: design manual recovery (an explicit
  release after checking the server).
- The server gains a tenant identity: move the confirmation's account association onto it.
- Live forwarder rebuilds arrive (ADR 0070): the boundary between reservation and confirmation
  moves into the same run.

## References

- ADR 0090 — Home adopts its SM Cloud identity in the background (T1–T7).
- ADRs 0088 and 0089 — the identity wire and the adoption endpoint.
- W-0021 dossier: the 5F.3 commit 4 ruling, design and R1–R4 (2026-10-08).
- `internal/config/config.go` `Service.Update` (the config mutex).
