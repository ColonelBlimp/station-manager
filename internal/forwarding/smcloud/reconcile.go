package smcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/reconcile"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Reconcile (ADR 0040 / sm-cloud-p1.md S4): detect + self-heal drift between
// the local logbook and its cloud backup. Periodically (and on demand via the
// daemon's POST /v1/smcloud/reconcile) the Reconciler computes the local
// {count, hash} over live rows with the SHARED internal/cloud/reconcile
// Summary — the same function the cloud service runs, which is what makes the
// two hashes comparable (µs truncation, UUID case folding, UnixMicro lines) —
// and GETs the cloud's. On mismatch it diffs the two manifests and re-enqueues
// the diverged UUIDs through the EXISTING forwarder queue (EnqueueUploads /
// EnqueueDeleteUploads): no separate repair path, and the ADR 0038
// forever-retry posture applies to the heal traffic too.
//
// Direction of trust: LOCAL IS AUTHORITATIVE. Local-newer/missing → push;
// local tombstone vs cloud live → push the tombstone. Cloud rows the local DB
// doesn't know at all (e.g. a previous database generation) are COUNTED AND
// LOGGED, never deleted — the cloud is a retentive superset. A cloud row
// NEWER than its local copy should not happen in P1's single-writer model and
// is logged as a warning for the operator; S5 restore is the tool that pulls
// cloud data down, not this routine.

// defaultReconcileInterval is the periodic cadence. Reconcile is a safety
// net behind the per-QSO push path, not a sync loop — hourly keeps a
// diverged logbook from staying diverged for long while staying near-free.
const defaultReconcileInterval = time.Hour

// reconcileStartupDelay defers the first run past daemon boot so the workers
// are up and any startup queue drain has begun before drift is measured.
const reconcileStartupDelay = 2 * time.Minute

// maxEnqueuePerRun bounds one run's repair batch (matches the API backfill
// cap). A bigger divergence heals across successive runs — the queue drains
// between them, so an unbounded first-run backfill can't swamp it.
const maxEnqueuePerRun = 5000

// Trigger names for the run-complete record — which path asked for the run.
// The on-demand summary used to exist only in the HTTP response the browser
// discarded (api-logging-gaps A2); the trigger keeps an operator press
// distinguishable from the hourly loop firing around the same time.
const (
	TriggerPeriodic = "periodic"
	TriggerManual   = "manual"
)

// ReconcileSummary is one run's outcome — returned by RunOnce and served by
// the on-demand endpoint.
type ReconcileSummary struct {
	InSync          bool   `json:"in_sync"`
	LocalCount      int    `json:"local_count"`      // live local rows
	CloudCount      int    `json:"cloud_count"`      // live cloud rows
	CloudLogbookID  int64  `json:"cloud_logbook_id"` // 0 = logbook not on the cloud yet
	EnqueuedUpserts int    `json:"enqueued_upserts"` // local-newer / cloud-missing rows queued
	EnqueuedDeletes int    `json:"enqueued_deletes"` // missed tombstones queued
	CloudOnly       int    `json:"cloud_only"`       // cloud rows unknown locally (logged, untouched)
	CloudNewer      int    `json:"cloud_newer"`      // cloud rows newer than local (warned, untouched)
	Truncated       bool   `json:"truncated"`        // maxEnqueuePerRun hit; next run continues
	Hash            string `json:"local_hash"`       // the local Summary hash (diagnostics)
}

// Reconciler runs the S4 loop for ONE smcloud destination + one local
// logbook (P1: the default logbook). Construct with NewReconciler; drive
// with Run (periodic) and/or RunOnce (on demand).
type Reconciler struct {
	db            *sqlite.Service
	qso           *qsoservice.Service
	log           *logging.Service
	client        *http.Client
	baseURL       string
	token         string
	cloudLogbook  string // cloud-side logbook NAME (mirrors the forwarder's)
	forwarderName string // queue destination for the heal traffic
	localLogbook  int64
	interval      time.Duration

	// runOnceOverride, when non-nil, replaces runOnce so a test can drive RunOnce's
	// post-run logging — specifically the F8 partial-mutation branch (a run that
	// committed queue upserts and THEN failed enqueueing deletes). No natural manifest
	// fixture reaches that path, so this is the seam; nil in production.
	runOnceOverride func() (ReconcileSummary, error)
}

