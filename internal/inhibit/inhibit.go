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
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/godbus/dbus/v5"
)

// surface is one way of asking the system to stay awake. Kept unexported and
// tiny so the composition logic is testable without a bus of any kind — the
// D-Bus code is the part that cannot be exercised in CI, so it is pushed to the
// edges and the decisions live here.
type surface interface {
	name() string
	inhibit(why string) (release func(), err error)
}

// Inhibitor holds an inhibition across every surface the host provides.
type Inhibitor struct {
	surfaces []surface
	log      logging.Logger
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
	}
}

// Inhibit takes an inhibition on every surface that will grant one and returns a
// single release func covering them all.
//
// Partial success is SUCCESS: a sway user with no ScreenSaver provider still gets
// logind's idle:sleep block, and a headless box gets the same. An error means the
// host granted nothing at all, and then nothing is held — the caller is expected
// to log it and carry on transmitting.
func (i *Inhibitor) Inhibit(why string) (func(), error) {
	var (
		releases []func()
		failures []string
	)
	for _, s := range i.surfaces {
		rel, err := s.inhibit(why)
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
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, rel := range releases {
				rel()
			}
		})
	}, nil
}

// ---- logind (system bus) ----

type logindSurface struct{}

func (l *logindSurface) name() string { return "logind" }

func (l *logindSurface) inhibit(why string) (func(), error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	obj := conn.Object("org.freedesktop.login1", dbus.ObjectPath("/org/freedesktop/login1"))
	var fd dbus.UnixFD
	// mode=block, not delay: delay only postpones the action by a few seconds,
	// which is useless across a multi-hour FT8 run.
	err = obj.Call("org.freedesktop.login1.Manager.Inhibit", 0,
		"idle:sleep", "Station Manager", why, "block").Store(&fd)
	if err != nil {
		return nil, fmt.Errorf("login1 Inhibit: %w", err)
	}
	// The inhibition lives as long as this fd is open — that IS the handle, and
	// closing it is the only way to release. Wrapped in os.File so the descriptor
	// is owned and closed exactly once by the Go runtime's accounting.
	f := os.NewFile(uintptr(fd), "logind-inhibit")
	return func() { _ = f.Close() }, nil
}

// ---- org.freedesktop.ScreenSaver (session bus) ----

type screenSaverSurface struct{}

func (s *screenSaverSurface) name() string { return "screensaver" }

func (s *screenSaverSurface) inhibit(why string) (func(), error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		// The expected, unremarkable case on a headless host: no session, no
		// screen to blank. logind still covers idle and sleep.
		return nil, fmt.Errorf("session bus: %w", err)
	}
	obj := conn.Object("org.freedesktop.ScreenSaver", dbus.ObjectPath("/org/freedesktop/ScreenSaver"))
	var cookie uint32
	err = obj.Call("org.freedesktop.ScreenSaver.Inhibit", 0, "Station Manager", why).Store(&cookie)
	if err != nil {
		return nil, fmt.Errorf("ScreenSaver Inhibit: %w", err)
	}
	return func() {
		// Best-effort: if the desktop went away the inhibition went with it.
		_ = obj.Call("org.freedesktop.ScreenSaver.UnInhibit", 0, cookie).Err
	}, nil
}
