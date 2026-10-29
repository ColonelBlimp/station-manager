---
number: 0097
title: Change the default logbook at an attended restart
status: Accepted
date: 2026-10-29
---

# 0097 — Change the default logbook at an attended restart

**Design accepted (operator, 2026-10-29), after design review with no blocking findings. No
implementation is authorised.** The acceptance criteria and the review that produced this record
live in [W-0024](../work/W-0024-settings-logbooks.md). Names of fields, headers, codes and endpoints
below are placeholders; the outcomes are what is decided.

## Context

W-0024 adds **Set as default** to Settings → Logbooks. The review of 2026-10-29 chose **restart
application** as one confirmed "Set as default and restart" operation, and asked for this record
before coding. Facts verified at `a997c0f3`:

- **Two pointers.** The archive file's `archive_metadata.default_logbook_id` is authoritative.
  Config's `default_logbook_id` is its projection: at every start the file's value wins when it
  names a row (`cmd/smd/archive_adopt.go:34-35,86-88`). After setup nothing changes either pointer:
  `PUT /v1/config` sets the field only while completing setup (`internal/api/handler_config.go:774-779`).
- **Readers in the running process:**
  - FT8 pins the daemon's **current** default when a session starts, through
    `currentStationIdentity` (`internal/api/handler_ft8_qso.go:45-47`). It does not use the tab's
    logbook.
  - A completed FT8 exchange logs to that pinned logbook (`cmd/smd/lifecycle_adapters.go:732-736`).
  - Phone / CW submits name a logbook taken from the tab's boot-time station context
    (`frontend/app/src/main.ts:448-456,563`).
  - The SM Cloud adopter targets Home's default only
    (`internal/forwarding/smcloud/adoption/adopter.go:177-197`).
  - The active archive's summary marks the default from the **file**
    (`internal/archive/summaries.go:184`).
  - Delete refuses config's default (`internal/api/handler_logbook.go:179`).
- **Config publication is immediate** (`internal/config/config.go:2463` onward).
- **The file pointer does not protect its logbook.** Logbook deletion is a soft delete
  (`UPDATE logbook SET deleted_at`, `internal/database/sqlite/api_context.go:1798` onward). The
  pointer's foreign key (`ON DELETE SET NULL`, `migrations/log/0012_archive_identity.up.sql:23`)
  fires only on a hard delete, so a soft-deleted logbook can stay named by the pointer.
- **Archive activation is the precedent** (`internal/archive/activate.go:136-216`). In order, it:
  1. runs single-flight;
  2. checks preflight;
  3. takes the FT8 admission seal, which refuses while a QSO is active, TX is in flight or TX is
     armed (`internal/ft8/servicetx.go:1637-1656`);
  4. takes the bridge seal (tune and keying);
  5. persists `pending_qso_archive_id`;
  6. requests the restart.

  If persisting fails, or if the restart request fails, it releases the seals. If the pending
  selector then cannot be cleared, it is left in place and the next start applies it. It does
  **not** seal Phone / CW submissions.
- **Tabs prove their archive** with `X-SM-Archive-Id`, which the daemon checks against the
  generation's served archive before any side effect (`internal/api/archive_guard.go`). No
  equivalent exists for the default logbook.
- **Held SM Cloud bindings.** An adopted SM Cloud binding not confirmed for the current station
  account is held at start (`cmd/smd/identity_wire.go:120-124`). While a binding is held, only the
  adopter can confirm it, and the adopter looks only at the default.

## Decision

**A new default takes effect only at the start of a replacement process. Within one process
generation, the effective default never changes.** The change is a single confirmed operation that
records the target, seals live admission and requests the restart, modelled on archive activation.

### 1. State and ownership

- **Pending intent lives in config.** It is stored as one selector naming the archive UUID, the
  target logbook's UUID and a **request id** minted when the change is accepted. It sits alongside
  `pending_qso_archive_id` (placeholder: `pending_default_logbook`). It is not a default: nothing
  reads it except the start, the status read, Cancel and the mutual refusals of §2. Its removal
  leaves a tombstone until the removal is confirmed durable (*Removing the selector*, below).
- **The authoritative default changes at startup.** The replacement process writes the file
  pointer **durably**, and that write is the commit point. Neither pointer changes during the
  request, so every reader listed in *Context* sees the effective default for the whole generation.
