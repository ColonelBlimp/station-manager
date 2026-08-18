package main

// ADR 0070 phase 3 — the daemon lifecycle adapters.
//
// daemon holds the constructed services + shared holders; registerLifecycle binds each graph node
// (lifecycle.go) to one orchestrator.Adapter whose Initialize/Start/Stop hooks call the same service
// transitions run() drove by hand. The orchestrator owns the ordering; these hooks own the work.
//
// Per-service fatal-vs-fail-soft is PRESERVED exactly as run() had it: bridge/ft8/enrichment, the DB
// open/migrate glue, and worker spawn return their error (→ Failed → rollback → the daemon exits);
// evidence and psk swallow-and-log a start failure (capture/upload stays idle, the daemon runs on).
//
// Construction placement: bridge and ft8 are constructed BEFORE orchestrator.Start (run()), because
// http composes their handles even when those nodes are config-disabled (pruned). enrichment and
// mailer — always active — are constructed ONCE in their own node's Initialize (the operator ruling
// that promoted them to nodes); evidence and psk (also always active) construct in their Initialize
// too, so http's evidence handle is always present.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/api"
	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/enums/dxcc"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/evidence"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/inhibit"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/lookup/refresher"
	"github.com/ColonelBlimp/station-manager/internal/pskreporter"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// daemon carries the daemon's constructed services + shared holders across the lifecycle hooks. Its
// fields are populated by run() (Wire + resolve + construct) and by the node Initialize hooks
// (enrichment/mailer/evidence/psk + the derived DB paths).
type daemon struct {
	// Configuration + the pre-logger startup facts the logging node narrates once it is up.
	cfg            config.Config
	cfgSvc         *config.Service
	cfgPath        string
	firstRunPath   string
	startupChanges []config.FieldChange

	// Beans resolved after Wire().
	logger *logging.Service
	hub    *events.Hub
	db     *sqlite.Service // log database
	refDB  *sqlite.Service // enrichment-cache database (reference.db)
	qso    *qsoservice.Service

	// DB paths derived once the log-DB node initialises (its Initialize sets DatabaseConfig.Path).
	logDBDir string
	refPath  string

	// Fleet. bridge + ft8 are constructed pre-orchestrator; evidence + psk in their node Initialize.
	bridge        *bridge.Service
	ft8           *ft8.Service
	evidence      *evidence.Service
	psk           *pskreporter.Service
	pskRxCall     string
	evidenceInit  bool // evidence.Initialize succeeded
	evidenceReady bool // evidence.Initialize AND Start both succeeded (⇒ capture sink may be wired)

	// Promoted infra, constructed once in-node.
	enrich     *lookup.Orchestrator
	refresher  *refresher.Service
	mailer     *email.Service
	server     *api.Server
	smcloudRec *smcloud.Reconciler

	// Worker context + drains (owned by run(); the fleet binds long-lived work to workerCtx).
	workerCtx    context.Context
	workerCancel context.CancelFunc
	// forwarderCancel cancels ONLY the forwarder workers + reconciler (a child of workerCtx set in
	// startWorkers), so their Rollback can drain them without cancelling the orchestrator's rollback
	// context. run()'s workerCancel still cascades to it.
	forwarderCancel context.CancelFunc
	workerWG        sync.WaitGroup
	qsoLogWG        sync.WaitGroup

	// HTTP serving + planned self-restart.
	errCh       chan error
	restartCh   chan struct{}
	restartOnce sync.Once

	// cleanShutdown is set by run() immediately before orchestrator.Shutdown, so the logging node's
	// Stop emits "smd stopped" only on a real shutdown — never on a start-failure rollback that also
	// unwinds a Running logging node through the same Stop hook.
	cleanShutdown bool
}

