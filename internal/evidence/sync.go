package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
)

// §5 sync engine (spot-network §5.1 amendments, operator rulings
// 2026-08-10). One loop goroutine owned by the service, two lanes over one
// HTTP client:
//
//   - LIVE: writeSlot commits → notifyLive() → debounced push of the
//     freshest unsynced rows. The one-cycle guarantee is the
//     HEALTHY-CHANNEL guarantee: during backoff a live signal coalesces
//     and waits for the retry time.
//   - BACKLOG: while unsynced rows remain, one batch per tick. A live
//     signal CANCELS an in-flight backlog request, and that intentional
//     cancellation does not advance backoff — otherwise "a fresh decode
//     never queues behind backlog" would be false.
//
// Selection is newest-first (UUIDv7 order) so current data leads every
// batch, with profiles FIRST as selection priority —
// retryable_missing_profile re-offers the referenced local profile even if
// previously marked synced (it heals an SMC-side restore). offered_at is
// conservative durable send-intent, written with COALESCE before dispatch.
// A response that is invalid or incomplete consumes no rows.
//
// The loop writes sync marks on the same *sql.DB the writer goroutine
// uses: WAL serialises the two writers under the 2 s busy_timeout, and
// every sync transaction is a handful of tiny UPDATEs — the §4.1 "one
// writer owns all EVIDENCE writes" rule is about slot evidence, which
// this loop never writes.

// Ratified engine constants (operator, 2026-08-10) — package vars so tests
// can dial them (the captureLinger pattern); none are operator
// configuration.
var (
	syncLiveDebounce    = time.Second
	syncBacklogBatch    = 500
	syncBacklogInterval = 10 * time.Second
	syncBackoffMin      = 30 * time.Second
	syncBackoffMax      = 15 * time.Minute
	syncHTTPTimeout     = 30 * time.Second
)

// Sync-health states surfaced by Status.Sync.State.
const (
	syncStateIdle    = "idle"
	syncStateBackoff = "backoff"
)

// SyncStatus is the sync half of the local honesty surface (SY1/SY6).
type SyncStatus struct {
	Enabled     bool             `json:"enabled"`
	State       string           `json:"state,omitempty"` // idle | backoff
	LastSuccess string           `json:"last_success_utc,omitempty"`
	LastError   string           `json:"last_error,omitempty"`
	Unsynced    map[string]int64 `json:"unsynced,omitempty"` // kind → count
	Quarantined int64            `json:"quarantined"`
}

// syncTables maps wire kinds onto archive tables, in SELECTION order:
// profiles first (§5.4 selection priority), then the slot kinds.
var syncTables = []struct{ kind, table string }{
	{evidencewire.KindProfile, "profiles"},
	{evidencewire.KindObservation, "observations"},
	{evidencewire.KindCoverage, "coverage"},
	{evidencewire.KindLossInterval, "loss_intervals"},
}

func tableForKind(kind string) string {
	for _, t := range syncTables {
		if t.kind == kind {
			return t.table
		}
	}
	return ""
}

// syncRow is one selected row: enough to build its wire record and to act
// on its outcome (profileRef drives the retryable_missing_profile
// re-offer).
type syncRow struct {
	kind, uuid string
	payload    []byte
	profileRef string
}

// notifyLive signals the sync loop that a slot's evidence just committed.
// Non-blocking and coalescing: a full channel means a wakeup is already
// pending, which is all a signal can mean. It also cancels an in-flight
// BACKLOG request (never a live one) — the ruling's intentional
// cancellation, which must not advance backoff.
func (s *Service) notifyLive() {
	if s.syncCh == nil {
		return
	}
	select {
	case s.syncCh <- struct{}{}:
	default:
	}
	s.mu.Lock()
	if s.syncCancelBacklog != nil {
		s.syncLiveInterrupt = true
		s.syncCancelBacklog()
	}
	s.mu.Unlock()
}

type syncResult int

const (
	syncIdle syncResult = iota
	syncOK
	syncTransient
	syncInterrupted
)

