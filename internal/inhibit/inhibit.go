// Package inhibit asks the desktop to stop idling, blanking and suspending while
// the station is transmitting.
//
// WHY THIS EXISTS: on 2026-07-28 an unattended FT8 run went silent mid-session —
// the daemon kept keying and the decode log kept recording "Transmitting", but no
// audio reached the rig for 24 minutes. The suspected trigger is a session/power
// event while the screen was blanked, of the same shape that destroyed the CAPTURE
// stream on 2026-07-18 ("KDE Plasma device fiddling destroyed+recreated the rig
// codec's PipeWire nodes mid-capture"). SM holds ONE playback device handle from
// arm to disarm, so a re-route while that handle is open silences every slot after
// it. A station that is transmitting should not let its host idle.
//
// TWO SURFACES, BECAUSE NEITHER IS UNIVERSAL:
//
//   - logind (org.freedesktop.login1, SYSTEM bus) — present on every systemd
//     distro, and on the non-systemd ones (Alpine, Void, Devuan, Gentoo) that ship
//     elogind, which exists precisely to provide this interface. Works headless.
//     Inhibits what=idle:sleep in mode=block. The inhibition is held by an open
//     file descriptor: CLOSING THE FD IS THE RELEASE, which is why the release
//     path closes rather than calling any Un- method.
//   - org.freedesktop.ScreenSaver (SESSION bus) — provided by KDE, GNOME, XFCE,
//     MATE and Cinnamon, but NOT by bare wlroots compositors and not at all
//     without a session bus. Returns a uint32 cookie freed with UnInhibit.
//
// logind alone does NOT reliably stop a desktop blanking the screen — it stops
// logind's own idle action. On KDE the screen lock is kscreenlocker's business and
// answers to the ScreenSaver interface instead. Since the suspected trigger was a
// blank, holding only one of these would likely have missed it, so both are taken
// and the caller keeps whichever the machine actually offers.
package inhibit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/godbus/dbus/v5"
)

// surface is one way of asking the system to stay awake. Kept unexported and
// tiny so the composition logic is testable without a bus of any kind — the
// D-Bus code is the part that cannot be exercised in CI, so it is pushed to the
// edges and the decisions live here.
type surface interface {
	name() string
	inhibit(why string) (release func() error, err error)
}

// Inhibitor holds an inhibition across every surface the host provides.
type Inhibitor struct {
	surfaces []surface
	log      logging.Logger
	timeout  time.Duration

	// inflight marks a surface whose previous acquire has not come back yet, so a
	// wedged one is attempted ONCE rather than once per arm. Keyed by index, not
	// by name, so a future surface that duplicated a name could not silently share
	// another's slot. See claim().
	mu       sync.Mutex
	inflight map[int]bool
}

// New builds an Inhibitor over both supported surfaces. Nothing connects to a bus
// here: a desktop session can come and go over a daemon's lifetime, so each
// Inhibit call connects afresh and a host that gains a session later starts
// working without a restart.
func New(log logging.Logger) *Inhibitor {
	if log == nil {
		log = logging.Noop()
	}
	return &Inhibitor{
		surfaces: []surface{&logindSurface{}, &screenSaverSurface{}},
		log:      log,
		timeout:  surfaceTimeout,
	}
}

// surfaceTimeout bounds ONE surface's acquire or release. godbus's obj.Call is
// CallWithContext(context.Background(), ...) and dbus.SystemBus()/SessionBus()
// do an unbounded Hello handshake, so a D-Bus peer that stays CONNECTED but
// stops replying blocks forever. That matters because this package is called
// synchronously from arm and disarm: an unbounded acquire hangs ArmTx with TX
// already armed, and an unbounded release hangs disarm ahead of the playback
// device being closed — on the closing path, ahead of the daemon exiting.
//
// The package contract is that inhibition is a COURTESY (a failure is logged and
// transmitting continues); a hang is not an error, so without this bound it slips
// past that contract silently instead of loudly.
//
// 2 s is the operator's figure (2026-07-28). A healthy D-Bus round trip is
// sub-millisecond, so it is ~1000x headroom for a merely-busy session bus, while
// capping a wedged one at a stall on the Enable Tx button that is barely
// noticeable. A var, not a const, to match txConfirmTimeout in internal/bridge —
// the house shape for a timeout tests need to shorten.
var surfaceTimeout = 2 * time.Second

