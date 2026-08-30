package qsoservice

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/bands"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// preserveSeconds protects stored seconds on an edit: when an incoming time
// arrives at coarse HHMM precision but the stored value has HHMMSS for the SAME
// minute, the stored (seconds-bearing) value is kept — editing an unrelated
// field must never silently drop precision the QSO already had. An incoming
// value that changes the minute, or that itself carries seconds, wins. Empty
// incoming falls through to the caller's required-field validation.
func preserveSeconds(incoming, existing string) string {
	if len(incoming) == 4 && len(existing) == 6 && existing[:4] == incoming {
		return existing
	}
	return incoming
}

// Update overlays a JSON patch body onto existing, validates the merged
// QSO, recomputes the dedupe key if any of its inputs changed (and rejects
// on collision), then persists. Returns the updated QSO.
//
// JSON keys not present in the body leave the corresponding field
// unchanged. Immutable fields — QSO identity, logbook, station callsign,
// dedupe key, forwarding state, enrichment — are always restored from
// existing, so clients cannot rewrite them via PATCH.
//
// Validation errors come back as *SubmitError. A dedupe collision is
// reported as *SubmitError with Code "duplicate_key" so the handler maps
// it to 409.
//
// src identifies which subsystem of the daemon initiated the edit; it
// is recorded on the qso_history audit row written inside the same
// transaction as the QSO update (ADR 0016 prep #2). The audit row's
// before_image is json.Marshal(existing) — the pre-edit snapshot, not
// the merged result — so replaying audit gives the row's history of
// states.
func (s *Service) Update(ctx context.Context, existing types.Qso, body []byte, src source.Source) (types.Qso, error) {
	const op errors.Op = "qsoservice.Update"

	// Snapshot the pre-edit state for the audit row NOW, before the merge below.
	// The merge unmarshals into a shallow copy of `existing`, and the one
	// reference-typed field (ContactHistory []ContactHistory) shares its backing
	// array — so a `contact_history` body would decode in place and mutate
	// `existing`'s elements. Marshalling here (not after the merge) captures the
	// true pre-edit state, which the audit row exists to record and which SM Cloud
	// sync consumes (ADR 0016). Latent today (stored rows carry no contact_history),
	// but cheap insurance.
	beforeImage, err := json.Marshal(existing)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to marshal pre-edit snapshot")
	}

	merged := existing
	// An empty body carries no edits — treat it as the no-op {} (the API handler maps
	// it the same way). Only a non-empty body is parsed, so a genuinely malformed body
	// still fails as invalid_json rather than being silently accepted.
	if len(body) > 0 {
		if err := json.Unmarshal(body, &merged); err != nil {
			return types.Qso{}, &SubmitError{Code: "invalid_json", Message: "failed to parse request body"}
		}
	}

	// ---- Restore immutables ----
	// Structural identity — set at creation, never editable.
	merged.ID = existing.ID
	merged.UUID = existing.UUID
	merged.LogbookID = existing.LogbookID
	merged.DedupeKey = existing.DedupeKey
	merged.LoggingStation.StationCallsign = existing.LoggingStation.StationCallsign
	// Forwarding state — owned by the forwarder, not the client.
	merged.SmQsoUploadDate = existing.SmQsoUploadDate
	merged.SmQsoUploadStatus = existing.SmQsoUploadStatus
	merged.SmFwrdByEmailDate = existing.SmFwrdByEmailDate
	merged.SmFwrdByEmailStatus = existing.SmFwrdByEmailStatus
	merged.QrzComUploadDate = existing.QrzComUploadDate
	merged.QrzComUploadStatus = existing.QrzComUploadStatus
	merged.ClubLogUploadDate = existing.ClubLogUploadDate
	// clublog_qso_upload_status is the JSON path the ADR-0039 stamp machinery
	// reads (HasUploadStamp / missing_from filter), so a client must not be able
	// to forge or clear the "already uploaded" signal that drives the backfill.
	merged.ClubLogUploadStatus = existing.ClubLogUploadStatus
	// QRZ Logbook per-QSO LOGID is imported provenance, not client-editable.
	merged.QrzlogLogid = existing.QrzlogLogid
	// Enrichment — populated by services, not user input.
	merged.CountryDetails = existing.CountryDetails
	merged.ContactHistory = existing.ContactHistory

	// ---- Normalize ----
	// Idempotent on canonical form: existing stored data is already in
	// canonical form (Submit does the same normalizations), so this only
	// has effect on fields the patch actually touched.
	merged.ContactedStation.Call = strings.ToUpper(strings.TrimSpace(merged.ContactedStation.Call))
	merged.QsoDetails.Band = strings.ToLower(strings.TrimSpace(merged.QsoDetails.Band))
	merged.QsoDetails.Mode = strings.ToUpper(strings.TrimSpace(merged.QsoDetails.Mode))
	merged.QsoDetails.Submode = strings.ToUpper(strings.TrimSpace(merged.QsoDetails.Submode))
	merged.QsoDetails.QsoDate = utils.SanitizeDateToYYYYMMDD(strings.TrimSpace(merged.QsoDetails.QsoDate))
	if raw := strings.TrimSpace(merged.QsoDetails.QsoDateOff); raw != "" {
		// A non-empty value that sanitizes to empty is malformed — reject it
		// rather than silently blanking it (which would then mis-report an
		// overnight QSO as missing its QSO_DATE_OFF).
		merged.QsoDetails.QsoDateOff = utils.SanitizeDateToYYYYMMDD(raw)
		if merged.QsoDetails.QsoDateOff == "" {
			return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "qso_date_off is not a valid date"}
		}
	} else {
		merged.QsoDetails.QsoDateOff = ""
	}
	// Store time at native precision (HHMM or HHMMSS) — no longer truncated. An
	// edit that arrives at coarser HHMM for the SAME minute keeps the stored
	// seconds (preserveSeconds), so editing an unrelated field on an FT8 QSO can't
	// silently drop its seconds. A changed minute, or supplied seconds, wins.
	merged.QsoDetails.TimeOn = preserveSeconds(
		utils.SanitizeTimeToADIF(strings.TrimSpace(merged.QsoDetails.TimeOn)), existing.QsoDetails.TimeOn)
	merged.QsoDetails.TimeOff = preserveSeconds(
		utils.SanitizeTimeToADIF(strings.TrimSpace(merged.QsoDetails.TimeOff)), existing.QsoDetails.TimeOff)
	merged.QsoDetails.RstSent = strings.TrimSpace(merged.QsoDetails.RstSent)
	merged.QsoDetails.RstRcvd = strings.TrimSpace(merged.QsoDetails.RstRcvd)
	merged.ContactedStation.Country = strings.TrimSpace(merged.ContactedStation.Country)
	if freq := strings.TrimSpace(merged.QsoDetails.Freq); freq != "" {
		kHz, err := utils.ParseFreqMHz(freq)
		if err != nil {
			return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("freq %q: %v", freq, err)}
		}
		merged.QsoDetails.Freq = utils.FormatFreqMHz(kHz)
		// BAND is a function of FREQ, so derive it from the canonical freq — the edit
		// overlay sends the OLD band on a VFO freq change, which would otherwise
		// persist an impossible BAND/FREQ pair (and a wrong dedupe key, and
		// contradictory ADIF to forwarders). Review 2026-06-19 M2. A freq in no
		// recognised band is rejected: IsValidBand and the freq→band table cover the
		// same 17 bands, so an unmapped freq is genuinely out-of-band, not a valid
		// band the table merely misses (2026-07-21 review finding 2). Symmetric with
		// Submit.
		derived := utils.FrequencyToBand(merged.QsoDetails.Freq)
		if derived == "" {
			return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("freq %s is not within a recognised amateur band", merged.QsoDetails.Freq)}
		}
		merged.QsoDetails.Band = strings.ToLower(derived)
	}

	// ---- Validate required-field invariants on the merged result ----
	if merged.ContactedStation.Call == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "call cannot be empty"}
	}
	if !IsValidCallsign(merged.ContactedStation.Call) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "call must be 3-32 characters and contain at least one digit"}
	}
	if merged.QsoDetails.Band == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "band cannot be empty"}
	}
	if !bands.IsValidBand(merged.QsoDetails.Band) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("band %q is not a recognised band", merged.QsoDetails.Band)}
	}
	if merged.QsoDetails.Mode == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "mode cannot be empty"}
	}
	if !modes.IsValidMode(merged.QsoDetails.Mode) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("mode %q is not a recognised mode", merged.QsoDetails.Mode)}
	}
	// Symmetric with Submit: a patch that names only MODE leaves the stored SUBMODE
	// behind, so the pair rejected at creation would otherwise re-form here and be
	// persisted + enqueued to update-capable forwarders (codex fcd45c45 P2).
	if err := validateSubmodeMatchesMode(merged.QsoDetails.Mode, merged.QsoDetails.Submode); err != nil {
		return types.Qso{}, err
	}
	// FREQ is required, mirroring Submit. A PATCH with an empty/whitespace freq
	// skips the normalization above and would otherwise reach the dedupe-key
	// ParseFreqMHz with an empty string, failing deep in the adapter as a 500
	// instead of a clean 400.
	if merged.QsoDetails.Freq == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "freq cannot be empty"}
	}
	if merged.QsoDetails.QsoDate == "" || !utils.IsValidDateYYYYMMDD(merged.QsoDetails.QsoDate) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "qso_date is not a valid date (expected YYYYMMDD)"}
	}
	if merged.QsoDetails.TimeOn == "" || !utils.IsValidTimeADIF(merged.QsoDetails.TimeOn) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "time_on is not a valid time (expected HHMM or HHMMSS)"}
	}
	if merged.QsoDetails.TimeOff == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "time_off cannot be empty"}
	}
	if !utils.IsValidTimeADIF(merged.QsoDetails.TimeOff) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "time_off is not a valid time (expected HHMM or HHMMSS)"}
	}
	// RST is mode-aware (review 2026-06-19 M1). FT8 reports are signed SNR in dB
	// and a caller-side bare-roger QSO can legitimately have no RST_RCVD — Submit
	// leaves FT8 reports empty rather than fabricating "59", so Update must not
	// reject an empty FT8 report either (otherwise a no-op or comment-only edit of
	// a valid bare-roger FT8 QSO fails with a 400). Phone/CW keep the requirement.
	if merged.QsoDetails.Mode != "FT8" {
		if merged.QsoDetails.RstSent == "" {
			return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "rst_sent cannot be empty"}
		}
		if merged.QsoDetails.RstRcvd == "" {
			return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "rst_rcvd cannot be empty"}
		}
	}
	if merged.ContactedStation.Country == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "country cannot be empty"}
	}
	// Schema-mirroring length caps, shared with prepareQso (review 2026-08-07 #3).
	if err := validateSchemaLengths(merged.QsoDetails.RstSent, merged.QsoDetails.RstRcvd,
		merged.ContactedStation.Country, false); err != nil {
		return types.Qso{}, err
	}

	// ---- Time coherence ----
	// Both directions, seconds-aware where both times carry them — the shared
	// validateTimeCoherence (review 2026-08-07 #4).
	if err := validateTimeCoherence(merged.QsoDetails.TimeOn, merged.QsoDetails.TimeOff,
		merged.QsoDetails.QsoDate, merged.QsoDetails.QsoDateOff, false); err != nil {
		return types.Qso{}, err
	}

	// ---- Dedupe recompute + collision check ----
	if err := s.recomputeDedupeKey(ctx, op, &merged, existing); err != nil {
		return types.Qso{}, err
	}

	// ---- No-effective-change short-circuit (AW-3) ----
	if result, handled, nerr := s.resolveNoOp(ctx, op, merged, existing); handled {
		return result, nerr
	}

	// ---- Atomic write: QSO update + update-action upload rows + qso_history ----
	// Symmetric with Submit: both ingest paths write under the same
	// one-fails-all-fail contract. A single transaction carries the QSO update,
	// the action.Update upload-queue rows for configured forwarders, and the
	// qso_history audit row.
	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to begin transaction")
	}
	defer cancel()

	if err = s.DB.UpdateQsoTx(ctx, tx, merged); err != nil {
		s.rollbackTx(tx, op)

		// Race window symmetric with Submit's post-InsertQsoTx UNIQUE
		// handler: the pre-tx dedupe-collision check above can return
		// ErrNotFound while a concurrent Submit/Update is committing
		// the same dedupe key, then this UPDATE hits the UNIQUE index
		// on (logbook_id, dedupe_key) WHERE deleted_at IS NULL. From
		// the client's point of view this is a duplicate-key conflict,
		// not a daemon error — translate it so the handler maps to 409
		// the same way the pre-check path does.
		if sqlite.IsUniqueConstraintError(err) {
			return types.Qso{}, &SubmitError{
				Code:    "duplicate_key",
				Message: "edit would collide with another QSO in this logbook",
			}
		}
		// The revision guard refused a stale snapshot (review 2026-08-07 #2): a
		// concurrent edit committed after this request fetched the row. Without
		// the refusal the second write would silently revert the first edit's
		// unrelated fields and append a duplicate before-image to the audit
		// chain. 409: the caller re-fetches and re-applies.
		if stderr.Is(err, errors.ErrStaleRevision) {
			return types.Qso{}, &SubmitError{
				Code:    "edit_conflict",
				Message: "the QSO changed while this edit was in flight — reload it and re-apply the edit",
			}
		}
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to update QSO")
	}

	// Enqueue one qso_upload row per ENABLED forwarder whose action_filter
	// includes 'update' (ADR 0039: `enabled` gates enqueue). Same tx as the QSO
	// update per the one-fails-all-fail
	// invariant. If the destination's filter is ["insert"] (LoTW-style write-
	// once), no row is inserted for it and the edit simply doesn't propagate
	// there — which matches the operator's declared intent.
	for _, fwd := range s.Config.Forwarders() {
		if !shouldEnqueue(fwd, action.Update) {
			continue
		}
		if err = s.DB.InsertQsoUploadTx(ctx, tx, merged.ID, action.Update, fwd.Name, fwd.Type, origin.Edit); err != nil {
			s.rollbackTx(tx, op)
			return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
		}
	}

	// Append the audit row inside the same tx (ADR 0016 prep #2). beforeImage was
	// marshalled at the top of Update — BEFORE the merge that can mutate existing's
	// shared ContactHistory backing array — so it holds the true pre-edit state,
	// not json.Marshal(merged). Replaying audit reconstructs each state the row
	// passed through.
	if err = s.DB.InsertQsoHistoryTx(ctx, tx, existing.UUID, action.Update, src, beforeImage); err != nil {
		s.rollbackTx(tx, op)
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to insert qso_history row")
	}

	if err = tx.Commit(); err != nil {
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to commit transaction")
	}

	s.Logger.InfoWith().
		Int64("qso_id", merged.ID).
		Int64("logbook_id", merged.LogbookID).
		Str("call", merged.ContactedStation.Call).
		Str("qso_date", merged.QsoDetails.QsoDate).
		Str("time_on", merged.QsoDetails.TimeOn).
		Str("freq_mhz", merged.QsoDetails.Freq).
		Str("band", merged.QsoDetails.Band).
		Str("mode", merged.QsoDetails.Mode).
		Msg("QSO updated")

	s.Hub.Publish(events.NameQsoUpdated, events.QsoUpdatedPayload{
		QsoUUID:   merged.UUID,
		QsoID:     merged.ID,
		LogbookID: merged.LogbookID,
	})

	return merged, nil
}