- **Effective default of a generation.** This is the file pointer **as read back at start** and
  projected into config. It is held for the generation, as the served archive is. After setup,
  config's `default_logbook_id` changes only at start.
- **Startup reconciliation** happens before anything reads the default, as part of the existing
  projection. Each step depends on the one before it.
  1. **Wrong archive.** A selector naming another archive is discarded and recorded. It cannot
     arise in ordinary operation, because each pending operation refuses the other's unresolved
     intent (§2 step 1); a hand edit could still produce one.
  2. **Target checks.** The daemon checks that the target exists in this archive, is not deleted,
     and still meets eligibility (L1: same callsign as the current file default).
  3. **Durable commit.** If the checks pass, the daemon writes the file pointer to the target in a
     **durable transaction** (`synchronous=FULL` on a pinned connection, read back inside the
     transaction). This is the precedent of ADR 0091 and ADR 0092, `durableTx` in
     `internal/database/sqlite/logbook_destination.go:474`. The ordinary pointer setter
     (`archive_identity.go:146`) is not enough: under `synchronous=NORMAL` a committed write can
     roll back after a power loss. Config could then durably clear the selector while the file
     returns to A, and the requested change would be lost.
  4. **Read back, and establish durability.**
     - **What a read shows.** Reading B shows that B is **visible**. It does not on its own show
       that B is **durable**.
     - **When B is durable.** B counts as durable only when **this start** has a durable
       transaction that wrote B and reported a successful commit. That transaction is the one in
       step 3, or a durable rewrite of B. Its own commit holds B at `synchronous=FULL`.
     - **Before the selector is cleared,** B must be durable.
     - **Failing the start.** Wherever the table below says *fail the start*, the start fails as it
       already does when the archive identity cannot be read (`cmd/smd/archive_adopt.go:45-48`).
       The selector is kept.
     - **Never "nothing changed".** An unreadable, unexpected or uncertain database outcome is
       never treated as "nothing changed".

     | Step 3 outcome | Pointer read back | Result |
     |---|---|---|
     | Durable commit reported | B | **Applied**: B is durable. |
     | Durable commit reported | unreadable, or anything but B | **Fail the start.** |
     | Refused before commit (durableTx writes nothing) | A | **Not applied, verified** (step 5). |
     | Commit outcome uncertain | A | **Not applied, verified**; the write failed, so the selector is kept (step 5). |
     | Commit outcome uncertain | B | **Durable rewrite of B.** If it reports success, **Applied**. Otherwise **fail the start**. |
     | Any | unreadable, or neither A nor B (for example NULL) | **Fail the start.** |
     | File already on B at the start of reconciliation | B | **Recovery after a crash** (below). |

     **Recovery after a crash: the file already names B.** An earlier start may have committed B
     and then stopped before it cleared the selector. Or it may have stopped with B visible but not
     durable. This start cannot tell which. It therefore:
     - skips step 2's not-applied outcomes, because A is not verified;
     - checks that B exists and is live. If B is invalid in the file, the state is unexpected, and
       the start fails;
     - performs the durable rewrite of B. If that reports success, the result is **Applied**.
       Otherwise the start fails.
  5. **Config update.** In one config update, the daemon projects the read-back pointer into
     `default_logbook_id` and resolves the selector:
     - **Applied:** the selector is cleared.
     - **Not applied, refused:** the target is missing, deleted or ineligible, which is a
       definitive refusal. The selector is cleared and the failure recorded.
     - **Not applied, write failed:** the pointer reads A but the write failed for any other
       reason. The selector is **kept**, and the change is retried at the next start.
  6. **The selector cannot be cleared, or its removal is not confirmed durable.** The in-memory
     projection still follows the read-back file, which is the existing rule. The removal outcomes,
     including *uncertain*, are defined under *Removing the selector* below. The status says which
     case it is:
     - **Applied, cleanup pending:** the selector names the effective default. A selector restored
       by a crash needs only cleanup: the next start finds the file already on B, and the recovery
       above applies. The state is **reported**, not hidden, because it blocks further default
       changes and archive activation.
     - **Not applied, intent still pending:** the selector names B and the file reads A. **This is
       not an abandoned change.** A later start applies B if B is then valid, so B stays
       protected from deletion, the status says the change is still pending, and Cancel is offered.

  **The daemon serves A only when A is verified:** the read-back pointer names A. **A default
  change never otherwise prevents the daemon from starting.**

