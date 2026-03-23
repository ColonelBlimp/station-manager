package facade

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
)

// defaultPollInterval is the fallback interval for the forwarding poller when no valid interval is configured.
const defaultPollInterval = 120 * time.Second

type forwarding struct {
	pollInterval    time.Duration
	maxWorkers      int
	forwardingQueue chan types.QsoUpload
	dbWriteQueue    chan func() error
	fetchPending    func() ([]types.QsoUpload, error)     // See: s.DatabaseService.FetchPendingUploads()
	sendAndMarkDone func(qsoUpload types.QsoUpload) error // See: s.forwardQso(qsoUpload)
	logger          *logging.Service

	// Worker lifecycle management
	wg             sync.WaitGroup
	workerRegistry sync.Map // map[string]bool - tracks running workers
	started        atomic.Bool
	stopping       atomic.Bool
}

// start initializes and starts the worker and polling goroutines for the forwarding process. Returns an error if context is nil.
func (f *forwarding) start(ctx context.Context, shutdown <-chan struct{}) error {
	const op errors.Op = "forwarding.start"
	if ctx == nil {
		return errors.New(op).Msg("Context is nil")
	}

	// Check if we're in the middle of stopping - prevents race with stop()
	if f.stopping.Load() {
		return errors.New(op).Msg("Forwarding is stopping, cannot start")
	}

	if !f.started.CompareAndSwap(false, true) {
		return errors.New(op).Msg("Forwarding already started")
	}

	// Double-check stopping flag after setting started (prevents TOCTOU race)
	if f.stopping.Load() {
		f.started.Store(false)
		return errors.New(op).Msg("Forwarding is stopping, cannot start")
	}

	// Start worker goroutines with tracking
	for i := 0; i < f.maxWorkers; i++ {
		workerID := i
		workerName := fmt.Sprintf("worker-%d", workerID)

		f.launchWorker(workerName, func() {
			f.workerLoop(ctx, shutdown, workerID)
		})
	}

	// Start the database write worker
	f.launchWorker("db-writer", func() {
		f.dbWriteWorkerLoop(ctx, shutdown)
	})

	// Start the polling goroutine
	f.launchWorker("poller", func() {
		f.pollerLoop(ctx, shutdown)
	})

	f.logger.InfoWith().
		Int("workers", f.maxWorkers+2).
		Msg("All forwarding workers launched")

	return nil
}

// launchWorker starts a worker goroutine with proper lifecycle tracking.
// The work function is a closure that captures ctx and shutdown from the calling context.
func (f *forwarding) launchWorker(name string, work func()) {
	// Register worker before launching
	f.workerRegistry.Store(name, true)

	f.wg.Add(1)
	go func() {
		defer func() {
			// Cleanup on exit
			f.workerRegistry.Delete(name)
			f.wg.Done()
			f.logger.DebugWith().Str("worker", name).Msg("Worker exited")

			// Recover from panics
			if r := recover(); r != nil {
				f.logger.ErrorWith().
					Str("worker", name).
					Interface("panic", r).
					Msg("Worker panicked")
			}
		}()

		f.logger.DebugWith().Str("worker", name).Msg("Worker started")

		// Run the actual work (work closure already has ctx and shutdown captured)
		work()
	}()
}

