package ft8

import (
	stderrors "errors"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Profile claim (ADR 0080, W-0019 slice 3). The FT view claims its profile
// BEFORE it subscribes to /v1/ft8/events, because a native EventSource cannot
// surface a refusal code. The claim is the one place the capture profile
// changes, and it changes only between sessions: with zero subscribers, no
// live session, TX disarmed and nothing in flight. Inside the capture linger
// that follows the last unsubscribe it does NOT reuse the old scheduler the
// way a same-profile reconnect does — it releases the capture, drops the
// replay cache and switches, so the next subscriber acquires fresh on the
// claimed profile.

// Profile-claim refusals. Each is a distinct wire code (internal/api maps them);
// a refusal made while a capture linger is pending, or while its teardown is
// still winding down, carries RetryAfter so the view can say how long the
// session the operator just left has to wind down and re-claim on time.
var (
	ErrProfileUnknown       = stderrors.New("ft8: unknown profile")
	ErrProfileMismatch      = stderrors.New("ft8: subscription names a profile other than the active one")
	ErrProfileBusy          = stderrors.New("ft8: a subscriber holds a capture on another profile")
	ErrProfileSessionActive = stderrors.New("ft8: a sequenced session is active")
	ErrProfileTxArmed       = stderrors.New("ft8: transmit is armed")
	ErrProfileTxInFlight    = stderrors.New("ft8: a transmission is in flight")
)

// windingDownRetry is the retry hint advertised while onLingerExpired's
// teardown (unattended disarm, capture release) is still running after the
// linger timer itself has been consumed: the exact remaining time is unknown,
// so the hint is a short re-claim interval rather than none at all.
const windingDownRetry = 250 * time.Millisecond

// ProfileRefusal wraps one of the refusal sentinels with the time the pending
// linger has left, so a client can re-claim when it elapses rather than poll.
type ProfileRefusal struct {
	Err        error
	RetryAfter time.Duration
}

func (r *ProfileRefusal) Error() string { return r.Err.Error() }
func (r *ProfileRefusal) Unwrap() error { return r.Err }

// profileByName resolves a wire mode name ("ft8" / "ft4", any case) to its
// profile.
func profileByName(name string) (Profile, bool) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case ProfileFT8.Name:
		return ProfileFT8, true
	case ProfileFT4.Name:
		return ProfileFT4, true
	}
	return Profile{}, false
}

// Profile returns the profile the next capture session will run on.
func (s *Service) Profile() Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profile
}

// activeProfile is the lock-free snapshot of the active profile for readers
// that hold none of the claim's gates: the tx publisher (under txMu, which
// must not take s.mu after it in that path), offset validation, the decode
// loop, the rung preflight. Written only by ClaimProfile, together with the
// gated s.profile.
func (s *Service) activeProfile() Profile {
	if p := s.profileSnap.Load(); p != nil {
		return *p
	}
	return ProfileFT8
}

// modeName is the active profile's wire name.
func (s *Service) modeName() string { return s.activeProfile().Name }

// lingerRemainingLocked is the retry hint for a refusal: the time left on a
// pending capture linger (never less than 1 ms while it is pending, so a
// retry at the advertised deadline still carries a hint), windingDownRetry
// while the expired linger's teardown is still running, and zero otherwise.
// Caller holds s.mu.
func (s *Service) lingerRemainingLocked() time.Duration {
	switch {
	case s.lingerTimer != nil:
		if d := time.Until(s.lingerDeadline); d > time.Millisecond {
			return d
		}
		return time.Millisecond
	case s.lingerWindingDown:
		return windingDownRetry
	}
	return 0
}

