package cat

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// lineProcessor tests
//
// Each test drives lineProcessor as a goroutine. To avoid non-deterministic
// time.Sleep, we use a "sentinel" pattern: after the item under test, we send
// a known-good state whose output we can read from statusChannel, guaranteeing
// the first item has already been processed.
// ---------------------------------------------------------------------------

// sentinelState returns a CatState that always produces a single "sentinel"="ok"
// status entry, so we can block on statusChannel to confirm processing.
func sentinelState() types.CatState {
	return types.CatState{
		Data:    "ok",
		Markers: []types.Marker{{Tag: "sentinel", Index: 0, Length: 2}},
	}
}

// runProcessor starts lineProcessor in a goroutine and returns a function to shut it down.
func runProcessor(t *testing.T, svc *Service) (shutdown chan struct{}, wait func()) {
	t.Helper()
	shutdown = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.lineProcessor(shutdown)
	}()
	return shutdown, func() {
		close(shutdown)
		<-done
	}
}

func TestLineProcessorEmptyMarkers(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 2),
		statusChannel:     make(chan types.CatStatus, 2),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	// State with no markers → logged error, skipped.
	svc.processingChannel <- types.CatState{Data: "test", Markers: nil}
	// Sentinel to prove the first item was consumed.
	svc.processingChannel <- sentinelState()

	status := <-svc.statusChannel
	require.Equal(t, "ok", status["sentinel"])

	// Only one status should be present (the sentinel).
	select {
	case <-svc.statusChannel:
		t.Fatal("unexpected extra status from empty-markers state")
	default:
	}
	wait()
}

func TestLineProcessorMarkerIndexNegative(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	svc.processingChannel <- types.CatState{
		Data: "ABCD",
		Markers: []types.Marker{
			{Tag: "neg", Index: -1, Length: 2}, // skipped
			{Tag: "good", Index: 0, Length: 4}, // "ABCD"
		},
	}

	status := <-svc.statusChannel
	require.Equal(t, "ABCD", status["good"])
	_, hasNeg := status["neg"]
	require.False(t, hasNeg, "negative-index marker should be omitted from status")
	wait()
}

func TestLineProcessorMarkerIndexBeyondData(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	svc.processingChannel <- types.CatState{
		Data: "AB",
		Markers: []types.Marker{
			{Tag: "oob", Index: 5, Length: 2},  // skipped
			{Tag: "good", Index: 0, Length: 2}, // "AB"
		},
	}

	status := <-svc.statusChannel
	require.Equal(t, "AB", status["good"])
	_, hasOOB := status["oob"]
	require.False(t, hasOOB)
	wait()
}

func TestLineProcessorMarkerEndClamped(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	// Data "AB" (len 2), marker Index=0, Length=10 → clamped to end=2 → "AB".
	svc.processingChannel <- types.CatState{
		Data:    "AB",
		Markers: []types.Marker{{Tag: "val", Index: 0, Length: 10}},
	}

	status := <-svc.statusChannel
	require.Equal(t, "AB", status["val"])
	wait()
}

func TestLineProcessorStartGteEnd(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	// Index=1, Length=0 → end=1, start=1 → skipped.
	svc.processingChannel <- types.CatState{
		Data: "AB",
		Markers: []types.Marker{
			{Tag: "empty", Index: 1, Length: 0},
			{Tag: "good", Index: 0, Length: 2},
		},
	}

	status := <-svc.statusChannel
	require.Equal(t, "AB", status["good"])
	_, hasEmpty := status["empty"]
	require.False(t, hasEmpty)
	wait()
}

func TestLineProcessorRawSlice(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	svc.processingChannel <- types.CatState{
		Data: "00014250000",
		Markers: []types.Marker{
			{Tag: "freq", Index: 0, Length: 11}, // no ValueMappings → raw
		},
	}

	status := <-svc.statusChannel
	require.Equal(t, "00014250000", status["freq"])
	wait()
}

func TestLineProcessorValueMappingMatched(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	svc.processingChannel <- types.CatState{
		Data: "1",
		Markers: []types.Marker{{
			Tag: "mode", Index: 0, Length: 1,
			ValueMappings: []types.ValueMapping{
				{Key: "1", Value: "LSB"},
				{Key: "2", Value: "USB"},
			},
		}},
	}

	status := <-svc.statusChannel
	require.Equal(t, "LSB", status["mode"])
	wait()
}