// ActiveWorkerCount returns the number of currently running workers
func (f *forwarding) ActiveWorkerCount() int {
	count := 0
	f.workerRegistry.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// stop gracefully shuts down all workers with timeout
func (f *forwarding) stop(timeout time.Duration) error {
	if !f.stopping.CompareAndSwap(false, true) {
		return nil // Already stopping
	}

	f.logger.InfoWith().Msg("Stopping forwarding workers")

	// Note: We do NOT close the channels here because workers might still be
	// writing to them (e.g., pollerLoop writes to forwardingQueue, sendAndMarkDone
	// writes to dbWriteQueue). Closing a channel while another goroutine is
	// sending to it causes a panic.
	//
	// Instead, workers will exit when they receive the shutdown signal or
	// context cancellation. The channels will be garbage collected after
	// all references are gone.

	// Wait with timeout for workers to exit
	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		f.logger.InfoWith().Msg("All forwarding workers stopped gracefully")
		// Now it's safe to close the channels since all workers have exited
		if f.forwardingQueue != nil {
			close(f.forwardingQueue)
		}
		if f.dbWriteQueue != nil {
			close(f.dbWriteQueue)
		}
		// Reset started flag after successful stop
		f.started.Store(false)
		return nil
	case <-time.After(timeout):
		activeWorkers := f.ActiveWorkerCount()
		f.logger.ErrorWith().
			Int("stuck_workers", activeWorkers).
			Msg("Timeout waiting for workers to stop")

		// Log which workers are still running
		f.workerRegistry.Range(func(key, value interface{}) bool {
			f.logger.WarnWith().
				Str("worker", key.(string)).
				Msg("Worker still running after timeout")
			return true
		})

		return errors.New("forwarding.stop").
			Msgf("Timeout after %v with %d workers still running", timeout, activeWorkers)
	}
}

// pollerLoop starts a loop that periodically fetches pending QSO uploads and attempts to enqueue them for processing.
func (f *forwarding) pollerLoop(ctx context.Context, shutdown <-chan struct{}) {
	f.logger.InfoWith().Msg("Starting forwarding poller")

	// Validate poll interval to prevent panic from NewTicker with non-positive duration
	pollInterval := f.pollInterval
	if pollInterval <= 0 {
		f.logger.WarnWith().
			Dur("configured_interval", pollInterval).
			Dur("fallback_interval", defaultPollInterval).
			Msg("Invalid poll interval configured, using default")
		pollInterval = defaultPollInterval
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-shutdown:
			return
		case <-ticker.C:
			// Check if we're stopping before doing any work
			if f.stopping.Load() {
				return
			}

			qsoUploads, err := f.fetchPending()
			if err != nil {
				f.logger.ErrorWith().Err(err).Msg("Failed to fetch pending uploads")
				continue
			}
			for _, qsoUpload := range qsoUploads {
				// Check stopping flag before each send attempt to avoid sending on closed channel
				if f.stopping.Load() {
					return
				}

				select {
				case f.forwardingQueue <- qsoUpload:
					// forwarded to the forwarding queue
				case <-ctx.Done():
					return
				case <-shutdown:
					return
				default:
					f.logger.WarnWith().
						Int64("upload_id", qsoUpload.ID).
						Msg("Forwarding queue full, dropping upload")
				}
			}
		}
	}
}

// workerLoop runs a worker goroutine to process QSO uploads from the forwarding queue until shutdown or context cancellation.
func (f *forwarding) workerLoop(ctx context.Context, shutdown <-chan struct{}, workerID int) {
	f.logger.InfoWith().Int("workerID", workerID).Msg("Starting forwarding worker")

	for {
		select {
		case <-ctx.Done():
			f.logger.InfoWith().Msg("Context done, shutting down forwarding worker")
			return
		case <-shutdown:
			f.logger.InfoWith().Msg("Forwarding worker shutting down")
			return
		case qsoUpload, ok := <-f.forwardingQueue:
			if !ok {
				return
			}

			// Do network call (can be concurrent)
			err := f.sendAndMarkDone(qsoUpload)

			// Note: Database writes are now handled within sendAndMarkDone via the dbWriteQueue
			// This maintains backward compatibility while ensuring serialized DB access
			if err != nil {
				f.logger.ErrorWith().Err(err).Msg("Error in processing or forwarding QSO")
			}
		}
	}
}

func (f *forwarding) dbWriteWorkerLoop(ctx context.Context, shutdown <-chan struct{}) {
	f.logger.InfoWith().Msg("Starting database write worker")

	for {
		select {
		case <-ctx.Done():
			return
		case <-shutdown:
			return
		case writeOp, ok := <-f.dbWriteQueue:
			if !ok {
				return
			}
			if err := writeOp(); err != nil {
				f.logger.ErrorWith().Err(err).Msg("Database write operation failed")
			}
		}
	}
}
