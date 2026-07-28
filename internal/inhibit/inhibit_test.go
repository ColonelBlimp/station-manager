package inhibit

// Behaviour spec for the multi-surface idle inhibitor, written before the
// implementation.
//
// The whole reason this package composes SURFACES rather than calling one API is
// portability, and the rules below are the portability contract stated as
// behaviour. logind (org.freedesktop.login1) is present on every systemd distro
// and on the non-systemd ones that ship elogind, and it lives on the SYSTEM bus so
// it works headless. org.freedesktop.ScreenSaver is provided by KDE, GNOME, XFCE,
// MATE and Cinnamon, but not by bare wlroots compositors and not without a session
// bus. Neither is guaranteed; between them they cover essentially every desktop SM
// will meet.
//
// So the contract is deliberately NOT "both must work". It is "hold whatever this
// system offers, and only admit failure when the system offers nothing" — which is
// what makes the feature degrade cleanly from a KDE desktop to a headless server
// instead of needing a per-distro matrix.
//
// Rule 4 exists because of a state this design creates: partial success means the
// release func must free a DIFFERENT set of surfaces each time, and the tempting
// implementation (release everything, assume everything was taken) double-frees or
// panics on the surfaces that never acquired.

// Rules 6-9 pin the BOUND on a surface that never answers. They exist because
// godbus's obj.Call is CallWithContext(context.Background(), ...) — a D-Bus peer
// that stays CONNECTED but stops replying blocks forever, and this package is
// called from the arm and disarm paths. A hang is not an error, so without a
// bound it slips straight past the "a failure is logged and transmitting
// continues" contract that rules 1-5 encode. Each of 7, 8 and 9 is a state the
// bound itself CREATES: other surfaces still needing to be held, a success that
// arrives after we gave up, and a release that hangs rather than an acquire.

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// hangingSurface models a D-Bus peer that is connected but has stopped replying.
// Which call hangs is selectable: the acquire and the release are separate round
// trips and they wedge the caller in different places — the acquire stalls ArmTx
// with TX already armed, the release stalls disarm ahead of the playback device
// being closed.
type hangingSurface struct {
	mu          sync.Mutex
	nameStr     string
	gate        chan struct{} // closed to let the hung call finally return
	hangAcquire bool
	hangRelease bool
	acquires    int
	releases    int
}

func (h *hangingSurface) name() string { return h.nameStr }

func (h *hangingSurface) inhibit(string) (func(), error) {
	if h.hangAcquire {
		<-h.gate
	}
	h.mu.Lock()
	h.acquires++
	h.mu.Unlock()
	return func() {
		if h.hangRelease {
			<-h.gate
		}
		h.mu.Lock()
		h.releases++
		h.mu.Unlock()
	}, nil
}

func (h *hangingSurface) counts() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.acquires, h.releases
}

// eventually polls until want is satisfied, so a rule about something happening
// in the BACKGROUND (rule 8) fails on its assertion rather than by deadlocking.
func eventually(t *testing.T, what string, want func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if want() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("timed out waiting for %s", what)
}

// mustNotBlock runs fn and fails with an assertion if it has not returned in
// time. Calling fn directly would hang the test binary instead, which reports as
// a panic dump rather than as the rule that was broken.
func mustNotBlock(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not return: a hung surface blocks the caller indefinitely", what)
	}
}

// inhibitBounded is mustNotBlock for the rules that need Inhibit's RESULT. Same
// reason: a rule about what survives a hang has to report as its own assertion,
// not by deadlocking every other test in the binary.
func inhibitBounded(t *testing.T, in *Inhibitor, why string) (func(), error) {
	t.Helper()
	type res struct {
		rel func()
		err error
	}
	ch := make(chan res, 1)
	go func() {
		rel, err := in.Inhibit(why)
		ch <- res{rel, err}
	}()
	select {
	case r := <-ch:
		return r.rel, r.err
	case <-time.After(2 * time.Second):
		t.Fatal("Inhibit did not return: a hung surface blocks the caller indefinitely")
		return nil, nil
	}
}

type fakeSurface struct {
	mu       sync.Mutex
	nameStr  string
	err      error
	acquires int
	releases int
}