| State | Effective default | Selector on disk | Live admission | Delete refused for | Operator sees | Cancel |
|---|---|---|---|---|---|---|
| Idle | A | — | open, on A | A | Default: A | — |
| Validating | A | — | open, on A | A | request in progress | — |
| Refused | A | — | open, on A | A | the reason: ineligible, busy, competing or pending operation, or stale request | — |
| Sealing | A | — | closing: none newly admitted; already-admitted ones finish on A | A | Changing default… | — |
| Pending | A | B | sealed | A and B | Changing to B — restarting | no (restart requested) |
| Restarting | (process exiting) | B | closed (daemon down) | — (no requests served) | daemon unreachable; entry gated | — |
| Effective | B | — | open, on B | B; A deletable if empty | Default: B | — |
| Applied, cleanup pending | B | B (equals effective), or an unconfirmed applied tombstone | open, on B | B | "Default: B. Cleanup pending: the record of this completed change has not been removed durably. Set as default and archive activation are unavailable until it is." Action: **Retry cleanup**; the next start also cleans up | no (nothing to cancel) |
| Withdrawal unconfirmed | A (verified) | an unconfirmed tombstone (§1 *Removing the selector*) | open, on A | A and B | "The change to B was cancelled/withdrawn/abandoned, but this is not yet confirmed on disk. If power is lost before it is, the change to B may still apply at a later start." Action: **Confirm** | no (already removed) |
| Aborted before restart | A | — | reopened on A | A | the failure code; nothing changed | — |
| Pending, unclear | A | B (could not be cleared) | reopened on A | A and B | "The change to B applies at the next restart" | **yes** |
| Not applied, intent still pending | A (verified) | B (kept, or clear failed) | open, on A | A and B | "The change to B was not applied at this start and is still pending: *reason*" | **yes** |
| Not applied, abandoned | A (verified) | — (failure recorded) | open, on A | A | "The change to B was not applied: *reason*" | — |

**Cancel** withdraws one pending request. It runs under the same single flight (§2 step 1) and
names the request id it was shown. The daemon clears the selector **only if it still holds that
request id**. Otherwise Cancel is refused (`pending_changed`) and the status is re-read, so a
delayed Cancel never clears a later request. Cancel is permitted only in **Pending, unclear** and
**Not applied, intent still pending**: the process is running on a verified A, and the selector
names a target that is not effective. It needs no restart and no seal. Its result follows
*Removing the selector*.

**Removing the selector.** A config write has three outcomes (`internal/config/config.go:743-757`):
- **durable**: the replacement survives a crash;
- **uncertain**: a nil error with `DurabilityUncertain`. The replacement is live, but the parent
  directory fsync failed, so its survival is unconfirmed;
- **failed**: nothing was replaced.

An uncertain removal is neither a confirmed removal nor a failure: after a power loss the earlier
file, still holding the selector, can return. Three removals can meet this. In the table,
"unconfirmed" means the tombstone described below is not yet confirmed.

| Removal | Durable | Uncertain | Failed |
|---|---|---|---|
| **Cancel** (B unapplied) | Cancelled. | **Withdrawal unconfirmed**: the status says "cancelled, not yet confirmed — the change to B may still apply at a later start if power is lost first". | The state is unchanged, still pending, and reported. |
| **Abort cleanup** after a failed restart request (§2 step 6; B unapplied) | Aborted before restart. | **Withdrawal unconfirmed**: "withdrawn, not yet confirmed". | Pending, unclear. |
| **Startup cleanup**, B applied | Effective. | **Applied, cleanup pending**: a restored selector only needs cleanup. | Applied, cleanup pending. |
| **Startup cleanup**, B refused | Not applied, abandoned. | **Withdrawal unconfirmed**: "abandoned, not yet confirmed". The restored selector would be judged again at a later start. | Not applied, intent still pending. |

**Removal leaves a tombstone, which is the recovery evidence.** A removal never deletes the
selector outright. It replaces it, in the same config write, with a **tombstone** naming:
- the request id;
- the target;
- the kind: cancelled, withdrawn, abandoned or applied.

The tombstone lets a later process, not only this one, know that a removal may not yet be durable.
An in-memory record would not survive an ordinary restart. A restart that merely reads the
replacement file proves visibility, not survival of a later power loss.

