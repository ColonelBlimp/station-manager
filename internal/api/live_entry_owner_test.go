package api

import "testing"

// The live-entry gate's seals are owned (W-0029 part 2, G5; codex-192). A
// default change seals the gate and keeps it sealed until the daemon restarts;
// an exit seals it for its own short time. Each releases only what it sealed:
// live entry is open only when nobody holds a seal. The gate's own Seal and
// Release are the default change's and the activation's hold, as before; an
// Owner is another holder.

func liveAdmitted(g *LiveEntryGate) bool {
	done, ok := g.Admit(true)
	if ok {
		done()
	}
	return ok
}

func TestLiveEntryGate_AnOwnerReleasesOnlyItsOwnSeal(t *testing.T) {
	t.Run("the default change holds; an exit seals and releases", func(t *testing.T) {
		g := NewLiveEntryGate()
		exit := g.Owner()
		if err := g.Seal(); err != nil { // the default change
			t.Fatal(err)
		}
		if err := exit.Seal(); err != nil {
			t.Fatal(err)
		}
		exit.Release()
		if !g.Closed() || liveAdmitted(g) {
			t.Fatal("the exit's release reopened live entry under the default change's seal")
		}
		g.Release()
		if g.Closed() || !liveAdmitted(g) {
			t.Fatal("live entry is still closed with no seal held")
		}
	})
	t.Run("an exit holds; the default change seals and keeps its seal", func(t *testing.T) {
		g := NewLiveEntryGate()
		exit := g.Owner()
		if err := exit.Seal(); err != nil {
			t.Fatal(err)
		}
		if err := g.Seal(); err != nil {
			t.Fatal(err)
		}
		exit.Release()
		if !g.Closed() || liveAdmitted(g) {
			t.Fatal("the exit's release reopened live entry under the default change's seal")
		}
	})
	t.Run("the default change releases; the exit's seal holds", func(t *testing.T) {
		g := NewLiveEntryGate()
		exit := g.Owner()
		if err := exit.Seal(); err != nil {
			t.Fatal(err)
		}
		if err := g.Seal(); err != nil {
			t.Fatal(err)
		}
		g.Release() // the default change failed and let go
		if !g.Closed() || liveAdmitted(g) {
			t.Fatal("the default change's release reopened live entry under the exit's seal")
		}
		exit.Release()
		if g.Closed() {
			t.Fatal("live entry is still closed with no seal held")
		}
	})
	t.Run("a release by a holder that holds nothing changes nothing", func(t *testing.T) {
		g := NewLiveEntryGate()
		exit, other := g.Owner(), g.Owner()
		if err := g.Seal(); err != nil {
			t.Fatal(err)
		}
		exit.Release()
		other.Release()
		exit.Release()
		if !g.Closed() {
			t.Fatal("a release by a holder with no seal reopened live entry")
		}
	})
	t.Run("sealing twice and releasing once lets go", func(t *testing.T) {
		g := NewLiveEntryGate()
		exit := g.Owner()
		_ = exit.Seal()
		_ = exit.Seal()
		exit.Release()
		if g.Closed() {
			t.Fatal("one holder's second seal outlived its release")
		}
		_ = g.Seal()
		_ = g.Seal()
		g.Release()
		if g.Closed() {
			t.Fatal("the gate's own second seal outlived its release")
		}
	})
}