// syncLoop is the engine goroutine. Runs only when cfg.Sync is enabled.
func (s *Service) syncLoop() {
	defer close(s.syncDone)
	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()
	go func() {
		<-s.quit
		cancelLoop() // Stop must not wait out a 30 s HTTP timeout
	}()

	var backoff time.Duration
	var retryAt time.Time
	ticker := time.NewTicker(syncBacklogInterval)
	defer ticker.Stop()

	for {
		live := false
		select {
		case <-s.quit:
			return
		case <-s.syncCh:
			live = true
			debounce := time.NewTimer(syncLiveDebounce)
		coalesce:
			for {
				select {
				case <-s.quit:
					debounce.Stop()
					return
				case <-s.syncCh: // the slot's burst folds into one batch
				case <-debounce.C:
					break coalesce
				}
			}
		case <-ticker.C:
		}
		if time.Now().Before(retryAt) {
			continue // unhealthy channel: even the live lane waits (SY5 is the healthy-channel guarantee)
		}

		switch s.syncOnce(loopCtx, live) {
		case syncOK, syncIdle:
			backoff = 0
			retryAt = time.Time{}
		case syncInterrupted:
			// Intentional live cancellation: no backoff advance; the pending
			// live signal fires on the next iteration.
		case syncTransient:
			if backoff == 0 {
				backoff = syncBackoffMin
			} else if backoff *= 2; backoff > syncBackoffMax {
				backoff = syncBackoffMax
			}
			retryAt = time.Now().Add(backoff)
		}
	}
}

// syncOnce selects, offers, and applies one batch. It NEVER consumes a row
// without a terminal outcome from a valid, complete response.
func (s *Service) syncOnce(parent context.Context, live bool) syncResult {
	rows, err := s.selectSyncBatch()
	if err != nil {
		s.noteSyncError("select batch: " + err.Error())
		return syncTransient
	}
	if len(rows) == 0 {
		s.mu.Lock()
		s.syncState = syncStateIdle
		s.mu.Unlock()
		return syncIdle
	}

	// SY9: durable send-intent BEFORE dispatch. COALESCE keeps the FIRST
	// intent timestamp — "possibly offered, unacknowledged" from the moment
	// any attempt could have put bytes on the wire.
	if err := s.markOffered(rows); err != nil {
		s.noteSyncError("mark offered: " + err.Error())
		return syncTransient
	}

	reqCtx, cancel := context.WithCancel(parent)
	defer cancel()
	if !live {
		s.mu.Lock()
		s.syncCancelBacklog = cancel
		s.mu.Unlock()
	}
	outcomes, err := s.postBatch(reqCtx, rows)
	if !live {
		s.mu.Lock()
		s.syncCancelBacklog = nil
		interrupted := s.syncLiveInterrupt
		s.syncLiveInterrupt = false
		s.mu.Unlock()
		if interrupted && err != nil {
			return syncInterrupted
		}
	}
	if err != nil {
		s.noteSyncError(err.Error())
		return syncTransient
	}

	if err := s.applyOutcomes(rows, outcomes); err != nil {
		s.noteSyncError("apply outcomes: " + err.Error())
		return syncTransient
	}
	s.mu.Lock()
	wasBackoff := s.syncState == syncStateBackoff
	s.syncState = syncStateIdle
	s.syncLastErr = ""
	s.syncLastSuccess = time.Now()
	s.mu.Unlock()
	if wasBackoff {
		s.log.InfoWith().Msg("evidence: sync recovered")
	}
	return syncOK
}

// noteSyncError records the fault for status and logs the TRANSITION into
// backoff once (honest silence, mandatory noise — per-attempt errors are
// Debug; an unreachable SMC for an evening would otherwise write hundreds
// of identical warns).
func (s *Service) noteSyncError(msg string) {
	s.mu.Lock()
	entering := s.syncState != syncStateBackoff
	s.syncState = syncStateBackoff
	s.syncLastErr = msg
	s.mu.Unlock()
	if entering {
		s.log.WarnWith().Str("error", msg).Msg("evidence: sync entering backoff")
	} else {
		s.log.DebugWith().Str("error", msg).Msg("evidence: sync attempt failed")
	}
}

