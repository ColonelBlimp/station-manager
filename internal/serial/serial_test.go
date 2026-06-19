package serial

import (
	"context"
	stderr "errors"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"
)

type mockPort struct {
	readCh  chan []byte
	writeMu sync.Mutex
	writes  [][]byte
	closed  bool

	mu sync.Mutex
	// errToReturn, if non-nil, will be returned on the next Read call
	// instead of data from readCh. This allows exercising error paths
	// from the reader loop.
	errToReturn error
}

func newMockPort() *mockPort {
	return &mockPort{readCh: make(chan []byte, 16)}
}

func (m *mockPort) Read(p []byte) (int, error) {
	m.mu.Lock()
	if m.errToReturn != nil {
		err := m.errToReturn
		m.errToReturn = nil
		m.mu.Unlock()
		return 0, err
	}
	m.mu.Unlock()

	b, ok := <-m.readCh
	if !ok {
		return 0, context.Canceled
	}
	n := copy(p, b)
	return n, nil
}

func (m *mockPort) Write(p []byte) (int, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	cp := make([]byte, len(p))
	copy(cp, p)
	m.writes = append(m.writes, cp)
	return len(p), nil
}

func (m *mockPort) Close() error {
	if !m.closed {
		close(m.readCh)
		m.closed = true
	}
	return nil
}

func (m *mockPort) SetReadTimeout(d time.Duration) error { return nil }

func TestExecSingleCommand(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	// simulate a response from the device
	mp.readCh <- []byte("OK\r")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.Exec(ctx, "FA")
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if resp != "OK" {
		t.Fatalf("expected response OK, got %q", resp)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	if string(mp.writes[0]) != "FA\r" {
		t.Fatalf("unexpected written data: %q", string(mp.writes[0]))
	}
}

func TestConcurrentWrites(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = c.WriteCommand(ctx, "CMD")
		}(i)
	}

	wg.Wait()

	if len(mp.writes) != 10 {
		t.Fatalf("expected 10 writes, got %d", len(mp.writes))
	}
}

func TestReadResponseTimeout(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.ReadResponse(ctx)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Fatalf("ReadResponse returned too early for timeout")
	}
}