- **Confirmed.** A removal is confirmed only when a config write **reported durable** has contents
  that hold the tombstone for that request id, and no active selector with that id. The write may be
  the removal itself, a Confirm, a startup rewrite, or any unrelated save.
  - An unrelated save confirms a removal **only when its written contents establish it**: the
    tombstone for that id is present and the active selector is absent.
  - Once confirmed, the tombstone is dropped by any later write. Losing that later write can only
    return the confirmed tombstone, because the confirming write was durable.
- **Unconfirmed.** Until confirmation, every config write carries the tombstone forward.
  Coordination continues as if the selector were present:
  - **Refused:** a new default change and archive activation (`default_change_unconfirmed`).
  - **Delete-protected:** the target stays protected while it is not effective.
  - **Cancel** is not offered, because nothing is active to cancel.
- **At every start.**
  - An **active selector** is treated as found (§1 steps 1–5). This includes one restored by a
    power loss that undid an uncertain removal; the caveat shown earlier covered that.
  - **Every tombstone found at startup is unconfirmed for this process.** Its stored fields do not
    record whether the previous process observed a durable result. The start performs a durable
    rewrite of config that keeps the tombstone.
    - **Reported durable:** the removal is confirmed.
    - **Uncertain or failed:** the state stays unconfirmed, with its status, its refusals and
      **Confirm** offered. The start continues on its verified default; a persistence failure here
      does not stop logging. The existing startup reconciliation, which continues past a persistence
      failure (`cmd/smd/archive_adopt.go:92-94`), is not relied on: the tombstone, not the
      reconciliation, carries the evidence.
  - **Neither present** means there is no unconfirmed removal.
- **Confirm and Retry cleanup carry the request id,** as Cancel does. Under the single flight the
  daemon rechecks, before writing, that the request is unresolved and that its state permits the
  action:
  - **Confirm** acts on *Withdrawal unconfirmed*. It performs a durable rewrite that keeps the
    tombstone. It **never removes or alters any selector**.
  - **Retry cleanup** acts on *Applied, cleanup pending*. If the active selector still names that
    id, and its target is the effective default, it replaces the selector with an applied tombstone.
    Otherwise it performs Confirm's durable rewrite.
  - **Anything else** is refused (`already_resolved` or `pending_changed`), nothing is written and
    the status is re-read. A delayed action therefore never touches a later request.

  The status never reports an unconditional cancellation, withdrawal or abandonment before the
  removal is confirmed.

**Acceptance cases for removal:**
1. **Delayed confirmation.**
   - Withdrawal of B (request R) is unconfirmed, and the operator's Confirm for R is delayed.
   - An unrelated config save, reported durable, writes R's tombstone and so confirms R.
   - A new change to C (request S) is then accepted.
   - The delayed Confirm for R is refused `already_resolved`, writes nothing, and C's selector S is
     untouched.
2. **Plain restart.** An uncertain Cancel is followed by an ordinary process restart, with no power
   loss. The start finds R's unconfirmed tombstone and performs the durable rewrite. It does
   **not** treat the missing active selector as confirmation.
3. **Power loss.** An uncertain Cancel is followed by a power loss. A power loss does not
   necessarily undo the uncertain replacement, so either state can survive:
   - the start finds the **active selector R**, treats it as found and judges it; or
   - the start finds **R's tombstone**, treats it as unconfirmed and performs the durable rewrite,
     as in case 2.
4. **Unrelated save that does not establish removal.** A durable save whose contents still hold
   the active selector R, such as a save made before the removal, confirms nothing.
5. **Retry cleanup after resolution.** A Retry cleanup naming R arrives after R's cleanup was
   confirmed and a new selector S was accepted. It is refused, and S is untouched.

### 2. Transition ordering

Each step refuses before any later step runs.

1. **Serialise, and refuse surviving intent.** The daemon takes the single flight that it shares
   with archive activation, Cancel and logbook deletion. A delete waits for the single flight, or is
   refused while it is held, so it never races the change. Sharing a lock is not enough on its own,
   because a change can reach *Pending, unclear* and release the lock with its selector still on
   disk. So, read from config under the single flight:
   - A default change is refused while **any** `pending_qso_archive_id` exists
     (`activation_pending`).
   - Archive activation is refused while **any** default selector exists (`default_change_pending`),
     until Cancel clears it or a start resolves it.
   - A competing operation in flight is refused (`change_in_progress` / `activation_in_progress`).
