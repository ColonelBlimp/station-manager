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
	errCh  chan error
	closed bool
}

func newFakeSerial() *fakeSerial {
	return &fakeSerial{
		lines: make(chan []byte, 64),
		errCh: make(chan error, 1),
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
// written via WriteCommand / WriteCommandBytes. Used to assert the
// INIT command was sent.
func (f *fakeSerial) recordedWrites() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.writes))
	for i, w := range f.writes {
		out[i] = append([]byte(nil), w...)
	}
	return out
}

func (f *fakeSerial) WriteCommand(ctx context.Context, cmd string) error {
	return f.WriteCommandBytes(ctx, []byte(cmd))
}

func (f *fakeSerial) WriteCommandBytes(ctx context.Context, cmd []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return serial.ErrClosed
	}
	f.writes = append(f.writes, append([]byte(nil), cmd...))
	return nil
}

func (f *fakeSerial) ReadResponse(ctx context.Context) (string, error) {
	b, err := f.ReadResponseBytes(ctx)
	return string(b), err
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

func (f *fakeSerial) Exec(ctx context.Context, cmd string) (string, error) {
	if err := f.WriteCommand(ctx, cmd); err != nil {
		return "", err
	}
	return f.ReadResponse(ctx)
}

func (f *fakeSerial) ExecBytes(ctx context.Context, cmd []byte) ([]byte, error) {
	if err := f.WriteCommandBytes(ctx, cmd); err != nil {
		return nil, err
	}
	return f.ReadResponseBytes(ctx)
}

func (f *fakeSerial) Errors() <-chan error {
	return f.errCh
}

func (f *fakeSerial) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.lines)
	close(f.errCh)
	return nil
}

// installFakeSerial wires a fakeSerial into a Service so the pipeline
// reads from / writes to it instead of opening a real port. Returns
// the fake so the caller can drive it.
func installFakeSerial(s *Service) *fakeSerial {
	f := newFakeSerial()
	s.openClient = func(_ serial.Config) (serial.Client, error) { return f, nil }
	return f
}
