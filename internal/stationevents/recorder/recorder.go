// Package recorder is the assembly-owned, non-blocking, bounded recorder of
// Station Events (W-0020, ADR 0076 §4/§5): producers hand it typed facts
// through the bridge and FT8 observer seams, a worker goroutine converts each
// fact to a row and writes it in the store's own best-effort transaction.
//
// The safety rule it exists for: a synchronous SQLite write from TX
// confirmation would stall the CAT read loop, and one from FT8 teardown would
// hold the sequencing gates. So every producer-facing call ENQUEUES and returns
// — never blocks, never retries, never touches the database on the producer's
// goroutine. Capacity is 64; overflow drops the NEWEST fact (ruling 8: the
// beginning and cause of a burst are the rows worth keeping) and logs one
// warning per drop carrying the kind and the recorder's cumulative drop count.
// A failed write is logged the same way and never retried.
package recorder

import (
	"context"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/stationevents"
)

// QueueCapacity is the bounded queue depth (operator ruling 8, 2026-09-14).
// The alarm probe cadence bounds any real burst far below it.
const QueueCapacity = 64

// Store is the one store method the recorder needs; *sqlite.Service satisfies
// it. Narrow so the AC8 tests can stall it.
type Store interface {
	RecordOperatorEvent(ctx context.Context, ev sqlite.OperatorEventInput) error
}

// Recorder is a single-use lifecycle service: New → Start(ctx) → Stop.
// Its method set satisfies bridge.AlarmObserver and ft8.SessionObserver (the
// compile-time assertions live in recorder_test.go, keeping this package free
// of both imports).
type Recorder struct {
	store Store
	log   logging.Logger
	build string

	// life is the ADR 0070 supervisor. Producers admit on enqueueLane
	// (MustDrain: an admitted enqueue always completes); the worker rides
	// workerLane (Cancellable: Stop cancels its context, which the loop reads
	// only BETWEEN writes — a write in flight completes, bounded by the store's
	// own context timeout, so a healthy store never loses the row Stop found
	// mid-write). Whatever is still queued when the worker has exited is
	// written by teardown — Stop owns the final drain, as the PSK uploader owns
	// its final flush.
	life        *lifecycle.Supervisor
	enqueueLane *lifecycle.Lane
	workerLane  *lifecycle.Lane

	queue chan stationevents.Fact

	dropMu sync.Mutex
	drops  uint64 // cumulative drop count (ruling 8), from any goroutine
}

// New builds the recorder. build is the daemon's canonical build string,
// stamped on every row (ADR 0076 §7). A nil logger becomes a no-op.
func New(store Store, log logging.Logger, build string) *Recorder {
	if log == nil {
		log = logging.Noop()
	}
	l := lifecycle.New()
	return &Recorder{
		store: store, log: log, build: build,
		life:        l,
		enqueueLane: l.RegisterLane("enqueue", lifecycle.MustDrain),
		workerLane:  l.RegisterLane("worker", lifecycle.Cancellable),
		queue:       make(chan stationevents.Fact, QueueCapacity),
	}
}

// Start launches the worker. Nothing to acquire: the store is the daemon's
// already-open log database.
func (r *Recorder) Start(ctx context.Context) error {
	return r.life.Start(ctx, nil, r.launch)
}

func (r *Recorder) launch(ctx context.Context, sc *lifecycle.StartScope) {
	done := sc.Track(r.workerLane)
	safego.GoCompletion(ctx, "stationevents.recorder", r.onPanic, func() { r.loop(ctx) }, true, done)
}

// Stop seals admission (later facts are dropped and logged), lets the worker
// finish its current write and exit, then drains what is still queued — each
// remaining fact written with the store's own timeout, stopping at the first
// failure because a store that just failed will not succeed for the next fact
// either; the rest are logged as dropped with the count. A stalled store
// therefore holds Stop for at most two of the store's timeouts.
func (r *Recorder) Stop() error {
	return r.life.Stop(r.teardown)
}

// ---- producer-facing seams (bridge.AlarmObserver, ft8.SessionObserver) ----

// ArchiveActivated / ArchiveActivationFailed are the archive switch outcomes
// (ADR 0071): recorded by the assembly at the lifecycle boundary — the
// promotion node after its write succeeds, the fallback generation once its
// events node is up. Best-effort like every fact: a full queue drops and logs.
func (r *Recorder) ArchiveActivated(archiveID, label string, at time.Time) {
	r.enqueue(stationevents.ArchiveActivated{ArchiveID: archiveID, Label: label, At: at})
}

func (r *Recorder) ArchiveActivationFailed(archiveID, label, code string, at time.Time) {
	r.enqueue(stationevents.ArchiveActivationFailed{ArchiveID: archiveID, Label: label, Code: code, At: at})
}