// NewReconciler builds a Reconciler from the SAME ForwarderConfig the smcloud
// forwarder runs on (url/token/logbook credentials) plus the local logbook it
// guards. The forwarder must be enabled — heal traffic goes through its queue.
func NewReconciler(fc types.ForwarderConfig, localLogbookID int64,
	db *sqlite.Service, qso *qsoservice.Service, log *logging.Service) (*Reconciler, error) {
	const op errors.Op = "smcloud.NewReconciler"

	if localLogbookID < 1 {
		return nil, errors.New(op).WithMsg("local logbook id is required")
	}
	if db == nil || qso == nil || log == nil {
		return nil, errors.New(op).WithMsg("db, qso service, and logger are required")
	}
	f, err := New(fc) // reuses the forwarder's credential parsing + validation
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	fwd := f.(*Forwarder)
	return &Reconciler{
		db:            db,
		qso:           qso,
		log:           log,
		client:        &http.Client{Timeout: DefaultHTTPTimeout},
		baseURL:       strings.TrimSuffix(fwd.putURL, "/v1/qsos"),
		token:         fwd.token,
		cloudLogbook:  fwd.logbook,
		forwarderName: fc.Name,
		localLogbook:  localLogbookID,
		interval:      defaultReconcileInterval,
	}, nil
}

// Run drives the periodic loop until ctx is cancelled: first run after
// reconcileStartupDelay, then every interval. Errors are logged, never fatal
// — reconcile is a safety net; the next tick tries again (a down cloud is
// business as usual on a flaky link).
func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTimer(reconcileStartupDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if _, err := r.RunOnce(ctx, TriggerPeriodic); err != nil {
			r.log.WarnWith().Err(err).Msg("smcloud reconcile: run failed (next tick retries)")
		}
		t.Reset(r.interval)
	}
}

func (r *Reconciler) logSummary(sum ReconcileSummary, trigger string) {
	ev := r.log.InfoWith().
		Str("trigger", trigger).
		Bool("in_sync", sum.InSync).
		Int("local", sum.LocalCount).
		Int("cloud", sum.CloudCount).
		Int("upserts", sum.EnqueuedUpserts).
		Int("deletes", sum.EnqueuedDeletes)
	if sum.CloudOnly > 0 {
		ev = ev.Int("cloud_only", sum.CloudOnly)
	}
	if sum.CloudNewer > 0 {
		ev = ev.Int("cloud_newer", sum.CloudNewer)
	}
	if sum.Truncated {
		ev = ev.Bool("truncated", true)
	}
	ev.Msg("smcloud reconcile: run complete")
}

// RunOnce performs one reconcile pass: local summary vs cloud summary; on
// mismatch, manifest diff + re-enqueue. Safe to call concurrently with the
// periodic loop (worst case: duplicate queue rows, which the UUID-keyed
// upsert absorbs idempotently).
//
// trigger names the asking path (TriggerPeriodic / TriggerManual) for the
// run-complete record. On success the record is written HERE, not by the
// callers, so no trigger path can complete a run without leaving one — the
// on-demand endpoint used to return its summary only to the browser
// (api-logging-gaps A2). A failed run logs nothing at this level: failure
// reporting stays the caller's (the loop's warn-and-retry; the endpoint's
// 500, whose cause writeServerError logs).
func (r *Reconciler) RunOnce(ctx context.Context, trigger string) (ReconcileSummary, error) {
	run := r.runOnce
	if r.runOnceOverride != nil {
		run = func(context.Context) (ReconcileSummary, error) { return r.runOnceOverride() }
	}
	sum, err := run(ctx)
	if err == nil {
		r.logSummary(sum, trigger)
		return sum, nil
	}
	// A failed run's ERROR reporting stays the caller's (the loop warns + retries; the
	// endpoint 500s, whose cause writeServerError logs). But a run can fail AFTER it has
	// already committed queue upserts — enqueue-upserts succeeds, enqueue-deletes fails —
	// and that partial mutation is durable state the "run failed" line alone hides: a run
	// that queued 400 upserts then failed looked identical to one that did nothing (F8).
	// Record the mutation here, once, at whichever caller.
	if sum.EnqueuedUpserts > 0 || sum.EnqueuedDeletes > 0 {
		r.log.WarnWith().
			Str("trigger", trigger).
			Int("upserts", sum.EnqueuedUpserts).
			Int("deletes", sum.EnqueuedDeletes).
			Msg("smcloud reconcile: run failed after partially mutating the queue")
	}
	return sum, err
}

