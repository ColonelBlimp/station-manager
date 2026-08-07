package ft8

import (
	"strings"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Caller-side happy path: we called CQ, DL9UW answered with JO41, we work it to RR73.
func TestCallerExchangeHappyPath(t *testing.T) {
	// Their answer (7Q5MLV DL9UW JO41) measured at our SNR of -8 → that's the report
	// we send. lower-case in, upper-case out.
	e := NewCallerExchange("7q5mlv", "dl9uw", "jo41", -8)

	if e.State != cqReporting {
		t.Fatalf("initial state = %d, want cqReporting", e.State)
	}
	if !e.HasSendSnr || e.SendSnr != -8 {
		t.Errorf("SendSnr = (%d, has=%v), want (-8, true)", e.SendSnr, e.HasSendSnr)
	}
	if e.TheirGrid != "JO41" {
		t.Errorf("TheirGrid = %q, want JO41", e.TheirGrid)
	}

	// Rung 1 — Reporting: their call + our (bare) report of them.
	if msg, ok := e.TxMessage(); !ok || msg != "DL9UW 7Q5MLV -08" {
		t.Fatalf("reporting TxMessage = (%q, %v), want (%q, true)", msg, ok, "DL9UW 7Q5MLV -08")
	}

	// They roger our report and send theirs (-15).
	e, ok := e.Advance("7Q5MLV DL9UW R-15")
	if !ok || e.State != cqRogering {
		t.Fatalf("after their R-report: ok=%v state=%d, want ok=true state=cqRogering", ok, e.State)
	}
	if !e.HasRcvdReport || e.RcvdReport != -15 {
		t.Errorf("RcvdReport = (%d, has=%v), want (-15, true)", e.RcvdReport, e.HasRcvdReport)
	}

	// Rung 2 — Rogering: send RR73.
	if msg, ok := e.TxMessage(); !ok || msg != "DL9UW 7Q5MLV RR73" {
		t.Fatalf("rogering TxMessage = (%q, %v), want (%q, true)", msg, ok, "DL9UW 7Q5MLV RR73")
	}

	// After we transmit RR73, the exchange is done and ready to log.
	e = e.Sent()
	if !e.Done() || e.State != cqDone {
		t.Fatalf("after Sent: done=%v state=%d, want done=true state=cqDone", e.Done(), e.State)
	}
	if msg, ok := e.TxMessage(); ok || msg != "" {
		t.Errorf("done TxMessage = (%q, %v), want (\"\", false)", msg, ok)
	}
}

// A bare roger (RRR / RR73) at Reporting also advances → Rogering, just without an
// RST_RCVD to log.
func TestCallerExchangeAdvancesOnBareRoger(t *testing.T) {
	for _, tok := range []string{"RRR", "RR73"} {
		e := NewCallerExchange("7Q5MLV", "DL9UW", "JO41", -8)
		e, ok := e.Advance("7Q5MLV DL9UW " + tok)
		if !ok || e.State != cqRogering {
			t.Fatalf("%s advance: ok=%v state=%d, want ok=true state=cqRogering", tok, ok, e.State)
		}
		if e.HasRcvdReport {
			t.Errorf("%s: HasRcvdReport = true, want false (bare roger carries no report)", tok)
		}
	}
}

func TestCallerExchangeOffRamps(t *testing.T) {
	base := NewCallerExchange("7Q5MLV", "DL9UW", "JO41", -8)

	t.Run("decode addressed to someone else", func(t *testing.T) {
		if _, ok := base.Advance("K1ABC DL9UW R-15"); ok {
			t.Error("advanced on a decode addressed to another station")
		}
	})
	t.Run("decode from someone else", func(t *testing.T) {
		if _, ok := base.Advance("7Q5MLV K1ABC R-15"); ok {
			t.Error("advanced on a decode from another station")
		}
	})
	t.Run("worked station re-answers with grid (didn't copy our report)", func(t *testing.T) {
		// Repeat case: they re-send their answer; we don't advance, we resend our report.
		if e, ok := base.Advance("7Q5MLV DL9UW JO41"); ok || e.State != cqReporting {
			t.Errorf("advanced on a repeated grid answer: ok=%v state=%d", ok, e.State)
		}
	})
	t.Run("unparseable", func(t *testing.T) {
		if _, ok := base.Advance("not a message"); ok {
			t.Error("advanced on an unparseable decode")
		}
	})
}

func TestCallerExchangeNoAdvanceFromTerminalStates(t *testing.T) {
	e := NewCallerExchange("7Q5MLV", "DL9UW", "JO41", -8)
	e, _ = e.Advance("7Q5MLV DL9UW R-15") // → cqRogering
	// A decode in cqRogering doesn't advance — completion is our RR73 TX (Sent).
	if _, ok := e.Advance("7Q5MLV DL9UW 73"); ok {
		t.Error("cqRogering advanced on a decode; should only complete on Sent()")
	}
	e = e.Sent() // → cqDone
	if _, ok := e.Advance("7Q5MLV DL9UW 73"); ok {
		t.Error("cqDone advanced on a decode; terminal state must not move")
	}
}

// Every caller-side TX message must be an encodable standard FT8 message — these go on
// the air. Uses the real 2026-06-12 on-air calls (7Q5MLV / DL9UW), so this also pins
// that the bare-report and RR73 forms encode for that pair.
func TestCallerExchangeMessagesAreEncodable(t *testing.T) {
	e := NewCallerExchange("7Q5MLV", "DL9UW", "JO41", -8)
	var msgs []string
	if m, ok := e.TxMessage(); ok { // reporting
		msgs = append(msgs, m)
	}
	e, _ = e.Advance("7Q5MLV DL9UW R-15")
	if m, ok := e.TxMessage(); ok { // rogering
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("collected %d TX messages, want 2", len(msgs))
	}
	for _, m := range msgs {
		if _, err := goft8.EncodeStandardMessage(m); err != nil {
			t.Errorf("EncodeStandardMessage(%q) failed: %v", m, err)
		}
	}
}

// TestCallerExchangeLogsTheReportItTransmits closes the review finding that the
// logged RST_SENT could differ from the report actually sent: formatReport clamped
// the on-air message while the raw SNR stayed on the exchange and rode through to
// the QSO. SNR 99 transmitted +49 and logged 99 — outbound, durable data going to
// QRZ/ClubLog. Reachable because their_snr arrives unvalidated from the client on
// the work-a-caller path (work_sequencer.go → NewCallerExchange).
func TestCallerExchangeLogsTheReportItTransmits(t *testing.T) {
	e := NewCallerExchange("7Q5MLV", "DL9UW", "JO41", 99)

	msg, ok := e.TxMessage()
	if !ok {
		t.Fatal("expected a report message to transmit")
	}
	// The token that actually goes on the air.
	onAir := msg[strings.LastIndex(msg, " ")+1:]

	// The same exchange, logged the way the sequencer logs it.
	c := CompletedQso{TheirCall: e.TheirCall, OurReport: e.SendSnr, HasOurReport: e.HasSendSnr}
	q := BuildQso(c, types.LoggingStation{Operator: "7Q5MLV"}, 1, time.Unix(0, 0).UTC(), nil)

	if want := "+49"; onAir != want {
		t.Fatalf("on-air report = %q, want %q", onAir, want)
	}
	if q.RstSent != "49" {
		t.Fatalf("logged RST_SENT = %q, want %q (the report transmitted, not the raw SNR)", q.RstSent, "49")
	}
	// The invariant itself, independent of the values above: what we log is what we
	// sent, ignoring the sign prefix formatReport adds for the air.
	if got := strings.TrimPrefix(onAir, "+"); got != q.RstSent {
		t.Fatalf("logged RST_SENT %q does not match the transmitted report %q", q.RstSent, got)
	}
}

// SendSnr clamps to the encodable report range (mirrors Exchange's clamp), so an
// out-of-range measurement never produces a message EncodeStandardMessage rejects.
func TestCallerExchangeClampsReport(t *testing.T) {
	e := NewCallerExchange("7Q5MLV", "DL9UW", "JO41", 99) // absurd SNR → clamps to +49
	msg, ok := e.TxMessage()
	if !ok || msg != "DL9UW 7Q5MLV +49" {
		t.Fatalf("clamped report TxMessage = (%q, %v), want (%q, true)", msg, ok, "DL9UW 7Q5MLV +49")
	}
	if _, err := goft8.EncodeStandardMessage(msg); err != nil {
		t.Errorf("clamped message not encodable: %v", err)
	}
}