// resolveNoOp detects a no-effective-change PATCH — a normalized, immutable-restored
// candidate matching the stored row in every operator-editable field — and resolves it
// without writing, so an empty / unknown-only / immutable-only / canonically equivalent
// edit (or a client retry) does not bump the revision, append an audit row, re-arm
// forwarders, or publish qso.updated. handled is true when the request is fully resolved:
// a no-op success, OR the edit_conflict / error a STALE snapshot must produce, because
// the short-circuit skips the revision-guarded write and must not report a false success
// when a concurrent edit or delete has moved the row past the caller's snapshot. handled
// is false when there is a real change to persist. The comparison uses editableView, not
// whole-struct equality, which would treat a storage/enrichment difference as an edit.
func (s *Service) resolveNoOp(ctx context.Context, op errors.Op, merged, existing types.Qso) (types.Qso, bool, error) {
	if !reflect.DeepEqual(editableView(merged), editableView(existing)) {
		return types.Qso{}, false, nil
	}
	current, ferr := s.DB.FetchQsoByUUIDWithContext(ctx, existing.UUID)
	if ferr != nil && !stderr.Is(ferr, errors.ErrNotFound) {
		return types.Qso{}, true, errors.New(op).WithErr(ferr).WithMsg("no-op revision check failed")
	}
	if ferr != nil || current.Revision != existing.Revision {
		return types.Qso{}, true, &SubmitError{
			Code:    "edit_conflict",
			Message: "the QSO changed while this edit was in flight — reload it and re-apply the edit",
		}
	}
	// Accepted residual window (codex ded00ab1 P2): a concurrent edit or delete can
	// commit between the revision read above and this return, leaving the returned
	// snapshot momentarily stale. It is benign — the no-op performs NO write, so it can
	// neither overwrite nor lose that concurrent change (no data loss), and any ordinary
	// post-commit response is stale-able the same way, reconciled by the next fetch/SSE.
	// A guard transaction would add locking without strengthening the operator-visible
	// zero-side-effect outcome, and the approved AC keeps the no-op transaction-free.
	return existing, true, nil
}

