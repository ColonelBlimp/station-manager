package email

import (
	"bufio"
	"bytes"
	"context"
	stderrs "errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// smtpFake is a minimal in-process SMTP server used for tests.
// Implements just enough of the protocol for the Service's
// connect-and-send path:
//
//	220 banner
//	250 EHLO
//	250 MAIL FROM
//	250 RCPT TO
//	354 / 250 DATA
//	221 QUIT
//
// Auth is accepted unconditionally — credential-validation testing
// belongs in net/smtp, not here.
type smtpFake struct {
	t        *testing.T
	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	captured []capturedMessage
}

type capturedMessage struct {
	from string
	to   string
	body string
}

func newSmtpFake(t *testing.T) *smtpFake {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &smtpFake{t: t, listener: l}
	f.wg.Add(1)
	go f.acceptLoop()
	return f
}

func (f *smtpFake) addr() string {
	return f.listener.Addr().String()
}

func (f *smtpFake) hostPort() (string, int) {
	host, portStr, _ := net.SplitHostPort(f.addr())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return host, port
}

func (f *smtpFake) close() {
	_ = f.listener.Close()
	f.wg.Wait()
}

func (f *smtpFake) messages() []capturedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capturedMessage, len(f.captured))
	copy(out, f.captured)
	return out
}

func (f *smtpFake) acceptLoop() {
	defer f.wg.Done()
	for {
		c, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			f.handle(c)
		}()
	}
}

func (f *smtpFake) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	flush := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}
	flush("220 fake.localhost ESMTP")

	var from, to string
	var bodyBuf bytes.Buffer
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
			flush("250-fake.localhost")
			flush("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH"):
			flush("235 OK")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			from = extractAddr(line[len("MAIL FROM:"):])
			flush("250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			to = extractAddr(line[len("RCPT TO:"):])
			flush("250 OK")
		case upper == "DATA":
			flush("354 send body")
			for {
				bl, berr := r.ReadString('\n')
				if berr != nil {
					return
				}
				if bl == ".\r\n" || bl == ".\n" {
					break
				}
				bodyBuf.WriteString(bl)
			}
			f.mu.Lock()
			f.captured = append(f.captured, capturedMessage{
				from: from,
				to:   to,
				body: bodyBuf.String(),
			})
			f.mu.Unlock()
			bodyBuf.Reset()
			flush("250 OK")
		case upper == "QUIT":
			flush("221 bye")
			return
		case upper == "RSET":
			flush("250 OK")
		case upper == "NOOP":
			flush("250 OK")
		default:
			flush("250 OK")
		}
	}
}

func extractAddr(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "<"); i >= 0 {
		s = s[i+1:]
		if j := strings.Index(s, ">"); j >= 0 {
			return s[:j]
		}
	}
	return s
}

// ----- tests -----

func TestSend_NilService_ReturnsErrMailerDisabled(t *testing.T) {
	var s *Service
	err := s.Send(context.Background(), Message{To: "a@b", Subject: "x"})
	if !stderrs.Is(err, ErrMailerDisabled) {
		t.Errorf("err = %v, want ErrMailerDisabled", err)
	}
}

func TestSend_DisabledMailer_ReturnsErrMailerDisabled(t *testing.T) {
	s := New(types.SmtpConfig{}, nil) // Enabled=false (zero value)
	err := s.Send(context.Background(), Message{To: "a@b", Subject: "x"})
	if !stderrs.Is(err, ErrMailerDisabled) {
		t.Errorf("err = %v, want ErrMailerDisabled", err)
	}
	if s.Enabled() {
		t.Errorf("Enabled() = true, want false when SmtpConfig.Enabled is false")
	}
}

func TestSend_MissingRecipient_ReturnsErrInvalidMessage(t *testing.T) {
	s := New(types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5}, nil)
	err := s.Send(context.Background(), Message{Subject: "x", Body: "y"})
	if !stderrs.Is(err, ErrInvalidMessage) {
		t.Errorf("err = %v, want ErrInvalidMessage", err)
	}
}

func TestSend_MissingSubject_ReturnsErrInvalidMessage(t *testing.T) {
	s := New(types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5}, nil)
	err := s.Send(context.Background(), Message{To: "a@b", Body: "y"})
	if !stderrs.Is(err, ErrInvalidMessage) {
		t.Errorf("err = %v, want ErrInvalidMessage", err)
	}
}