2. **Validate** in the daemon (L1). The request names:
   - the target logbook UUID;
   - the effective default that the tab believes in.

   It is refused when:
   - the believed default differs from the effective one (**stale**; never retargeted);
   - the target is not a live logbook of the served archive;
   - the target's callsign differs from the effective default's;
   - the target is already effective;
   - a default selector already exists, whatever its state; it must first be cancelled or resolved;
   - no restart port is wired.
3. **Seal TX** with the existing archive-activation seals. FT8 admission refuses while a QSO is
   active, TX is in flight or TX is armed. The bridge seal covers tune and keying. A refusal is
   `tx_busy` with the owner's reason, and **nothing has been changed**. The change never ends a
   running FT8 session.
4. **Seal live entry.** This seal is new. A live Phone / CW submission (§4) that is already
   admitted finishes, committed or failed, on the effective default before the seal is held. Every
   later live submission is refused with `default_change_pending`, and nothing is written. FT8
   admission is already closed by step 3. An FT8 contact that completed before step 3 logs to its
   pinned logbook while the process drains, because shutdown waits for that work
   (`cmd/smd/lifecycle_adapters.go:214`).
5. **Persist the pending selector** with the durable replace.
   - **Definitive failure:** the seals are released and the request is refused with
     `default_change_persist_failed`. Nothing is on disk.
   - **Uncertain durability:** the change proceeds. A start under either truth is safe: it applies
     the change or comes up unchanged, and reports which.
6. **Request the restart.**
   - **The request fails:** the daemon clears the selector (compare-and-clear on its request id),
     releases every seal and refuses with `restart_failed`.
   - **The clear also fails:** the state is *Pending, unclear*. The seals are still released, and
     the next start applies the change.
   - **The clear is uncertain** (`DurabilityUncertain`): the state is *Withdrawal unconfirmed*
     (§1 *Removing the selector*). The seals are released, and the status carries the caveat.
7. **Respond and exit.** The response reports `accepted, restarting` and the durability. The
   process then exits through the ordinary lifecycle drain.

**Admission reopens at one of three points:**
- in the replacement process, on whichever default it started with;
- immediately on the effective default, after an abort in step 5 or step 6;
- on the effective default in *Pending, unclear*, which the operator can Cancel.

No live submission or FT8 admission lands on the target before the start that makes it effective.
An explicit operation that names the target logbook (§4) is not a live admission.

### 3. Failure recovery

| Failure | Disk afterwards | Effective default | Admission |
|---|---|---|---|
| Refusal in steps 1–4 | unchanged | A | open (a seal taken earlier is released) |
| Persist fails definitively (step 5) | unchanged | A | reopened |
| Crash after step 5, before the restart request | selector B | A until the respawn, then B | down, then open on B |
| Restart request fails, selector cleared durably | unchanged | A | reopened |
| Restart request fails, clear uncertain | selector removed, removal unconfirmed | A; *Withdrawal unconfirmed* | reopened on A; **Confirm** offered; B protected |
| Restart request fails, clear fails | selector B | A | reopened on A; B applies at the next restart unless cancelled |
| Replacement crashes after the durable file write, before the config update | file B, selector B | B at the next start, after a durable rewrite (*Recovery after a crash*) | open on B |
| Crash with B visible but not durable | file B or A, selector B | whatever the next start reads: B → durable rewrite, then applied; A → applied afresh | open |
| Selector cannot be cleared after B is applied, or the clear is uncertain | file B, selector B (or removal unconfirmed) | B (*Applied, cleanup pending*) | open on B; **Retry cleanup** offered |
| Commit uncertain, B readable, and the durable rewrite fails | file B (not durable), selector B | none: the start fails | closed until a start establishes B or reads A |
| Pointer unreadable or unexpected after any commit outcome | unknown, selector B | none: the start fails | closed |
| Target refused at start (missing, deleted, ineligible) | file A, selector cleared, failure recorded | A (verified) | open on A |
| File write fails at start, pointer reads A | file A, selector B kept | A (verified); retried at the next start | open on A; Cancel offered |
| Target refused and the selector cannot be cleared | file A, selector B | A (verified); *Not applied, intent still pending* | open on A; Cancel offered |
| Power loss after a durable commit | file B (FULL survives power loss) | B | open on B |
| Power loss after an uncertain Cancel, abort or abandonment | the selector may return | A until the next start, which judges the restored selector | open; the caveat was shown |

