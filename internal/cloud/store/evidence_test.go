package store

/*
   §5 sync slice — the SMC ingest half of the acceptance set (server
   semantics behind SY2/SY3/SY4/SY6; the client half and the full SY1–SY9
   header live in internal/evidence's sync spec). Real Postgres via
   testStore — house rule, no mocks. Each rule names its nearest
   confusable behaviour:

   E1  Uniform upsert: a mixed four-kind batch stores every row and
       answers accepted per row — one contract, no kind exempted (§5.1).
   E2  already_present: re-offering (kind, uuid) with the SAME (digest_v,
       digest) is terminal-benign and leaves the stored row untouched —
       distinguishable from accepted (first arrival) and from E3.
   E3  Digest conflict: same (kind, uuid), different digest →
       permanent_reject("digest_conflict"), stored row untouched — never
       silently replaced or deduplicated (§5.2 "silently wronged").
   E4  Missing profile self-heals: an observation whose payload carries a
       non-null profile_uuid with no stored profile row →
       retryable_missing_profile and the row is NOT stored; after the
       profile lands, the SAME offer is accepted. Wire ordering carries
       nothing (§5.1): a single batch holding both the profile and its
       referencing observation succeeds in either order — the client's
       profile-first rule is SELECTION priority, not a wire requirement.
   E5  A NULL-profile observation is accepted as explicitly unprofiled,
       never retried (§5.4 amendment — legacy_unprofiled is
       indistinguishable from any other NULL here).
   E6  Row faults never block batch-mates: malformed payload, the
       unknown kind (the retention kind is live since the retention slice), an invalid (non-v7) uuid, a missing digest
       each answer permanent_reject(reason) on THEIR row while a valid
       row in the same batch lands accepted (§5.1's quarantine contract,
       server half).
   E7  Tenant scoping: the same (kind, uuid) under two tenants are
       independent rows — tenant B can neither read nor conflict with
       tenant A's digest.
   E8  Exactly one outcome per record, positionally, uuid echoed — the
       client treats any other shape as consuming no rows.
   E9  (codex-P1 fix, 2026-08-10) The digest is VERIFIED, never trusted:
       a well-formed digest that does not match the payload's canonical
       form → permanent_reject("digest_mismatch") and nothing stored — a
       digest-reusing client bug must not slip content past the identity
       guarantee (the confusable design: compare claimed digests only,
       which turns E3's loud conflict into silent dedup). An unsupported
       digest version → permanent_reject("unsupported_digest_version"):
       the server vouches only for identities it can compute.
   E10 (codex-P1 fix, 2026-08-10) Payloads are stored VERBATIM,
       byte-for-byte, so the stored digest stays verifiable against the
       stored payload forever. The confusable store is jsonb: it reorders
       keys (harmless to digest v1) but may also reformat numeric
       LEXEMES, which ARE digest content — a normalizing column corrupts
       identity on export/replay.
   E11 (retention slice, 2026-08-10) The reserved `retention` kind
       activates: a retention record stores and answers accepted under
       the unchanged contract — no kind is exempt from any rule.
   E12 (retention slice) Supersession is tombstone-then-delete, ONE
       transaction: a summary carrying `supersedes` deletes its DIRECT
       predecessors' rows after inserting persistent tombstones for them,
       so a later old-backup re-offer of a deleted predecessor answers
       `tombstoned` (that outcome's first activation) — without the
       tombstone, the confusable design quietly re-creates the
       predecessor and supersession is not idempotent. Re-offering the
       summary itself is already_present.
   E13 (retention slice) Tombstones gate EVERY kind's upsert, checked
       before storage — a tombstoned (tenant, kind, uuid) never re-enters
       the store, whatever content it carries.
*/

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

func evRec(t *testing.T, kind, uuid, payload string) evidencewire.Record {
	t.Helper()
	digest, err := evidencewire.DigestV1Hex([]byte(payload))
	if err != nil {
		t.Fatalf("digest %s: %v", kind, err)
	}
	return evidencewire.Record{
		Kind: kind, UUID: uuid,
		DigestV: evidencewire.DigestVersion1, Digest: digest,
		Payload: json.RawMessage(payload),
	}
}

