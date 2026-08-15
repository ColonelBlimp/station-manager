//go:build cgo

package ft8

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// mutexBuf is a concurrent-safe log sink: the pump goroutine writes onPanic from
// another goroutine while the test reads, so a plain bytes.Buffer would race.
type mutexBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (m *mutexBuf) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.b.Write(p)
}

func (m *mutexBuf) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.b.String()
}

// L9 — a panic in the CGO capture pump must be recovered and reported (attributed to
// the named goroutine), then LOG-AND-DIE: the pump closes m.out/m.done on the way out
// so the scheduler sees the stream end and the higher layer restarts capture.
// respawn=false — the pump closes m.out, so a respawn would send on a closed channel.
func TestCapturePump_PanicIsHandledAndPumpDies(t *testing.T) {
	var once atomic.Bool
	pumpPanicForTest = func() {
		if once.CompareAndSwap(false, true) {
			panic("boom: capture pump exploded")
		}
	}
	defer func() { pumpPanicForTest = nil }()

	var mb mutexBuf
	m := &malgoSource{
		log:  logging.NewForWriter(&mb),
		out:  make(chan []int16, 4),
		done: make(chan struct{}),
	}

	m.launchPump(context.Background(), make(chan []float32))

	// The pump DIED (log-and-die): it closes m.done on the way out.
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not die (m.done not closed) after the panic")
	}

	// And the panic was reported, attributed. Poll: onPanic runs (in safego) AFTER the
	// pump's deferred close of m.done, so a plain read here could race that write.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := mb.String()
		if strings.Contains(s, "capture pump panicked") && strings.Contains(s, "ft8.capture.pump") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no structured capture-pump panic line; log:\n%s", s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