**An ambiguous response is an unknown outcome.** This covers a timeout, or a connection dropped by
the exit. The tab must not report success or failure. It re-reads the state once the daemon
answers, as described in §4.

### 4. Client contract

- **Boot.** A tab reads the effective default (its UUID, name and count) and any pending or failed
  change inside the existing identity bracket. A fresh tab during *Pending* therefore sees the
  effective default, the pending target and sealed live entry.
- **Proof on live routes.** Every live-entry route carries the logbook UUID the tab treats as
  default (placeholder header: `X-SM-Default-Logbook`). The live-entry routes are `POST /v1/qso` and
  every FT8 route that admits a new session or transmission. The daemon checks it against the
  generation's effective default **before any side effect**, beside `X-SM-Archive-Id`.
  - A **missing header** is refused with a reload instruction. This also stops a tab served by an
    older SPA.
  - **FT8:** a mismatch is refused (`default_logbook_mismatch`). FT8 pins the daemon's default, so a
    mismatch means the tab shows a different logbook from the one the contact would go to.
  - **`POST /v1/qso` naming the believed default** is a live submission. It is refused on a mismatch
    and refused with `default_change_pending` while live entry is sealed. It is **never retargeted**.
  - **`POST /v1/qso` naming another logbook** is an explicit operation on that logbook. It is not
    refused for a stale default; the archive guard and the existing checks still apply.
  - **Unaffected:** operations addressed to a QSO by UUID (edit, delete), session export and email,
    imports and backfills carry no default assertion.
- **Reconnect** compares the effective default as well as the archive identity. A mismatch reloads
  or gates the tab, as an archive switch does. This is the convenience path; the daemon check is the
  guarantee.
- **One established context.** The asserted default and the `?logbook=` of a live submission both
  come from the tab's established station context, the one read inside the identity bracket at boot.
  They are taken together when the submission is built. Nothing re-reads or refreshes them during
  the submission, so the assertion always describes the context the operator was entering into.
- **The Set as default request** also carries the believed default, so a stale tab cannot start a
  change.
- **Unknown outcome.** The tab polls the status read until the daemon answers, then shows its
  effective, pending and failure state.

### 4a. Unlogged work: ADR 0087's policy carried over

A default change restarts the daemon and reloads every tab, just as an archive switch does. The
reload path discards unlogged Phone / CW entry; ADR 0087 removed ADR 0085's recovery. Set as default
therefore adopts ADR 0087's confirmation policy for the archive switch (its items 2, 3, 8 and 9):

- **A Log in flight in this tab** refuses Set as default. This is checked before the confirmation
  and again before the request is sent.
- **An unknown Log outcome** in this tab does not refuse. The confirmation says the QSO **may
  already be logged** and should be checked in the current default's Logbook. That is distinct from
  an unlogged QSO.
- **An unfinished entry in this tab,** including a partial one, is named in the confirmation, which
  offers to **discard it and continue** or **Cancel**. Cancel sends nothing and keeps everything.
- **Other tabs:** the confirmation always warns that unlogged work in any other tab will be lost.
  That covers an unlogged Phone / CW QSO and unsaved Settings changes. An FT8 / FT4 exchange in
  progress cannot be lost this way, because it refuses the change (§2 step 3).
- **No notice after the reload,** and nothing is carried across it, as in ADR 0087. A submission
  refused with `default_change_pending` in another tab is part of the stated loss.

The exact wording stays open.

### 5. SM Cloud recovery: adoption follows its marker, not the default

This decision amends ADR 0090 §1 and ADR 0091 §4 for marked bindings.

- **Who the adopter works on.** Any enabled Home SM Cloud binding that holds an adoption
  reservation (`adoption_reserved_at`) or an adoption (`remote_adopted_at`) not confirmed for the
  current station account. This applies **whichever logbook is the default**. The adopter attempts
  each such binding in turn.