// registerLifecycle registers the plan nodes + one adapter per node and returns the orchestrator.
// Adapters bind by node id; nil hooks take the orchestrator's documented defaults (nil Active ⇒
// always active; nil Start ⇒ auto-promote to Running once Initialized; nil Stop ⇒ trivially Drained).
func (d *daemon) registerLifecycle(c *iocdi.Container) (*orchestrator.Orchestrator, error) {
	if err := registerLifecycleNodes(c); err != nil {
		return nil, err
	}
	o := orchestrator.New(c)

	stopErr := func(f func() error) func(context.Context) error {
		return func(context.Context) error { return f() }
	}
	stopVoid := func(f func()) func(context.Context) error {
		return func(context.Context) error { f(); return nil }
	}
	// rollbackVia adapts a resource closer to the Rollback hook. The orchestrator drives Rollback —
	// NOT Stop — for a node whose OWN Start returned an error (its milestone is still Initialized, not
	// Running). A node that opened a resource in Initialize or partway through Start (the log file,
	// an open DB connection, the refresher) must release it on that path too, or the handle leaks
	// until process exit. The cleanup is the same closer Stop uses (codex 3a850604 P2).
	rollbackVia := func(f func() error) func(orchestrator.Milestone) error {
		return func(orchestrator.Milestone) error { return f() }
	}

	adapters := []orchestrator.Adapter{
		// --- Beans ---
		{NodeID: nodeConfig, Initialize: d.cfgSvc.Initialize},
		{NodeID: nodeHub, Stop: stopVoid(d.hub.Close)},
		{NodeID: nodeLogging, Initialize: d.logger.Initialize, Start: d.startLogging,
			Stop: stopErr(d.stopLogging), Rollback: rollbackVia(d.logger.Close)},
		{NodeID: nodeLogDB, Initialize: d.db.Initialize, Start: d.startLogDB,
			Stop: stopErr(d.db.Close), Rollback: rollbackVia(d.db.Close)},
		{NodeID: nodeRefDB, Initialize: d.refDB.Initialize, Start: d.startRefDB,
			Stop: stopErr(d.refDB.Close), Rollback: rollbackVia(d.refDB.Close)},
		{NodeID: nodeQso, Initialize: d.initQso, Start: d.startQso},

		// --- Fleet + promoted infra ---
		{NodeID: nodeBridge, Active: d.bridge.Enabled, Initialize: d.bridge.Initialize,
			Start: d.bridge.Start, Stop: stopErr(d.bridge.Stop), Rollback: rollbackVia(d.bridge.Stop)},
		{NodeID: nodeEnrichment, Initialize: d.initEnrichment, Start: d.startEnrichment,
			Stop: d.stopEnrichment, Rollback: rollbackVia(d.closeEnrichment)},
		{NodeID: nodeMailer, Initialize: d.initMailer},
		{NodeID: nodeEvidence, Initialize: d.initEvidence, Start: d.startEvidence, Stop: stopVoid(d.stopEvidenceSvc)},
		{NodeID: nodePsk, Initialize: d.initPsk, Start: d.startPsk, Stop: stopErr(d.stopPskSvc)},
		{NodeID: nodeFt8, Active: d.ft8.Enabled, Initialize: d.initFt8, Start: d.ft8.Start, Stop: stopErr(d.ft8.Stop)},
		{NodeID: nodeWorkers, Start: d.startWorkers, PrepareStop: d.workerPrepareStop,
			Stop: stopVoid(d.workerWG.Wait), Rollback: rollbackVia(d.drainWorkers)},
		{NodeID: nodeQsoLog, Stop: stopVoid(d.qsoLogWG.Wait)},
		{NodeID: nodeHTTP, Initialize: d.initHTTP, Start: d.startHTTP,
			PrepareStop: d.httpStopAccepting, Stop: d.stopHTTP},
	}
	for _, a := range adapters {
		if err := o.Register(a); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// ---- logging ----

// startLogging narrates the startup context once the log file is open, then loads the optional
// modes/DXCC overrides (loud-and-fatal, matching run()). These lines used to sit inline in run(),
// but the logger is a bean that only initialises here, so they move with it.
func (d *daemon) startLogging(context.Context) error {
	const op errors.Op = "smd.startLogging"
	if d.firstRunPath != "" {
		d.logger.InfoWith().Str("path", d.firstRunPath).Msg("first run: wrote default config to disk")
	}
	if len(d.startupChanges) > 0 {
		d.logger.InfoWith().Str("source", "startup").Int("change_count", len(d.startupChanges)).
			Interface("changes", d.startupChanges).Msg("config saved")
	}
	d.logger.InfoWith().Msg("smd starting")
	d.logger.InfoWith().Str("level", d.cfg.Logging.Level).Msg("logging configured")
	for _, w := range config.Warnings(d.cfg) {
		d.logger.WarnWith().Msg(w)
	}
	if raw, rerr := os.ReadFile(d.cfgPath); rerr == nil {
		for _, k := range config.UnknownKeys(raw) {
			d.logger.WarnWith().Str("key", k).
				Msg("config: unrecognised key ignored (typo? — value falls back to default)")
		}
	}
	if err := modes.LoadOverride(d.cfg.DataDir); err != nil {
		d.logger.ErrorWith().Err(err).Msg("modes: override load failed")
		return errors.New(op).WithErr(err).WithMsg("load modes override")
	}
	if err := dxcc.LoadOverride(d.cfg.DataDir); err != nil {
		d.logger.ErrorWith().Err(err).Msg("dxcc: override load failed")
		return errors.New(op).WithErr(err).WithMsg("load dxcc override")
	}
	return nil
}

// stopLogging is the logging node's Stop. Because logging DrainAfter every other node, by the time
// this runs a clean shutdown has drained everything else — so it emits the final "smd stopped" line
// immediately before closing the logger. On a start-failure rollback (cleanShutdown==false) it just
// closes, with no misleading "smd stopped". If any node was non-drained, logging is Skipped and this
// never runs (the logger is left open for process reclamation).
func (d *daemon) stopLogging() error {
	if d.cleanShutdown {
		d.logger.InfoWith().Msg("smd stopped")
	}
	return d.logger.Close()
}

// ---- databases ----

// startLogDB opens + migrates the log database. It first derives the log-DB directory and the
// reference.db path (its Initialize populated DatabaseConfig.Path), then runs the one-time reference
// split BEFORE either connection opens (it re-keys both DBs' migration tracking).
func (d *daemon) startLogDB(context.Context) error {
	const op errors.Op = "smd.startLogDB"
	d.logDBDir = filepath.Dir(d.db.DatabaseConfig.Path)
	d.refPath = filepath.Join(d.logDBDir, referenceDBFilename)
	if err := sqlite.BootstrapReferenceSplit(
		d.db.DatabaseConfig.Path, d.refPath, filepath.Join(d.logDBDir, "backups"), d.logger,
	); err != nil {
		return errors.New(op).WithErr(err).WithMsg("bootstrap reference split")
	}
	d.db.SetMigrationSets(sqlite.MigrationSetLog)
	if err := d.db.Open(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("open database")
	}
	if err := d.db.Migrate(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("run migrations")
	}
	return nil
}

// startRefDB opens + migrates reference.db, then secures both DB files (both connections are open by
// now: this node waits on the log-DB node).
func (d *daemon) startRefDB(context.Context) error {
	const op errors.Op = "smd.startRefDB"
	d.refDB.SetMigrationSets(sqlite.MigrationSetReference)
	d.refDB.SetDatabasePath(d.refPath)
	if err := d.refDB.Open(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("open reference database")
	}
	if err := d.refDB.Migrate(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("run reference migrations")
	}
	if err := sqlite.SecureDataFiles(d.cfgSvc.WorkingDir(), filepath.Join(d.logDBDir, "backups"),
		d.logger, d.db.DatabaseConfig.Path, d.refPath); err != nil {
		return errors.New(op).WithErr(err).WithMsg("secure database files")
	}
	d.logger.InfoWith().Msg("databases open and migrated")
	return nil
}

// ---- qso service ----

func (d *daemon) initQso() error {
	if err := d.qso.Initialize(); err != nil {
		return err
	}
	// Pin MY_RIG attribution to the startup rig (the one the bridge binds), not the live default.
	d.qso.SetActiveRig(d.cfg.DefaultRigID)
	return nil
}

// startQso self-heals the default logbook row (needs the log DB open) and refreshes the snapshot in
// case a corrected DefaultLogbookID was persisted.
func (d *daemon) startQso(context.Context) error {
	const op errors.Op = "smd.startQso"
	if err := ensureDefaultLogbook(context.Background(), d.db, d.cfgSvc, d.logger); err != nil {
		return errors.New(op).WithErr(err).WithMsg("ensure default logbook")
	}
	d.cfg = d.cfgSvc.Snapshot()
	return nil
}

// ---- enrichment (promoted node) ----

// initEnrichment constructs the shared lookup runtime ONCE (the operator ruling: both ft8 and http
// consume this single instance). A construction error is FATAL — it prevents the dependent fleet
// Starts and the orchestrator attributes it to this node.
func (d *daemon) initEnrichment() error {
	const op errors.Op = "smd.initEnrichment"
	orch, ref, err := buildEnrichment(d.workerCtx, d.cfg, d.cfgSvc, d.db, d.refDB, d.logger)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("build enrichment pipeline")
	}
	d.enrich, d.refresher = orch, ref
	return nil
}

// startEnrichment starts the refresher (buildEnrichment already Initialized it). With no providers
// enabled the refresher is nil and there is nothing to start.
func (d *daemon) startEnrichment(ctx context.Context) error {
	if d.refresher == nil {
		return nil
	}
	return d.refresher.Start(ctx)
}

func (d *daemon) closeEnrichment() error {
	if d.refresher == nil {
		return nil
	}
	return d.refresher.Stop()
}

func (d *daemon) stopEnrichment(context.Context) error { return d.closeEnrichment() }

// ---- mailer (promoted node) ----

func (d *daemon) initMailer() error {
	d.mailer = email.New(d.cfg.Smtp, d.logger)
	return nil
}

// ---- evidence (always active, fail-soft) ----

func (d *daemon) initEvidence() error {
	evidencePath := evidence.RelocateArchive(
		filepath.Join(d.cfgSvc.WorkingDir(), "evidence.db"),
		filepath.Join(d.logDBDir, "evidence.db"),
		d.logger,
	)
	evCfg := evidence.Config{
		Capture:  d.cfg.Evidence.Capture,
		CapBytes: d.cfg.Evidence.CapBytes,
		Path:     evidencePath,
		Antennas: d.cfg.Evidence.Antennas,
	}
	if d.cfg.Evidence.Sync {
		if url, token, err := config.EvidenceSyncCredentials(d.cfg); err != nil {
			d.logger.ErrorWith().Err(err).Msg("evidence: sync enabled but credentials unresolved; sync stays off")
		} else {
			evCfg.Sync, evCfg.SyncURL, evCfg.SyncToken = true, url, token
		}
	}
	d.evidence = evidence.New(evCfg, d.logger)
	if err := d.evidence.Initialize(); err != nil {
		d.logger.ErrorWith().Err(err).Msg("evidence: init failed; capture stays idle") // fail-soft
		return nil
	}
	d.evidenceInit = true
	return nil
}

func (d *daemon) startEvidence(context.Context) error {
	if !d.evidenceInit {
		return nil
	}
	if err := d.evidence.Start(); err != nil {
		d.logger.ErrorWith().Err(err).Msg("evidence: start failed; capture stays idle") // fail-soft
		return nil
	}
	d.evidenceReady = true
	return nil
}

func (d *daemon) stopEvidenceSvc() {
	if d.evidence != nil {
		d.evidence.Stop()
	}
}

// ---- psk (always active, fail-soft start) ----

func (d *daemon) initPsk() error {
	d.pskRxCall = strings.TrimSpace(d.cfg.LoggingStation.StationCallsign)
	if d.pskRxCall == "" {
		d.pskRxCall = strings.TrimSpace(d.cfg.LoggingStation.Operator)
	}
	d.psk = pskreporter.New(
		pskreporter.Config{
			Enabled:   d.cfg.PskReporter.Enabled,
			Host:      d.cfg.PskReporter.Host,
			Port:      d.cfg.PskReporter.Port,
			StatePath: filepath.Join(d.cfgSvc.WorkingDir(), "pskreporter.id"),
		},
		pskreporter.Receiver{
			Call:     d.pskRxCall,
			Locator:  strings.TrimSpace(d.cfg.LoggingStation.MyGridsquare),
			Software: "StationManager " + buildinfo.Version,
			Antenna:  strings.TrimSpace(d.cfg.LoggingStation.MyAntenna),
		},
		d.logger,
	)
	return nil
}

func (d *daemon) startPsk(ctx context.Context) error {
	if err := d.psk.Start(ctx); err != nil {
		d.logger.WarnWith().Err(err).Msg("pskreporter: start failed; FT8 spot upload disabled") // fail-soft
	}
	return nil
}

func (d *daemon) stopPskSvc() error {
	if d.psk == nil {
		return nil
	}
	return d.psk.Stop()
}

// ---- ft8 ----

// initFt8 wires the FT8 subsystem's seams (all constructed by now) and runs its Initialize. Pruned
// when FT8 is config-disabled, so this never wires against a rig that is off.
func (d *daemon) initFt8() error {
	const op errors.Op = "smd.initFt8"
	d.ft8.SetTxKeyer(ft8Keyer{d.bridge})
	if types.ResolveFt8InhibitIdle(d.cfg.ActiveFt8().TX) {
		d.ft8.SetIdleInhibitor(inhibit.New(d.logger))
	}
	if d.bridge.Enabled() {
		d.ft8.SetCatGate(d.bridge.RigConnected)
		d.ft8.SetDialSource(d.bridge.CurrentDialMHz)
		d.ft8.SetCaptureListener(d.bridge.SetFt8CaptureLive)
	}
	d.ft8.SetQsoLogger(d.ft8QsoLogger())
	if d.evidenceReady && d.cfg.Evidence.Capture {
		d.ft8.SetEvidenceSink(d.evidenceSink())
	}
	d.ft8.SetDecodeSink(d.pskDecodeSink())
	if err := d.ft8.Initialize(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("initialize ft8")
	}
	return nil
}

func (d *daemon) evidenceSink() func(ft8.EvidenceSlot) {
	return func(es ft8.EvidenceSlot) {
		start, err := time.Parse(time.RFC3339, es.Slot.StartUTC)
		if err != nil {
			return // a malformed slot ref cannot be archived honestly
		}
		d.evidence.CaptureSlot(evidence.SlotCapture{
			SlotStart:   start,
			DialMHz:     es.DialMHz,
			DialTracked: es.DialTracked,
			Outcome:     evidence.SlotOutcome(es.Outcome),
			Decodes:     es.Decodes,
		})
	}
}

func (d *daemon) pskDecodeSink() func(ft8.DecodeReport) {
	return func(r ft8.DecodeReport) {
		if !d.cfg.PskReporter.Enabled || d.pskRxCall == "" {
			return
		}
		dialMHz := r.DialMHz
		if dialMHz == 0 {
			return
		}
		t, err := time.Parse(time.RFC3339, r.Slot.StartUTC)
		if err != nil {
			return
		}
		dialHz := uint32(dialMHz * 1e6)
		unix := uint32(t.Unix())
		for _, dec := range r.Decodes {
			call, grid, ok := ft8.SpotFrom(dec.Text)
			if !ok || call == d.pskRxCall {
				continue
			}
			d.psk.AddSpot(pskreporter.Spot{
				Call:     call,
				Grid:     grid,
				FreqHz:   dialHz + uint32(dec.FreqHz),
				SNR:      int8(dec.SNR),
				Mode:     "FT8",
				TimeUnix: unix,
			})
		}
	}
}

// ft8QsoLogger builds the completed-FT8-exchange log+enrich sink (run's off-pipeline goroutine).
func (d *daemon) ft8QsoLogger() func(context.Context, ft8.CompletedQso) {
	ft8LogPanic := func(name string, pv any, stack []byte) {
		d.logger.ErrorWith().Str("goroutine", name).Interface("panic", pv).
			Str("stack", string(stack)).Msg("ft8: qso-log goroutine panicked")
	}
	return func(_ context.Context, c ft8.CompletedQso) {
		launchFt8QsoLog(&d.qsoLogWG, ft8LogPanic, func(ctx context.Context) {
			snap := d.cfgSvc.Snapshot()
			c.DialFreqMHz = resolveQsoDialMHz(c.DialFreqMHz, d.bridge.CurrentDialMHz)
			logbookID := c.LogbookID
			if logbookID < 1 {
				logbookID = snap.DefaultLogbookID
			}
			q := ft8.BuildQso(c, snap.LoggingStation, logbookID, time.Now().UTC(), d.logger)
			if q.ContestId == "ARRL-FD" && q.RstRcvd == "" && snap.Ft8.FieldDay != nil {
				q.RstRcvd = strings.TrimSpace(snap.Ft8.FieldDay.DefaultRstRcvd)
			}
			if d.enrich != nil {
				enr := d.enrich.Enrich(ctx, q.Call)
				grid := q.Gridsquare
				q.ContactedStation = enr.Station
				q.Call = c.TheirCall
				if grid != "" {
					q.Gridsquare = grid
				}
				q.CountryDetails = enr.Country
			}
			rawW := float64(d.bridge.CurrentPowerW())
			if rawW == 0 {
				rawW = snap.Station.DefaultPower
			}
			if rawW > 0 {
				mult := 1.0
				if snap.Station.AmpEnabled && snap.Station.AmpMultiplier > 0 {
					mult = snap.Station.AmpMultiplier
				}
				q.TxPwr = strconv.Itoa(int(math.Round(rawW * mult)))
			}
			rec := adif.QsoToRecord(q)
			rec.ApplyQslDefaults(snap.Qsl)
			res, err := d.qso.Submit(ctx, q.LogbookID, rec, c.AllowDuplicate)
			if err != nil {
				d.logger.ErrorWith().Err(err).Str("call", c.TheirCall).
					Msg("ft8: failed to log completed QSO")
				return
			}
			if res.Status == "duplicate" {
				d.logger.WarnWith().Str("call", c.TheirCall).Str("band", q.Band).Str("uuid", res.UUID).
					Msg("ft8: completed QSO matched an existing row and was NOT stored — same station, band and minute")
			}
			d.logger.InfoWith().Str("call", c.TheirCall).Str("band", q.Band).
				Str("country", q.Country).Msg("ft8: completed QSO logged")
			d.ft8.PublishQsoLogged(ft8.NewLoggedQso(q, res.UUID, d.logger))
		})
	}
}

// ---- forwarder workers + smcloud reconciler ----

// startWorkers runs the orphan sweep + disabled-forwarder discard, spawns the forwarder workers, and
// launches the first enabled smcloud reconciler (which rides the workers' WaitGroup lifecycle).
func (d *daemon) startWorkers(ctx context.Context) error {
	const op errors.Op = "smd.startWorkers"

	// The workers + reconciler run on their OWN cancelable child of ctx so drainWorkers (the node's
	// Rollback) can drain them WITHOUT cancelling ctx — which is also the orchestrator's Start/rollback
	// context; cancelling it would make runBounded see rollback as timed out and halt the rest of the
	// unwind (codex 1fbbd41d P2). run()'s workerCancel cancels ctx's parent, which still cascades here.
	fctx, fcancel := context.WithCancel(ctx)
	d.forwarderCancel = fcancel

	sweepCtx, sweepCancel := context.WithTimeout(context.Background(), 10*time.Second)
	n, err := d.db.ResetOrphanedUploadsWithContext(sweepCtx)
	sweepCancel()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("reset orphaned upload rows")
	}
	if n > 0 {
		d.logger.InfoWith().Int64("reset", n).Msg("forwarder: orphaned in_progress rows reset to pending")
	}
	for _, fc := range d.cfg.Forwarders {
		if fc.Enabled {
			continue
		}
		discardCtx, discardCancel := context.WithTimeout(context.Background(), 10*time.Second)
		discarded, derr := d.db.DiscardQueuedUploadsForForwarderWithContext(discardCtx, fc.Name)
		discardCancel()
		if derr != nil {
			return errors.New(op).WithErr(derr).WithMsgf("discard queued uploads for disabled forwarder %q", fc.Name)
		}
		if discarded > 0 {
			d.logger.WarnWith().Str("forwarder", fc.Name).Int64("discarded", discarded).
				Msg("forwarder disabled; discarded queued uploads (re-upload via the logbook app)")
		}
	}

	if err := spawnForwarderWorkers(fctx, &d.workerWG, d.cfg.Forwarders, d.db, d.qso, d.logger, d.hub); err != nil {
		return errors.New(op).WithErr(err).WithMsg("spawn forwarder workers")
	}

	d.startReconciler(fctx)
	return nil
}

