package ft8

import (
	"testing"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
)

func TestParseReport(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"-13", -13, true},
		{"+04", 4, true},
		{"-01", -1, true},
		{"+00", 0, true},
		{"-50", -50, true},
		{"-1", 0, false},   // not zero-padded
		{"13", 0, false},   // no sign
		{"R-13", 0, false}, // R prefix not stripped here
		{"+0a", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := parseReport(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseReport(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFormatReport(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{-13, "-13"},
		{4, "+04"},
		{0, "+00"},
		{-1, "-01"},
		{-60, "-50"}, // clamp low to encodable range
		{60, "+49"},  // clamp high
		{-50, "-50"},
		{49, "+49"},
	}
	for _, c := range cases {
		if got := formatReport(c.in); got != c.want {
			t.Errorf("formatReport(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLooksLikeCall(t *testing.T) {
	calls := []string{"K1ABC", "G0XYZ", "G3XYZ/P", "2E0ABC", "VP8/G0XYZ"}
	for _, c := range calls {
		if !looksLikeCall(c) {
			t.Errorf("looksLikeCall(%q) = false, want true", c)
		}
	}
	notCalls := []string{"CQ", "DX", "POTA", "FN42", "IO91", "73", "AB", "ABCDE", "-10"}
	for _, c := range notCalls {
		if looksLikeCall(c) {
			t.Errorf("looksLikeCall(%q) = true, want false", c)
		}
	}
}

func TestParseMessage(t *testing.T) {
	cases := []struct {
		text   string
		kind   msgKind
		to     string
		from   string
		grid   string
		report int
	}{
		{"CQ K1ABC FN42", msgCQ, "", "K1ABC", "FN42", 0},
		{"CQ DX S56GD JN65", msgCQ, "", "S56GD", "JN65", 0},
		{"CQ POTA W2ABC", msgCQ, "", "W2ABC", "", 0},
		{"cq  k1abc  fn42", msgCQ, "", "K1ABC", "FN42", 0}, // case + whitespace
		{"G0XYZ K1ABC IO91", msgGrid, "G0XYZ", "K1ABC", "IO91", 0},
		{"G0XYZ K1ABC -10", msgReport, "G0XYZ", "K1ABC", "", -10},
		{"G0XYZ K1ABC +03", msgReport, "G0XYZ", "K1ABC", "", 3},
		{"G0XYZ K1ABC R-12", msgRReport, "G0XYZ", "K1ABC", "", -12},
		{"G0XYZ K1ABC R+05", msgRReport, "G0XYZ", "K1ABC", "", 5},
		{"G0XYZ K1ABC RRR", msgRoger, "G0XYZ", "K1ABC", "", 0},
		{"G0XYZ K1ABC RR73", msgRoger, "G0XYZ", "K1ABC", "", 0},
		{"G0XYZ K1ABC 73", msg73, "G0XYZ", "K1ABC", "", 0},
		{"G0XYZ K1ABC", msgOther, "G0XYZ", "K1ABC", "", 0}, // bare call-to-call
		{"", msgOther, "", "", "", 0},
		{"CQ DX", msgOther, "", "", "", 0}, // CQ with no call
		{"random noise", msgOther, "", "", "", 0},
	}
	for _, c := range cases {
		m := parseMessage(c.text)
		if m.kind != c.kind || m.to != c.to || m.from != c.from || m.grid != c.grid || m.report != c.report {
			t.Errorf("parseMessage(%q) = %+v, want kind=%d to=%q from=%q grid=%q report=%d",
				c.text, m, c.kind, c.to, c.from, c.grid, c.report)
		}
	}
}

// The happy path: answering CQ K1ABC FN42 as G0XYZ IO91, walking the full
// CQ→73 ladder. We transmit in the slot parity opposite K1ABC.
func TestExchangeHappyPath(t *testing.T) {
	e := NewExchange("g0xyz", "io91", "k1abc") // lower-case in, upper-case out

	// Rung 1 — Calling: send our call + grid.
	if msg, ok := e.TxMessage(); !ok || msg != "K1ABC G0XYZ IO91" {
		t.Fatalf("calling TxMessage = (%q, %v), want (%q, true)", msg, ok, "K1ABC G0XYZ IO91")
	}

	// They report us at -10; our decode of their signal measures -12.
	e, ok := e.Advance("G0XYZ K1ABC -10", -12)
	if !ok || e.State != txReporting {
		t.Fatalf("after their report: ok=%v state=%d, want ok=true state=txReporting", ok, e.State)
	}
	if !e.HasRcvdReport || e.RcvdReport != -10 {
		t.Errorf("RcvdReport = (%d, has=%v), want (-10, true)", e.RcvdReport, e.HasRcvdReport)
	}
	if !e.HasSendSnr || e.SendSnr != -12 {
		t.Errorf("SendSnr = (%d, has=%v), want (-12, true)", e.SendSnr, e.HasSendSnr)
	}

	// Rung 3 — Reporting: roger + our measured report (-12 → R-12).
	if msg, ok := e.TxMessage(); !ok || msg != "K1ABC G0XYZ R-12" {
		t.Fatalf("reporting TxMessage = (%q, %v), want (%q, true)", msg, ok, "K1ABC G0XYZ R-12")
	}

	// They roger us with RR73.
	e, ok = e.Advance("G0XYZ K1ABC RR73", -11)
	if !ok || e.State != txConfirming {
		t.Fatalf("after their RR73: ok=%v state=%d, want ok=true state=txConfirming", ok, e.State)
	}

	// Rung 5 — Confirming: send 73.
	if msg, ok := e.TxMessage(); !ok || msg != "K1ABC G0XYZ 73" {
		t.Fatalf("confirming TxMessage = (%q, %v), want (%q, true)", msg, ok, "K1ABC G0XYZ 73")
	}

	// After we transmit the 73, the exchange is done and ready to log.
	e = e.Sent()
	if !e.Done() || e.State != txDone {
		t.Fatalf("after Sent: done=%v state=%d, want done=true state=txDone", e.Done(), e.State)
	}
	if msg, ok := e.TxMessage(); ok || msg != "" {
		t.Errorf("done TxMessage = (%q, %v), want (\"\", false)", msg, ok)
	}
}

// RRR (instead of RR73) at rung 4 also advances Reporting → Confirming.
func TestExchangeAdvancesOnRRR(t *testing.T) {
	e := NewExchange("G0XYZ", "IO91", "K1ABC")
	e, _ = e.Advance("G0XYZ K1ABC -10", -12)
	e, ok := e.Advance("G0XYZ K1ABC RRR", -11)
	if !ok || e.State != txConfirming {
		t.Fatalf("RRR advance: ok=%v state=%d, want ok=true state=txConfirming", ok, e.State)
	}
}

func TestExchangeOffRamps(t *testing.T) {
	base := NewExchange("G0XYZ", "IO91", "K1ABC")

	t.Run("worked station answers someone else", func(t *testing.T) {
		e, ok := base.Advance("SP9ABC K1ABC -05", -12)
		if ok || e.State != txCalling {
			t.Fatalf("ok=%v state=%d, want ok=false state=txCalling", ok, e.State)
		}
	})

	t.Run("a different station replies to us", func(t *testing.T) {
		e, ok := base.Advance("G0XYZ SP9ABC -05", -12)
		if ok || e.State != txCalling {
			t.Fatalf("ok=%v state=%d, want ok=false state=txCalling", ok, e.State)
		}
	})

	t.Run("worked station sends another CQ", func(t *testing.T) {
		e, ok := base.Advance("CQ K1ABC FN42", -12)
		if ok || e.State != txCalling {
			t.Fatalf("ok=%v state=%d, want ok=false state=txCalling", ok, e.State)
		}
	})

	t.Run("unparseable decode", func(t *testing.T) {
		e, ok := base.Advance("not a message", -12)
		if ok || e.State != txCalling {
			t.Fatalf("ok=%v state=%d, want ok=false state=txCalling", ok, e.State)
		}
	})

	t.Run("wrong token for the rung — grid while calling", func(t *testing.T) {
		// A bare grid reply does not carry a report, so it does not advance
		// Calling (which waits for a report to us).
		e, ok := base.Advance("G0XYZ K1ABC IO91", -12)
		if ok || e.State != txCalling {
			t.Fatalf("ok=%v state=%d, want ok=false state=txCalling", ok, e.State)
		}
	})

	t.Run("RR73 while still calling does not skip a rung", func(t *testing.T) {
		e, ok := base.Advance("G0XYZ K1ABC RR73", -12)
		if ok || e.State != txCalling {
			t.Fatalf("ok=%v state=%d, want ok=false state=txCalling", ok, e.State)
		}
	})
}

// SendSnr is refreshed from the latest advancing decode so rung 3 reports the
// freshest measurement, not a stale one.
func TestExchangeReportsFreshSnr(t *testing.T) {
	e := NewExchange("G0XYZ", "IO91", "K1ABC")
	e, _ = e.Advance("G0XYZ K1ABC -10", -7)
	if msg, _ := e.TxMessage(); msg != "K1ABC G0XYZ R-07" {
		t.Fatalf("TxMessage = %q, want %q", msg, "K1ABC G0XYZ R-07")
	}
}

// An out-of-range SNR is clamped in the transmitted report so the message stays
// encodable.
func TestExchangeClampsReport(t *testing.T) {
	e := NewExchange("G0XYZ", "IO91", "K1ABC")
	e, _ = e.Advance("G0XYZ K1ABC -10", -75)
	if msg, _ := e.TxMessage(); msg != "K1ABC G0XYZ R-50" {
		t.Fatalf("TxMessage = %q, want %q", msg, "K1ABC G0XYZ R-50")
	}
}

// Every message the resolver emits across the whole ladder must be encodable by
// go-ft8's standard-message packer — the RF-safe offline guarantee (ADR 0029):
// if the resolver can only produce encodable messages, no rung can fail at TX
// time. Walk the ladder and feed each transmitted message to the encoder.
func TestExchangeMessagesAreEncodable(t *testing.T) {
	e := NewExchange("G0XYZ", "IO91", "K1ABC")
	steps := []struct {
		name    string
		advance func(Exchange) Exchange
	}{
		{"calling", func(e Exchange) Exchange { return e }},
		{"reporting", func(e Exchange) Exchange { e, _ = e.Advance("G0XYZ K1ABC -10", -12); return e }},
		{"confirming", func(e Exchange) Exchange { e, _ = e.Advance("G0XYZ K1ABC RR73", -11); return e }},
	}
	for _, s := range steps {
		e = s.advance(e)
		msg, ok := e.TxMessage()
		if !ok {
			t.Fatalf("%s: no TxMessage", s.name)
		}
		if _, err := goft8.EncodeStandardMessage(msg); err != nil {
			t.Errorf("%s: EncodeStandardMessage(%q) failed: %v", s.name, msg, err)
		}
	}
}