func obsPayload(profileUUID string) string {
	p := "null"
	if profileUUID != "" {
		p = fmt.Sprintf("%q", profileUUID)
	}
	return fmt.Sprintf(`{"slot_start_utc":"2026-08-10T12:00:00Z","dial_mhz":14.074,"dial_tracked":true,`+
		`"freq_hz":1204.5,"dt_sec":0.3,"snr":-8,"payload":"ECA=","parse_status":"parsed",`+
		`"text":"CQ K1ABC FN42","prov_algorithm":"bp","decoder_build":"v0.9.0","profile_uuid":%s}`, p)
}

func upsertEv(t *testing.T, s *Store, tid int64, recs ...evidencewire.Record) []evidencewire.RowOutcome {
	t.Helper()
	out, err := s.UpsertEvidence(context.Background(), tid, recs)
	if err != nil {
		t.Fatalf("UpsertEvidence: %v", err)
	}
	if len(out) != len(recs) {
		t.Fatalf("E8: %d outcomes for %d records — want exactly one per record", len(out), len(recs))
	}
	for i, o := range out {
		if o.UUID != recs[i].UUID {
			t.Fatalf("E8: outcome %d echoes uuid %q, want %q (positional)", i, o.UUID, recs[i].UUID)
		}
	}
	return out
}

func evCount(t *testing.T, s *Store, tid int64, kind string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM evidence_records WHERE tenant_id = $1 AND kind = $2`, tid, kind).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", kind, err)
	}
	return n
}

func evScalar(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("scalar (%s): %v", query, err)
	}
	return n
}

func TestEvidence_MixedBatchAccepted(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	pUUID := utils.NewUUIDv7At(now)

	out := upsertEv(t, s, tid,
		evRec(t, evidencewire.KindProfile, pUUID, `{"lineage":"DX Commander","version":1,"valid_from":"2026-08-10T00:00:00Z","name":"DX Commander","bands":"30m,40m,80m","noise_floor":"not_measured"}`),
		evRec(t, evidencewire.KindObservation, utils.NewUUIDv7At(now), obsPayload(pUUID)),
		evRec(t, evidencewire.KindCoverage, utils.NewUUIDv7At(now), `{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":1}`),
		evRec(t, evidencewire.KindLossInterval, utils.NewUUIDv7At(now), `{"start_utc":"2026-08-10T11:00:00Z","end_utc":"2026-08-10T11:01:00Z","slots":4,"observations":9,"reason":"cap","remote_status":"never_offered","dial_mhz":14.074}`),
	)
	for i, o := range out {
		if o.Outcome != evidencewire.OutcomeAccepted {
			t.Fatalf("E1: record %d outcome = %q (%s), want accepted", i, o.Outcome, o.Reason)
		}
	}
	for _, kind := range []string{"observation", "coverage", "loss_interval", "profile"} {
		if n := evCount(t, s, tid, kind); n != 1 {
			t.Fatalf("E1: %d stored %s rows, want 1", n, kind)
		}
	}
}