// drainWorkers cancels the (partially-spawned) forwarder workers then waits them out. It is the
// workers node's Rollback: on a startWorkers failure the workers are not yet cancelled (run()'s
// deferred workerCancel fires only after run returns). It cancels ONLY the forwarder child context,
// never ctx/workerCtx, so it cannot halt the orchestrator's rollback of the remaining nodes.
func (d *daemon) drainWorkers() error {
	if d.forwarderCancel != nil {
		d.forwarderCancel()
	}
	d.workerWG.Wait()
	return nil
}

// workerPrepareStop is the workers node's non-blocking PrepareStop: it cancels the forwarder child
// context so the workers begin exiting before the drain reaches workers.Stop (which Wait()s them).
func (d *daemon) workerPrepareStop() {
	if d.forwarderCancel != nil {
		d.forwarderCancel()
	}
}

func (d *daemon) startReconciler(ctx context.Context) {
	for _, fc := range d.cfg.Forwarders {
		if !fc.Enabled || fc.Type != smcloud.Type {
			continue
		}
		if d.cfg.DefaultLogbookID < 1 {
			d.logger.WarnWith().Str("forwarder", fc.Name).
				Msg("smcloud reconciler skipped: no default logbook yet (first-run setup pending)")
			return
		}
		rec, rerr := smcloud.NewReconciler(fc, d.cfg.DefaultLogbookID, d.db, d.qso, d.logger)
		if rerr != nil {
			d.logger.ErrorWith().Err(rerr).Str("forwarder", fc.Name).Msg("smcloud reconciler build failed")
			return
		}
		d.smcloudRec = rec
		reconcilePanic := func(name string, pv any, stack []byte) {
			d.logger.ErrorWith().Str("goroutine", name).Interface("panic", pv).
				Bytes("stack", stack).Msg("smcloud reconciler panic recovered")
		}
		safego.GoTracked(ctx, fc.Name+"-reconcile", reconcilePanic, func() { rec.Run(ctx) }, true, &d.workerWG)
		d.logger.InfoWith().Str("forwarder", fc.Name).Int64("logbook_id", d.cfg.DefaultLogbookID).
			Msg("smcloud reconciler started")
		return
	}
}

