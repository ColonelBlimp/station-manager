package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// UpsertEvidence ingests one §5 sync batch for a tenant: exactly one outcome
// per record, positionally, uuid echoed. Row faults answer on their own row
// and never abort batch-mates; an error return means the BATCH failed as a
// unit (server fault) and no outcome may be trusted.
//
// One transaction covers the batch, and profile records are processed before
// every other kind REGARDLESS of wire order — §5.1 pins that ordering on the
// wire carries nothing, so a batch holding an observation before the profile
// it references must succeed; the client's profile-first rule is selection
// priority on its side, not a protocol requirement.
func (s *Store) UpsertEvidence(ctx context.Context, tenantID int64, recs []evidencewire.Record) ([]evidencewire.RowOutcome, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("smcloud: begin evidence batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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
		evidencewire.KindLossInterval, evidencewire.KindProfile:
	default:
		// Includes the reserved retention kind until the retention slice.
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

	if rec.Kind == evidencewire.KindObservation {
		var ref struct {
			ProfileUUID *string `json:"profile_uuid"`
		}
		if err := json.Unmarshal(rec.Payload, &ref); err != nil {
			return reject("malformed_payload"), nil
		}
		// A NULL profile is EXPLICITLY unprofiled (§5.4 amendment) — accepted,
		// never retried. Only a non-null reference is checked for existence.
		if ref.ProfileUUID != nil && *ref.ProfileUUID != "" {
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

	res, err := tx.ExecContext(ctx,
		`INSERT INTO evidence_records (tenant_id, kind, uuid, digest_v, digest, payload)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id, kind, uuid) DO NOTHING`,
		tenantID, rec.Kind, rec.UUID, rec.DigestV, rec.Digest, string(rec.Payload))
	if err != nil {
		return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: insert evidence record: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return evidencewire.RowOutcome{}, fmt.Errorf("smcloud: evidence rows-affected: %w", err)
	} else if n == 1 {
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}, nil
	}

	// Conflict: the digest decides between the benign restored-backup
	// re-offer and a client bug — never silently deduplicate (§5.2).
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
