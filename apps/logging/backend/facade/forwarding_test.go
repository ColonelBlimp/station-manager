package facade

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
)

func TestForwardingStart(t *testing.T) {
	logger := &logging.Service{} // Mock logger

	tests := []struct {
		name    string
		ctx     context.Context
		wantErr bool
	}{
		{
			name:    "valid context",
			ctx:     context.Background(),
			wantErr: false,
		},
		{
			name:    "nil context",
			ctx:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &forwarding{
				pollInterval:    1 * time.Second,
				maxWorkers:      2,
				forwardingQueue: make(chan types.QsoUpload, 10),
				dbWriteQueue:    make(chan func() error, 10),
				fetchPending: func() ([]types.QsoUpload, error) {
					return nil, nil
				},
				sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
					return nil
				},
				logger: logger,
			}

			shutdown := make(chan struct{})
			err := f.start(tt.ctx, shutdown)

			if (err != nil) != tt.wantErr {
				t.Errorf("start() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify workers were started
				time.Sleep(100 * time.Millisecond)
				activeCount := f.ActiveWorkerCount()
				expectedWorkers := f.maxWorkers + 2 // workers + db-writer + poller
				if activeCount != expectedWorkers {
					t.Errorf("ActiveWorkerCount() = %d, want %d", activeCount, expectedWorkers)
				}

				// Stop workers
				close(shutdown)
				_ = f.stop(2 * time.Second)
			}
		})
	}
}

func TestForwardingStartIdempotent(t *testing.T) {
	logger := &logging.Service{}

	f := &forwarding{
		pollInterval:    1 * time.Second,
		maxWorkers:      2,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			return nil, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			return nil
		},
		logger: logger,
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// First start should succeed
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("First start() failed: %v", err)
	}

	// Second start should fail (already started)
	err = f.start(ctx, shutdown)
	if err == nil {
		t.Error("Second start() should fail but succeeded")
	}

	// Cleanup
	close(shutdown)
	_ = f.stop(2 * time.Second)
}

func TestForwardingStartDuringStop(t *testing.T) {
	logger := &logging.Service{}

	f := &forwarding{
		pollInterval:    1 * time.Second,
		maxWorkers:      2,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			return nil, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			return nil
		},
		logger: logger,
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// Start the forwarder
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)

	// Signal shutdown
	close(shutdown)

	// Immediately try to start again while stop is happening
	// This should fail because stopping flag is set
	go func() {
		_ = f.stop(5 * time.Second)
	}()

	// Give stop a moment to set the stopping flag
	time.Sleep(10 * time.Millisecond)

	// Try to start - should fail because we're stopping
	err = f.start(ctx, make(chan struct{}))
	if err == nil {
		t.Error("start() during stop should fail but succeeded")
	}

	// Wait for stop to complete
	time.Sleep(200 * time.Millisecond)
}

func TestForwardingStop(t *testing.T) {
	logger := &logging.Service{}

	f := &forwarding{
		pollInterval:    1 * time.Second,
		maxWorkers:      2,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			return nil, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			return nil
		},
		logger: logger,
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// Start workers
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Give workers time to start
	time.Sleep(200 * time.Millisecond)

	// Signal shutdown and stop workers with longer timeout
	close(shutdown)
	err = f.stop(5 * time.Second)
	if err != nil {
		t.Errorf("stop() error = %v", err)
	}

	// Verify all workers stopped
	activeCount := f.ActiveWorkerCount()
	if activeCount != 0 {
		t.Errorf("ActiveWorkerCount() = %d after stop, want 0", activeCount)
	}
}

func TestForwardingStopTimeout(t *testing.T) {
	logger := &logging.Service{}

	// Create a worker that will block
	blockingWorker := make(chan struct{})

	f := &forwarding{
		pollInterval:    1 * time.Second,
		maxWorkers:      1,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			<-blockingWorker // Block until test ends
			return nil, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			return nil
		},
		logger: logger,
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// Start workers
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)

	// Stop with very short timeout (workers won't finish in time)
	err = f.stop(10 * time.Millisecond)
	if err == nil {
		t.Error("stop() should timeout but succeeded")
	}

	// Unblock the worker
	close(blockingWorker)

	// Give workers time to actually stop
	time.Sleep(100 * time.Millisecond)
}

