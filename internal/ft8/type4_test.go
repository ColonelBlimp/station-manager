package ft8

import "testing"

// These are fast, decode-free unit tests over the pure type-4 ladder logic. They feed the
// CANONICAL decoded forms (as the shipped decoder renders them — our call hashed to
// "<...>") straight into parseType4 / Advance, so they don't need a transmitter or a
// decode pass. TestType4_RoundTrip separately proves those canonical forms actually
// encode+decode.

func TestParseType4(t *testing.T) {
	cases := []struct {
		text     string
		wantFrom string
		wantTo   string
		wantKind msgKind
	}{
		// Their roger to us (us hashed in the addressed slot).
		{"<...> PJ4/NA2AA RR73", "PJ4/NA2AA", "<...>", msgRoger},
		{"<...> PJ4/NA2AA RRR", "PJ4/NA2AA", "<...>", msgRoger},
		{"<...> PJ4/NA2AA 73", "PJ4/NA2AA", "<...>", msg73},
		// Their bare call to us (work-a-caller opening).
		{"<...> PJ4/NA2AA", "PJ4/NA2AA", "<...>", msgOther},
		// Spelled addressed call also accepted (e.g. a peer resolved our hash).
		{"7Q5MLV PJ4/NA2AA RR73", "PJ4/NA2AA", "7Q5MLV", msgRoger},
		// Our own echo (we are hashed as the sender) — from is hashed, so NOT one of our
		// replies to match: parseType4 rejects a hashed sender.
		{"PJ4/NA2AA <...> 73", "", "", msgOther},
		// Standard/non-type-4 lines parse to zero.
		{"CQ PJ4/NA2AA", "", "", msgOther},
		{"", "", "", msgOther},
		{"K1ABC", "", "", msgOther},
	}
	for _, c := range cases {
		got := parseType4(c.text)
		if got.from != c.wantFrom || got.to != c.wantTo || got.kind != c.wantKind {
			t.Errorf("parseType4(%q) = {from:%q to:%q kind:%d}; want {from:%q to:%q kind:%d}",
				c.text, got.from, got.to, got.kind, c.wantFrom, c.wantTo, c.wantKind)
		}
	}
}

func TestIsHashedCall(t *testing.T) {
	yes := []string{"<...>", "<NA2AA>", "<A>"}
	no := []string{"", "<", ">", "PJ4/NA2AA", "7Q5MLV", "<...", "...>"}
	for _, s := range yes {
		if !isHashedCall(s) {
			t.Errorf("isHashedCall(%q) = false; want true", s)
		}
	}
	for _, s := range no {
		if isHashedCall(s) {
			t.Errorf("isHashedCall(%q) = true; want false", s)
		}
	}
}

// TestT4Exchange_Ladder walks the answer-a-CQ ladder: opening → (their roger) → our 73.
func TestT4Exchange_Ladder(t *testing.T) {
	e := NewT4Exchange("7Q5MLV", "PJ4/NA2AA", "", -12)
	if e.State != t4Calling {
		t.Fatalf("initial state = %v; want t4Calling", e.State)
	}
	if !e.HasSendSnr || e.SendSnr != -12 {
		t.Fatalf("SendSnr not captured: has=%v snr=%d", e.HasSendSnr, e.SendSnr)
	}

	msg, ok := e.TxMessage()
	if !ok || msg != "PJ4/NA2AA 7Q5MLV" {
		t.Fatalf("opening TxMessage = %q,%v; want %q,true", msg, ok, "PJ4/NA2AA 7Q5MLV")
	}

	// A decode for someone else must NOT advance us.
	if _, adv := e.Advance("<...> PJ4/NA2AA RR73"); !adv {
		t.Fatal("their roger to us should advance")
	}
	if _, adv := e.Advance("<...> K7XYZ RR73"); adv {
		t.Fatal("a roger from a different station must not advance")
	}

	e2, adv := e.Advance("<...> PJ4/NA2AA RR73")
	if !adv || e2.State != t4Confirming {
		t.Fatalf("after their roger: adv=%v state=%v; want true,t4Confirming", adv, e2.State)
	}
	msg, ok = e2.TxMessage()
	if !ok || msg != "PJ4/NA2AA 7Q5MLV 73" {
		t.Fatalf("confirming TxMessage = %q,%v; want %q,true", msg, ok, "PJ4/NA2AA 7Q5MLV 73")
	}

	e3 := e2.Sent()
	if e3.State != t4Done || !e3.Done() {
		t.Fatalf("after Sent: state=%v done=%v; want t4Done,true", e3.State, e3.Done())
	}
	if _, ok := e3.TxMessage(); ok {
		t.Fatal("done exchange should have nothing to transmit")
	}
}

// TestT4Exchange_AdvanceOnDirect73 lets a partner who skips RR73 straight to 73 still
// move us to confirming (we send our 73 and log).
func TestT4Exchange_AdvanceOnDirect73(t *testing.T) {
	e := NewT4Exchange("7Q5MLV", "PJ4/NA2AA", "", -5)
	e2, adv := e.Advance("<...> PJ4/NA2AA 73")
	if !adv || e2.State != t4Confirming {
		t.Fatalf("direct 73 should advance to confirming; adv=%v state=%v", adv, e2.State)
	}
}

// TestT4WorkExchange_Ladder walks the work-a-caller ladder: single RR73 → done.
func TestT4WorkExchange_Ladder(t *testing.T) {
	e := NewT4WorkExchange("7Q5MLV", "PJ4/NA2AA", "", 3)
	if e.State != t4wRogering {
		t.Fatalf("initial state = %v; want t4wRogering", e.State)
	}
	msg, ok := e.TxMessage()
	if !ok || msg != "PJ4/NA2AA 7Q5MLV RR73" {
		t.Fatalf("rogering TxMessage = %q,%v; want %q,true", msg, ok, "PJ4/NA2AA 7Q5MLV RR73")
	}
	// A received decode never advances the work side (it completes on our own transmit).
	if _, adv := e.Advance("<...> PJ4/NA2AA 73"); adv {
		t.Fatal("work side must not advance on a received decode")
	}
	e2 := e.Sent()
	if e2.State != t4wDone || !e2.Done() {
		t.Fatalf("after Sent: state=%v done=%v; want t4wDone,true", e2.State, e2.Done())
	}
	if _, ok := e2.TxMessage(); ok {
		t.Fatal("done work exchange should have nothing to transmit")
	}
}

// TestNewT4Exchange_Normalises upper-cases calls and trims an over-long grid to 4 chars.
func TestNewT4Exchange_Normalises(t *testing.T) {
	e := NewT4Exchange(" 7q5mlv ", "pj4/na2aa", "fk52xy", 0)
	if e.OurCall != "7Q5MLV" || e.TheirCall != "PJ4/NA2AA" {
		t.Errorf("calls not upper-cased/trimmed: our=%q their=%q", e.OurCall, e.TheirCall)
	}
	if e.TheirGrid != "FK52" {
		t.Errorf("grid not trimmed to 4: %q", e.TheirGrid)
	}
}
