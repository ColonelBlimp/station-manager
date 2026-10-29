package api

import "sync"

// LiveEntryGate admits live Phone / CW submissions — those to the effective
// default logbook — and is the seal a default change takes (ADR 0097 §2 step
// 4). Seal closes admission AT ONCE — a live submission arriving after that is
// refused, writing nothing — and then waits for every submission already
// admitted to finish, committed or failed. Release reopens it. A submission
// naming another logbook is a deliberate operation and is never refused here.
type LiveEntryGate struct {
	mu       sync.Mutex
	drained  *sync.Cond // signalled when inFlight reaches zero
	closed   bool
	inFlight int
}

// NewLiveEntryGate returns an open gate.
func NewLiveEntryGate() *LiveEntryGate {
	g := &LiveEntryGate{}
	g.drained = sync.NewCond(&g.mu)
	return g
}

// Admit admits a submission; live says it names the effective default. ok is
// false when a live one is refused because admission is closed; done must be
// called when an admitted one finishes, however it ends.
func (g *LiveEntryGate) Admit(live bool) (done func(), ok bool) {
	if !live {
		return func() {}, true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, false
	}
	g.inFlight++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.inFlight--
			if g.inFlight == 0 {
				g.drained.Broadcast()
			}
		})
	}, true
}

// Seal closes admission, then waits for every admitted live submission to
// finish. It never refuses: what it waits for is already running.
func (g *LiveEntryGate) Seal() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	for g.inFlight > 0 {
		g.drained.Wait()
	}
	return nil
}

// Release reopens admission.
func (g *LiveEntryGate) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = false
}

// Closed reports whether admission is closed: Seal has begun, whether it is
// still draining or done.
func (g *LiveEntryGate) Closed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

// InFlight is the number of admitted live submissions not yet finished.
func (g *LiveEntryGate) InFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inFlight
}