// TestSend_HappyPath_DeliversToFakeServer is the round-trip — connect
// to an in-process SMTP fake, send a message with an attachment,
// verify the captured envelope carries From/To/Subject + multipart
// body.
func TestSend_HappyPath_DeliversToFakeServer(t *testing.T) {
	fake := newSmtpFake(t)
	t.Cleanup(fake.close)

	host, port := fake.hostPort()
	s := New(types.SmtpConfig{
		Enabled:    true,
		Host:       host,
		Port:       port,
		From:       "daemon@example.com",
		Username:   "u",
		Password:   "p",
		TimeoutSec: 5,
		// StartTLS deliberately off — fake speaks plaintext only.
	}, nil)

	err := s.Send(context.Background(), Message{
		To:      "qsl@example.com",
		Subject: "Session ADIF",
		Body:    "ADIF attached.",
		Attachments: []Attachment{{
			Filename:    "session.adi",
			ContentType: "application/x-adif",
			Body:        []byte("<call:5>K1ABC<eor>"),
		}},
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	msgs := fake.messages()
	if len(msgs) != 1 {
		t.Fatalf("captured %d messages, want 1", len(msgs))
	}
	m := msgs[0]
	if m.from != "daemon@example.com" {
		t.Errorf("MAIL FROM = %q, want daemon@example.com", m.from)
	}
	if m.to != "qsl@example.com" {
		t.Errorf("RCPT TO = %q, want qsl@example.com", m.to)
	}
	if !strings.Contains(m.body, "Subject: Session ADIF") {
		t.Errorf("body missing Subject header; got:\n%s", m.body)
	}
	if !strings.Contains(m.body, "From: daemon@example.com") {
		t.Errorf("body missing From header; got:\n%s", m.body)
	}
	if !strings.Contains(m.body, "To: qsl@example.com") {
		t.Errorf("body missing To header; got:\n%s", m.body)
	}
	if !strings.Contains(m.body, "Content-Type: multipart/mixed") {
		t.Errorf("body missing multipart wrapper; got:\n%s", m.body)
	}
	if !strings.Contains(m.body, `filename="session.adi"`) {
		t.Errorf("body missing attachment filename; got:\n%s", m.body)
	}
	if !strings.Contains(m.body, "Content-Type: application/x-adif") {
		t.Errorf("body missing attachment Content-Type; got:\n%s", m.body)
	}
	if !strings.Contains(m.body, "ADIF attached.") {
		t.Errorf("body missing the operator-typed text; got:\n%s", m.body)
	}
}

// TestSend_NoAttachments_ProducesPlainTextEnvelope — the body-only
// branch (no multipart wrapper). Verifies the simpler envelope is
// also wire-correct.
func TestSend_NoAttachments_ProducesPlainTextEnvelope(t *testing.T) {
	fake := newSmtpFake(t)
	t.Cleanup(fake.close)
	host, port := fake.hostPort()
	s := New(types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", TimeoutSec: 5,
	}, nil)

	err := s.Send(context.Background(), Message{
		To: "to@x", Subject: "s", Body: "hello world",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	msgs := fake.messages()
	if len(msgs) != 1 {
		t.Fatalf("captured %d, want 1", len(msgs))
	}
	body := msgs[0].body
	if strings.Contains(body, "multipart/mixed") {
		t.Errorf("plain-text branch wrapped in multipart; got:\n%s", body)
	}
	if !strings.Contains(body, "Content-Type: text/plain") {
		t.Errorf("missing text/plain Content-Type; got:\n%s", body)
	}
	if !strings.Contains(body, "hello world") {
		t.Errorf("missing body text; got:\n%s", body)
	}
}

// TestSend_DefaultRecipient_SnapshottedFromConfig — the operator's
// "I always send to the same QSL manager" default. SessionPanel
// reads this at mount to pre-fill the recipient input.
func TestSend_DefaultRecipient_SnapshottedFromConfig(t *testing.T) {
	s := New(types.SmtpConfig{
		Enabled:          true,
		Host:             "x",
		Port:             25,
		From:             "f@x",
		DefaultRecipient: "qsl@manager.example",
		TimeoutSec:       5,
	}, nil)
	if got := s.DefaultRecipient(); got != "qsl@manager.example" {
		t.Errorf("DefaultRecipient = %q, want qsl@manager.example", got)
	}
}

func TestDefaultRecipient_NilService_ReturnsEmpty(t *testing.T) {
	var s *Service
	if got := s.DefaultRecipient(); got != "" {
		t.Errorf("nil-Service DefaultRecipient = %q, want empty", got)
	}
}

// TestSend_DialFailure_ReturnsWrappedError — host pointing at a
// closed port surfaces a transport error (not nil, not ErrMailerDisabled,
// not ErrInvalidMessage). Caller handler folds these into 502.
func TestSend_DialFailure_ReturnsWrappedError(t *testing.T) {
	// Listen then close to get a known-unused port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	_ = l.Close()

	s := New(types.SmtpConfig{
		Enabled:    true,
		Host:       "127.0.0.1",
		Port:       addr.Port,
		From:       "f@x",
		TimeoutSec: 2,
	}, nil)

	err = s.Send(context.Background(), Message{To: "to@x", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatalf("expected dial failure, got nil error")
	}
	if stderrs.Is(err, ErrMailerDisabled) {
		t.Errorf("dial failure misclassified as ErrMailerDisabled")
	}
	if stderrs.Is(err, ErrInvalidMessage) {
		t.Errorf("dial failure misclassified as ErrInvalidMessage")
	}
}

// Ensure the imports stay used even if a future refactor drops one.
var _ = io.EOF
