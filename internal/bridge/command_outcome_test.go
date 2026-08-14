package bridge

import (
	"bytes"
	"context"
	stderr "errors"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// bufCommandLog swaps the service's command logger for a buffer-backed one (window
// long enough that only an explicit flush emits a coalesced run). Same-package.
func bufCommandLog(s *Service) *bytes.Buffer {
	var buf bytes.Buffer
	s.cmdLog = newCommandLog(logging.NewForWriter(&buf), time.Hour)
	return &buf
}

// L4 acceptance: a successful command produces ONE durable outcome at the bridge
// boundary — op-id, protocol, op, value, batch, applied — the record the HTTP 202
// alone did not carry. The returned op-id is what the handler echoes.
func TestSendCommands_LogsOutcome_Applied(t *testing.T) {
	s, _ := newCommandTestService(t)
	buf := bufCommandLog(s)

	opID, err := s.SendCommands(context.Background(), []RigCommand{{Op: "set_mode", Value: "DATA-U"}})
	if err != nil {
		t.Fatalf("SendCommands: %v", err)
	}
	if opID == "" {
		t.Fatal("SendCommands must return a generated op-id")
	}

	got := clLines(buf.String(), "rig command applied")
	if len(got) != 1 {
		t.Fatalf("want one outcome, got %d: %q", len(got), buf.String())
	}
	for _, want := range []string{
		`"op_id":"` + opID + `"`, `"protocol":"kenwood"`,
		`"ops":["set_mode"]`, `"values":["DATA-U"]`, `"batch":1`, `"applied":1`,
	} {
		if !strings.Contains(got[0], want) {
			t.Errorf("outcome missing %s: %s", want, got[0])
		}
	}
}

// L4 acceptance: rapid freq-steps do not log per step — they coalesce into one
// summary (a different op or the quiet window flushes them).
func TestSendCommands_CoalescesFreqSteps(t *testing.T) {
	s, _ := newCommandTestService(t)
	buf := bufCommandLog(s)

	for _, v := range []string{"14074000", "14074100", "14074200"} {
		if _, err := s.SendCommands(context.Background(), []RigCommand{{Op: "set_freq", Value: v}}); err != nil {
			t.Fatalf("SendCommands(set_freq %s): %v", v, err)
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("freq-steps must coalesce, not log per step: %q", buf.String())
	}
	s.cmdLog.flush()
	got := clLines(buf.String(), "coalesced VFO step")
	if len(got) != 1 || !strings.Contains(got[0], `"count":3`) || !strings.Contains(got[0], `"last_value":"14074200"`) {
		t.Fatalf("want one coalesced summary (count 3, last 14074200): %q", buf.String())
	}
}

// L4 headline confusable state — proven end-to-end over the real CI-V path: a batch
// whose SECOND op the rig NAKs is recorded as PARTIALLY applied (applied 1 of 2), and
// names the failed index and op. That is what tells a partial batch from a full one.
func TestSendCommandsCIV_LogsPartialApply(t *testing.T) {
	reply := func(w []byte) []byte {
		// CI-V frame: FE FE <to> <from> <cmd> …; cmd 06 == set_mode → NAK, else OK.
		if len(w) >= 5 && w[4] == 0x06 {
			return append([]byte(nil), civAckNGFrame...)
		}
		return append([]byte(nil), civAckOKFrame...)
	}
	s, _, _, cleanup := startedCIVServiceWith(t, reply)
	defer cleanup()
	buf := bufCommandLog(s)

	opID, err := s.SendCommands(context.Background(), []RigCommand{
		{Op: "set_freq", Value: "18100000"},
		{Op: "set_mode", Value: "USB-D"},
	})
	if !stderr.Is(err, ErrCommandRejected) {
		t.Fatalf("err = %v, want ErrCommandRejected", err)
	}

	got := clLines(buf.String(), "partially applied")
	if len(got) != 1 || !strings.Contains(got[0], `"level":"warn"`) {
		t.Fatalf("a mid-batch NAK must warn as a partial apply: %q", buf.String())
	}
	for _, want := range []string{
		`"op_id":"` + opID + `"`, `"protocol":"icom_civ"`,
		`"applied":1`, `"batch":2`, `"failed_index":1`, `"failed_op":"set_mode"`,
	} {
		if !strings.Contains(got[0], want) {
			t.Errorf("partial outcome missing %s: %s", want, got[0])
		}
	}
	// And it must NOT read as a full apply.
	if strings.Contains(buf.String(), `"applied":2`) {
		t.Errorf("a NAKed batch must not read as fully applied: %s", buf.String())
	}
}