// ClaimProfile selects the capture profile for the sessions that follow. The
// same profile is a no-op, whatever else is live. A different profile is
// refused — nothing changes — while a subscriber holds a capture
// (ErrProfileBusy), a transmission is in flight (ErrProfileTxInFlight), a
// sequenced session is active (ErrProfileSessionActive) or TX is armed
// (ErrProfileTxArmed), in that precedence; a refusal inside the linger of a
// session the operator just left carries RetryAfter.
//
// Serialised through the established gates, in their established order:
// seqGate excludes session starts and the disarm-with-abandon path, txMu
// excludes arm / disarm and in-flight changes (armTx reads the profile under
// txMu → s.mu, so an arm that lands after the switch builds its controller on
// the new profile, never the old), and s.mu owns the subscriber count and the
// capture. The gates are held across the capture drain too: with the session
// idle and TX disarmed — the conditions just checked under those gates — the
// decode loop can reach txMu or seqGate only through a session rung or a TX
// slot, neither of which exists, so the drain cannot block on them. The drain
// itself drops s.mu (releaseCaptureLocked), so the one thing that can change
// during it is the subscriber count: a subscriber that connected meanwhile has
// already re-acquired on the OLD profile, and the claim is then refused rather
// than switched under it.
func (s *Service) ClaimProfile(name string) (Profile, error) {
	const op errors.Op = "ft8.Service.ClaimProfile"
	p, ok := profileByName(name)
	if !ok {
		return Profile{}, errors.New(op).WithErr(ErrProfileUnknown).WithMsgf("mode %q", name)
	}

	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	s.txMu.Lock()
	defer s.txMu.Unlock()
	armed, inFlight := s.txArmed, s.txInFlight
	sessionActive := s.seq.Active() // txMu → seq.mu is the established order
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.profile.Name == p.Name {
		return s.profile, nil
	}
	refuse := func(sentinel error) (Profile, error) {
		return s.profile, errors.New(op).WithErr(&ProfileRefusal{Err: sentinel, RetryAfter: s.lingerRemainingLocked()})
	}
	switch {
	case s.subCount > 0:
		return refuse(ErrProfileBusy)
	case inFlight:
		return refuse(ErrProfileTxInFlight)
	case sessionActive:
		return refuse(ErrProfileSessionActive)
	case armed:
		return refuse(ErrProfileTxArmed)
	}

	// Idle: bypass the linger. The pending release would have kept the old
	// scheduler for a reconnect; the claim wants it gone.
	if s.lingerTimer != nil {
		s.lingerTimer.Stop()
		s.lingerTimer = nil
	}
	if s.capturing {
		s.releaseCaptureLocked() // drains with s.mu dropped; re-acquires only for a new subscriber
	} else {
		// capturing=false does not mean every capture worker has finished:
		// onCaptureLoopExit clears the flag when the scheduler dies and returns
		// without waiting for the decoder, which drains its buffered slots on
		// its own. Join them before the switch so the old profile's decoder
		// cannot publish into, or repopulate the replay cache of, the new one.
		// Same s.mu-dropped wait as releaseCaptureLocked's drain — and the same
		// tail: acquisition is suppressed while joining (a subscriber arriving
		// now would otherwise start a new session and add its long-lived
		// workers to the very WaitGroup being waited on), then any subscriber
		// present re-acquires on the OLD profile and the claim is refused.
		s.joiningWorkers = true
		s.mu.Unlock()
		s.wg.Wait()
		s.mu.Lock()
		s.joiningWorkers = false
		if s.subCount > 0 && !s.stopped {
			s.startCaptureLocked()
		}
	}
	if s.subCount > 0 || s.capturing {
		return refuse(ErrProfileBusy)
	}

	// The sequencer commits first: its refusal (a session that somehow started
	// despite the gates) must leave the replay cache and the profile untouched.
	if err := s.seq.setProfile(p); err != nil {
		return refuse(ErrProfileSessionActive)
	}
	// Profile-dependent replay dies with the old profile: a late subscriber must
	// not be handed the previous profile's last slot, tx frame or session.
	s.hub.clearReplay()
	from := s.profile.Name
	s.profile = p
	snap := p
	s.profileSnap.Store(&snap)
	s.log.InfoWith().Str("from", from).Str("to", p.Name).Msg("ft8: profile claimed")

	// Republish the tx state carrying the new mode so the next subscriber's
	// replay names the profile. txMu is held, so build the frame here rather
	// than through publishTxState; the hub never takes txMu or s.mu.
	s.hub.publish(hubEvent{name: EventTx, payload: s.txStateLocked()})
	return p, nil
}

// SubscribeMode is Subscribe with the profile the client expects named: the
// validation, the hub subscription and the subscriber count are ONE admission
// under s.mu, so a claim cannot switch the profile between the check and the
// count and leave a stream labelled for the other lattice holding the capture
// (ADR 0080). An empty mode admits unconditionally, like Subscribe.
func (s *Service) SubscribeMode(mode string) (<-chan hubEvent, func(), error) {
	const op errors.Op = "ft8.Service.SubscribeMode"
	s.mu.Lock()
	if mode != "" && !strings.EqualFold(mode, s.profile.Name) {
		active := s.profile.Name
		s.mu.Unlock()
		return nil, nil, errors.New(op).WithErr(ErrProfileMismatch).WithMsgf("active %s, requested %q", active, mode)
	}
	ch, unsub := s.hub.subscribe() // s.mu → hub.mu is the established order (clearActivity)
	s.onSubscriberAddedLocked()
	s.mu.Unlock()
	return ch, s.onceUnsubscribe(unsub), nil
}

// captureProfileForTest reports the profile the live capture session was
// built on (zero when none). Test seam only.
func (s *Service) captureProfileForTest() Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.captureProfile
}

// setProfileForTest installs a profile the way ClaimProfile does — the gated
// field, the lock-free snapshot and the sequencer — without the claim's idle
// checks. Test seam only.
func (s *Service) setProfileForTest(p Profile) {
	s.mu.Lock()
	s.profile = p
	snap := p
	s.profileSnap.Store(&snap)
	s.mu.Unlock()
	if s.seq != nil {
		_ = s.seq.setProfile(p)
	}
}
