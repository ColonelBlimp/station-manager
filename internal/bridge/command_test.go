package bridge

import (
	"context"
	stderr "errors"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newCommandTestService builds an FTdx10-configured Service with a fakeSerial
// wired in as the active client, WITHOUT starting the pipeline — SendCommand
// only needs an active client and the configured rigdef, so the test sets
// activeClient directly and skips pipeline-goroutine timing. FTdx10 (not the
// ft710 default) because the exposed set_freq/set_mode commands live in its
// rigdef.
func newCommandTestService(t *testing.T) (*Service, *fakeSerial) {
	t.Helper()
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake"},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, &logging.Service{})
	fake := newFakeSerial()
	s.mu.Lock()
	s.activeClient = fake
	// Identity is confirmed in the happy-path helper — the write gate (H2)
	// blocks SendCommands until the rig identifies as the configured driver.
	s.identityConfirmed = true
	s.mu.Unlock()
	return s, fake
}

// TestSendCommand covers the happy path: exposed ops encode through
// cat.EncodeCommand (pad for set_freq, value_map for set_mode) and the
// resulting wire bytes reach the serial client in order.
func TestSendCommand(t *testing.T) {
	s, fake := newCommandTestService(t)

	cases := []struct {
		op, value, want string
	}{
		{"set_freq", "14074000", "FA014074000;"},
		{"set_mode", "DATA-U", "MD0C;"},
	}
	for _, tc := range cases {
		if err := s.SendCommand(context.Background(), tc.op, tc.value); err != nil {
			t.Fatalf("SendCommand(%q, %q): %v", tc.op, tc.value, err)
		}
	}

	writes := fake.recordedWrites()
	if len(writes) != len(cases) {
		t.Fatalf("recorded %d writes, want %d: %q", len(writes), len(cases), writes)
	}
	for i, tc := range cases {
		if string(writes[i]) != tc.want {
			t.Errorf("write[%d] = %q, want %q", i, writes[i], tc.want)
		}
	}
}

// TestSendCommand_RejectsBeforeWrite proves the bad-command paths never touch
// the serial port — even with an active, identity-verified client (the
// newCommandTestService default): a not-exposed, unknown, unmapped, or
// invalid-padded op returns the matching cat sentinel and records zero writes.
func TestSendCommand_RejectsBeforeWrite(t *testing.T) {
	cases := []struct {
		name    string
		op      string
		value   string
		wantErr error
	}{
		{"not exposed", "PLAYBACK", "5", cat.ErrCommandNotExposed},
		{"unknown op", "frobnicate", "x", cat.ErrUnknownCommand},
		{"unmapped mode", "set_mode", "NOT-A-MODE", cat.ErrUnmappedValue},
		// Padded-value validation (review 2026-06-05 M1): a non-digit set_power
		// is rejected at encode, so a malformed "PCabc;" never reaches the wire.
		{"non-digit set_power", "set_power", "abc", cat.ErrInvalidPaddedValue},
		{"over-wide set_freq", "set_freq", "1407400000", cat.ErrInvalidPaddedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fake := newCommandTestService(t)
			err := s.SendCommand(context.Background(), tc.op, tc.value)
			if !stderr.Is(err, tc.wantErr) {
				t.Fatalf("SendCommand(%q) error = %v, want %v", tc.op, err, tc.wantErr)
			}
			if w := fake.recordedWrites(); len(w) != 0 {
				t.Errorf("expected no writes on rejected command, got %q", w)
			}
		})
	}
}

// TestSendCommand_NoActiveClient covers the no-silent-no-op contract: with no
// rig connected, an operator command fails with ErrRigNotConnected rather than
// succeeding silently (contrast TriggerBootstrap, which no-ops).
func TestSendCommand_NoActiveClient(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake"},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, &logging.Service{})
	// activeClient left nil — pipeline never started.
	err := s.SendCommand(context.Background(), "set_freq", "14074000")
	if !stderr.Is(err, ErrRigNotConnected) {
		t.Fatalf("SendCommand with no active client = %v, want ErrRigNotConnected", err)
	}
}

// TestSendCommand_RefusesUnverifiedIdentity covers the H2 write gate: a rig is
// connected (activeClient set) but its identity is NOT confirmed as the
// configured driver. The command must be refused with ErrRigIdentityUnverified
// and nothing may reach the wire — covers a driver typo / unrecognised /
// never-identified rig.
func TestSendCommand_RefusesUnverifiedIdentity(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.mu.Lock()
	s.identityConfirmed = false // connected, but identity not (yet) confirmed
	s.mu.Unlock()

	err := s.SendCommand(context.Background(), "set_freq", "14074000")
	if !stderr.Is(err, ErrRigIdentityUnverified) {
		t.Fatalf("SendCommand with unverified identity = %v, want ErrRigIdentityUnverified", err)
	}
	if n := len(fake.recordedWrites()); n != 0 {
		t.Errorf("a command was written despite unverified identity (%d writes)", n)
	}
}

// TestSendCommands covers the batch path: multiple ops encode and concatenate
// into a single CAT line in one write (the atomic "tune to band" shape).
func TestSendCommands(t *testing.T) {
	s, fake := newCommandTestService(t)

	err := s.SendCommands(context.Background(), []RigCommand{
		{Op: "set_freq", Value: "14074000"},
		{Op: "set_mode", Value: "DATA-U"},
	})
	if err != nil {
		t.Fatalf("SendCommands: %v", err)
	}
	writes := fake.recordedWrites()
	if len(writes) != 1 {
		t.Fatalf("recorded %d writes, want 1 (a batch is one line): %q", len(writes), writes)
	}
	if got := string(writes[0]); got != "FA014074000;MD0C;" {
		t.Errorf("batch line = %q, want %q", got, "FA014074000;MD0C;")
	}
}

// TestSendCommands_RejectsWholeBatchBeforeWrite proves all-or-nothing: a bad op
// anywhere in the batch fails the whole thing with nothing written.
func TestSendCommands_RejectsWholeBatchBeforeWrite(t *testing.T) {
	s, fake := newCommandTestService(t)

	err := s.SendCommands(context.Background(), []RigCommand{
		{Op: "set_freq", Value: "14074000"},
		{Op: "frobnicate", Value: "x"},
	})
	if !stderr.Is(err, cat.ErrUnknownCommand) {
		t.Fatalf("SendCommands error = %v, want ErrUnknownCommand", err)
	}
	if w := fake.recordedWrites(); len(w) != 0 {
		t.Errorf("expected no writes on rejected batch, got %q", w)
	}
}

func TestSendCommands_Empty(t *testing.T) {
	s, _ := newCommandTestService(t)
	if err := s.SendCommands(context.Background(), nil); err == nil {
		t.Fatal("SendCommands(nil) = nil, want error")
	}
}