// Inhibit takes an inhibition on every surface that will grant one and returns a
// single release func covering them all.
//
// Partial success is SUCCESS: a sway user with no ScreenSaver provider still gets
// logind's idle:sleep block, and a headless box gets the same. An error means the
// host granted nothing at all, and then nothing is held — the caller is expected
// to log it and carry on transmitting.
func (i *Inhibitor) Inhibit(why string) (func() error, error) {
	var (
		releases []func() error
		failures []string
	)
	for idx, s := range i.surfaces {
		rel, err := i.acquireBounded(idx, s, why)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", s.name(), err))
			continue
		}
		releases = append(releases, rel)
		i.log.DebugWith().Str("surface", s.name()).Msg("inhibit: holding idle inhibition")
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("no idle-inhibition surface available (%s)", strings.Join(failures, "; "))
	}
	if len(failures) > 0 {
		// Worth saying once: the operator gets partial protection, and on KDE the
		// missing one is likely to be the ScreenSaver surface that actually stops
		// the blank we care about.
		i.log.DebugWith().Str("unavailable", strings.Join(failures, "; ")).
			Msg("inhibit: some idle-inhibition surfaces unavailable")
	}

	// sync.Once, because the caller may legitimately race an acquire against a
	// disarm and free the same handle twice. Against a real cookie a double free
	// could cancel an inhibition a LATER arm had taken.
	var (
		once       sync.Once
		releaseErr error
	)
	return func() error {
		once.Do(func() {
			for _, rel := range releases {
				releaseErr = errors.Join(releaseErr, i.releaseBounded(rel))
			}
		})
		return releaseErr
	}, nil
}

// acquireBounded runs ONE surface's acquire under i.timeout.
//
// The bound is per surface rather than one deadline across the whole call, and
// that is deliberate: a shared deadline consumed by a hung FIRST surface would
// leave the second with no budget, so a wedged ScreenSaver would cost us the
// logind lock that was working perfectly — exactly what rule 7 forbids. The price
// is that the worst case is len(surfaces) x timeout, which only arises when EVERY
// surface is wedged, i.e. on a desktop that has already stopped functioning.
//
// A surface that answers LATE is not discarded. Its release is called in the
// background, because an inhibition whose release func nobody holds never ends —
// the desktop would stop idling for the life of the process, which is worse than
// the fault this package mitigates (rule 8).
// claim reserves a surface for one acquire attempt, returning false when the
// previous attempt is still outstanding. Lazily allocated so a hand-built
// Inhibitor (the tests) needs no extra construction.
func (i *Inhibitor) claim(idx int) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.inflight == nil {
		i.inflight = make(map[int]bool)
	}
	if i.inflight[idx] {
		return false
	}
	i.inflight[idx] = true
	return true
}

func (i *Inhibitor) unclaim(idx int) {
	i.mu.Lock()
	delete(i.inflight, idx)
	i.mu.Unlock()
}

func (i *Inhibitor) acquireBounded(idx int, s surface, why string) (func() error, error) {
	// One outstanding attempt per surface. godbus's SystemBus()/SessionBus() hold
	// a PACKAGE-GLOBAL mutex across the whole connect+auth+Hello handshake and
	// take no context, so a bus that accepts the socket but never finishes the
	// handshake blocks its goroutine forever and every later caller behind it.
	// The bound below returns control to the caller but does not stop that work
	// stacking: without this guard each arm would strand another pair — the
	// surface call, plus the cleanup goroutine waiting on its result — for the
	// life of the process (codex P2 on 0188672a, a defect introduced by the bound
	// itself). The claim clears when the attempt finally answers, so a bus that
	// recovers is retried.
	if !i.claim(idx) {
		return nil, fmt.Errorf("previous attempt has not answered yet")
	}
	type result struct {
		rel func() error
		err error
	}
	// Buffered: the surface goroutine must always be able to complete its send,
	// so the timeout path leaves nothing blocked on an unread channel.
	ch := make(chan result, 1)
	go func() {
		rel, err := s.inhibit(why)
		ch <- result{rel: rel, err: err}
	}()

	timer := time.NewTimer(i.timeout)
	defer timer.Stop()
	// Exactly ONE reader of ch clears the claim — the fast path here, or the
	// cleanup goroutine below. Clearing it anywhere else would let a second
	// attempt start while the first was still blocked.
	select {
	case r := <-ch:
		i.unclaim(idx)
		return r.rel, r.err
	case <-timer.C:
		go func() {
			r := <-ch
			i.unclaim(idx)
			if r.rel != nil {
				i.log.DebugWith().Str("surface", s.name()).
					Msg("inhibit: surface answered after the bound; releasing the late inhibition")
				if err := r.rel(); err != nil {
					i.log.WarnWith().Err(err).Str("surface", s.name()).
						Msg("inhibit: late inhibition release failed")
				}
			}
		}()
		return nil, fmt.Errorf("no answer within %s", i.timeout)
	}
}

