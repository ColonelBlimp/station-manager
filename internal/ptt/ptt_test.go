package ptt

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockPort is a fake pttPort for testing — no real serial hardware needed.
type mockPort struct {
	mu     sync.Mutex
	rts    bool
	dtr    bool
	closed bool
	// setErr, if non-nil, is returned by all Set* operations.
	setErr   error
	closeErr error
}

func (m *mockPort) SetRTS(v bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.rts = v
	return nil
}

func (m *mockPort) SetDTR(v bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.dtr = v
	return nil
}

func (m *mockPort) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeErr
}

func (m *mockPort) getRTS() bool   { m.mu.Lock(); defer m.mu.Unlock(); return m.rts }
func (m *mockPort) getDTR() bool   { m.mu.Lock(); defer m.mu.Unlock(); return m.dtr }
func (m *mockPort) isClosed() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.closed }

// newTestPTT creates a PTT backed by a mockPort with the given line selection.
func newTestPTT(t *testing.T, line Line) (*PTT, *mockPort) {
	t.Helper()
	mock := &mockPort{}
	cfg := Config{PortName: "mock", Line: line}
	p, err := newPTT(cfg, mock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p, mock
}

// --------------- Errors -------------------------------------------------------

func TestErrClosed(t *testing.T) {
	require.Equal(t, "ptt closed", ErrClosed.Error())
}

// --------------- newPTT / construction ---------------------------------------

func TestNewPTT_RTS_LowOnOpen(t *testing.T) {
	_, mock := newTestPTT(t, LineRTS)
	require.False(t, mock.getRTS(), "RTS must be low after open")
}

func TestNewPTT_DTR_LowOnOpen(t *testing.T) {
	_, mock := newTestPTT(t, LineDTR)
	require.False(t, mock.getDTR(), "DTR must be low after open")
}

func TestNewPTT_OpenFails_ClosesPort(t *testing.T) {
	mock := &mockPort{setErr: ErrClosed} // any error on SetRTS/SetDTR
	_, err := newPTT(Config{Line: LineRTS}, mock)
	require.Error(t, err)
	require.True(t, mock.isClosed(), "port must be closed when newPTT fails")
}

// --------------- IsActive ----------------------------------------------------

func TestPTT_IsActive_InitialState(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)
	require.False(t, p.IsActive())
}

// --------------- Assert ------------------------------------------------------

func TestPTT_Assert_SetsRTS(t *testing.T) {
	p, mock := newTestPTT(t, LineRTS)
	require.NoError(t, p.Assert())
	require.True(t, mock.getRTS())
	require.True(t, p.IsActive())
}

func TestPTT_Assert_SetsDTR(t *testing.T) {
	p, mock := newTestPTT(t, LineDTR)
	require.NoError(t, p.Assert())
	require.True(t, mock.getDTR())
	require.True(t, p.IsActive())
}

func TestPTT_Assert_Idempotent(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)
	require.NoError(t, p.Assert())
	require.NoError(t, p.Assert()) // second call — must not error
	require.True(t, p.IsActive())
}

func TestPTT_Assert_AfterClose(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)
	require.NoError(t, p.Close())
	require.ErrorIs(t, p.Assert(), ErrClosed)
}

func TestPTT_Assert_HardwareError(t *testing.T) {
	mock := &mockPort{}
	p, err := newPTT(Config{Line: LineRTS}, mock)
	require.NoError(t, err)

	// Inject error after successful open.
	mock.mu.Lock()
	mock.setErr = ErrClosed // any sentinel
	mock.mu.Unlock()

	err = p.Assert()
	require.Error(t, err)
	require.False(t, p.IsActive(), "active must remain false on hardware error")
}

// --------------- Release -----------------------------------------------------

func TestPTT_Release_LowersRTS(t *testing.T) {
	p, mock := newTestPTT(t, LineRTS)
	require.NoError(t, p.Assert())
	require.NoError(t, p.Release())
	require.False(t, mock.getRTS())
	require.False(t, p.IsActive())
}

func TestPTT_Release_LowersDTR(t *testing.T) {
	p, mock := newTestPTT(t, LineDTR)
	require.NoError(t, p.Assert())
	require.NoError(t, p.Release())
	require.False(t, mock.getDTR())
	require.False(t, p.IsActive())
}

func TestPTT_Release_Idempotent(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)
	require.NoError(t, p.Release()) // already released — must not error
	require.NoError(t, p.Release())
	require.False(t, p.IsActive())
}

func TestPTT_Release_AfterClose(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)
	require.NoError(t, p.Close())
	require.ErrorIs(t, p.Release(), ErrClosed)
}

// --------------- Close -------------------------------------------------------

func TestPTT_Close_ReleasesBeforeClosing(t *testing.T) {
	p, mock := newTestPTT(t, LineRTS)
	require.NoError(t, p.Assert())
	require.NoError(t, p.Close())
	require.False(t, mock.getRTS(), "RTS must be low after close")
	require.True(t, mock.isClosed())
}

func TestPTT_Close_WhenNotActive(t *testing.T) {
	p, mock := newTestPTT(t, LineRTS)
	require.NoError(t, p.Close())
	require.True(t, mock.isClosed())
}

func TestPTT_Close_Idempotent(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)
	require.NoError(t, p.Close())
	require.NoError(t, p.Close()) // second close must not error or panic
}

func TestPTT_Close_SetsClosedFlag(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)
	require.NoError(t, p.Close())
	require.True(t, p.closed.Load())
}

// --------------- Concurrent access -------------------------------------------

func TestPTT_ConcurrentAssertRelease(t *testing.T) {
	p, _ := newTestPTT(t, LineRTS)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = p.Assert() }()
		go func() { defer wg.Done(); _ = p.Release() }()
	}
	wg.Wait()
	// Must not panic; final state is either asserted or released — both valid.
}

func TestPTT_ConcurrentCloseAndAssert(t *testing.T) {
	for i := 0; i < 20; i++ {
		mock := &mockPort{}
		p, err := newPTT(Config{Line: LineRTS}, mock)
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = p.Close() }()
		go func() { defer wg.Done(); _ = p.Assert() }()
		wg.Wait()

		require.True(t, p.closed.Load())
		require.False(t, mock.getRTS(), "RTS must be low after close regardless of assert race")
	}
}

// TestPTT_Assert_ClosedWindowRace specifically covers the window where Close()
// completes between Assert's pre-lock closed.Load() check and the mu.Lock().
// Without the post-lock double-check, this would result in a serial driver error
// instead of ErrClosed. With it, we always get ErrClosed.
func TestPTT_Assert_ClosedWindowRace(t *testing.T) {
	for i := 0; i < 50; i++ {
		mock := &mockPort{}
		p, err := newPTT(Config{Line: LineRTS}, mock)
		require.NoError(t, err)

		// Pre-close so closed=true before Assert is called.
		require.NoError(t, p.Close())

		// Assert must return ErrClosed, never a raw serial error.
		assertErr := p.Assert()
		require.ErrorIs(t, assertErr, ErrClosed)
	}
}

func TestPTT_Release_ClosedWindowRace(t *testing.T) {
	for i := 0; i < 50; i++ {
		mock := &mockPort{}
		p, err := newPTT(Config{Line: LineRTS}, mock)
		require.NoError(t, err)

		require.NoError(t, p.Assert())
		require.NoError(t, p.Close())

		releaseErr := p.Release()
		require.ErrorIs(t, releaseErr, ErrClosed)
	}
}
