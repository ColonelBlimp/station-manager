package api

import "sync"

// LiveEntryGate admits live Phone / CW submissions — those to the effective
// default logbook — and is the seal a default change takes (ADR 0097 §2 step
// 4). Seal closes admission AT ONCE — a live submission arriving after that is
// refused, writing nothing — and then waits for every submission already
// admitted to finish, committed or failed. Release reopens it. A submission
// naming another logbook is a deliberate operation and is never refused here.
//
// Seals are OWNED (W-0029 part 2, G5). More than one operation can need live
// entry closed at once: a default change keeps its seal until the daemon
// restarts, and a confirmed exit of the app seals for its own short time. Each
// holder releases only what it sealed, and admission is open only while no
// holder has a seal. The gate's own Seal and Release are one holder, the one
// the archive manager uses for a default change and an activation; Owner makes
// another.
type LiveEntryGate struct {
	mu       sync.Mutex
	drained  *sync.Cond // signalled when inFlight reaches zero
	held     int        // how many holders have a seal; admission is closed while it is not zero
	own      bool       // the gate's own holder has a seal
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
	if g.held > 0 {
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

// seal takes a holder's seal (a second one by the same holder adds nothing),
// then waits for every admitted live submission to finish.
func (g *LiveEntryGate) seal(holding *bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !*holding {
		*holding = true
		g.held++
	}
	for g.inFlight > 0 {
		g.drained.Wait()
	}
}

// release gives up a holder's seal, if it has one. Admission reopens when the
// last holder lets go.
func (g *LiveEntryGate) release(holding *bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if *holding {
		*holding = false
		g.held--
	}
}

// Seal closes admission for the gate's own holder, then waits for every
// admitted live submission to finish. It never refuses: what it waits for is
// already running.
func (g *LiveEntryGate) Seal() error {
	g.seal(&g.own)
	return nil
}

// Release gives up the gate's own holder's seal. Admission stays closed while
// another holder has one.
func (g *LiveEntryGate) Release() { g.release(&g.own) }

// LiveEntrySeal is one more holder's seal on a gate (LiveEntryGate.Owner).
type LiveEntrySeal struct {
	g       *LiveEntryGate
	holding bool // guarded by g.mu
}

// Owner returns a new holder of this gate's seal.
func (g *LiveEntryGate) Owner() *LiveEntrySeal { return &LiveEntrySeal{g: g} }

// Seal closes admission for this holder, then waits for every admitted live
// submission to finish.
func (o *LiveEntrySeal) Seal() error {
	o.g.seal(&o.holding)
	return nil
}

// Release gives up this holder's seal, and no other's.
func (o *LiveEntrySeal) Release() { o.g.release(&o.holding) }

// Closed reports whether admission is closed: some holder's Seal has begun,
// whether it is still draining or done.
func (g *LiveEntryGate) Closed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.held > 0
}

// InFlight is the number of admitted live submissions not yet finished.
func (g *LiveEntryGate) InFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inFlight
}