func TestForwardingWorkerLoop(t *testing.T) {
	logger := &logging.Service{}

	processedCount := atomic.Int32{}
	errorCount := atomic.Int32{}

	f := &forwarding{
		pollInterval:    1 * time.Second,
		maxWorkers:      2,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			return nil, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			processedCount.Add(1)
			if qsoUpload.ID == 999 {
				errorCount.Add(1)
				return errors.New("simulated error")
			}
			return nil
		},
		logger: logger,
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// Start workers
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Send some test QSO uploads
	testUploads := []types.QsoUpload{
		{ID: 1, QsoID: 100, Service: "test"},
		{ID: 2, QsoID: 101, Service: "test"},
		{ID: 999, QsoID: 102, Service: "test"}, // This one will error
	}

	for _, upload := range testUploads {
		f.forwardingQueue <- upload
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify results
	if processedCount.Load() != 3 {
		t.Errorf("Processed count = %d, want 3", processedCount.Load())
	}

	if errorCount.Load() != 1 {
		t.Errorf("Error count = %d, want 1", errorCount.Load())
	}

	// Cleanup
	close(shutdown)
	_ = f.stop(2 * time.Second)
}

func TestForwardingPollerLoop(t *testing.T) {
	logger := &logging.Service{}

	fetchCallCount := atomic.Int32{}
	enqueueCount := atomic.Int32{}

	f := &forwarding{
		pollInterval:    100 * time.Millisecond, // Fast polling for test
		maxWorkers:      1,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			fetchCallCount.Add(1)
			// Return some pending uploads
			return []types.QsoUpload{
				{ID: 1, QsoID: 100, Service: "test"},
			}, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			enqueueCount.Add(1)
			return nil
		},
		logger: logger,
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// Start workers
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Wait for a few poll cycles
	time.Sleep(350 * time.Millisecond)

	// Should have polled at least 3 times
	if fetchCallCount.Load() < 3 {
		t.Errorf("Fetch call count = %d, want >= 3", fetchCallCount.Load())
	}

	// Cleanup
	close(shutdown)
	_ = f.stop(2 * time.Second)
}

func TestForwardingDBWriteWorker(t *testing.T) {
	logger := &logging.Service{}

	executedOps := atomic.Int32{}
	var mu sync.Mutex
	executionOrder := []int{}

	f := &forwarding{
		pollInterval:    1 * time.Second,
		maxWorkers:      2,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			return nil, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			return nil
		},
		logger: logger,
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// Start workers
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Send some DB write operations
	for i := 1; i <= 5; i++ {
		opNum := i
		f.dbWriteQueue <- func() error {
			executedOps.Add(1)
			mu.Lock()
			executionOrder = append(executionOrder, opNum)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond) // Simulate DB write
			return nil
		}
	}

	// Wait for all operations to complete
	time.Sleep(200 * time.Millisecond)

	// Verify all operations executed
	if executedOps.Load() != 5 {
		t.Errorf("Executed operations = %d, want 5", executedOps.Load())
	}

	// Verify operations executed in order (serialized)
	mu.Lock()
	for i, opNum := range executionOrder {
		if opNum != i+1 {
			t.Errorf("Operation at index %d = %d, want %d (operations not serialized)", i, opNum, i+1)
		}
	}
	mu.Unlock()

	// Cleanup
	close(shutdown)
	_ = f.stop(2 * time.Second)
}

func TestLaunchWorker(t *testing.T) {
	logger := &logging.Service{}

	f := &forwarding{
		logger: logger,
	}

	workExecuted := make(chan bool, 1)
	workerPanicked := make(chan interface{}, 1)

	// Test normal execution
	f.launchWorker("test-worker", func() {
		workExecuted <- true
	})

	select {
	case <-workExecuted:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Worker did not execute work")
	}

	// Test panic recovery
	f.launchWorker("panic-worker", func() {
		defer func() {
			if r := recover(); r != nil {
				workerPanicked <- r
			}
		}()
		panic("test panic")
	})

	// Worker should recover from panic and not crash the test
	time.Sleep(100 * time.Millisecond)

	// Verify worker was cleaned up from registry
	time.Sleep(100 * time.Millisecond)
	count := f.ActiveWorkerCount()
	if count != 0 {
		t.Errorf("ActiveWorkerCount() = %d after workers finished, want 0", count)
	}
}

func TestActiveWorkerCount(t *testing.T) {
	logger := &logging.Service{}

	f := &forwarding{
		pollInterval:    1 * time.Second,
		maxWorkers:      3,
		forwardingQueue: make(chan types.QsoUpload, 10),
		dbWriteQueue:    make(chan func() error, 10),
		fetchPending: func() ([]types.QsoUpload, error) {
			return nil, nil
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			return nil
		},
		logger: logger,
	}

	// Initially should be 0
	if count := f.ActiveWorkerCount(); count != 0 {
		t.Errorf("Initial ActiveWorkerCount() = %d, want 0", count)
	}

	ctx := context.Background()
	shutdown := make(chan struct{})

	// Start workers
	err := f.start(ctx, shutdown)
	if err != nil {
		t.Fatalf("start() failed: %v", err)
	}

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)

	// Should have maxWorkers + db-writer + poller
	expectedCount := f.maxWorkers + 2
	count := f.ActiveWorkerCount()
	if count != expectedCount {
		t.Errorf("ActiveWorkerCount() after start = %d, want %d", count, expectedCount)
	}

	// Stop workers
	close(shutdown)
	_ = f.stop(2 * time.Second)

	// Should be 0 again
	if count := f.ActiveWorkerCount(); count != 0 {
		t.Errorf("ActiveWorkerCount() after stop = %d, want 0", count)
	}
}
