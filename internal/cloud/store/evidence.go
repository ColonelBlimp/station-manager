package store

import (
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"

	"github.com/lib/pq"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// maxSupersedesPerSummary bounds a compaction summary's DIRECT predecessor
// list at ingest (package review, 2026-08-10): each predecessor is two SQL
// round trips inside the batch transaction, so an unbounded list would let one
// authenticated request hold a connection across an arbitrary number of them.
// The client's compactionMaxPreds is 64; the server accepts a small margin.
const maxSupersedesPerSummary = 128

// evidenceBatchAttempts bounds SERIALIZABLE retries for one batch (P1 on the
// tombstone/insert race, package review 2026-08-10): a serialization failure
// re-runs the whole idempotent batch a few times before surfacing as an error
// the client retries.
const evidenceBatchAttempts = 5

// afterTombstoneProbeHook is a test-only seam (nil in production) invoked
// after a record's tombstone probe and before its insert, so a test can force
// the concurrent tombstone-then-insert interleaving.
var afterTombstoneProbeHook func(kind, uuid string)

// UpsertEvidence ingests one §5 sync batch for a tenant: exactly one outcome
// per record, positionally, uuid echoed. Row faults answer on their own row
// and never abort batch-mates; an error return means the BATCH failed as a
// unit (server fault) and no outcome may be trusted.
//
// The batch runs at SERIALIZABLE isolation so the tombstone probe and the
// record insert (separate tables) cannot interleave to resurrect a superseded
// record (package review, 2026-08-10): Postgres SSI aborts one of a dangerous
// pair and the whole batch — idempotent — is retried. A persistent failure
// surfaces as an error; the client re-offers.
func (s *Store) UpsertEvidence(ctx context.Context, tenantID int64, recs []evidencewire.Record) ([]evidencewire.RowOutcome, error) {
	for attempt := 0; ; attempt++ {
		outcomes, err := s.upsertEvidenceOnce(ctx, tenantID, recs)
		if err == nil {
			return outcomes, nil
		}
		if attempt < evidenceBatchAttempts-1 && isSerializationFailure(err) {
			continue
		}
		return nil, err
	}
}

// isSerializationFailure reports a Postgres serialization_failure (40001) or
// deadlock_detected (40P01) — the retryable outcomes of SERIALIZABLE conflict.
func isSerializationFailure(err error) bool {
	var pqErr *pq.Error
	if stderrors.As(err, &pqErr) {
		return pqErr.Code == "40001" || pqErr.Code == "40P01"
	}
	return false
}

func (s *Store) upsertEvidenceOnce(ctx context.Context, tenantID int64, recs []evidencewire.Record) ([]evidencewire.RowOutcome, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("smcloud: begin evidence batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Profile records are processed before every other kind REGARDLESS of
	// wire order (§5.1 pins that ordering on the wire carries nothing), so a
	// batch holding an observation before the profile it references succeeds.
	outcomes := make([]evidencewire.RowOutcome, len(recs))
	for _, pass := range []func(evidencewire.Record) bool{
		func(r evidencewire.Record) bool { return r.Kind == evidencewire.KindProfile },
		func(r evidencewire.Record) bool { return r.Kind != evidencewire.KindProfile },
	} {
		for i, rec := range recs {
			if !pass(rec) {
				continue
			}
			o, err := upsertOneEvidence(ctx, tx, tenantID, rec)
			if err != nil {
				return nil, err
			}
			o.UUID = rec.UUID
			outcomes[i] = o
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("smcloud: commit evidence batch: %w", err)
	}
	return outcomes, nil
}

func reject(reason string) evidencewire.RowOutcome {
	return evidencewire.RowOutcome{Outcome: evidencewire.OutcomePermanentReject, Reason: reason}
}

func upsertOneEvidence(ctx context.Context, tx *sql.Tx, tenantID int64, rec evidencewire.Record) (evidencewire.RowOutcome, error) {
	// Row-fault validation happens in Go, before any SQL: a constraint
	// violation would abort the whole transaction, turning one bad row into
	// a batch fault — exactly what §5.1's quarantine contract forbids.
	switch rec.Kind {
	case evidencewire.KindObservation, evidencewire.KindCoverage,
		evidencewire.KindLossInterval, evidencewire.KindProfile,
		evidencewire.KindRetention:
	default:
		return reject("unsupported_kind"), nil
	}
	if !utils.IsValidUUIDv7(rec.UUID) {
		return reject("invalid_uuid"), nil
	}
	// The digest is VERIFIED, never trusted (codex-P1 fix 2026-08-10): a
	// claimed digest is not identity — a digest-reusing client bug would
	// otherwise slip changed content past E3's conflict check as
	// already_present, the silent dedup §5.2 forbids. Verification also
	// pins the version boundary: the server vouches only for canonical
	// forms it can compute, so an unknown digest version rejects rather
	// than storing an unverifiable identity.
	if rec.DigestV != evidencewire.DigestVersion1 {
		return reject("unsupported_digest_version"), nil
	}
	computed, err := evidencewire.DigestV1Hex(rec.Payload)
	if err != nil {
		return reject("malformed_payload"), nil
	}
	if computed != rec.Digest {
		return reject("digest_mismatch"), nil
	}

	// Parse + fully VALIDATE any supersedes list before any mutation (package
	// review, 2026-08-10): a malformed, self-referential, or oversized list
	// rejects the row without tombstoning or deleting anything.
	supersedes, out, ok := validatedSupersedes(rec)
	if !ok {
		return out, nil
	}

	// An observation's non-null profile reference is validated in Go before
	// the DB probe (package review, 2026-08-10): the raw string used to reach
	// a Postgres UUID comparison, whose syntax error aborted the whole batch,
	// and a valid-but-non-v7 ref would retry forever though its profile can
	// never be ingested. An empty string is neither null nor a valid ref.
	if rec.Kind == evidencewire.KindObservation {
		var ref struct {
			ProfileUUID *string `json:"profile_uuid"`
		}
		if err := json.Unmarshal(rec.Payload, &ref); err != nil {
			return reject("malformed_payload"), nil
		}
		// A NULL profile is EXPLICITLY unprofiled (§5.4 amendment) — accepted,
		// never retried. A present reference must be a valid v7 UUID.
		if ref.ProfileUUID != nil {
			if !utils.IsValidUUIDv7(*ref.ProfileUUID) {
				return reject("invalid_profile_ref"), nil
			}
			var exists bool
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM evidence_records
				  WHERE tenant_id = $1 AND kind = $2 AND uuid = $3)`,
				tenantID, evidencewire.KindProfile, *ref.ProfileUUID).Scan(&exists); err != nil {
				return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: profile existence probe: %w", err)
			}
			if !exists {
				return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeRetryableMissingProfile}, nil
			}
		}
	}

	// Tombstones gate EVERY kind, before any storage logic (E13): a
	// superseded or deleted identity never re-enters the store, whatever
	// content it carries — this is what makes supersession deletion (and
	// §8 deletion later) idempotent against old-backup re-offers.
	var dead bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM evidence_tombstones
		  WHERE tenant_id = $1 AND kind = $2 AND uuid = $3)`,
		tenantID, rec.Kind, rec.UUID).Scan(&dead); err != nil {
		return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: tombstone probe: %w", err)
	}
	if dead {
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeTombstoned}, nil
	}

	// Test-only seam (P1 concurrent-resurrection test): pause a re-offer here,
	// AFTER it observed no tombstone and BEFORE it inserts, so a concurrent
	// supersession can tombstone+delete this identity in between.
	if hook := afterTombstoneProbeHook; hook != nil {
		hook(rec.Kind, rec.UUID)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO evidence_records (tenant_id, kind, uuid, digest_v, digest, payload)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id, kind, uuid) DO NOTHING`,
		tenantID, rec.Kind, rec.UUID, rec.DigestV, rec.Digest, string(rec.Payload))
	if err != nil {
		return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: insert evidence record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: evidence rows-affected: %w", err)
	}
	if n != 1 {
		// Conflict: the digest decides between the benign restored-backup
		// re-offer and a client bug — never silently deduplicate (§5.2). A
		// rejected summary must NOT have superseded anything, which is why
		// supersession below is gated on a fresh accept.
		var storedV int
		var storedDigest string
		if err := tx.QueryRowContext(ctx,
			`SELECT digest_v, digest FROM evidence_records
			  WHERE tenant_id = $1 AND kind = $2 AND uuid = $3`,
			tenantID, rec.Kind, rec.UUID).Scan(&storedV, &storedDigest); err != nil {
			return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: read conflicting digest: %w", err)
		}
		if storedV == rec.DigestV && storedDigest == rec.Digest {
			return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAlreadyPresent}, nil
		}
		return reject("digest_conflict"), nil
	}

	// Supersession runs ONLY for a freshly-ACCEPTED summary (package review
	// fix, 2026-08-10): tombstone-then-delete each DIRECT predecessor, in the
	// same transaction as the accept, so a later re-offer of a predecessor
	// answers tombstoned. A conflicting summary reached the reject above and
	// mutates nothing; an already_present re-offer already superseded them on
	// its first accept.
	for _, pred := range supersedes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO evidence_tombstones (tenant_id, kind, uuid) VALUES ($1, $2, $3)
			 ON CONFLICT DO NOTHING`, tenantID, rec.Kind, pred); err != nil {
			return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: supersession tombstone: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM evidence_records WHERE tenant_id = $1 AND kind = $2 AND uuid = $3`,
			tenantID, rec.Kind, pred); err != nil {
			return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: supersession delete: %w", err)
		}
	}
	return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}, nil
}

// validatedSupersedes extracts a loss/retention summary's DIRECT predecessor
// list and enforces every rule before any mutation: valid v7 UUIDs, no
// self-supersession, and a bounded count. Returns (list, rejectOutcome,
// ok). The list is sorted so concurrent summaries take identity locks (and
// hit rows) in a consistent order.
func validatedSupersedes(rec evidencewire.Record) ([]string, evidencewire.RowOutcome, bool) {
	if rec.Kind != evidencewire.KindLossInterval && rec.Kind != evidencewire.KindRetention {
		return nil, evidencewire.RowOutcome{}, true
	}
	var sup struct {
		Supersedes []string `json:"supersedes"`
	}
	if err := json.Unmarshal(rec.Payload, &sup); err != nil {
		return nil, reject("malformed_payload"), false
	}
	if len(sup.Supersedes) > maxSupersedesPerSummary {
		return nil, reject("too_many_supersedes"), false
	}
	for _, pred := range sup.Supersedes {
		if !utils.IsValidUUIDv7(pred) {
			return nil, reject("malformed_payload"), false
		}
		if pred == rec.UUID {
			return nil, reject("self_supersession"), false
		}
	}
	out := append([]string(nil), sup.Supersedes...)
	sort.Strings(out)
	return out, evidencewire.RowOutcome{}, true
}