// ---- http server ----

func (d *daemon) initHTTP() error {
	d.server = api.New(d.cfg, buildinfo.Version, d.cfgSvc, d.qso, d.db, d.logger, d.hub, d.enrich, d.mailer, d.bridge, d.ft8)
	d.server.SetEvidence(d.evidence)
	if d.smcloudRec != nil {
		d.server.SetSmcloudReconcile(func(ctx context.Context) (any, error) {
			return d.smcloudRec.RunOnce(ctx, smcloud.TriggerManual)
		})
	}
	if os.Getenv("SM_SELF_RESTART") == "1" {
		d.server.SetRestart(func() {
			d.restartOnce.Do(func() {
				restartRequested.Store(true)
				close(d.restartCh)
			})
		})
	}
	return nil
}

// startHTTP launches ListenAndServe on the daemon socket; a bind error lands on errCh, which run()'s
// wait loop selects on (exactly as before).
func (d *daemon) startHTTP(context.Context) error {
	go func() { d.errCh <- d.server.ListenAndServe(d.cfg.SocketPath) }()
	return nil
}

func (d *daemon) httpStopAccepting() {
	if d.server != nil {
		d.server.StopAccepting()
	}
}

func (d *daemon) stopHTTP(ctx context.Context) error {
	if d.server == nil {
		return nil
	}
	return d.server.Shutdown(ctx)
}
