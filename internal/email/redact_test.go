package email

// H4 (docs/reviews/internal-codebase-logging-gaps.md) — email logs must not retain PII: the
// "email sent" Info line carried the full recipient + the operator-supplied subject, the API
// error path logged the full recipient, and SMTP MAIL FROM / RCPT TO error strings embedded
// the raw addresses. Operator rulings 2026-08-16: log to_domain (domain only; "unknown" when
// unparseable — never the raw address); a fixed kind=session_email instead of the subject
// (subject never logged); and redact the SMTP error-chain addresses to stage context +
// from_domain/to_domain.

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestDomain(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "example.com",
		"a@b@example.org":   "example.org", // last @ wins
		"@example.net":      "example.net", // no local part is fine (no PII)
		"no-at-sign":        "unknown",
		"alice@":            "unknown",
		"":                  "unknown",
		"   ":               "unknown",
	}
	for in, want := range cases {
		if got := Domain(in); got != want {
			t.Errorf("Domain(%q) = %q, want %q", in, got, want)
		}
	}
}

func oneRecord(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	var found map[string]any
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec["message"] == msg {
			found = rec
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%q records = %d, want 1\n%s", msg, n, buf.String())
	}
	return found
}

// C1: the "email sent" record carries to_domain + kind and NEVER the raw recipient or subject.
func TestSend_LogRedactsRecipientAndKind(t *testing.T) {
	fake := newSmtpFake(t)
	defer fake.close()
	host, port := fake.hostPort()

	buf := &bytes.Buffer{}
	s := New(types.SmtpConfig{Enabled: true, Host: host, Port: port, From: "daemon@station.example", TimeoutSec: 5},
		logging.NewForWriter(buf))

	err := s.Send(context.Background(), Message{
		To: "alice@recipient.example", Subject: "Secret Session Report 2026", Body: "hi", Kind: "session_email",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	rec := oneRecord(t, buf, "email sent")
	if rec["to_domain"] != "recipient.example" {
		t.Errorf("to_domain = %v, want recipient.example", rec["to_domain"])
	}
	if rec["kind"] != "session_email" {
		t.Errorf("kind = %v, want session_email", rec["kind"])
	}
	if _, ok := rec["subject"]; ok {
		t.Errorf("subject must never be logged: %v", rec["subject"])
	}
	if _, ok := rec["to"]; ok {
		t.Errorf("raw recipient must never be logged: %v", rec["to"])
	}
	// Belt-and-braces: neither the raw recipient nor the subject appears anywhere on the line.
	if strings.Contains(buf.String(), "alice@recipient.example") || strings.Contains(buf.String(), "Secret Session") {
		t.Errorf("raw recipient or subject leaked into the log:\n%s", buf.String())
	}
}

// C2/ruling 3: an SMTP RCPT rejection produces an error carrying the to_domain and stage
// context, never the raw recipient.
func TestSend_RcptRejectError_RedactsAddress(t *testing.T) {
	fake := newSmtpFake(t)
	fake.rejectRcpt = true
	defer fake.close()
	host, port := fake.hostPort()

	s := New(types.SmtpConfig{Enabled: true, Host: host, Port: port, From: "daemon@station.example", TimeoutSec: 5}, nil)

	err := s.Send(context.Background(), Message{To: "alice@recipient.example", Subject: "s", Body: "hi", Kind: "session_email"})
	if err == nil {
		t.Fatal("want an error when RCPT is rejected")
	}
	msg := err.Error()
	if strings.Contains(msg, "alice@recipient.example") || strings.Contains(msg, "alice") {
		t.Errorf("error leaked the raw recipient: %v", msg)
	}
	if !strings.Contains(msg, "recipient.example") {
		t.Errorf("error should carry the to_domain, got: %v", msg)
	}
}
