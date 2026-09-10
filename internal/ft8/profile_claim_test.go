package ft8

import (
	"context"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// W-0019 slice 3 (ADR 0080): the profile claim. The FT view claims its profile
// before it subscribes; the claim refuses while anything of the other profile
// is live (a subscriber's capture, a session, armed or in-flight TX) with a
// distinct sentinel, carries the pending linger as RetryAfter, and with an
// idle subsystem inside the linger releases the old capture rather than
// letting the next subscriber reuse it.

func TestClaimProfile_UnknownName(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	_, err := s.ClaimProfile("ft9")
	require.ErrorIs(t, err, ErrProfileUnknown)
	require.Equal(t, "FT8", s.Profile().Name)
}

func TestClaimProfile_SameProfileIsANoOpEvenWhileBusy(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })
	_, unsub := s.Subscribe()
	defer unsub()

	p, err := s.ClaimProfile("FT8")
	require.NoError(t, err)
	require.Equal(t, "FT8", p.Name)
	require.Equal(t, 1, src.startCount(), "a same-profile claim never touches the capture")
}

func TestClaimProfile_RefusedWhileASubscriberHoldsTheCapture(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })
	_, unsub := s.Subscribe()
	defer unsub()

	_, err := s.ClaimProfile("ft4")
	require.ErrorIs(t, err, ErrProfileBusy)
	require.Equal(t, "FT8", s.Profile().Name, "a refused claim changes nothing")
	require.Equal(t, 1, src.startCount())
	require.Equal(t, 0, src.stopCount(), "the live capture is untouched")
}

func TestClaimProfile_RefusedWhileArmed(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) })

	_, err := s.ClaimProfile("ft4")
	require.ErrorIs(t, err, ErrProfileTxArmed)
	require.Equal(t, "FT8", s.Profile().Name)
}

// Leaving FT8 mid-session and clicking FT4: the linger's unattended disarm is
// what ends the session (invariant 5) — characterised here first — and until
// it fires the claim is refused with the time it has left; afterwards it succeeds.
func TestClaimProfile_MidLingerOfALiveSession_RefusedThenSucceeds(t *testing.T) {
	withShortLinger(t, 300*time.Millisecond)
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	require.NoError(t, s.StartCallCq("7Q5MLV", "KH78", 1500, 14.074, "operator_pick", "", 1))
	require.True(t, s.seq.Active())

	// The operator was attending (a subscriber), then left: linger pending.
	_, unsub := s.Subscribe()
	unsub()

	_, err := s.ClaimProfile("ft4")
	require.ErrorIs(t, err, ErrProfileSessionActive)
	var ref *ProfileRefusal
	require.True(t, stderrors.As(err, &ref), "a refusal inside the linger carries RetryAfter")
	require.Greater(t, ref.RetryAfter, time.Duration(0))
	require.LessOrEqual(t, ref.RetryAfter, 300*time.Millisecond)
	require.True(t, s.seq.Active(), "the claim itself never ends a session")
	require.Equal(t, "FT8", s.Profile().Name)

	// Characterisation of what leaving mid-session does today: the linger's
	// unattended disarm ends the session.
	require.Eventually(t, func() bool { return !s.seq.Active() }, 2*time.Second, 10*time.Millisecond,
		"the unattended disarm at linger expiry ends the session")

	p, err := s.ClaimProfile("ft4")
	require.NoError(t, err)
	require.Equal(t, "FT4", p.Name)
	require.Equal(t, "FT4", s.seq.profile.Name, "the sequencer follows the claim")
}