func TestEvidence_AlreadyPresentLeavesRowUntouched(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	rec := evRec(t, evidencewire.KindCoverage, utils.NewUUIDv7At(time.Now()),
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":3}`)

	if out := upsertEv(t, s, tid, rec); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("first offer = %q, want accepted", out[0].Outcome)
	}
	var before time.Time
	if err := s.db.QueryRow(`SELECT received_at FROM evidence_records WHERE uuid = $1`, rec.UUID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if out := upsertEv(t, s, tid, rec); out[0].Outcome != evidencewire.OutcomeAlreadyPresent {
		t.Fatalf("E2: re-offer = %q, want already_present (the restored-backup case)", out[0].Outcome)
	}
	var after time.Time
	if err := s.db.QueryRow(`SELECT received_at FROM evidence_records WHERE uuid = $1`, rec.UUID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before) {
		t.Fatalf("E2: re-offer touched the stored row (received_at %v → %v)", before, after)
	}
}

func TestEvidence_DigestConflictRejectsAndPreserves(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	uuid := utils.NewUUIDv7At(time.Now())
	orig := evRec(t, evidencewire.KindCoverage, uuid,
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":3}`)
	upsertEv(t, s, tid, orig)

	altered := evRec(t, evidencewire.KindCoverage, uuid,
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":9}`)
	out := upsertEv(t, s, tid, altered)
	if out[0].Outcome != evidencewire.OutcomePermanentReject || out[0].Reason != "digest_conflict" {
		t.Fatalf("E3: altered re-offer = %q/%q, want permanent_reject/digest_conflict", out[0].Outcome, out[0].Reason)
	}
	var count int
	if err := s.db.QueryRow(`SELECT (payload::jsonb->>'decode_count')::int FROM evidence_records WHERE uuid = $1`, uuid).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("E3: stored row was altered (decode_count %d, want the original 3)", count)
	}
}

func TestEvidence_MissingProfileRetryableThenHeals(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	pUUID := utils.NewUUIDv7At(now)
	obs := evRec(t, evidencewire.KindObservation, utils.NewUUIDv7At(now), obsPayload(pUUID))

	out := upsertEv(t, s, tid, obs)
	if out[0].Outcome != evidencewire.OutcomeRetryableMissingProfile {
		t.Fatalf("E4: unknown profile ref = %q, want retryable_missing_profile", out[0].Outcome)
	}
	if n := evCount(t, s, tid, "observation"); n != 0 {
		t.Fatalf("E4: retryable row was stored (%d rows) — it must not be", n)
	}

	profile := evRec(t, evidencewire.KindProfile, pUUID, `{"lineage":"DX Commander","version":1,"valid_from":"2026-08-10T00:00:00Z","name":"DX Commander","bands":"30m,40m,80m","noise_floor":"not_measured"}`)
	upsertEv(t, s, tid, profile)
	if out := upsertEv(t, s, tid, obs); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E4: post-profile re-offer = %q, want accepted (the self-heal)", out[0].Outcome)
	}

	// Wire ordering carries nothing: observation BEFORE its profile in one
	// batch must still fully succeed.
	p2 := utils.NewUUIDv7At(now)
	out = upsertEv(t, s, tid,
		evRec(t, evidencewire.KindObservation, utils.NewUUIDv7At(now), obsPayload(p2)),
		evRec(t, evidencewire.KindProfile, p2, `{"lineage":"VHQ Hex beam","version":1,"valid_from":"2026-08-10T00:00:00Z","name":"VHQ Hex beam","bands":"20m","noise_floor":"not_measured"}`),
	)
	if out[0].Outcome != evidencewire.OutcomeAccepted || out[1].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E4: obs-before-profile batch = %q/%q, want accepted/accepted — wire order must carry nothing", out[0].Outcome, out[1].Outcome)
	}
}

func TestEvidence_NullProfileAcceptedAsUnprofiled(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	out := upsertEv(t, s, tid, evRec(t, evidencewire.KindObservation, utils.NewUUIDv7At(time.Now()), obsPayload("")))
	if out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E5: null-profile observation = %q, want accepted — explicitly unprofiled, never retried", out[0].Outcome)
	}
}