- **Evidence** is judged against **that binding's own logbook**: every cloud UUID under the name
  must be a local QSO of that logbook. Everything else is unchanged: the protected names, the
  reservation before sending, retries that judge again, account coordination and the status texts
  (with "Home's default logbook" reworded to name the binding's logbook).
- **New adoptions start only on Home's effective default's unmarked binding,** as now.
- **The concrete recovery path** for a former default whose binding is reserved or adopted but
  unconfirmed:
  - The same background adopter completes confirmation. It already does this for the default: its
    first check runs at start, it retries hourly, and a changed account re-arms it.
  - Once confirmed, the binding leaves the held set at the next start, as today.
  - The held binding's status names its logbook and the adopter's current state.
- **The binding-enable rule changes too.** Today `planSmcloud` lets a claimed binding — reserved
  or adopted — be newly enabled only when it is Home's default (`internal/archive/bindings.go:122`:
  `home && isDefault || b.Enabled`). Otherwise it is refused, with the text "make its logbook the
  default to use it" (`docs/v2-design/api-endpoints.md:325`). Because the adopter works only on
  enabled bindings, a **disabled** former default would still need switching back.
  - **Amendment:** in the adopted Home archive, a claimed binding may be re-enabled on its own
    logbook whether or not that logbook is the default. The claim already fixes the owner.
  - **Kept unchanged:**
    - the identity-conflict refusal, which is checked first;
    - born-on-identity permanence (ADR 0092);
    - protected names across every save (ADR 0091 §3), and the credential lock;
    - the rule that a claimed binding is never re-stamped as born on identity.
  - **Outside Home** nothing changes: managed archives have no claims.
  - The refusal text and its documentation change in the same implementation.
- **State per binding.** The adopter today keeps one subject's state: one suppression, one
  unresolved outcome, one logged outcome and one status
  (`internal/forwarding/smcloud/adoption/adopter.go:94-101`). With more than one subject, each
  binding keeps its own, keyed by its subject (archive, binding, logbook, name and account):
  - its status, reported per binding;
  - its unresolved outcome, which a later outcome for another binding never resolves or overwrites
    (ADR 0091 A5 applies per subject);
  - its retry suppression after a terminal outcome (ruling Q4), which never suppresses another
    binding.
- **Two-binding acceptance case.** The former default B1 holds a reservation with an uncertain
  outcome, or an adoption not confirmed for the current account. It may be disabled first and then
  re-enabled. The new default B2 has an enabled, unmarked SM Cloud binding under another cloud
  name. Then:
  - B1 is confirmed through the background adopter without B1's logbook becoming the default;
  - B2's initiation is judged on its own evidence;
  - a terminal refusal or an unresolved outcome on either binding leaves the other's status,
    uncertainty and retries unchanged;
  - after B1's confirmation and a restart, B1 leaves the held set and B2's state is unaffected.
- **The operator never has to make the old logbook the default again** to recover it.
- **An unmarked former default** is neither held nor adopted. It stays on the legacy wire and its
  uploads continue. Adoption can be started for it only by making it the default again (ADR 0090
  unchanged). The confirmation of Set as default says so when the current default has an enabled,
  unadopted SM Cloud binding.

This is an **implementation prerequisite** of Set as default (W-0024).

## Alternatives considered

### Live application (W-0024 L): the running process switches its default

Rejected. **Both options need the stale-client guard of §4**: under either one, a tab built before
the change could log to the previous default. Restart does not remove that guard. Its advantage is
narrower: it avoids an **in-process identity transition**. Under live application, each reader of
the default would have to switch together, and that switch would have to be coordinated with:
- an FT8 session pinned at start;
- the adopter's pinned attempt;
- the bindings support rules;
- the active archive's summary;
- the delete check.

Under restart, each generation has one fixed default and every component starts against it. The
cost is a restart for an action expected to be rare.

### Pending intent in the archive file: write the file pointer at request time

The file would hold the saved default, and config would keep the effective one until the start.
Rejected for three reasons:
- the file pointer is read during the generation: the summary would mark the target as Default
  before it is effective (`internal/archive/summaries.go:184`);
- the pointer does not protect a pending target: deletion is a soft delete, which its foreign
  key never sees, so the target could be soft-deleted while the file still named it;
- the authoritative pointer would disagree with the effective default for the length of a
  transition.

### Write config's `default_logbook_id` and call it restart-required

Rejected. Config publication is immediate, so FT8's next arm would pin the target before the
restart.

### Save now, restart separately

Rejected by review (L2): it leaves a saved-but-not-effective state open for as long as the operator
takes to restart.

