package bridge

import (
	"context"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/serial"
)

// fakeSerial is a hardware-free stand-in for serial.Client used by
// in-package bridge tests. Tests drive scenarios by calling feedLine
// to enqueue rig pushes; the pipeline's ReadResponseBytes consumes
// them in order. Writes (INIT command, etc.) are recorded for
// assertion.
//
// Concurrency: the lines channel is the synchronisation point —
// feedLine sends, ReadResponseBytes receives. Close is serialised
// against feedLine via mu so a feed-after-close cannot panic on
// "send on closed channel".
type fakeSerial struct {
	mu     sync.Mutex
	lines  chan []byte
	writes [][]byte
	closed bool

	// onWrite, when set, is called for each WriteCommandBytes with a copy of the
	// written bytes; a non-nil return is enqueued as a rig reply (delimiter
	// already stripped, like feedLine). Models a rig that acknowledges every
	// command — used by the CI-V wait-for-ACK command tests to feed the FB/FA
	// per write without hand-sequencing each reply. Nil (default) = no
	// auto-reply, so existing tests are unaffected.
	onWrite func(written []byte) []byte

	// writeErr, when set, makes every WriteCommandBytes fail with it (the write
	// does not land). Models a port that stops accepting writes — used to drive
	// the no-data re-probe WRITE failure path (B9). Nil (default) = writes
	// succeed, so existing tests are unaffected.
	writeErr error
	// closeErr, when set, is returned by the first Close (the port is still
	// marked closed and its channel closed). Models a port that won't release —
	// the cause of a busy reopen (B8). Nil (default) = clean close.
	closeErr error
}

func newFakeSerial() *fakeSerial {
	return &fakeSerial{
		lines: make(chan []byte, 64),
	}
}

// feedLine queues a framed rig response (without the line delimiter)
// for the next ReadResponseBytes consumer. Returns false if the fake
// has been Closed or its buffer is full — tests treat the latter as
// a bug in the test (lines should drain promptly).
func (f *fakeSerial) feedLine(line []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	cp := append([]byte(nil), line...)
	select {
	case f.lines <- cp:
		return true
	default:
		return false
	}
}

// recordedWrites returns a snapshot of the bytes the pipeline has
// written via WriteCommandBytes. Used to assert the INIT command was sent.
func (f *fakeSerial) recordedWrites() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.writes))
	for i, w := range f.writes {
		out[i] = append([]byte(nil), w...)
	}
	return out
}

// setWriteErr toggles the write-failure injection under f.mu, so a test can flip
// it WHILE readLoop is writing (the reset-streak test) without racing the locked
// read in WriteCommandBytes.
func (f *fakeSerial) setWriteErr(err error) {
	f.mu.Lock()
	f.writeErr = err
	f.mu.Unlock()
}

func (f *fakeSerial) WriteCommandBytes(ctx context.Context, cmd []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return serial.ErrClosed
	}
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, append([]byte(nil), cmd...))
	if f.onWrite != nil {
		if reply := f.onWrite(append([]byte(nil), cmd...)); reply != nil {
			// Non-blocking send under mu (buffered 64); mirrors feedLine. A full
			// buffer is a test bug (replies should drain promptly).
			select {
			case f.lines <- reply:
			default:
			}
		}
	}
	return nil
}

func (f *fakeSerial) ReadResponseBytes(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case line, ok := <-f.lines:
		if !ok {
			return nil, serial.ErrClosed
		}
		return line, nil
	}
}

func (f *fakeSerial) ExecBytes(ctx context.Context, cmd []byte) ([]byte, error) {
	if err := f.WriteCommandBytes(ctx, cmd); err != nil {
		return nil, err
	}
	return f.ReadResponseBytes(ctx)
}

func (f *fakeSerial) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.lines)
	return f.closeErr
}

// installFakeSerial wires a fakeSerial into a Service so the pipeline
// reads from / writes to it instead of opening a real port. Returns
// the fake so the caller can drive it.
func installFakeSerial(s *Service) *fakeSerial {
	f := newFakeSerial()
	s.openClient = func(_ serial.Config) (serial.Client, error) { return f, nil }
	return f
}
