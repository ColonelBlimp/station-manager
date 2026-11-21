package api

import "testing"

// The live-entry gate's seals are owned (W-0029 part 2, G5; codex-192). A
// default change seals the gate and keeps it sealed until the daemon restarts;
// an exit seals it for its own short time. Each releases only what it sealed:
// live entry is open only when nobody holds a seal. The gate's own Seal and
// Release are the default change's hold, as before; an Owner is another
// holder, and the one there is belongs to the exit. (An archive activation
// seals FT8 and the rig, not live entry.)

func liveAdmitted(g *LiveEntryGate) bool { return liveRefusal(g) == LiveEntryAdmitted }

// liveRefusal is a live submission's answer from the gate; one admitted is finished at once.
func liveRefusal(g *LiveEntryGate) LiveEntryRefusal {
	done, refused := g.Admit(true)
	if refused == LiveEntryAdmitted {
		done()
	}
	return refused
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

// A refused live submission is told which holder refused it (the operator's ruling of
// 2026-11-21; codex-210). The default change's answer is given whenever its seal is held;
// the exit's only when the exit's is the one seal. Each release leaves the other's reason.
func TestLiveEntryGate_ARefusalNamesTheHolder(t *testing.T) {
	g := NewLiveEntryGate()
	exit := g.Owner()
	want := func(when string, want LiveEntryRefusal) {
		t.Helper()
		if got := liveRefusal(g); got != want {
			t.Fatalf("%s: a live submission got %v, want %v", when, got, want)
		}
	}
	want("nobody holds a seal", LiveEntryAdmitted)

	_ = exit.Seal()
	want("the exit alone", LiveEntryClosing)
	_ = exit.Seal()
	want("the exit, sealed twice", LiveEntryClosing)

	_ = g.Seal()
	want("the exit, then the default change", LiveEntryDefaultChanging)
	exit.Release()
	want("the exit has let go", LiveEntryDefaultChanging)
	exit.Release()
	want("the exit lets go a second time", LiveEntryDefaultChanging)

	_ = exit.Seal()
	g.Release()
	want("the default change has let go, the exit holds again", LiveEntryClosing)
	g.Release()
	want("the default change lets go a second time", LiveEntryClosing)

	exit.Release()
	want("the last holder has let go", LiveEntryAdmitted)

	// A submission to another logbook is never the gate's to refuse.
	_ = exit.Seal()
	_ = g.Seal()
	if done, refused := g.Admit(false); refused != LiveEntryAdmitted {
		t.Fatalf("a deliberate submission got %v under both seals, want admitted", refused)
	} else {
		done()
	}
}