func TestEvidence_RowFaultsDoNotBlockBatchmates(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	good := evRec(t, evidencewire.KindCoverage, utils.NewUUIDv7At(now),
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":1}`)
	malformed := evidencewire.Record{Kind: evidencewire.KindCoverage, UUID: utils.NewUUIDv7At(now),
		DigestV: 1, Digest: good.Digest, Payload: json.RawMessage(`{"unterminated":`)}
	unknown := evRec(t, "telemetry", utils.NewUUIDv7At(now), `{"anything":1}`)
	badUUID := evRec(t, evidencewire.KindCoverage, "not-a-uuid",
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":1}`)
	noDigest := evidencewire.Record{Kind: evidencewire.KindCoverage, UUID: utils.NewUUIDv7At(now),
		DigestV: 1, Digest: "", Payload: good.Payload}

	out := upsertEv(t, s, tid, malformed, unknown, badUUID, noDigest, good)
	for i, o := range out[:4] {
		if o.Outcome != evidencewire.OutcomePermanentReject || o.Reason == "" {
			t.Fatalf("E6: faulty record %d = %q/%q, want permanent_reject with a reason", i, o.Outcome, o.Reason)
		}
	}
	if out[4].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E6: the valid batch-mate = %q, want accepted — row faults must not block it", out[4].Outcome)
	}
}

func TestEvidence_DigestIsVerifiedNotTrusted(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	payload := `{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":1}`

	forged := evRec(t, evidencewire.KindCoverage, utils.NewUUIDv7At(now), payload)
	forged.Digest = strings.Repeat("a", 64) // well-formed, wrong for this payload
	out := upsertEv(t, s, tid, forged)
	if out[0].Outcome != evidencewire.OutcomePermanentReject || out[0].Reason != "digest_mismatch" {
		t.Fatalf("E9: forged digest = %q/%q, want permanent_reject/digest_mismatch — a claimed digest is not identity", out[0].Outcome, out[0].Reason)
	}
	if n := evCount(t, s, tid, "coverage"); n != 0 {
		t.Fatalf("E9: %d rows stored under a forged digest, want 0", n)
	}

	v2 := evRec(t, evidencewire.KindCoverage, utils.NewUUIDv7At(now), payload)
	v2.DigestV = 2
	out = upsertEv(t, s, tid, v2)
	if out[0].Outcome != evidencewire.OutcomePermanentReject || out[0].Reason != "unsupported_digest_version" {
		t.Fatalf("E9: unknown digest version = %q/%q, want permanent_reject/unsupported_digest_version", out[0].Outcome, out[0].Reason)
	}
}

func TestEvidence_PayloadStoredByteForByte(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	// Deliberately non-canonical: unsorted keys AND a trailing-zero numeric
	// lexeme — the exact distinctions a normalizing store destroys.
	payload := `{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.0740,"dial_tracked":true,"decode_count":1}`
	rec := evRec(t, evidencewire.KindCoverage, utils.NewUUIDv7At(time.Now()), payload)
	if out := upsertEv(t, s, tid, rec); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("offer = %q (%s), want accepted", out[0].Outcome, out[0].Reason)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT payload FROM evidence_records WHERE uuid = $1`, rec.UUID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != payload {
		t.Fatalf("E10: stored payload differs from the submitted bytes:\n got %s\nwant %s", stored, payload)
	}
	re, err := evidencewire.DigestV1Hex([]byte(stored))
	if err != nil || re != rec.Digest {
		t.Fatalf("E10: stored payload no longer verifies against its digest (recomputed %q, stored %q, err %v)", re, rec.Digest, err)
	}
}

func retentionPayload(supersedes string) string {
	base := `{"start_utc":"2026-08-10T10:00:00Z","end_utc":"2026-08-10T11:00:00Z","observations":120,"coverage":240,"reason":"cap","acknowledged":true`
	if supersedes != "" {
		return base + `,"supersedes":` + supersedes + `}`
	}
	return base + `}`
}

// Package review (2026-08-10): an observation whose non-null profile_uuid is
// not a valid v7 UUID must be a PER-ROW permanent_reject, never a batch abort.
// The raw string used to reach a Postgres UUID comparison, whose syntax error
// rolled back the whole transaction (HTTP 500) and took valid batch-mates with
// it; an empty string was silently treated as unprofiled; a valid non-v7 UUID
// retried forever though such a profile can never be ingested.
func TestEvidence_InvalidProfileRefRejectedNotAborted(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	// obsPayloadRef puts an explicit profile_uuid JSON value in place (so the
	// empty-string and invalid-syntax cases are reachable — obsPayload maps ""
	// onto JSON null).
	obsPayloadRef := func(profileJSON string) string {
		return fmt.Sprintf(`{"slot_start_utc":"2026-08-10T12:00:00Z","dial_mhz":14.074,"dial_tracked":true,`+
			`"freq_hz":1204.5,"dt_sec":0.3,"snr":-8,"payload":"ECA=","parse_status":"parsed",`+
			`"text":"CQ K1ABC FN42","prov_algorithm":"bp","decoder_build":"v0.9.0","profile_uuid":%s}`, profileJSON)
	}

	cases := []struct{ name, profileJSON string }{
		{"invalid syntax", `"not-a-uuid"`},
		{"empty string", `""`},
		{"valid but non-v7", `"0197f9a0-0000-4000-8000-000000000001"`}, // version 4, never ingestable
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := evRec(t, evidencewire.KindObservation, utils.NewUUIDv7At(now), obsPayloadRef(c.profileJSON))
			good := evRec(t, evidencewire.KindCoverage, utils.NewUUIDv7At(now),
				`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":1}`)
			out, err := s.UpsertEvidence(context.Background(), tid, []evidencewire.Record{bad, good})
			if err != nil {
				t.Fatalf("P1: an invalid profile ref must be a per-row reject, not a batch abort: %v", err)
			}
			if out[0].Outcome != evidencewire.OutcomePermanentReject || out[0].Reason != "invalid_profile_ref" {
				t.Fatalf("P1: bad ref = %q/%q, want permanent_reject/invalid_profile_ref", out[0].Outcome, out[0].Reason)
			}
			if out[1].Outcome != evidencewire.OutcomeAccepted {
				t.Fatalf("P1: valid batch-mate = %q, want accepted (not rolled back)", out[1].Outcome)
			}
		})
	}
}