func (f *fakeSurface) name() string { return f.nameStr }

func (f *fakeSurface) inhibit(why string) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.acquires++
	return func() {
		f.mu.Lock()
		f.releases++
		f.mu.Unlock()
	}, nil
}

func (f *fakeSurface) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquires, f.releases
}

func newTestInhibitor(surfaces ...surface) *Inhibitor {
	// Generous: rules 1-5 use fakes that answer instantly, so the bound must
	// never be what those rules are measuring.
	return newTestInhibitorWithTimeout(2*time.Second, surfaces...)
}

func newTestInhibitorWithTimeout(d time.Duration, surfaces ...surface) *Inhibitor {
	return &Inhibitor{surfaces: surfaces, log: logging.Noop(), timeout: d}
}

// Rule 1 — one working surface is enough. A sway user with no ScreenSaver
// provider still gets logind; a headless box still gets logind.
func TestInhibit_SucceedsWhenAnySurfaceSucceeds(t *testing.T) {
	good := &fakeSurface{nameStr: "logind"}
	bad := &fakeSurface{nameStr: "screensaver", err: errors.New("no session bus")}

	rel, err := newTestInhibitor(bad, good).Inhibit("testing")
	if err != nil {
		t.Fatalf("want success when one surface works, got: %v", err)
	}
	if rel == nil {
		t.Fatal("want a non-nil release func on success")
	}
	if acq, _ := good.counts(); acq != 1 {
		t.Errorf("working surface: want 1 acquisition, got %d", acq)
	}
}

// Rule 2 — an error ONLY when nothing at all could be held, and in that case
// nothing is left holding. The ft8 layer treats the error as non-fatal, so a
// false success here would silently claim protection that does not exist.
func TestInhibit_FailsOnlyWhenEverySurfaceFails(t *testing.T) {
	a := &fakeSurface{nameStr: "logind", err: errors.New("no system bus")}
	b := &fakeSurface{nameStr: "screensaver", err: errors.New("no session bus")}

	rel, err := newTestInhibitor(a, b).Inhibit("testing")
	if err == nil {
		t.Fatal("want an error when every surface fails")
	}
	if rel != nil {
		t.Error("want a nil release func when nothing was acquired")
	}
	for _, f := range []*fakeSurface{a, b} {
		if acq, _ := f.counts(); acq != 0 {
			t.Errorf("%s: want 0 acquisitions, got %d", f.name(), acq)
		}
	}
}

// Rule 3 — release frees EVERY surface that was acquired. Freeing only the first
// leaves the desktop half-inhibited with nothing left to free it.
func TestInhibit_ReleaseFreesEverySurfaceAcquired(t *testing.T) {
	a := &fakeSurface{nameStr: "logind"}
	b := &fakeSurface{nameStr: "screensaver"}

	rel, err := newTestInhibitor(a, b).Inhibit("testing")
	if err != nil {
		t.Fatalf("inhibit: %v", err)
	}
	rel()

	for _, f := range []*fakeSurface{a, b} {
		acq, relN := f.counts()
		if acq != 1 || relN != 1 {
			t.Errorf("%s: want 1 acquire and 1 release, got %d/%d", f.name(), acq, relN)
		}
	}
}

// Rule 4 — with a surface that failed, release must free the ones that worked and
// leave the failed one alone. This is the partial-success state the design
// creates, and the state a "release everything" implementation gets wrong.
func TestInhibit_ReleaseSkipsSurfacesThatNeverAcquired(t *testing.T) {
	good := &fakeSurface{nameStr: "logind"}
	bad := &fakeSurface{nameStr: "screensaver", err: errors.New("not provided")}

	rel, err := newTestInhibitor(good, bad).Inhibit("testing")
	if err != nil {
		t.Fatalf("inhibit: %v", err)
	}
	rel()

	if acq, relN := good.counts(); acq != 1 || relN != 1 {
		t.Errorf("working surface: want 1/1, got %d/%d", acq, relN)
	}
	if _, relN := bad.counts(); relN != 0 {
		t.Errorf("failed surface: want 0 releases, got %d", relN)
	}
}