// A cross-profile claim inside the five-second linger must NOT reuse the old
// scheduler the way a same-profile reconnect does: it releases the capture,
// drops the replay cache, switches, and the next subscriber acquires fresh on
// the new profile.
func TestClaimProfile_InsideLinger_ReleasesCaptureAndSwitches(t *testing.T) {
	withShortLinger(t, 10*time.Second) // long: the claim, not the timer, must do the release
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	require.Equal(t, 1, src.startCount())
	// Something in the replay cache from the FT8 session.
	s.hub.publish(hubEvent{name: EventDecode, payload: DecodeReport{
		Slot: ProfileFT8.SlotRefFromTime(time.Now().UTC()), Decodes: []DecodeLine{{Text: "CQ K1ABC FN42"}},
	}})
	unsub() // linger pending, capture still live
	require.Equal(t, 0, src.stopCount())

	p, err := s.ClaimProfile("ft4")
	require.NoError(t, err)
	require.Equal(t, "FT4", p.Name)
	require.Equal(t, 1, src.stopCount(), "the old capture is released, not reused")
	require.Equal(t, 1, src.startCount(), "nothing re-acquires with zero subscribers")
	require.Equal(t, "FT4", s.Profile().Name)

	// The next subscriber acquires a fresh session on FT4 and sees no FT8 replay:
	// the first frames are the fresh tx state carrying the mode, never the
	// FT8 session's cached decode.
	ch, unsub2 := s.Subscribe()
	defer unsub2()
	require.Equal(t, 2, src.startCount(), "the new subscriber acquires on the claimed profile")
	require.Equal(t, "FT4", s.captureProfileForTest().Name)
	sawMode := ""
	for done := false; !done; {
		select {
		case ev := <-ch:
			switch ev.name {
			case EventDecode:
				t.Fatalf("FT8 replay leaked across the profile switch: %+v", ev.payload)
			case EventTx:
				sawMode = ev.payload.(TxState).Mode
			}
		case <-time.After(200 * time.Millisecond):
			done = true
		}
	}
	require.Equal(t, "FT4", sawMode, "the replayed tx state names the active profile")
}

func TestTxState_CarriesMode(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	ch, unsub := s.Subscribe()
	defer unsub()
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) })
	st := drainTxState(t, ch, func(st TxState) bool { return st.Armed })
	require.Equal(t, "FT8", st.Mode)
}

// The stream's own guard behind the claim: a subscription naming another
// profile is refused before it counts as a subscriber; the active profile's
// name (any case) and no name both subscribe.
func TestHTTPHandler_RefusesAMismatchedModeBeforeSubscribing(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "?mode=ft4")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), `"ft8_profile_mismatch"`)
	s.mu.Lock()
	subs := s.subCount
	s.mu.Unlock()
	require.Equal(t, 0, subs, "a refused subscription never counts")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?mode=Ft8", nil)
	ok, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer ok.Body.Close()
	require.Equal(t, http.StatusOK, ok.StatusCode)
	require.Equal(t, "text/event-stream", ok.Header.Get("Content-Type"))
}

func TestClaimProfile_RefusedWhileATransmissionIsInFlight(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) }) // disarm cancels the in-flight send
	require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500))
	require.True(t, s.txInFlightNow(), "the fake player never completes: the send stays in flight")

	_, err := s.ClaimProfile("ft4")
	require.ErrorIs(t, err, ErrProfileTxInFlight)
	require.Equal(t, "FT8", s.Profile().Name)
}

// Combined states report the highest-precedence cause: a subscriber's capture
// beats everything, in-flight beats armed, an active session beats armed.
func TestClaimProfile_RefusalPrecedence(t *testing.T) {
	t.Run("subscriber beats armed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		t.Cleanup(func() { _ = s.ArmTx(false) })
		_, unsub := s.Subscribe()
		defer unsub()
		_, err := s.ClaimProfile("ft4")
		require.ErrorIs(t, err, ErrProfileBusy)
		require.NotErrorIs(t, err, ErrProfileTxArmed)
	})
	t.Run("in-flight beats armed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		t.Cleanup(func() { _ = s.ArmTx(false) })
		require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500))
		_, err := s.ClaimProfile("ft4")
		require.ErrorIs(t, err, ErrProfileTxInFlight)
		require.NotErrorIs(t, err, ErrProfileTxArmed)
	})
	t.Run("session beats armed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		t.Cleanup(func() { _ = s.ArmTx(false) })
		require.NoError(t, s.StartCallCq("7Q5MLV", "KH78", 1500, 14.074, "operator_pick", "", 1))
		_, err := s.ClaimProfile("ft4")
		require.ErrorIs(t, err, ErrProfileSessionActive)
		require.NotErrorIs(t, err, ErrProfileTxArmed)
	})
}