// Package review (2026-08-10): a summary whose insert is REJECTED (digest
// conflict) must NOT tombstone or delete its predecessors — supersession only
// takes effect for an accepted summary, or evidence is permanently removed
// with no accepted replacement.
func TestEvidence_ConflictingSummaryDoesNotSupersede(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()

	// R (uuid U) is already stored with content A; Q is a live predecessor.
	rUUID := utils.NewUUIDv7At(now)
	upsertEv(t, s, tid, evRec(t, evidencewire.KindRetention, rUUID, retentionPayload("")))
	qUUID := utils.NewUUIDv7At(now)
	upsertEv(t, s, tid, evRec(t, evidencewire.KindRetention, qUUID, retentionPayload("")))

	// A DIFFERENT-content summary re-uses U (→ digest_conflict) and lists Q.
	altered := retentionPayload(fmt.Sprintf(`[%q]`, qUUID))
	altered = altered[:len(altered)-1] + `,"note":"different"}`
	conflicting := evRec(t, evidencewire.KindRetention, rUUID, altered)
	out := upsertEv(t, s, tid, conflicting)
	if out[0].Outcome != evidencewire.OutcomePermanentReject || out[0].Reason != "digest_conflict" {
		t.Fatalf("fixture: want digest_conflict, got %q/%q", out[0].Outcome, out[0].Reason)
	}
	// Q must SURVIVE — the rejected summary must not have superseded it.
	if n := evScalar(t, s, `SELECT COUNT(*) FROM evidence_records WHERE uuid = $1`, qUUID); n != 1 {
		t.Fatal("P1: a digest-conflicting summary deleted its predecessor Q")
	}
	if out := upsertEv(t, s, tid, evRec(t, evidencewire.KindRetention, qUUID, retentionPayload(""))); out[0].Outcome != evidencewire.OutcomeAlreadyPresent {
		t.Fatalf("P1: Q was tombstoned by a rejected summary (re-offer = %q, want already_present)", out[0].Outcome)
	}
}

// Package review (2026-08-10): a summary may not supersede itself (would create
// a live record AND its own tombstone), and the supersedes list is bounded so
// one request cannot hold a transaction across an unbounded number of round
// trips.
func TestEvidence_SupersedesBounds(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()

	selfUUID := utils.NewUUIDv7At(now)
	self := evRec(t, evidencewire.KindRetention, selfUUID, retentionPayload(fmt.Sprintf(`[%q]`, selfUUID)))
	if out := upsertEv(t, s, tid, self); out[0].Outcome != evidencewire.OutcomePermanentReject || out[0].Reason != "self_supersession" {
		t.Fatalf("P2: self-supersession = %q/%q, want permanent_reject/self_supersession", out[0].Outcome, out[0].Reason)
	}
	if n := evScalar(t, s, `SELECT COUNT(*) FROM evidence_records WHERE uuid = $1`, selfUUID); n != 0 {
		t.Fatal("P2: a self-superseding summary must store nothing")
	}
	if n := evScalar(t, s, `SELECT COUNT(*) FROM evidence_tombstones WHERE uuid = $1`, selfUUID); n != 0 {
		t.Fatal("P2: a self-superseding summary must tombstone nothing")
	}

	// Over the per-summary cap.
	preds := make([]string, maxSupersedesPerSummary+1)
	for i := range preds {
		preds[i] = fmt.Sprintf("%q", utils.NewUUIDv7At(now))
	}
	over := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now),
		retentionPayload("["+strings.Join(preds, ",")+"]"))
	if out := upsertEv(t, s, tid, over); out[0].Outcome != evidencewire.OutcomePermanentReject || out[0].Reason != "too_many_supersedes" {
		t.Fatalf("P2: oversized supersedes = %q/%q, want permanent_reject/too_many_supersedes", out[0].Outcome, out[0].Reason)
	}
}