// recomputeDedupeKey recomputes merged's dedupe key when a key-driving field changed
// (call/band/mode/freq/date/minute) and rejects a pre-tx collision with another row in
// the same logbook. TimeOn is compared at minute precision — the key is minute-based, so
// a seconds-only edit leaves it unchanged and this is a no-op. The pre-tx check is a
// fast path; the cross-handler race is closed by the UNIQUE backstop in UpdateQsoTx, not
// a pre-tx lock sqlite would serialize every writer against.
func (s *Service) recomputeDedupeKey(ctx context.Context, op errors.Op, merged *types.Qso, existing types.Qso) error {
	dedupeChanged := merged.ContactedStation.Call != existing.ContactedStation.Call ||
		merged.QsoDetails.Band != existing.QsoDetails.Band ||
		merged.QsoDetails.Mode != existing.QsoDetails.Mode ||
		merged.QsoDetails.Freq != existing.QsoDetails.Freq ||
		merged.QsoDetails.QsoDate != existing.QsoDetails.QsoDate ||
		utils.TimeToHHMM(merged.QsoDetails.TimeOn) != utils.TimeToHHMM(existing.QsoDetails.TimeOn)
	if !dedupeChanged {
		return nil
	}
	// Hash input uses the int-kHz string for determinism (the same contract as Submit);
	// merged.Freq is canonical MHz, so this parse cannot fail.
	kHz, _ := utils.ParseFreqMHz(merged.QsoDetails.Freq)
	newKey := ComputeDedupeKey(
		merged.ContactedStation.Call,
		merged.QsoDetails.Band,
		merged.QsoDetails.Mode,
		strconv.FormatInt(kHz, 10),
		merged.QsoDetails.QsoDate,
		utils.TimeToHHMM(merged.QsoDetails.TimeOn),
	)
	collision, err := s.DB.FetchQsoByDedupeKeyWithContext(ctx, merged.LogbookID, newKey)
	if err == nil && collision.ID != merged.ID {
		return &SubmitError{Code: "duplicate_key", Message: "edit would collide with another QSO in this logbook"}
	}
	if err != nil && !stderr.Is(err, errors.ErrNotFound) {
		return errors.New(op).WithErr(err).WithMsg("dedupe collision check failed")
	}
	merged.DedupeKey = newKey
	return nil
}