func (r *Reconciler) runOnce(ctx context.Context) (ReconcileSummary, error) {
	const op errors.Op = "smcloud.Reconciler.RunOnce"

	local, err := r.db.FetchQsoManifestWithContext(ctx, r.localLogbook)
	if err != nil {
		return ReconcileSummary{}, errors.New(op).WithErr(err).WithMsg("local manifest")
	}
	liveEntries := make([]reconcile.Entry, 0, len(local))
	for _, e := range local {
		if !e.Deleted {
			liveEntries = append(liveEntries, reconcile.Entry{UUID: e.UUID, ModifiedAt: e.ModifiedAt, Revision: e.Revision})
		}
	}
	localCount, localHash := reconcile.Summary(liveEntries)
	sum := ReconcileSummary{LocalCount: localCount, Hash: localHash}

	// Resolve the cloud logbook id by name. Absent = nothing pushed yet:
	// everything local-live is divergence (the first backfill).
	cloudID, err := r.cloudLogbookID(ctx)
	if err != nil {
		return ReconcileSummary{}, errors.New(op).WithErr(err).WithMsg("resolve cloud logbook")
	}
	sum.CloudLogbookID = cloudID

	cloud := map[string]cloudEntry{}
	if cloudID > 0 {
		cr, err := r.cloudReconcile(ctx, cloudID)
		if err != nil {
			return ReconcileSummary{}, errors.New(op).WithErr(err).WithMsg("cloud reconcile summary")
		}
		sum.CloudCount = cr.Count
		if cr.Count == localCount && cr.Hash == localHash {
			sum.InSync = true
			return sum, nil
		}
		if cloud, err = r.cloudManifest(ctx, cloudID); err != nil {
			return ReconcileSummary{}, errors.New(op).WithErr(err).WithMsg("cloud manifest")
		}
	}

	upserts, deletes, cloudOnly, cloudNewer := diffManifests(local, cloud)
	sum.CloudOnly = cloudOnly
	sum.CloudNewer = cloudNewer
	if cloudOnly > 0 {
		r.log.InfoWith().Int("count", cloudOnly).
			Msg("smcloud reconcile: cloud holds rows unknown locally (retentive superset — untouched; restore pulls them if wanted)")
	}
	if cloudNewer > 0 {
		r.log.WarnWith().Int("count", cloudNewer).
			Msg("smcloud reconcile: cloud rows NEWER than local — unexpected in single-writer P1; left untouched")
	}

	if len(upserts) > maxEnqueuePerRun {
		upserts, sum.Truncated = upserts[:maxEnqueuePerRun], true
	}
	if len(deletes) > maxEnqueuePerRun {
		deletes, sum.Truncated = deletes[:maxEnqueuePerRun], true
	}

	if len(upserts) > 0 {
		res, err := r.qso.EnqueueUploads(ctx, r.forwarderName, upserts, true, origin.Reconcile)
		if err != nil {
			return sum, errors.New(op).WithErr(err).WithMsg("enqueue upserts")
		}
		sum.EnqueuedUpserts = res.Enqueued
	}
	if len(deletes) > 0 {
		res, err := r.qso.EnqueueDeleteUploads(ctx, r.forwarderName, deletes, origin.Reconcile)
		if err != nil {
			return sum, errors.New(op).WithErr(err).WithMsg("enqueue deletes")
		}
		sum.EnqueuedDeletes = res.Enqueued
	}
	return sum, nil
}

// cloudEntry is the cloud manifest reduced to what the diff needs.
type cloudEntry struct {
	modified time.Time
	revision int64
	deleted  bool
}

// compareVersions orders two (revision, modified_at) version pairs per
// ADR 0050: revision first, modified_at (µs-truncated) breaking ties —
// exactly the cloud upsert guard's order. Returns <0 when a is older,
// 0 when equal, >0 when a is newer. With both revisions 0 (legacy rows)
// this degenerates to the pre-0050 pure-timestamp comparison.
func compareVersions(aRev int64, aMod time.Time, bRev int64, bMod time.Time) int {
	if aRev != bRev {
		if aRev < bRev {
			return -1
		}
		return 1
	}
	return aMod.Truncate(time.Microsecond).Compare(bMod.Truncate(time.Microsecond))
}

