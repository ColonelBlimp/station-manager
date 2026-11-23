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
// holder has a seal. There are three holders, and a refusal says which
// (LiveEntryRefusal): the gate's own Seal and Release, which the archive
// manager uses for a default change; SwitchOwner, for an archive switch
// (W-0029 part 3, C); and Owner, which is the app's exit.
type LiveEntryGate struct {
	mu        sync.Mutex
	drained   *sync.Cond // signalled when inFlight reaches zero
	held      int        // how many holders have a seal; admission is closed while it is not zero
	own       bool       // the gate's own holder has a seal: a default change
	switching bool       // the switch's holder has a seal
	inFlight  int
}

// NewLiveEntryGate returns an open gate.
func NewLiveEntryGate() *LiveEntryGate {
	g := &LiveEntryGate{}
	g.drained = sync.NewCond(&g.mu)
	return g
}

// LiveEntryRefusal says why a live submission was refused. The zero value is
// "not refused".
type LiveEntryRefusal int

const (
	// LiveEntryAdmitted: the submission was admitted.
	LiveEntryAdmitted LiveEntryRefusal = iota
	// LiveEntryDefaultChanging: the gate's own holder has a seal. A default
	// change is pending, the daemon restarts and the page reloads.
	LiveEntryDefaultChanging
	// LiveEntryClosing: only an Owner has a seal, which is the app's confirmed
	// exit closing its session. No restart and no reload follow.
	LiveEntryClosing
	// LiveEntrySwitching: an archive switch has a seal (SwitchOwner), and no
	// default change has. If the switch succeeds the daemon restarts, on the
	// archive asked for or, if that fails to start, on the one it left. If the
	// switch fails it releases its seal and nothing restarts.
	LiveEntrySwitching
)

// Admit admits a submission; live says it names the effective default. A live
// one is refused while admission is closed, and refused says by which holder;
// done must be called when an admitted one finishes, however it ends.
//
// The reason is decided in the same hold of the lock as the refusal. Looked up
// afterwards it could describe a state a release had already changed, and a
// refusal reports only a reason that is true. The order when more than one
// holder is sealed: a default change, then a switch, then the exit. A restart
// may follow the first two, so their answer is the one to act on; a default change
// and a switch refuse each other and are not sealed together in practice. A
// second kind of Owner would need a reason of its own here.
func (g *LiveEntryGate) Admit(live bool) (done func(), refused LiveEntryRefusal) {
	if !live {
		return func() {}, LiveEntryAdmitted
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held > 0 {
		switch {
		case g.own:
			return nil, LiveEntryDefaultChanging
		case g.switching:
			return nil, LiveEntrySwitching
		}
		return nil, LiveEntryClosing
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
	}, LiveEntryAdmitted
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

// LiveEntrySeal is one more holder's seal on a gate (LiveEntryGate.Owner,
// LiveEntryGate.SwitchOwner).
type LiveEntrySeal struct {
	g       *LiveEntryGate
	holding *bool // whether this holder has its seal; guarded by g.mu
}

// Owner returns a new holder of this gate's seal: the app's exit.
func (g *LiveEntryGate) Owner() *LiveEntrySeal { return &LiveEntrySeal{g: g, holding: new(bool)} }

// SwitchOwner returns the archive switch's holder of this gate's seal. There
// is one: every call returns a holder of the same seal, as one archive switch
// is in progress at a time.
func (g *LiveEntryGate) SwitchOwner() *LiveEntrySeal {
	return &LiveEntrySeal{g: g, holding: &g.switching}
}

// Seal closes admission for this holder, then waits for every admitted live
// submission to finish.
func (o *LiveEntrySeal) Seal() error {
	o.g.seal(o.holding)
	return nil
}

// Release gives up this holder's seal, and no other's.
func (o *LiveEntrySeal) Release() { o.g.release(o.holding) }

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