// Rule 5 — release is idempotent. The ft8 layer can race an acquire against a
// disarm, and its recovery path calls the release it was handed; a double free
// against a real D-Bus cookie would either error or, worse, cancel an inhibition
// a LATER arm had legitimately taken.
func TestInhibit_ReleaseIsIdempotent(t *testing.T) {
	a := &fakeSurface{nameStr: "logind"}

	rel, err := newTestInhibitor(a).Inhibit("testing")
	if err != nil {
		t.Fatalf("inhibit: %v", err)
	}
	rel()
	rel()
	rel()

	if _, relN := a.counts(); relN != 1 {
		t.Errorf("want exactly 1 underlying release after 3 calls, got %d", relN)
	}
}

// Rule 6 — a surface that never answers must not block the caller. Inhibit runs
// on the ARM path with txArmed ALREADY true, so an unbounded wait leaves the
// operator's arm request hanging on a wedged desktop service while the rig is
// armed and the SPA shows nothing resolved.
func TestInhibit_DoesNotBlockOnAHungAcquire(t *testing.T) {
	hung := &hangingSurface{nameStr: "screensaver", gate: make(chan struct{}), hangAcquire: true}
	defer close(hung.gate)

	in := newTestInhibitorWithTimeout(30*time.Millisecond, hung, &fakeSurface{nameStr: "logind"})
	mustNotBlock(t, "Inhibit", func() { _, _ = in.Inhibit("testing") })
}

// Rule 7 — a hung surface must not cost us the surfaces that DO work. Rule 1
// already covers a surface that ERRORS; a hang is the other way one can be
// unavailable, and it must degrade the same way rather than failing the lot.
func TestInhibit_HungSurfaceStillLeavesOthersHeld(t *testing.T) {
	hung := &hangingSurface{nameStr: "screensaver", gate: make(chan struct{}), hangAcquire: true}
	defer close(hung.gate)
	good := &fakeSurface{nameStr: "logind"}

	in := newTestInhibitorWithTimeout(30*time.Millisecond, hung, good)
	rel, err := inhibitBounded(t, in, "testing")
	if err != nil {
		t.Fatalf("want success while one surface hangs, got: %v", err)
	}
	if acq, _ := good.counts(); acq != 1 {
		t.Errorf("working surface: want 1 acquisition, got %d", acq)
	}
	rel()
	if _, relN := good.counts(); relN != 1 {
		t.Errorf("working surface: want 1 release, got %d", relN)
	}
}

// Rule 8 — a surface that succeeds AFTER we gave up waiting must still be
// released. This is the dangerous half of the bound: an inhibition whose release
// func nobody holds never ends, so the desktop stops idling for the life of the
// process — strictly worse than the fault being mitigated, and the same reasoning
// that makes a refused arm hold nothing in internal/ft8.
func TestInhibit_LateAcquireIsReleasedNotOrphaned(t *testing.T) {
	slow := &hangingSurface{nameStr: "screensaver", gate: make(chan struct{}), hangAcquire: true}

	in := newTestInhibitorWithTimeout(30*time.Millisecond, &fakeSurface{nameStr: "logind"}, slow)
	rel, err := inhibitBounded(t, in, "testing")
	if err != nil {
		t.Fatalf("inhibit: %v", err)
	}
	rel()

	// Only now does the slow surface answer — after Inhibit returned and after the
	// caller already released everything it was given.
	close(slow.gate)

	eventually(t, "the late acquisition to be released", func() bool {
		acq, relN := slow.counts()
		return acq == 1 && relN == 1
	})
}

// Rule 9 — a hung RELEASE must not block the caller either. disarmTxLocked
// invokes the release BEFORE it waits for the in-flight transmission, abandons
// the session and closes the playback device, so a release that never returns
// leaves the audio device open and, on the closing path, stops the daemon exiting.
func TestInhibit_DoesNotBlockOnAHungRelease(t *testing.T) {
	slow := &hangingSurface{nameStr: "screensaver", gate: make(chan struct{}), hangRelease: true}
	defer close(slow.gate)

	in := newTestInhibitorWithTimeout(30*time.Millisecond, slow)
	rel, err := inhibitBounded(t, in, "testing")
	if err != nil {
		t.Fatalf("inhibit: %v", err)
	}
	mustNotBlock(t, "release", rel)
}
