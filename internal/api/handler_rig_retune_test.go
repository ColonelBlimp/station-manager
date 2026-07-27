package api

import (
	"encoding/json"
	stderr "errors"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	RETUNE-WHILE-TRANSMITTING specification, written before the implementation
	(2026-07-27, from on-air dogfood).

	The operator hit this working a CQ run: clicking a band button mid-transmission
	returned 409 "the rig is transmitting (tune or FT8); try again once transmission
	ends". That refusal is protective — switching bands with PTT up hot-switches
	relays under RF, which is how amps get damaged — but it leaves the same intent
	handled two different ways:

	    nudge the VFO mid-transmission  -> PTT drops, session ends, TX disarms (~2 s)
	    click a band button             -> refused; nothing happens

	Both are the operator saying "I have decided to leave". The physical route acts
	on it; the software route argues with it. The fix is not to permit switching
	under RF — it is to perform, from this end, the same sequence the dial guard
	already performs from the other.

	Rules:

	 19. A retuning command is NOT refused because SM is transmitting. TX is stopped
	     FIRST — PTT down, RF ceased, session ended, TX disarmed — and only then is
	     the command written to the rig. The rig is never switched while keyed.
	 20. If TX cannot be stopped, the retune does NOT proceed, and the operator is
	     told. Leaving them on frequency and transmitting is strictly better than
	     switching a keyed rig.
	 21. Only RETUNING commands do this. A command that does not move the rig off
	     frequency must not tear down a working session as a side effect.

	Rule 21 is the constraint that keeps 19 honest: "stop TX first" is the right
	answer for a band change and quite wrong for a mode change.

	The stop itself is injected (cmd/smd wires it to the FT8 subsystem), matching
	SetTxKeyer / SetDialSource / SetCatGate — internal/api composes the two
	subsystems without either importing the other. ORDERING is asserted through what
	reached the rig: this server's bridge has no serial client, so a command that
	gets as far as the write fails with rig_not_connected. Seeing that code proves
	the write was attempted; not seeing it proves it never was.
*/

var errStopFailed = stderr.New("could not stop transmitting")

// retuneServer builds a rig-capable server with a recording stop-hook installed.
func retuneServer(t *testing.T, stopErr error) (*Server, *int) {
	t.Helper()
	srv := rigTestServer(t)
	calls := 0
	srv.stopTxForRetune = func() error {
		calls++
		return stopErr
	}
	return srv, &calls
}

// newServerWithFt8 builds a Server through the real constructor, which is the only
// way to observe what New wires.
func newServerWithFt8(t *testing.T, withFt8 bool) *Server {
	t.Helper()
	base := testServer(t)
	var f *ft8.Service
	if withFt8 {
		f = ft8.NewService(types.Ft8Config{Enabled: true}, base.logger, t.TempDir())
	}
	return New(config.Config{}, "test", base.cfg, base.qso, base.db, base.logger,
		base.hub, base.enrich, base.mailer, base.bridge, f)
}

func rigErrorCode(t *testing.T, body string) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return e.Code
}

// --- 19: stop transmitting, then retune -------------------------------------

// No 409, and the sequence is stop-then-write — never write-while-keyed. The
// ordering is the safety property: a test that only checked "not refused" would be
// satisfied by an implementation that switched the rig under RF.
func TestRigRetune_StopsTransmitBeforeWritingTheCommand(t *testing.T) {
	srv, calls := retuneServer(t, nil)

	w := postRigCommand(t, srv, `{"op":"set_band","value":"40m"}`)

	if *calls != 1 {
		t.Fatalf("the stop hook must run before a retuning command; calls = %d", *calls)
	}
	if code := rigErrorCode(t, w.Body.String()); code != "rig_not_connected" {
		t.Fatalf("the command must go on to be written (this bridge has no client, so "+
			"rig_not_connected is how we see it was attempted); got %q / %d",
			code, w.Code)
	}
}

// --- 20: a failed stop cancels the retune -----------------------------------

// Better to leave the operator transmitting where they are than to switch a rig we
// cannot confirm is unkeyed.
func TestRigRetune_DoesNotRetuneWhenTxCannotBeStopped(t *testing.T) {
	srv, calls := retuneServer(t, errStopFailed)

	w := postRigCommand(t, srv, `{"op":"set_band","value":"40m"}`)

	if *calls != 1 {
		t.Fatalf("the stop must have been attempted; calls = %d", *calls)
	}
	if code := rigErrorCode(t, w.Body.String()); code == "rig_not_connected" {
		t.Fatal("the command reached the rig despite TX not being confirmed stopped")
	}
	if w.Code < 400 {
		t.Fatalf("the operator must be told the band change did not happen; got %d", w.Code)
	}
}

// --- 21: only retuning commands ---------------------------------------------

// "Stop TX first" is right for a band change and wrong for anything that does not
// move the rig off frequency. Without this, any command through this path would
// silently end a working session.
func TestRigRetune_OnlyRetuningCommandsStopTransmit(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantStop bool
	}{
		{"set_band retunes", `{"op":"set_band","value":"40m"}`, true},
		{"set_freq retunes", `{"op":"set_freq","value":7074000}`, true},
		{"set_mode does not", `{"op":"set_mode","value":"USB"}`, false},
		{"a batch containing a retune counts", `{"commands":[{"op":"set_mode","value":"USB"},{"op":"set_band","value":"40m"}]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, calls := retuneServer(t, nil)

			postRigCommand(t, srv, tc.body)

			got := *calls > 0
			if got != tc.wantStop {
				t.Fatalf("stopped = %v, want %v — only a command that moves the rig off "+
					"frequency may tear down a session", got, tc.wantStop)
			}
		})
	}
}

// A server with no hook wired (no FT8 subsystem) must still serve rig commands.
func TestRigRetune_NoHookIsHarmless(t *testing.T) {
	srv := rigTestServer(t)
	srv.stopTxForRetune = nil

	w := postRigCommand(t, srv, `{"op":"set_band","value":"40m"}`)

	if code := rigErrorCode(t, w.Body.String()); code != "rig_not_connected" {
		t.Fatalf("with nothing to stop the command proceeds as before; got %q / %d", code, w.Code)
	}
}

// --- the wire itself ---------------------------------------------------------

// Both halves of this behaviour pass in isolation whether or not they are
// connected: the handler works against an injected hook, and the FT8 subsystem's
// StopForRetune works against a service. If nothing installs one into the other,
// every test above stays green and the rig still gets switched while keyed on air.
// That exact gap — two correct halves, no wire — is what made a previous round's
// guard fire nowhere (codex P1 on 6e974717), so the connection gets its own test.
func TestRigRetune_ServerWiresTheStopHookFromTheFt8Subsystem(t *testing.T) {
	t.Run("wired when an FT8 subsystem is present", func(t *testing.T) {
		srv := newServerWithFt8(t, true)
		if srv.stopTxForRetune == nil {
			t.Fatal("nothing would stop TX before a band change; the rig would be switched keyed")
		}
	})

	t.Run("absent without one", func(t *testing.T) {
		srv := newServerWithFt8(t, false)
		if srv.stopTxForRetune != nil {
			t.Fatal("there is no transmitter to stop; a hook here could only fail confusingly")
		}
	})
}