func (r *Recorder) TxAlarmRaised(code string, at time.Time) {
	r.enqueue(stationevents.TxAlarmRaised{Code: code, At: at})
}

func (r *Recorder) TxAlarmCleared(code string, raisedAt, at time.Time) {
	r.enqueue(stationevents.TxAlarmCleared{Code: code, RaisedAt: raisedAt, At: at})
}

func (r *Recorder) DriveAlarmRaised(code string, at time.Time) {
	r.enqueue(stationevents.DriveAlarmRaised{Code: code, At: at})
}

func (r *Recorder) DriveAlarmRecovered(code string, at time.Time) {
	r.enqueue(stationevents.DriveAlarmRecovered{Code: code, At: at})
}

func (r *Recorder) ExchangeTerminated(cause, partnerCall, rung string, at time.Time) {
	r.enqueue(stationevents.ExchangeTerminated{Cause: cause, PartnerCall: partnerCall, Rung: rung, At: at})
}

func (r *Recorder) TxDisarmed(cause string, at time.Time) {
	r.enqueue(stationevents.TxDisarmed{Cause: cause, At: at})
}

// enqueue is the ONLY producer path. It never blocks: a full queue drops this
// (newest) fact; a recorder that is not Running (before Start, or sealed at
// Stop) drops it too. Both are logged with the kind and the cumulative count.
// The admission ticket covers the send, so a sealed recorder can never see a
// send after its worker has gone — the lane is waited before teardown.
func (r *Recorder) enqueue(f stationevents.Fact) {
	done, ok := r.enqueueLane.Admit()
	if !ok {
		r.dropped(f, "recorder not running")
		return
	}
	defer done()
	select {
	case r.queue <- f:
	default:
		r.dropped(f, "queue full")
	}
}

// dropped logs one warning per dropped fact with the kind and the cumulative
// drop count (ruling 8). The counter's mutex guards an increment only, so a
// producer can never wait on it for long.
//
// Ruling 8 requires one warning for each lost fact. This log write runs on the
// producer's goroutine and can wait on the logger; the queue handoff itself
// never waits on SQLite, and no database work or retry runs on that goroutine.
func (r *Recorder) dropped(f stationevents.Fact, why string) {
	r.droppedWithErr(f, why, nil)
}

func (r *Recorder) droppedWithErr(f stationevents.Fact, why string, err error) {
	r.dropMu.Lock()
	r.drops++
	n := r.drops
	r.dropMu.Unlock()
	event := r.log.WarnWith().
		Str("kind", kindOf(f)).
		Str("reason", why).
		Uint64("drops", n)
	if err != nil {
		event = event.Err(err)
	}
	event.Msg("station events: event dropped, not recorded")
}

// Drops reports the cumulative drop count.
func (r *Recorder) Drops() uint64 {
	r.dropMu.Lock()
	defer r.dropMu.Unlock()
	return r.drops
}

// loop writes facts until the supervisor context is cancelled at Stop. The
// cancel is observed only between writes: a write carries its own uncancelled
// context so the store's timeout, not the shutdown, bounds it — cancelling a
// write in flight would lose a row the store was about to commit.
func (r *Recorder) loop(ctx context.Context) {
	for {
		// Cancellation first: a select alone picks at random when both a fact
		// and the cancel are ready, and the worker must hand the rest of the
		// queue to teardown's drain rather than race it.
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case f := <-r.queue:
			if err := r.write(f); err != nil {
				r.droppedWithErr(f, "write failed", err)
			}
		}
	}
}

// write records one fact. Its caller logs a failed fact once and never retries
// (ruling 8): the store's own transaction has already rolled back, and a retry
// loop is exactly the storage-latency coupling the recorder exists to avoid.
func (r *Recorder) write(f stationevents.Fact) error {
	return r.store.RecordOperatorEvent(context.Background(), rowFor(f, r.build))
}

// teardown drains the queue after the worker has exited and every admitted
// enqueue has completed. It stops at the first failed write.
func (r *Recorder) teardown() error {
	for {
		select {
		case f := <-r.queue:
			if err := r.write(f); err != nil {
				r.droppedWithErr(f, "write failed at shutdown", err)
				r.abandonRemaining()
				return nil
			}
		default:
			return nil
		}
	}
}

// abandonRemaining counts the queued facts after the failed write as dropped,
// one warning each, so the log names every event that never landed.
func (r *Recorder) abandonRemaining() {
	for {
		select {
		case f := <-r.queue:
			r.dropped(f, "store unavailable at shutdown")
		default:
			return
		}
	}
}

func (r *Recorder) onPanic(name string, panicValue any, stack []byte) {
	r.log.ErrorWith().
		Str("goroutine", name).
		Interface("panic", panicValue).
		Bytes("stack", stack).
		Msg("station events: recorder goroutine panicked (recovered)")
}
