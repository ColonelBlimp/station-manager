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
       reserved/unknown kind, an invalid (non-v7) uuid, a missing digest
       each answer permanent_reject(reason) on THEIR row while a valid
       row in the same batch lands accepted (§5.1's quarantine contract,
       server half).
   E7  Tenant scoping: the same (kind, uuid) under two tenants are
       independent rows — tenant B can neither read nor conflict with
       tenant A's digest.
   E8  Exactly one outcome per record, positionally, uuid echoed — the
       client treats any other shape as consuming no rows.
*/

import (
	"context"
	"encoding/json"
	"fmt"
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
	if err := s.db.QueryRow(`SELECT (payload->>'decode_count')::int FROM evidence_records WHERE uuid = $1`, uuid).Scan(&count); err != nil {
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
	reserved := evRec(t, evidencewire.KindRetention, utils.NewUUIDv7At(now), `{"anything":1}`)
	badUUID := evRec(t, evidencewire.KindCoverage, "not-a-uuid",
		`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":1}`)
	noDigest := evidencewire.Record{Kind: evidencewire.KindCoverage, UUID: utils.NewUUIDv7At(now),
		DigestV: 1, Digest: "", Payload: good.Payload}

	out := upsertEv(t, s, tid, malformed, reserved, badUUID, noDigest, good)
	for i, o := range out[:4] {
		if o.Outcome != evidencewire.OutcomePermanentReject || o.Reason == "" {
			t.Fatalf("E6: faulty record %d = %q/%q, want permanent_reject with a reason", i, o.Outcome, o.Reason)
		}
	}
	if out[4].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("E6: the valid batch-mate = %q, want accepted — row faults must not block it", out[4].Outcome)
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