// Package review (2026-08-10): a concurrent re-offer of a predecessor must not
// resurrect it past a supersession. The tombstone probe and the record insert
// touch separate tables, so under READ COMMITTED a re-offer that saw no
// tombstone could commit its insert after another transaction tombstoned and
// deleted that UUID. SERIALIZABLE + retry closes it: the re-offer aborts on the
// conflict and, on retry, sees the tombstone. The hook forces the interleaving
// deterministically.
func TestEvidence_ConcurrentReofferCannotResurrect(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	pUUID := utils.NewUUIDv7At(now)
	pRec := evRec(t, evidencewire.KindRetention, pUUID, retentionPayload(""))
	upsertEv(t, s, tid, pRec) // P is live

	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	afterTombstoneProbeHook = func(kind, uuid string) {
		if kind == evidencewire.KindRetention && uuid == pUUID {
			once.Do(func() { close(reached); <-release })
		}
	}
	defer func() { afterTombstoneProbeHook = nil }()

	// A: re-offer P; it probes (no tombstone), then blocks before its insert.
	aErr := make(chan error, 1)
	go func() {
		_, err := s.UpsertEvidence(context.Background(), tid, []evidencewire.Record{pRec})
		aErr <- err
	}()

	<-reached
	// B: a summary supersedes P (tombstone + delete), committing while A waits.
	summary := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now),
		retentionPayload(fmt.Sprintf(`[%q]`, pUUID)))
	if out := upsertEv(t, s, tid, summary); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("fixture: summary = %q, want accepted", out[0].Outcome)
	}

	close(release) // A resumes: inserts P, then commits (or serialization-fails + retries)
	if err := <-aErr; err != nil {
		t.Fatalf("A batch failed: %v", err)
	}

	// P must NOT be resurrected: it is tombstoned and NOT live.
	if tomb := evScalar(t, s, `SELECT COUNT(*) FROM evidence_tombstones WHERE uuid = $1`, pUUID); tomb != 1 {
		t.Fatalf("P1: P must be tombstoned by the supersession (tombstones=%d)", tomb)
	}
	if live := evScalar(t, s, `SELECT COUNT(*) FROM evidence_records WHERE uuid = $1`, pUUID); live != 0 {
		t.Fatalf("P1: P was RESURRECTED — %d live record(s) exist after supersession", live)
	}
}

func TestEvidence_RetentionKindActivates(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	rec := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(time.Now()), retentionPayload(""))
	if out := upsertEv(t, s, tid, rec); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E11: retention record = %q (%s), want accepted — the reserved kind is live", out[0].Outcome, out[0].Reason)
	}
	if n := evCount(t, s, tid, "retention"); n != 1 {
		t.Fatalf("E11: %d stored retention rows, want 1", n)
	}
}