func TestLineProcessorValueMappingNotMatched(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	svc.processingChannel <- types.CatState{
		Data: "9",
		Markers: []types.Marker{{
			Tag: "mode", Index: 0, Length: 1,
			ValueMappings: []types.ValueMapping{
				{Key: "1", Value: "LSB"},
				{Key: "2", Value: "USB"},
			},
		}},
	}

	status := <-svc.statusChannel
	require.Equal(t, "", status["mode"], "unmatched mapping should produce empty string")
	wait()
}

func TestLineProcessorMultipleMarkers(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown, wait := runProcessor(t, svc)
	defer func() {
		select {
		case <-shutdown:
		default:
			wait()
		}
	}()

	svc.processingChannel <- types.CatState{
		Data: "14250001",
		Markers: []types.Marker{
			{Tag: "freq", Index: 0, Length: 5},
			{Tag: "mode", Index: 5, Length: 3, ValueMappings: []types.ValueMapping{
				{Key: "001", Value: "LSB"},
			}},
		},
	}

	status := <-svc.statusChannel
	require.Equal(t, "14250", status["freq"])
	require.Equal(t, "LSB", status["mode"])
	wait()
}

func TestLineProcessorShutdown(t *testing.T) {
	svc := &Service{
		LoggerService:     &logging.Service{},
		processingChannel: make(chan types.CatState, 1),
		statusChannel:     make(chan types.CatStatus, 1),
	}

	shutdown := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.lineProcessor(shutdown)
	}()

	close(shutdown)
	<-done // should return promptly
}

// ---------------------------------------------------------------------------
// sendStatusWithEviction tests
// ---------------------------------------------------------------------------

func TestSendStatusWithEvictionNormalSend(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		statusChannel: make(chan types.CatStatus, 1),
	}
	shutdown := make(chan struct{})

	ok := svc.sendStatusWithEviction(types.CatStatus{"freq": "14250"}, shutdown)
	require.True(t, ok)

	status := <-svc.statusChannel
	require.Equal(t, "14250", status["freq"])
}

func TestSendStatusWithEvictionFullChannelEvict(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		statusChannel: make(chan types.CatStatus, 1),
	}
	shutdown := make(chan struct{})

	// Fill the channel with an old status.
	svc.statusChannel <- types.CatStatus{"old": "data"}

	// Send new status — should evict old and insert new.
	ok := svc.sendStatusWithEviction(types.CatStatus{"new": "data"}, shutdown)
	require.True(t, ok)

	status := <-svc.statusChannel
	require.Equal(t, "data", status["new"])
	require.Empty(t, status["old"], "old status should have been evicted")
}

func TestSendStatusWithEvictionShutdown(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		statusChannel: make(chan types.CatStatus), // unbuffered → blocks
	}
	shutdown := make(chan struct{})
	close(shutdown)

	ok := svc.sendStatusWithEviction(types.CatStatus{"freq": "14250"}, shutdown)
	require.False(t, ok, "should return false when shutdown is signaled")
}

// ---------------------------------------------------------------------------
// tryEvictOldestStatus tests
// ---------------------------------------------------------------------------

func TestTryEvictOldestStatusUnbufferedChannel(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		statusChannel: make(chan types.CatStatus), // cap == 0
	}
	shutdown := make(chan struct{})

	ok := svc.tryEvictOldestStatus(shutdown)
	require.False(t, ok, "unbuffered channel should return false (cannot evict)")
}

func TestTryEvictOldestStatusEmptyChannelRace(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		statusChannel: make(chan types.CatStatus, 1), // buffered but empty
	}
	shutdown := make(chan struct{})

	ok := svc.tryEvictOldestStatus(shutdown)
	require.True(t, ok, "empty buffered channel should hit default and return true")
}

func TestTryEvictOldestStatusEvicts(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		statusChannel: make(chan types.CatStatus, 1),
	}
	shutdown := make(chan struct{})

	// Fill the channel.
	svc.statusChannel <- types.CatStatus{"old": "val"}

	ok := svc.tryEvictOldestStatus(shutdown)
	require.True(t, ok)

	// Channel should now be empty.
	select {
	case <-svc.statusChannel:
		t.Fatal("channel should be empty after eviction")
	default:
	}
}