### Leave Phone / CW unsealed until the exit, as archive activation does

Rejected for this change. Sealing makes the boundary exact: live submissions admitted before the
seal go to A, and none are admitted after it. The cost is that a submission in the seconds before
the restart is refused and has to be entered again after the reload. Whether archive activation
should adopt the same seal is left open.

### SM Cloud: refuse Set as default until adoption is confirmed

Rejected. With an old server, or a refused adoption, the refusal could last indefinitely, and it
does not cover a confirmation lost later through an account change.

### SM Cloud: recover by making the logbook the default again

This is today's text. Rejected as the recovery path: it forces live logging back onto the former
logbook just to finish a cloud confirmation.

### SM Cloud: a manual "confirm adoption" action

Rejected. The background adopter already performs confirmation with every judgement and safeguard,
and a second path would duplicate them.

## Consequences

- **New config field** (the pending selector, with a failure code): `config.md` and the config
  validation change with the implementation. A status read reports the effective default, the
  pending default and the last failure.
- **New live-entry seal** around live Phone / CW admission, and a **new default assertion** on
  live-entry routes. `api-endpoints.md` changes with them. Every SPA call path on those routes must
  send the assertion.
- **The single flight** now covers logbook deletion and Cancel as well as activation and default
  changes. Each pending operation refuses the other's unresolved selector (§2 step 1).
- **The adopter changes target selection and keeps state per binding** (§5). It needs tests for a
  former default that is reserved, adopted but unconfirmed, disabled then re-enabled, and unmarked,
  and for the two-binding case. The bindings enable rule (`planSmcloud`) changes with it.
- **Durable startup commit:** the start's pointer write uses the durable transaction, and its
  read-back decides the outcome. A visible but unproven B is rewritten durably before the selector is
  cleared, and an unreadable or unexpected pointer fails the start rather than guessing (§1).
- **Selector removal has three outcomes** (durable, uncertain, failed). A removal leaves a
  tombstone in config that persists until a durable write confirms it. Meanwhile coordination and
  protection stay in force, the status carries the caveat, and Confirm or Retry cleanup is offered,
  each naming its request id (§1 *Removing the selector*).
- **Unlogged work** follows ADR 0087's policy for Set as default (§4a): the in-flight refusal, the
  discard-or-cancel confirmation and the warning about other tabs.
- **A restart interrupts live operation** (rig streams, FT8 decoding, open tabs reload) for each
  default change. A change is refused while FT8 is busy rather than ending the run.
- **Not affected:** upload routing (by each QSO's logbook; ADR 0082) and existing bindings.

## Unresolved (not decided by this record)

- Placeholder names: the config field, the header, the error codes and the status read.
- The confirmation wording (W-0024 AC2), including the SM Cloud note in §5.
- Whether archive activation adopts the live-entry seal.

## Triggers to revisit

- **Frequent default changes** (for example, several per contest weekend) would make the restart
  cost real. Reconsider live application then, with the §4 guard kept.
- **Different-call logbooks as default** (W-0024, out of scope) change eligibility and the SM Cloud
  rules; revisit §2 step 2 and §5.
- **Restart without a service manager.** If the daemon gains a supported restart without
  `SM_SELF_RESTART`, the `restart_unavailable` refusal changes.

## References

- [W-0024](../work/W-0024-settings-logbooks.md) (acceptance criteria, the 2026-10-29 review),
  [W-0021](../work/W-0021-qso-archives.md) (first slice), [W-0023](../work/W-0023-contest-logging.md).
- ADR 0055 (logbook as station identity), ADR 0071 (archives and activation by attended restart),
  ADR 0082 (per-logbook bindings), ADR 0084 (archive summaries), ADR 0087 (archive switch from
  Settings, stated loss; supersedes ADR 0085's draft recovery), ADR 0090, ADR 0091 and ADR 0092
  (SM Cloud adoption), ADR 0094 (archive request guard).
- `internal/archive/activate.go`, `internal/archive/bindings.go`, `internal/api/archive_guard.go`,
  `cmd/smd/archive_adopt.go`, `internal/database/sqlite/logbook_destination.go` (`durableTx`),
  `internal/api/handler_ft8_qso.go`, `internal/forwarding/smcloud/adoption/adopter.go`,
  `cmd/smd/identity_wire.go`.