// The retry hint never vanishes exactly when it is needed: at the advertised
// deadline a still-pending linger reports at least 1 ms, and while the expired
// linger's teardown is winding down the claim still advertises a retry.
func TestClaimProfile_RetryHint_AtDeadlineAndWhileWindingDown(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) })

	// A linger pending with its deadline already reached (a retry that lands
	// exactly on time): the hint floors at 1 ms.
	s.mu.Lock()
	s.lingerTimer = time.AfterFunc(time.Hour, func() {})
	s.lingerDeadline = time.Now().Add(-time.Microsecond)
	s.mu.Unlock()
	t.Cleanup(func() {
		s.mu.Lock()
		if s.lingerTimer != nil {
			s.lingerTimer.Stop()
			s.lingerTimer = nil
		}
		s.mu.Unlock()
	})
	_, err := s.ClaimProfile("ft4")
	var ref *ProfileRefusal
	require.ErrorIs(t, err, ErrProfileTxArmed)
	require.True(t, stderrors.As(err, &ref))
	require.Equal(t, time.Millisecond, ref.RetryAfter)

	// The timer consumed, teardown still running: winding down advertises a retry.
	s.mu.Lock()
	s.lingerTimer.Stop()
	s.lingerTimer = nil
	s.lingerWindingDown = true
	s.mu.Unlock()
	_, err = s.ClaimProfile("ft4")
	require.True(t, stderrors.As(err, &ref))
	require.Equal(t, windingDownRetry, ref.RetryAfter)

	// Teardown finished, nothing pending: no hint.
	s.mu.Lock()
	s.lingerWindingDown = false
	s.mu.Unlock()
	_, err = s.ClaimProfile("ft4")
	require.True(t, stderrors.As(err, &ref))
	require.Equal(t, time.Duration(0), ref.RetryAfter)
}

// The stream's admission is one step under s.mu: a mismatched mode is refused
// with no subscriber counted; the active name admits and counts.
func TestSubscribeMode_AtomicAdmission(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	_, _, err := s.SubscribeMode("ft4")
	require.ErrorIs(t, err, ErrProfileMismatch)
	s.mu.Lock()
	require.Equal(t, 0, s.subCount)
	s.mu.Unlock()

	_, unsub, err := s.SubscribeMode("FT8")
	require.NoError(t, err)
	s.mu.Lock()
	require.Equal(t, 1, s.subCount)
	s.mu.Unlock()
	unsub()
	unsub() // idempotent
	s.mu.Lock()
	require.Equal(t, 0, s.subCount)
	s.mu.Unlock()
}