// selectSyncBatch reads the next batch: offerable profiles first, then the
// newest unsynced slot rows — every kind bounded by the ONE batch cap
// (codex-P1 fix 2026-08-10: profile-first is a priority, not a cap
// exemption; an over-cap envelope 400s at the server, consumes nothing,
// and would retry the same oversized set forever). Leftover profiles ride
// later rounds. Newest-first is what makes "current ahead of backlog" hold
// inside every batch, live or drained.
func (s *Service) selectSyncBatch() ([]syncRow, error) {
	var rows []syncRow
	remaining := syncBacklogBatch
	for _, t := range syncTables {
		if remaining <= 0 {
			break
		}
		selected, err := s.selectKind(t.kind, t.table, remaining)
		if err != nil {
			return nil, err
		}
		rows = append(rows, selected...)
		remaining -= len(selected)
	}
	return rows, nil
}

func (s *Service) selectKind(kind, table string, limit int) ([]syncRow, error) {
	rs, err := s.db.Query(
		`SELECT uuid FROM `+table+` WHERE synced = 0 AND quarantine_reason IS NULL ORDER BY uuid DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, err
	}
	var uuids []string
	for rs.Next() {
		var u string
		if err := rs.Scan(&u); err != nil {
			_ = rs.Close()
			return nil, err
		}
		uuids = append(uuids, u)
	}
	if err := rs.Close(); err != nil {
		return nil, err
	}
	out := make([]syncRow, 0, len(uuids))
	for _, u := range uuids {
		row, err := s.loadSyncRow(kind, u)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// Payload shapes: the REPLAY-COMPLETE row content (§5.3 amendment), field
// names shared with the SMC side through the wire contract's convention
// (the server extracts observation.profile_uuid for its §5.4 probe).
type observationPayload struct {
	SlotStartUTC         string  `json:"slot_start_utc"`
	DialMHz              float64 `json:"dial_mhz"`
	DialTracked          bool    `json:"dial_tracked"`
	FreqHz               float64 `json:"freq_hz"`
	DTSec                float64 `json:"dt_sec"`
	SNR                  int64   `json:"snr"`
	Payload              []byte  `json:"payload"`
	ParseStatus          string  `json:"parse_status"`
	Text                 *string `json:"text"`
	ProvAlgorithm        string  `json:"prov_algorithm"`
	ProvAPProfile        string  `json:"prov_ap_profile"`
	ProvAPSource         string  `json:"prov_ap_source"`
	MetricSync           float64 `json:"metric_sync"`
	MetricHardSync       int64   `json:"metric_hard_sync"`
	MetricCostasGeo      float64 `json:"metric_costas_geo"`
	MetricCostasMinBlock float64 `json:"metric_costas_min_block"`
	MetricBlocks         int64   `json:"metric_blocks"`
	MetricHardErrors     int64   `json:"metric_hard_errors"`
	MetricDMin           float64 `json:"metric_dmin"`
	DecoderBuild         string  `json:"decoder_build"`
	ProfileUUID          *string `json:"profile_uuid"`
	UnprofiledReason     *string `json:"unprofiled_reason"`
}

type coveragePayload struct {
	SlotStartUTC string  `json:"slot_start_utc"`
	Outcome      string  `json:"outcome"`
	DialMHz      float64 `json:"dial_mhz"`
	DialTracked  bool    `json:"dial_tracked"`
	DecodeCount  int64   `json:"decode_count"`
}

type lossPayload struct {
	StartUTC     string  `json:"start_utc"`
	EndUTC       string  `json:"end_utc"`
	Slots        int64   `json:"slots"`
	Observations int64   `json:"observations"`
	Reason       string  `json:"reason"`
	RemoteStatus string  `json:"remote_status"`
	DialMHz      float64 `json:"dial_mhz"`
}

type profileSyncPayload struct {
	Lineage    string   `json:"lineage"`
	Version    int64    `json:"version"`
	ValidFrom  string   `json:"valid_from"`
	Name       string   `json:"name"`
	Type       *string  `json:"type"`
	HeightM    *float64 `json:"height_m"`
	Feedline   *string  `json:"feedline"`
	Locator    *string  `json:"locator"`
	Bands      string   `json:"bands"`
	NoiseFloor string   `json:"noise_floor"`
}

func (s *Service) loadSyncRow(kind, uuid string) (syncRow, error) {
	row := syncRow{kind: kind, uuid: uuid}
	var payload any
	switch kind {
	case evidencewire.KindObservation:
		var p observationPayload
		var tracked int
		if err := s.db.QueryRow(
			`SELECT slot_start_utc, dial_mhz, dial_tracked, freq_hz, dt_sec, snr, payload,
				parse_status, text, prov_algorithm, prov_ap_profile, prov_ap_source,
				metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
				metric_blocks, metric_hard_errors, metric_dmin, decoder_build,
				profile_uuid, unprofiled_reason
			 FROM observations WHERE uuid = ?`, uuid).Scan(
			&p.SlotStartUTC, &p.DialMHz, &tracked, &p.FreqHz, &p.DTSec, &p.SNR, &p.Payload,
			&p.ParseStatus, &p.Text, &p.ProvAlgorithm, &p.ProvAPProfile, &p.ProvAPSource,
			&p.MetricSync, &p.MetricHardSync, &p.MetricCostasGeo, &p.MetricCostasMinBlock,
			&p.MetricBlocks, &p.MetricHardErrors, &p.MetricDMin, &p.DecoderBuild,
			&p.ProfileUUID, &p.UnprofiledReason); err != nil {
			return row, err
		}
		p.DialTracked = tracked != 0
		if p.ProfileUUID != nil {
			row.profileRef = *p.ProfileUUID
		}
		payload = p
	case evidencewire.KindCoverage:
		var p coveragePayload
		var tracked int
		if err := s.db.QueryRow(
			`SELECT slot_start_utc, outcome, dial_mhz, dial_tracked, decode_count
			 FROM coverage WHERE uuid = ?`, uuid).Scan(
			&p.SlotStartUTC, &p.Outcome, &p.DialMHz, &tracked, &p.DecodeCount); err != nil {
			return row, err
		}
		p.DialTracked = tracked != 0
		payload = p
	case evidencewire.KindLossInterval:
		var p lossPayload
		if err := s.db.QueryRow(
			`SELECT start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz
			 FROM loss_intervals WHERE uuid = ?`, uuid).Scan(
			&p.StartUTC, &p.EndUTC, &p.Slots, &p.Observations, &p.Reason, &p.RemoteStatus, &p.DialMHz); err != nil {
			return row, err
		}
		payload = p
	case evidencewire.KindProfile:
		var p profileSyncPayload
		if err := s.db.QueryRow(
			`SELECT lineage, version, valid_from, name, type, height_m, feedline, locator, bands, noise_floor
			 FROM profiles WHERE uuid = ?`, uuid).Scan(
			&p.Lineage, &p.Version, &p.ValidFrom, &p.Name, &p.Type, &p.HeightM,
			&p.Feedline, &p.Locator, &p.Bands, &p.NoiseFloor); err != nil {
			return row, err
		}
		payload = p
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return row, err
	}
	row.payload = b
	return row, nil
}

// markOffered writes the send-intent, per table, in one transaction that
// commits BEFORE dispatch (SY9).
func (s *Service) markOffered(rows []syncRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Nano precision: send-intents can be milliseconds apart (a re-offer
	// under dialed-down backoff, a fast retry), and a second-granular
	// timestamp would make consecutive intents indistinguishable — which
	// also blinds any test to an overwrite that COALESCE must prevent.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	byTable := map[string][]string{}
	for _, r := range rows {
		byTable[tableForKind(r.kind)] = append(byTable[tableForKind(r.kind)], r.uuid)
	}
	for table, uuids := range byTable {
		q := `UPDATE ` + table + ` SET offered_at = COALESCE(offered_at, ?) WHERE uuid IN (` +
			placeholders(len(uuids)) + `)`
		args := make([]any, 0, len(uuids)+1)
		args = append(args, now)
		for _, u := range uuids {
			args = append(args, u)
		}
		if _, err := tx.Exec(q, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// postBatch sends one envelope and validates the response SHAPE: exactly
// one outcome per record, positionally, uuid echoed. Anything else — a
// non-200, a short array, a mismatched uuid, undecodable JSON — is
// invalid and consumes no rows (operator ruling 2026-08-10).
func (s *Service) postBatch(ctx context.Context, rows []syncRow) ([]evidencewire.RowOutcome, error) {
	req := evidencewire.PutRequest{Records: make([]evidencewire.Record, len(rows))}
	for i, r := range rows {
		digest, err := evidencewire.DigestV1Hex(r.payload)
		if err != nil {
			return nil, fmt.Errorf("digest %s/%s: %w", r.kind, r.uuid, err)
		}
		req.Records[i] = evidencewire.Record{
			Kind: r.kind, UUID: r.uuid,
			DigestV: evidencewire.DigestVersion1, Digest: digest,
			Payload: json.RawMessage(r.payload),
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut,
		strings.TrimSuffix(s.cfg.SyncURL, "/")+"/v1/evidence", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.cfg.SyncToken)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.syncClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("evidence sync: SMC answered %d", resp.StatusCode)
	}
	var out evidencewire.PutResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("evidence sync: undecodable response: %w", err)
	}
	if len(out.Outcomes) != len(rows) {
		return nil, fmt.Errorf("evidence sync: %d outcomes for %d records — incomplete response consumes nothing",
			len(out.Outcomes), len(rows))
	}
	for i, o := range out.Outcomes {
		if o.UUID != rows[i].uuid {
			return nil, fmt.Errorf("evidence sync: outcome %d answers %q, offered %q — mismatched response consumes nothing",
				i, o.UUID, rows[i].uuid)
		}
	}
	return out.Outcomes, nil
}

// applyOutcomes marks rows per their terminal outcomes in one transaction.
// An unknown outcome string leaves its row untouched (re-offered later) —
// forward-compatible and conservative.
func (s *Service) applyOutcomes(rows []syncRow, outcomes []evidencewire.RowOutcome) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, o := range outcomes {
		r := rows[i]
		table := tableForKind(r.kind)
		switch o.Outcome {
		case evidencewire.OutcomeAccepted, evidencewire.OutcomeAlreadyPresent,
			evidencewire.OutcomeTombstoned, evidencewire.OutcomeSuppressed:
			if _, err := tx.Exec(`UPDATE `+table+` SET synced = 1 WHERE uuid = ?`, r.uuid); err != nil {
				return err
			}
		case evidencewire.OutcomePermanentReject:
			reason := o.Reason
			if reason == "" {
				reason = "unspecified"
			}
			if _, err := tx.Exec(`UPDATE `+table+` SET quarantine_reason = ? WHERE uuid = ?`, reason, r.uuid); err != nil {
				return err
			}
		case evidencewire.OutcomeRetryableMissingProfile:
			// Selection priority, not envelope order (operator ruling): the
			// referenced LOCAL profile re-offers even if previously synced —
			// SMC restored from backup is exactly this signature.
			if r.profileRef != "" {
				if _, err := tx.Exec(`UPDATE profiles SET synced = 0 WHERE uuid = ?`, r.profileRef); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// syncStatusLocked builds Status.Sync. Requires s.mu held.
func (s *Service) syncStatusLocked() *SyncStatus {
	ss := &SyncStatus{Enabled: s.cfg.Sync}
	if !s.cfg.Sync || s.db == nil {
		return ss
	}
	ss.State = s.syncState
	if ss.State == "" {
		ss.State = syncStateIdle
	}
	ss.LastError = s.syncLastErr
	if !s.syncLastSuccess.IsZero() {
		ss.LastSuccess = s.syncLastSuccess.UTC().Format(time.RFC3339)
	}
	ss.Unsynced = map[string]int64{}
	for _, t := range syncTables {
		var n int64
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM ` + t.table + ` WHERE synced = 0 AND quarantine_reason IS NULL`).Scan(&n); err == nil {
			ss.Unsynced[t.kind] = n
		}
		var q int64
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM ` + t.table + ` WHERE quarantine_reason IS NOT NULL`).Scan(&q); err == nil {
			ss.Quarantined += q
		}
	}
	return ss
}