// releaseBounded bounds one surface's release. Unlike an acquire there is nothing
// to salvage — the caller only needs control back — so a late release simply
// finishes in its own goroutine. The caller is disarmTxLocked, which invokes this
// BEFORE it waits for the in-flight transmission, abandons the session and closes
// the playback device; on the closing path it is also ahead of the daemon exiting.
func (i *Inhibitor) releaseBounded(rel func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- rel()
	}()

	timer := time.NewTimer(i.timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("release did not answer within %s", i.timeout)
	}
}

// ---- logind (system bus) ----

type logindSurface struct{}

func (l *logindSurface) name() string { return "logind" }

func (l *logindSurface) inhibit(why string) (func() error, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	obj := conn.Object("org.freedesktop.login1", dbus.ObjectPath("/org/freedesktop/login1"))
	var fd dbus.UnixFD
	// CallWithContext, never Call: godbus's Call is CallWithContext(Background)
	// and waits forever on a peer that is connected but not replying. The
	// Inhibitor bounds this from the outside too, but only that bound returns
	// control to the CALLER — without a deadline here the abandoned goroutine
	// would live for the life of the process.
	ctx, cancel := context.WithTimeout(context.Background(), surfaceTimeout)
	defer cancel()
	// mode=block, not delay: delay only postpones the action by a few seconds,
	// which is useless across a multi-hour FT8 run.
	err = obj.CallWithContext(ctx, "org.freedesktop.login1.Manager.Inhibit", 0,
		"idle:sleep", "Station Manager", why, "block").Store(&fd)
	if err != nil {
		return nil, fmt.Errorf("login1 Inhibit: %w", err)
	}
	// The inhibition lives as long as this fd is open — that IS the handle, and
	// closing it is the only way to release. Wrapped in os.File so the descriptor
	// is owned and closed exactly once by the Go runtime's accounting.
	f := os.NewFile(uintptr(fd), "logind-inhibit")
	return f.Close, nil
}

// ---- org.freedesktop.ScreenSaver (session bus) ----

type screenSaverSurface struct{}

func (s *screenSaverSurface) name() string { return "screensaver" }

func (s *screenSaverSurface) inhibit(why string) (func() error, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		// The expected, unremarkable case on a headless host: no session, no
		// screen to blank. logind still covers idle and sleep.
		return nil, fmt.Errorf("session bus: %w", err)
	}
	obj := conn.Object("org.freedesktop.ScreenSaver", dbus.ObjectPath("/org/freedesktop/ScreenSaver"))
	var cookie uint32
	ctx, cancel := context.WithTimeout(context.Background(), surfaceTimeout)
	defer cancel()
	err = obj.CallWithContext(ctx, "org.freedesktop.ScreenSaver.Inhibit", 0, "Station Manager", why).Store(&cookie)
	if err != nil {
		return nil, fmt.Errorf("ScreenSaver Inhibit: %w", err)
	}
	return func() error {
		// A FRESH deadline, not the acquire's: this closure runs at disarm, which
		// may be hours later, and the acquire's context expired long ago.
		rctx, rcancel := context.WithTimeout(context.Background(), surfaceTimeout)
		defer rcancel()
		return obj.CallWithContext(rctx, "org.freedesktop.ScreenSaver.UnInhibit", 0, cookie).Err
	}, nil
}