func TestEvidence_SupersessionTombstonesThenDeletes(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	predA := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now), retentionPayload(""))
	predB := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now), retentionPayload(""))
	upsertEv(t, s, tid, predA, predB)

	summary := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now),
		retentionPayload(fmt.Sprintf(`[%q,%q]`, predA.UUID, predB.UUID)))
	if out := upsertEv(t, s, tid, summary); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E12: summary = %q (%s), want accepted", out[0].Outcome, out[0].Reason)
	}
	if n := evCount(t, s, tid, "retention"); n != 1 {
		t.Fatalf("E12: %d retention rows after supersession, want 1 (the summary; predecessors deleted)", n)
	}
	// The deleted predecessor is TOMBSTONED: an old backup's re-offer must
	// answer tombstoned, never quietly re-create the row.
	if out := upsertEv(t, s, tid, predA); out[0].Outcome != evidencewire.OutcomeTombstoned {
		t.Fatalf("E12: re-offered predecessor = %q, want tombstoned — supersession must be idempotent", out[0].Outcome)
	}
	if n := evCount(t, s, tid, "retention"); n != 1 {
		t.Fatalf("E12: predecessor re-offer re-created a row (%d rows)", n)
	}
	// The summary itself re-offers as already_present, unchanged contract.
	if out := upsertEv(t, s, tid, summary); out[0].Outcome != evidencewire.OutcomeAlreadyPresent {
		t.Fatalf("E12: summary re-offer = %q, want already_present", out[0].Outcome)
	}

	// Loss-interval supersession follows the identical rule.
	lossA := evRec(t, evidencewire.KindLossInterval, utils.NewUUIDv7At(now),
		`{"start_utc":"2026-08-10T09:00:00Z","end_utc":"2026-08-10T09:01:00Z","slots":4,"observations":9,"reason":"cap","remote_status":"never_offered","dial_mhz":14.074}`)
	upsertEv(t, s, tid, lossA)
	lossSummary := evRec(t, evidencewire.KindLossInterval, utils.NewUUIDv7At(now),
		fmt.Sprintf(`{"start_utc":"2026-08-10T09:00:00Z","end_utc":"2026-08-10T09:05:00Z","slots":20,"observations":45,"reason":"cap","remote_status":"never_offered","dial_mhz":14.074,"supersedes":[%q]}`, lossA.UUID))
	if out := upsertEv(t, s, tid, lossSummary); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E12: loss summary = %q (%s), want accepted", out[0].Outcome, out[0].Reason)
	}
	if out := upsertEv(t, s, tid, lossA); out[0].Outcome != evidencewire.OutcomeTombstoned {
		t.Fatalf("E12: superseded loss re-offer = %q, want tombstoned", out[0].Outcome)
	}
}

func TestEvidence_TombstonesGateEveryKind(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q5MLV")
	now := time.Now()
	pred := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now), retentionPayload(""))
	upsertEv(t, s, tid, pred)
	summary := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now),
		retentionPayload(fmt.Sprintf(`[%q]`, pred.UUID)))
	upsertEv(t, s, tid, summary)

	// DIFFERENT content under the tombstoned identity: still tombstoned —
	// the gate runs before any digest/storage logic (E13).
	altered := evRec(t, evidencewire.KindRetention, pred.UUID, retentionPayload(""))
	altered.Payload = json.RawMessage(`{"start_utc":"2000-01-01T00:00:00Z","end_utc":"2000-01-01T01:00:00Z","observations":1,"coverage":1,"reason":"cap","acknowledged":true}`)
	d, err := evidencewire.DigestV1Hex(altered.Payload)
	if err != nil {
		t.Fatal(err)
	}
	altered.Digest = d
	if out := upsertEv(t, s, tid, altered); out[0].Outcome != evidencewire.OutcomeTombstoned {
		t.Fatalf("E13: altered offer under a tombstoned identity = %q, want tombstoned", out[0].Outcome)
	}
}

func TestEvidence_TenantScoped(t *testing.T) {
	s := testStore(t)
	tidA, _ := seedTenantLogbook(t, s, "7Q5MLV")
	tidB, _ := seedTenantLogbook(t, s, "7Q8AC")
	uuid := utils.NewUUIDv7At(time.Now())

	a := evRec(t, evidencewire.KindCoverage, uuid,
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":1}`)
	b := evRec(t, evidencewire.KindCoverage, uuid,
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"no_decode","dial_mhz":7.074,"dial_tracked":true,"decode_count":0}`)
	if out := upsertEv(t, s, tidA, a); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("tenant A offer = %q, want accepted", out[0].Outcome)
	}
	if out := upsertEv(t, s, tidB, b); out[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E7: tenant B same-uuid different-content offer = %q, want accepted — tenants are independent", out[0].Outcome)
	}
}