// Deterministic interleaving: an arm that lands while a claim is mid-release
// (held inside the fake source's Stop, with seqGate, txMu and s.mu taken) must
// wait on txMu, then build its controller on the profile the claim installed —
// never an FT8 controller under an FT4 service.
func TestClaimProfile_ArmDuringClaim_SerialisesOnTheNewProfile(t *testing.T) {
	withShortLinger(t, 10*time.Second)
	src := newFakeSource()
	src.stopEntered = make(chan struct{})
	src.stopRelease = make(chan struct{})
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}}, logging.Noop(), src)
	player := newFakeTxPlayer()
	s.newPlayer = func(string, int) (txPlayer, error) { return player, nil }
	s.SetTxKeyer(&fakeKeyer{})
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() {
		select {
		case src.stopRelease <- struct{}{}:
		default:
		}
		_ = s.ArmTx(false)
		_ = s.Stop()
	})

	_, unsub := s.Subscribe()
	unsub() // linger pending, capture live

	claimDone := make(chan error, 1)
	go func() {
		_, err := s.ClaimProfile("ft4")
		claimDone <- err
	}()
	<-src.stopEntered // the claim is inside releaseCaptureLocked, gates held

	armDone := make(chan error, 1)
	go func() { armDone <- s.ArmTx(true) }()
	select {
	case err := <-armDone:
		t.Fatalf("ArmTx returned (%v) while the claim held txMu; it must wait", err)
	case <-time.After(150 * time.Millisecond):
	}

	src.stopRelease <- struct{}{} // let the release finish; the claim completes
	require.NoError(t, <-claimDone)
	require.NoError(t, <-armDone)
	require.Equal(t, "FT4", s.Profile().Name)

	s.txMu.Lock()
	ctrlProfile := s.txCtrl.profile.Name
	s.txMu.Unlock()
	require.Equal(t, "FT4", ctrlProfile, "the arm that waited built its controller on the claimed profile")
}

// Requests validate against the profile they will PROCEED under. An offset
// that fits FT8's 50 Hz signal below the 3000 Hz passband edge but not FT4's
// 84 Hz one, issued while a claim to FT4 is mid-release (gates held), must wait
// behind the claim and then be refused as a bad offset — never validated as FT8
// and keyed as FT4.
func TestRequests_ValidateUnderTheClaimedProfile(t *testing.T) {
	withShortLinger(t, 10*time.Second)
	src := newFakeSource()
	src.stopEntered = make(chan struct{})
	src.stopRelease = make(chan struct{})
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}}, logging.Noop(), src)
	s.newPlayer = func(string, int) (txPlayer, error) { return newFakeTxPlayer(), nil }
	s.SetTxKeyer(&fakeKeyer{})
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() {
		select {
		case src.stopRelease <- struct{}{}:
		default:
		}
		_ = s.Stop()
	})

	const offset = 2950.0 // 2950 + 50 ≤ 3000 (FT8 fits); 2950 + 84 > 3000 (FT4 does not)
	require.NoError(t, s.validateTxOffset("test", offset), "sanity: FT8 accepts the offset")
	require.Error(t, func() error {
		saved := s.activeProfile()
		ft4 := ProfileFT4
		s.profileSnap.Store(&ft4)
		defer func() { s.profileSnap.Store(&saved) }()
		return s.validateTxOffset("test", offset)
	}(), "sanity: FT4 rejects the offset")

	_, unsub := s.Subscribe()
	unsub() // linger pending, capture live

	claimDone := make(chan error, 1)
	go func() {
		_, err := s.ClaimProfile("ft4")
		claimDone <- err
	}()
	<-src.stopEntered // the claim holds seqGate, txMu and s.mu inside the release

	txDone := make(chan error, 1)
	go func() { txDone <- s.TransmitNext("CQ K1ABC FN42", offset) }()
	cqDone := make(chan error, 1)
	go func() { cqDone <- s.StartCallCq("7Q5MLV", "KH78", offset, 14.080, "operator_pick", "", 1) }()
	select {
	case err := <-txDone:
		t.Fatalf("TransmitNext returned (%v) while the claim held seqGate; it must wait", err)
	case err := <-cqDone:
		t.Fatalf("StartCallCq returned (%v) while the claim held seqGate; it must wait", err)
	case <-time.After(150 * time.Millisecond):
	}

	src.stopRelease <- struct{}{}
	require.NoError(t, <-claimDone)
	require.Equal(t, "FT4", s.Profile().Name)
	require.ErrorIs(t, <-txDone, ErrTxBadOffset, "validated under FT4 after the barrier, not FT8 before it")
	require.ErrorIs(t, <-cqDone, ErrTxBadOffset, "validated under FT4 after the barrier, not FT8 before it")
}