// editableView blanks every field the update contract does NOT let a client edit — the
// structural immutables and forwarder/enrichment state restored above, plus the
// column-only storage metadata (modified_at, deleted_at, revision) — so two QSOs
// compare equal iff their operator-editable fields match. It is the AW-3 no-op test's
// projection: kept in lockstep with the immutable-restore block in Update, and used
// instead of whole-struct equality, which would treat a storage/enrichment difference
// as an operator edit. DedupeKey is derived from editable fields, so it is excluded too.
func editableView(q types.Qso) types.Qso {
	q.ID = 0
	q.UUID = ""
	q.LogbookID = 0
	q.DedupeKey = ""
	q.ModifiedAt = time.Time{}
	q.DeletedAt = time.Time{}
	q.Revision = 0
	q.LoggingStation.StationCallsign = ""
	q.SmQsoUploadDate, q.SmQsoUploadStatus = "", ""
	q.SmFwrdByEmailDate, q.SmFwrdByEmailStatus = "", ""
	q.QrzComUploadDate, q.QrzComUploadStatus = "", ""
	q.ClubLogUploadDate, q.ClubLogUploadStatus = "", ""
	q.QrzlogLogid = ""
	q.CountryDetails = types.Country{}
	q.ContactHistory = nil
	return q
}