func TestCloseWhileReading(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx := t.Context()

	done := make(chan struct{})
	ready := make(chan struct{})
	go func() {
		defer close(done)
		close(ready)
		_, _ = c.ReadResponse(ctx)
	}()

	// Wait for the goroutine to be scheduled and about to block in ReadResponse.
	<-ready

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	select {
	case <-done:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("ReadResponse goroutine did not exit after Close")
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx := t.Context()

	done := make(chan struct{})
	ready := make(chan struct{})
	go func() {
		defer close(done)
		close(ready)
		_, _ = c.ReadResponse(ctx)
	}()

	// Wait for the goroutine to be scheduled and about to block in ReadResponse.
	<-ready

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	select {
	case <-done:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("ReadResponse did not unblock after Close")
	}
}

// TestErrorsStreamClosesOnCloseWithoutError verifies that the Errors
// channel is closed when the port is closed without a terminal read
// error, so callers can reliably range over it.
func TestErrorsStreamClosesOnCloseWithoutError(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	select {
	case _, ok := <-c.Errors():
		if ok {
			t.Fatalf("expected Errors() channel to be closed without value after Close")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for Errors() channel to close after Close")
	}
}

func TestValidateConfigDefaultsAndSuccess(t *testing.T) {
	in := Config{
		PortName: "ttyS0",
		BaudRate: 9600,
		// DataBits, StopBits, Parity left at zero to exercise defaults.
	}

	got, err := validateConfig(in)
	if err != nil {
		t.Fatalf("validateConfig unexpected error: %v", err)
	}

	if got.PortName != "ttyS0" {
		t.Fatalf("expected PortName to be %q, got %q", "ttyS0", got.PortName)
	}
	if got.BaudRate != 9600 {
		t.Fatalf("expected BaudRate to be 9600, got %d", got.BaudRate)
	}
	if got.DataBits != 8 {
		t.Fatalf("expected DataBits default of 8, got %d", got.DataBits)
	}
}

func TestValidateConfigFailures(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing port name", Config{BaudRate: 9600}},
		{"invalid baud", Config{PortName: "ttyS0", BaudRate: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateConfig(tt.cfg)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

// zeroWritePort is a SerialPort implementation that always reports
// success but writes 0 bytes, which should be treated as an error by
// WriteCommand to avoid spinning indefinitely.
type zeroWritePort struct{}

func (z *zeroWritePort) Read(p []byte) (int, error)           { return 0, context.Canceled }
func (z *zeroWritePort) Write(p []byte) (int, error)          { return 0, nil }
func (z *zeroWritePort) Close() error                         { return nil }
func (z *zeroWritePort) SetReadTimeout(d time.Duration) error { return nil }

func TestWriteCommandZeroWriteIsError(t *testing.T) {
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(&zeroWritePort{}, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := c.WriteCommand(ctx, "CMD"); err == nil {
		t.Fatalf("expected error when underlying Write returns 0 bytes, got nil")
	}
}

// overflowPort feeds a single line that exceeds maxLineSize by sending
// data in chunks without any delimiter so that readerLoop overflows the
// internal line buffer and drops the line.
type overflowPort struct {
	mockPort
}

func newOverflowPort() *overflowPort {
	return &overflowPort{mockPort{readCh: make(chan []byte, 16)}}
}

// feedOverflow sends enough delimiter-free chunks through readCh to
// push lineBuf past maxLineSize. Each chunk is sized to exactly fill
// the 512-byte read buffer so the mock port delivers it without
// truncation.
func feedOverflow(readCh chan<- []byte) {
	chunk := make([]byte, defaultBufSize)
	for i := range chunk {
		chunk[i] = 'A'
	}
	numChunks := (maxLineSize / defaultBufSize) + 2 // ensure we exceed
	for range numChunks {
		readCh <- chunk
	}
}

// TestOversizedLineCountedAndDropped: an oversized line is COUNTED via
// DroppedLines (recoverable diagnostic, review 2026-06-19 M1) and discarded —
// no response is delivered for it.
func TestOversizedLineCountedAndDropped(t *testing.T) {
	o := newOverflowPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(o, cfg)

	// Feed enough data to exceed maxLineSize (4096) without any delimiter, then
	// end the oversized line with its delimiter.
	feedOverflow(o.readCh)
	o.readCh <- []byte(";")

	deadline := time.Now().Add(500 * time.Millisecond)
	for c.DroppedLines() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("oversized line was not counted (DroppedLines still 0)")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// No response line: the oversized line was dropped, not delivered.
	if _, err := c.ReadResponse(ctx); err == nil {
		t.Fatalf("expected error or timeout when reading after oversized line; got nil")
	}
}

// TestOversizedLineDoesNotHideTerminalError is the M1 regression: a recoverable
// oversized-line drop is counted (NOT placed on the one-slot terminal errCh), so
// a subsequent non-timeout read error is still delivered on Errors() rather than
// dropped because a warning had filled the slot.
func TestOversizedLineDoesNotHideTerminalError(t *testing.T) {
	o := newOverflowPort()
	cfg := Config{PortName: "mock", BaudRate: 9600, LineDelimiter: ';'}
	c := newPort(o, cfg)

	// Oversized line (no delimiter) → counted + discarding.
	feedOverflow(o.readCh)

	// Wait for the overflow to register BEFORE injecting the terminal error —
	// mockPort.Read checks errToReturn before readCh, so setting it too early
	// would short-circuit the buffered overflow chunks and skip the count.
	deadline := time.Now().Add(500 * time.Millisecond)
	for c.DroppedLines() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("oversized line was not counted before error injection")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Inject a terminal read error, then wake the reader with one more discarded
	// chunk so its NEXT Read returns the injected error.
	injected := stderr.New("device removed")
	o.mu.Lock()
	o.errToReturn = injected
	o.mu.Unlock()
	o.readCh <- []byte("noise-no-delim")

	select {
	case err, ok := <-c.Errors():
		if !ok || !stderr.Is(err, injected) {
			t.Fatalf("expected the injected terminal error, got ok=%v err=%v", ok, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("terminal read error was not delivered (hidden by the oversized drop?)")
	}
	if c.DroppedLines() == 0 {
		t.Errorf("oversized line should have been counted in DroppedLines")
	}
}

// TestConcurrentReadResponseCalls documents and exercises the current
// behavior when ReadResponse is called concurrently from multiple
// goroutines. The implementation does not guarantee safe concurrent
// reads, but this test ensures that, at minimum, the calls do not
// deadlock or panic under moderate contention.
func TestConcurrentReadResponseCalls(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	// Feed a handful of well-formed lines.
	go func() {
		for range 10 {
			mp.readCh <- []byte("RSP;")
		}
		close(mp.readCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			for {
				_, err := c.ReadResponse(ctx)
				if err != nil {
					return
				}
			}
		})
	}

	wg.Wait()
}

func TestWriteCommandBytesAppendsDelimiter(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.WriteCommandBytes(ctx, []byte("FA")); err != nil {
		t.Fatalf("WriteCommandBytes error: %v", err)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	if string(mp.writes[0]) != "FA\r" {
		t.Fatalf("unexpected written data: %q", string(mp.writes[0]))
	}
}

func TestWriteCommandBytesPreservesExistingDelimiter(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.WriteCommandBytes(ctx, []byte("FA\r")); err != nil {
		t.Fatalf("WriteCommandBytes error: %v", err)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	if string(mp.writes[0]) != "FA\r" {
		t.Fatalf("unexpected written data: %q", string(mp.writes[0]))
	}
}

func TestWriteCommandBytesEmptyNoop(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.WriteCommandBytes(ctx, nil); err != nil {
		t.Fatalf("WriteCommandBytes(nil) error: %v", err)
	}
	if err := c.WriteCommandBytes(ctx, []byte{}); err != nil {
		t.Fatalf("WriteCommandBytes(empty) error: %v", err)
	}

	if len(mp.writes) != 0 {
		t.Fatalf("expected no writes for empty commands, got %d", len(mp.writes))
	}
}

func TestReadResponseBytesReturnsBytesWithoutDelimiter(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\n',
	}

	c := newPort(mp, cfg)

	// simulate a single response line terminated by '\n'
	mp.readCh <- []byte("OK\n")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("ReadResponseBytes error: %v", err)
	}
	if string(resp) != "OK" {
		t.Fatalf("expected response 'OK', got %q", string(resp))
	}
}

func TestReadResponseBytesWithChunkedInput(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	// Simulate fragmented input: "ABCD;EF;" split over multiple reads.
	mp.readCh <- []byte("AB")
	mp.readCh <- []byte("CD;EF;")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp1, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("first ReadResponseBytes error: %v", err)
	}
	if string(resp1) != "ABCD" {
		t.Fatalf("expected first response 'ABCD', got %q", string(resp1))
	}

	resp2, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("second ReadResponseBytes error: %v", err)
	}
	if string(resp2) != "EF" {
		t.Fatalf("expected second response 'EF', got %q", string(resp2))
	}
}

func TestExecBytesRoundTripBinaryPayload(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	cmd := []byte{0x00, 0x01, 0xFF}
	// Response includes delimiter, which should be stripped by ReadResponseBytes.
	mp.readCh <- []byte{0x10, 0x20, '\r'}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ExecBytes(ctx, cmd)
	if err != nil {
		t.Fatalf("ExecBytes error: %v", err)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	expectedWritten := append(append([]byte{}, cmd...), '\r')
	if string(mp.writes[0]) != string(expectedWritten) {
		t.Fatalf("unexpected written data: %v, expected %v", mp.writes[0], expectedWritten)
	}

	expectedResp := []byte{0x10, 0x20}
	if string(resp) != string(expectedResp) {
		t.Fatalf("unexpected response data: %v, expected %v", resp, expectedResp)
	}
}

func TestWriteCommandBytesContextCancelledBeforeWrite(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.WriteCommandBytes(ctx, []byte("CMD")); err == nil {
		t.Fatalf("expected error when context is cancelled before write, got nil")
	}
	if len(mp.writes) != 0 {
		t.Fatalf("expected no writes when context is cancelled, got %d", len(mp.writes))
	}
}

func TestReadResponseBytesContextTimeoutWhileWaiting(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.ReadResponseBytes(ctx)
	if err == nil {
		t.Fatalf("expected timeout error from ReadResponseBytes, got nil")
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Fatalf("ReadResponseBytes returned too early for timeout")
	}
}

func TestExecBytesAfterCloseReturnsErrClosed(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := c.ExecBytes(ctx, []byte("CMD")); err == nil {
		t.Fatalf("expected error from ExecBytes after Close, got nil")
	}
}

func TestReadResponseBytesAfterCloseReturnsErrClosed(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := c.ReadResponseBytes(ctx); err == nil {
		t.Fatalf("expected error from ReadResponseBytes after Close, got nil")
	}
}

func TestWriteCommandBytesConcurrentWrites(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_ = c.WriteCommandBytes(ctx, []byte("CMD"))
		})
	}

	wg.Wait()

	if len(mp.writes) != 10 {
		t.Fatalf("expected 10 writes, got %d", len(mp.writes))
	}
}

// TestOversizedLineTailIsDiscarded verifies that after an oversized line is
// dropped, the remaining tail bytes (up to and including the delimiter) are
// also discarded. The next well-formed line should be delivered cleanly
// without any spurious partial data from the dropped line.
func TestOversizedLineTailIsDiscarded(t *testing.T) {
	o := newOverflowPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(o, cfg)

	// Feed enough small chunks to exceed maxLineSize without any delimiter.
	feedOverflow(o.readCh)

	// Now feed the tail of the oversized line (ending with delimiter)
	// followed by a well-formed line. Without the discard logic, "TAIL"
	// would be emitted as a spurious response.
	o.readCh <- []byte("TAIL;GOOD;")

	// The oversized line is counted, not sent on Errors() (review M1).
	deadline := time.Now().Add(500 * time.Millisecond)
	for c.DroppedLines() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("oversized line was not counted (DroppedLines still 0)")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// The first response must be "GOOD", not "TAIL".
	resp, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("ReadResponseBytes error: %v", err)
	}
	if string(resp) != "GOOD" {
		t.Fatalf("expected response %q, got %q (tail of oversized line leaked)", "GOOD", string(resp))
	}
}

// TestFramingResumesAfterOversizedLine verifies that the reader loop
// fully recovers from an oversized-line discard: multiple well-formed
// lines sent after the overflow delimiter must all be correctly framed
// and delivered in order.
func TestFramingResumesAfterOversizedLine(t *testing.T) {
	o := newOverflowPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(o, cfg)

	// 1. Trigger overflow (no delimiter).
	feedOverflow(o.readCh)

	// 2. End the oversized line with its delimiter.
	o.readCh <- []byte(";")

	// 3. Send several normal lines, some fragmented across chunks.
	o.readCh <- []byte("FIRST;SEC")
	o.readCh <- []byte("OND;")
	o.readCh <- []byte("THIRD;")

	// The oversized line is COUNTED (recoverable), not sent on the terminal
	// Errors() channel (review 2026-06-19 M1). Wait for the drop to register.
	deadline := time.Now().Add(500 * time.Millisecond)
	for c.DroppedLines() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("oversized line was not counted (DroppedLines still 0)")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	expected := []string{"FIRST", "SECOND", "THIRD"}
	for _, want := range expected {
		got, err := c.ReadResponse(ctx)
		if err != nil {
			t.Fatalf("ReadResponse(%q) error: %v", want, err)
		}
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Additional mock types for new tests
// ---------------------------------------------------------------------------

// failClosePort is a mock whose Close returns an error.
type failClosePort struct {
	mockPort
	closeErr error
}

func (f *failClosePort) Close() error {
	if !f.closed {
		close(f.readCh)
		f.closed = true
	}
	return f.closeErr
}

// timeoutErr satisfies the Timeout() interface so readerLoop treats it
// as recoverable.
type timeoutErr struct{}

func (timeoutErr) Error() string { return "mock timeout" }
func (timeoutErr) Timeout() bool { return true }

// zeroThenDataPort returns (0, nil) on the first Read, then delivers
// real data from readCh on subsequent calls.
type zeroThenDataPort struct {
	mockPort
	mu    sync.Mutex
	first bool
}

func newZeroThenDataPort() *zeroThenDataPort {
	return &zeroThenDataPort{
		mockPort: mockPort{readCh: make(chan []byte, 16)},
		first:    true,
	}
}

func (z *zeroThenDataPort) Read(p []byte) (int, error) {
	z.mu.Lock()
	if z.first {
		z.first = false
		z.mu.Unlock()
		return 0, nil
	}
	z.mu.Unlock()
	return z.mockPort.Read(p)
}

// partialWritePort writes only 1 byte per Write call, forcing the
// write loop to iterate.
type partialWritePort struct {
	mockPort
}

func newPartialWritePort() *partialWritePort {
	return &partialWritePort{mockPort{readCh: make(chan []byte, 16)}}
}

func (pw *partialWritePort) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	pw.writeMu.Lock()
	defer pw.writeMu.Unlock()
	cp := make([]byte, 1)
	cp[0] = p[0]
	pw.writes = append(pw.writes, cp)
	return 1, nil
}

// failWritePort returns an error from Write.
type failWritePort struct {
	mockPort
	writeErr error
}

func (f *failWritePort) Write(p []byte) (int, error) {
	return 0, f.writeErr
}

// ---------------------------------------------------------------------------
// New tests
// ---------------------------------------------------------------------------

// 1. WriteCommand early-return for empty string (distinct from WriteCommandBytes).
func TestWriteCommandEmptyStringReturnsNil(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	if err := c.WriteCommand(context.Background(), ""); err != nil {
		t.Fatalf("WriteCommand('') should return nil, got %v", err)
	}
	if len(mp.writes) != 0 {
		t.Fatalf("expected 0 writes for empty string, got %d", len(mp.writes))
	}
}

// 2. Close is idempotent — second call returns nil.
func TestCloseIdempotent(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	if err := c.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should return nil, got %v", err)
	}
}

// 3. Close surfaces the underlying port's Close error.
func TestClosePropagatesUnderlyingPortError(t *testing.T) {
	wantErr := stderr.New("device disconnect")
	fp := &failClosePort{
		mockPort: mockPort{readCh: make(chan []byte, 16)},
		closeErr: wantErr,
	}
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(fp, cfg)

	err := c.Close()
	if err == nil {
		t.Fatalf("expected error from Close, got nil")
	}
	if !stderr.Is(err, wantErr) {
		t.Fatalf("expected underlying error %v, got %v", wantErr, err)
	}
}

// 4. Default line delimiter ('\r') is applied when LineDelimiter is 0.
func TestNewPortDefaultLineDelimiter(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName: "mock",
		BaudRate: 9600,
		// LineDelimiter intentionally left at zero
	}
	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.WriteCommand(ctx, "FA"); err != nil {
		t.Fatalf("WriteCommand error: %v", err)
	}
	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	written := mp.writes[0]
	if written[len(written)-1] != '\r' {
		t.Fatalf("expected default delimiter '\\r', got %q", written)
	}
}

// 5. readerLoop continues after a timeout error from Read.
func TestReaderLoopContinuesOnTimeoutError(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	// Inject a timeout error; the reader loop should recover.
	mp.mu.Lock()
	mp.errToReturn = timeoutErr{}
	mp.mu.Unlock()

	// Then feed a valid line.
	mp.readCh <- []byte("OK;")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ReadResponse(ctx)
	if err != nil {
		t.Fatalf("expected response after timeout recovery, got error: %v", err)
	}
	if resp != "OK" {
		t.Fatalf("expected %q, got %q", "OK", resp)
	}
}

// 6. readerLoop surfaces a non-timeout Read error on Errors().
func TestReaderLoopSurfacesNonTimeoutReadError(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	injectedErr := stderr.New("device removed")
	mp.mu.Lock()
	mp.errToReturn = injectedErr
	mp.mu.Unlock()

	// Unblock the reader after the error path (Read returns errToReturn,
	// then the loop exits; it won't read from readCh again).
	// The mockPort.Read would block on readCh after errToReturn is consumed,
	// but the loop returns on non-timeout errors, so we just need to
	// drain Errors().

	select {
	case err, ok := <-c.Errors():
		if !ok {
			t.Fatalf("expected error before channel close")
		}
		if !stderr.Is(err, injectedErr) {
			t.Fatalf("expected injected error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for error on Errors()")
	}
}

// 7. readerLoop skips zero-byte reads and continues.
func TestReaderLoopZeroByteReadContinues(t *testing.T) {
	zp := newZeroThenDataPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(zp, cfg)

	// First Read returns (0, nil), second delivers data.
	zp.readCh <- []byte("DATA;")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ReadResponse(ctx)
	if err != nil {
		t.Fatalf("ReadResponse error: %v", err)
	}
	if resp != "DATA" {
		t.Fatalf("expected %q, got %q", "DATA", resp)
	}
}

// 8. Write loop handles partial writes correctly.
func TestWriteCommandBytesPartialWrite(t *testing.T) {
	pw := newPartialWritePort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(pw, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.WriteCommandBytes(ctx, []byte("AB")); err != nil {
		t.Fatalf("WriteCommandBytes error: %v", err)
	}

	// "AB" + ";" = 3 bytes, each written one at a time.
	pw.writeMu.Lock()
	defer pw.writeMu.Unlock()
	if len(pw.writes) != 3 {
		t.Fatalf("expected 3 partial writes, got %d", len(pw.writes))
	}
	var assembled []byte
	for _, w := range pw.writes {
		assembled = append(assembled, w...)
	}
	if string(assembled) != "AB;" {
		t.Fatalf("expected assembled write %q, got %q", "AB;", string(assembled))
	}
}

// 9. Write returns an error from the underlying port.
func TestWriteCommandBytesWriteError(t *testing.T) {
	wantErr := stderr.New("write fault")
	fp := &failWritePort{
		mockPort: mockPort{readCh: make(chan []byte, 16)},
		writeErr: wantErr,
	}
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(fp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.WriteCommandBytes(ctx, []byte("CMD"))
	if err == nil {
		t.Fatalf("expected write error, got nil")
	}
	if !stderr.Is(err, wantErr) {
		t.Fatalf("expected underlying error %v, got %v", wantErr, err)
	}
}

// 10. validateConfig defaults StopBits and Parity correctly.
func TestValidateConfigDefaultStopBitsAndParity(t *testing.T) {
	got, err := validateConfig(Config{
		PortName: "ttyS0",
		BaudRate: 9600,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.StopBits != serial.OneStopBit {
		t.Fatalf("expected StopBits default OneStopBit, got %v", got.StopBits)
	}
	if got.Parity != serial.NoParity {
		t.Fatalf("expected Parity default NoParity, got %v", got.Parity)
	}
}

// 11. validateConfig preserves explicitly set values.
func TestValidateConfigPreservesExplicitValues(t *testing.T) {
	in := Config{
		PortName: "ttyS0",
		BaudRate: 115200,
		DataBits: 7,
		StopBits: serial.TwoStopBits,
		Parity:   serial.EvenParity,
	}
	got, err := validateConfig(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DataBits != 7 {
		t.Fatalf("expected DataBits 7, got %d", got.DataBits)
	}
	if got.StopBits != serial.TwoStopBits {
		t.Fatalf("expected TwoStopBits, got %v", got.StopBits)
	}
	if got.Parity != serial.EvenParity {
		t.Fatalf("expected EvenParity, got %v", got.Parity)
	}
}

// 12. putReadBuf rejects a buffer with the wrong capacity.
func TestPutReadBufRejectsWrongCapacity(t *testing.T) {
	// Get a correctly-sized buffer and return it.
	correct := getReadBuf()
	putReadBuf(correct)

	// Put a wrongly-sized buffer — it should be silently discarded.
	wrong := make([]byte, defaultBufSize+1)
	putReadBuf(wrong)

	// Next get must return a buffer with the default capacity, not the
	// wrong one.
	b := getReadBuf()
	defer putReadBuf(b)
	if cap(b) != defaultBufSize {
		t.Fatalf("expected buf cap %d, got %d", defaultBufSize, cap(b))
	}
}

// 13. Close while the responses channel is full does not deadlock.
func TestCloseWhileResponseChannelFull(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	// Fill the responses channel to capacity.
	for range responsesBufSize {
		mp.readCh <- []byte("X;")
	}

	// Give the reader loop time to process the data into the channel.
	// We wait by reading one response — if the channel is full, the
	// reader loop will be blocked in the emit select. That's exactly
	// the state we want to test Close against.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.ReadResponse(ctx); err != nil {
		t.Fatalf("ReadResponse error: %v", err)
	}

	// Now close — this must not deadlock.
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Close()
	}()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("Close deadlocked while responses channel was full")
	}
}

// 14. The 3-index slice trick prevents mutation of the caller's backing array.
func TestWriteCommandBytesDoesNotMutateCallerSlice(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:      "mock",
		BaudRate:      9600,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	// Create a slice with spare capacity so a naive append would write
	// the delimiter into the caller's backing array.
	backing := make([]byte, 3, 8)
	copy(backing, "CMD")
	cmd := backing[:3]

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.WriteCommandBytes(ctx, cmd); err != nil {
		t.Fatalf("WriteCommandBytes error: %v", err)
	}

	// The byte at index 3 in the backing array must NOT have been
	// overwritten with the delimiter.
	if backing[:4][3] != 0 {
		t.Fatalf("caller's backing array was mutated: byte[3] = %q, expected 0", backing[:4][3])
	}
}

// Bonus: negative baud rate (extends TestValidateConfigFailures).
func TestValidateConfigNegativeBaudRate(t *testing.T) {
	_, err := validateConfig(Config{
		PortName: "ttyS0",
		BaudRate: -1,
	})
	if err == nil {
		t.Fatalf("expected error for negative baud rate, got nil")
	}
}

// hangWritePort blocks in Write (and Read) until Close is called, simulating a
// driver/HW fault where a serial write never returns. Used to exercise the
// write watchdog (review 2026-06-04 H4).
type hangWritePort struct {
	closeCh chan struct{}
	once    sync.Once
}

func newHangWritePort() *hangWritePort { return &hangWritePort{closeCh: make(chan struct{})} }

func (h *hangWritePort) Read(p []byte) (int, error) {
	<-h.closeCh
	return 0, context.Canceled
}

func (h *hangWritePort) Write(p []byte) (int, error) {
	<-h.closeCh // hang until Close unblocks us
	return 0, context.Canceled
}

func (h *hangWritePort) Close() error {
	h.once.Do(func() { close(h.closeCh) })
	return nil
}

func (h *hangWritePort) SetReadTimeout(d time.Duration) error { return nil }

// TestWriteCommandBytesWatchdogClosesOnHang covers review-finding H4: with a
// positive WriteTimeoutMS, a write that never completes must NOT hang the
// caller forever. The watchdog closes the port (the only lever, since the lib
// has no write deadline), the call returns ErrWriteTimeout, and the now-closed
// port rejects further writes.
func TestWriteCommandBytesWatchdogClosesOnHang(t *testing.T) {
	hp := newHangWritePort()
	cfg := Config{
		PortName:       "hang",
		BaudRate:       9600,
		DataBits:       8,
		StopBits:       1,
		LineDelimiter:  ';',
		WriteTimeoutMS: 50,
	}
	c := newPort(hp, cfg)

	start := time.Now()
	err := c.WriteCommandBytes(context.Background(), []byte("FA;"))
	elapsed := time.Since(start)

	if !stderr.Is(err, ErrWriteTimeout) {
		t.Fatalf("err = %v, want ErrWriteTimeout", err)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("returned in %v, want ≥ ~50ms (watchdog should wait the timeout)", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("returned in %v — watchdog did not bound the hung write", elapsed)
	}
	// The watchdog closed the port; subsequent writes short-circuit.
	if err := c.WriteCommandBytes(context.Background(), []byte("FB;")); !stderr.Is(err, ErrClosed) {
		t.Errorf("post-watchdog write err = %v, want ErrClosed", err)
	}
}

// TestWriteCommandBytesWatchdogAllowsFastWrite confirms the watchdog is inert
// on a normal (fast) write: a generous WriteTimeoutMS never fires, the write
// succeeds, and the port stays open.
func TestWriteCommandBytesWatchdogAllowsFastWrite(t *testing.T) {
	mp := newMockPort()
	cfg := Config{
		PortName:       "mock",
		BaudRate:       9600,
		DataBits:       8,
		StopBits:       1,
		LineDelimiter:  ';',
		WriteTimeoutMS: 2000,
	}
	c := newPort(mp, cfg)
	defer c.Close()

	if err := c.WriteCommandBytes(context.Background(), []byte("FA;")); err != nil {
		t.Fatalf("WriteCommandBytes: %v", err)
	}
	mp.writeMu.Lock()
	n := len(mp.writes)
	mp.writeMu.Unlock()
	if n != 1 {
		t.Errorf("recorded %d writes, want 1", n)
	}
	// Port still usable.
	if err := c.WriteCommandBytes(context.Background(), []byte("FB;")); err != nil {
		t.Errorf("second write failed; watchdog wrongly closed the port: %v", err)
	}
}