// diffManifests computes the heal sets. Versions compare revision-first with
// timestamps at the protocol's microsecond resolution (both sides truncated),
// and UUIDs fold to lower case, matching reconcile.Summary's canonicalisation:
//
//	upserts — local live rows the cloud is missing, holds stale (lower
//	          version — including same-second divergence the timestamp
//	          alone can't see), or holds as a tombstone a newer-or-equal
//	          local edit resurrects.
//	deletes — local tombstones the cloud still shows live.
//	cloudOnly — cloud rows with no local counterpart (never touched).
//	cloudNewer — cloud rows strictly newer than local (never touched).
func diffManifests(local []types.QsoManifestEntry, cloud map[string]cloudEntry) (upserts, deletes []string, cloudOnly, cloudNewer int) {
	seen := make(map[string]struct{}, len(local))
	for _, l := range local {
		key := strings.ToLower(strings.TrimSpace(l.UUID))
		seen[key] = struct{}{}
		c, ok := cloud[key]
		cmp := compareVersions(l.Revision, l.ModifiedAt, c.revision, c.modified)
		switch {
		case !ok:
			if !l.Deleted {
				upserts = append(upserts, l.UUID)
			}
			// A local tombstone the cloud never saw needs no push — there is
			// nothing upstream to delete.
		case l.Deleted && !c.deleted:
			deletes = append(deletes, l.UUID)
		case !l.Deleted && cmp > 0:
			upserts = append(upserts, l.UUID) // stale cloud copy (or resurrect over tombstone)
		case !l.Deleted && c.deleted && cmp == 0:
			// Cloud tombstone at the same version as a live local row: push
			// the live row — local is authoritative (edit-after-delete wins).
			upserts = append(upserts, l.UUID)
		case cmp < 0:
			cloudNewer++
		}
	}
	for key := range cloud {
		if _, ok := seen[key]; !ok {
			cloudOnly++
		}
	}
	return upserts, deletes, cloudOnly, cloudNewer
}

// ---- cloud client reads ------------------------------------------------------

// maxManifestBytes bounds a reconcile GET body (see the read in get()). Sized
// for the full manifest of a very large logbook, not the 1 MiB submit-response
// cap.
const maxManifestBytes = 64 << 20 // 64 MiB

func (r *Reconciler) get(ctx context.Context, path string, out any) error {
	const op errors.Op = "smcloud.Reconciler.get"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("build GET %s", path)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := r.client.Do(req)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("GET %s", path)
	}
	defer func() { _ = resp.Body.Close() }()
	// maxManifestBytes, NOT the 1 MiB submit-response cap: the reconcile GETs
	// include the FULL unpaginated manifest (one uuid+modified_at+revision+
	// deleted entry per QSO, ~110 B each), and — because get() sets no
	// Accept-Encoding, so Go's transport transparently decompresses — this cap
	// applies to the DECOMPRESSED JSON. The old 1 MiB truncated any logbook past
	// ~9k QSOs, failing the decode and silently halting reconciliation (review
	// 2026-07-20 internal/forwarding #1). 64 MiB covers ~500k entries — far
	// beyond any single logbook — while still bounding a rogue response.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("read GET %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New(op).WithMsgf("GET %s: HTTP %d (body: %s)", path, resp.StatusCode,
			bodySnippet(body, errorSnippetLen))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New(op).WithErr(err).WithMsgf("parse GET %s", path)
	}
	return nil
}

// cloudLogbookID resolves the configured cloud logbook name to its id;
// 0 when the logbook doesn't exist cloud-side yet.
func (r *Reconciler) cloudLogbookID(ctx context.Context) (int64, error) {
	var out struct {
		Logbooks []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"logbooks"`
	}
	if err := r.get(ctx, "/v1/logbooks", &out); err != nil {
		return 0, err
	}
	for _, lb := range out.Logbooks {
		if lb.Name == r.cloudLogbook {
			return lb.ID, nil
		}
	}
	return 0, nil
}

func (r *Reconciler) cloudReconcile(ctx context.Context, logbookID int64) (struct {
	Count int    `json:"count"`
	Hash  string `json:"hash"`
}, error) {
	var out struct {
		Count int    `json:"count"`
		Hash  string `json:"hash"`
	}
	err := r.get(ctx, fmt.Sprintf("/v1/logbooks/%d/reconcile", logbookID), &out)
	return out, err
}

func (r *Reconciler) cloudManifest(ctx context.Context, logbookID int64) (map[string]cloudEntry, error) {
	var out struct {
		Entries []struct {
			UUID       string    `json:"uuid"`
			ModifiedAt time.Time `json:"modified_at"`
			Revision   int64     `json:"revision"`
			Deleted    bool      `json:"deleted"`
		} `json:"entries"`
	}
	if err := r.get(ctx, fmt.Sprintf("/v1/logbooks/%d/manifest", logbookID), &out); err != nil {
		return nil, err
	}
	m := make(map[string]cloudEntry, len(out.Entries))
	for _, e := range out.Entries {
		m[strings.ToLower(strings.TrimSpace(e.UUID))] = cloudEntry{modified: e.ModifiedAt, revision: e.Revision, deleted: e.Deleted}
	}
	return m, nil
}
