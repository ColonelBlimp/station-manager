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

import (
	"errors"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

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
	return &Inhibitor{surfaces: surfaces, log: logging.Noop()}
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
