package email

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	stderrs "errors"
	"fmt"
	"io"
	"math/big"
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
	t          *testing.T
	listener   net.Listener
	wg         sync.WaitGroup
	mu         sync.Mutex
	captured   []capturedMessage
	rejectRcpt bool // when set, RCPT TO gets a 550 so the Rcpt error path fires (H4)
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
			if f.rejectRcpt {
				flush("550 mailbox unavailable")
				continue
			}
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

// --- STARTTLS test fake (review 2026-06-19 M1) ---

// selfSignedCert returns a server certificate valid for 127.0.0.1 plus a CA
// pool that trusts it, so the STARTTLS happy-path can verify end-to-end.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(crand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// starttlsFake is a one-connection SMTP server that advertises STARTTLS,
// upgrades the connection with the test certificate, and accepts the rest of
// the dialogue over TLS. Records whether a full DATA exchange completed AFTER
// the TLS upgrade, so a test can assert the message really went over TLS.
type starttlsFake struct {
	listener net.Listener
	cert     tls.Certificate
	wg       sync.WaitGroup
	mu       sync.Mutex
	gotData  bool
}

func newStartTLSFake(t *testing.T, cert tls.Certificate) *starttlsFake {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &starttlsFake{listener: l, cert: cert}
	f.wg.Add(1)
	go f.serve()
	return f
}

func (f *starttlsFake) hostPort() (string, int) {
	host, portStr, _ := net.SplitHostPort(f.listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return host, port
}

func (f *starttlsFake) close() {
	_ = f.listener.Close()
	f.wg.Wait()
}

func (f *starttlsFake) sawData() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotData
}

func (f *starttlsFake) serve() {
	defer f.wg.Done()
	c, err := f.listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	// Plaintext phase: banner, EHLO (advertise STARTTLS), STARTTLS → 220.
	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	send := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
	send("220 fake.localhost ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		up := strings.ToUpper(strings.TrimRight(line, "\r\n"))
		switch {
		case strings.HasPrefix(up, "EHLO") || strings.HasPrefix(up, "HELO"):
			send("250-fake.localhost")
			send("250 STARTTLS")
		case up == "STARTTLS":
			send("220 ready to start TLS")
			f.serveTLS(c)
			return
		default:
			send("500 expected STARTTLS")
			return
		}
	}
}

func (f *starttlsFake) serveTLS(c net.Conn) {
	tlsConn := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}})
	if err := tlsConn.Handshake(); err != nil {
		return
	}
	r := bufio.NewReader(tlsConn)
	w := bufio.NewWriter(tlsConn)
	send := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		up := strings.ToUpper(strings.TrimRight(line, "\r\n"))
		switch {
		case strings.HasPrefix(up, "EHLO") || strings.HasPrefix(up, "HELO"):
			send("250 fake.localhost")
		case up == "DATA":
			send("354 send body")
			for {
				bl, berr := r.ReadString('\n')
				if berr != nil {
					return
				}
				if bl == ".\r\n" || bl == ".\n" {
					break
				}
			}
			f.mu.Lock()
			f.gotData = true
			f.mu.Unlock()
			send("250 OK")
		case up == "QUIT":
			send("221 bye")
			return
		default:
			send("250 OK")
		}
	}
}

// TestSend_StartTLS_HappyPath exercises the production-default encrypted path
// (review 2026-06-19 M1): the client must STARTTLS-upgrade, verify the server
// cert against the configured Host (127.0.0.1 IP SAN), and complete DATA over
// TLS.
func TestSend_StartTLS_HappyPath(t *testing.T) {
	cert, pool := selfSignedCert(t)
	fake := newStartTLSFake(t, cert)
	defer fake.close()
	host, port := fake.hostPort()

	s := New(types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", StartTLS: true, TimeoutSec: 5,
	}, nil)
	s.tlsRoots = pool // trust the test cert

	if err := s.Send(context.Background(), Message{To: "a@b", Subject: "s", Body: "hi"}); err != nil {
		t.Fatalf("STARTTLS send failed: %v", err)
	}
	if !fake.sawData() {
		t.Error("server did not receive DATA over the TLS connection")
	}
}

// TestSend_StartTLS_FailsClosed: StartTLS=true against a server that does NOT
// offer STARTTLS must fail, never silently falling back to plaintext.
func TestSend_StartTLS_FailsClosed(t *testing.T) {
	fake := newSmtpFake(t) // plain fake, no STARTTLS advertised
	defer fake.close()
	host, port := fake.hostPort()

	s := New(types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", StartTLS: true, TimeoutSec: 5,
	}, nil)
	if err := s.Send(context.Background(), Message{To: "a@b", Subject: "s", Body: "hi"}); err == nil {
		t.Fatal("STARTTLS send against a non-TLS server should fail, not fall back to plaintext")
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

// TestSend_RejectsInvalidInputBeforeNetwork guards review 2026-06-19 M2: the
// mailer validates From, To, subject, and attachment content type at its own
// boundary (before any dial), returning ErrInvalidMessage — a future internal
// caller can't smuggle a bad recipient, a CR/LF header-injection, or a malformed
// attachment type past it. Host "x" never resolves, so reaching ErrInvalidMessage
// proves the reject happens before network I/O.
func TestSend_RejectsInvalidInputBeforeNetwork(t *testing.T) {
	cases := []struct {
		name string
		from string
		msg  Message
	}{
		{"bad from", "not-an-address!!", Message{To: "a@b", Subject: "s"}},
		{"bad to", "f@x", Message{To: "nope", Subject: "s"}},
		{"multiple recipients", "f@x", Message{To: "a@b, c@d", Subject: "s"}},
		{"crlf in subject", "f@x", Message{To: "a@b", Subject: "s\r\nBcc: evil@x"}},
		{"crlf in recipient", "f@x", Message{To: "a@b\r\nBcc: evil@x", Subject: "s"}},
		{"control char in filename", "f@x", Message{To: "a@b", Subject: "s",
			Attachments: []Attachment{{Filename: "bad\r\nname.adi", ContentType: "text/plain", Body: []byte("x")}}}},
		{"invalid attachment content type", "f@x", Message{To: "a@b", Subject: "s",
			Attachments: []Attachment{{Filename: "f.adi", ContentType: "not a type", Body: []byte("x")}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: tc.from, TimeoutSec: 5}, nil)
			if err := s.Send(context.Background(), tc.msg); !stderrs.Is(err, ErrInvalidMessage) {
				t.Errorf("Send = %v, want ErrInvalidMessage", err)
			}
		})
	}
}

// TestBuildMimeEnvelope_AttachmentFormatting pins the mime.FormatMediaType path
// (review M2): an empty content type defaults to application/octet-stream, and a
// non-ASCII filename is RFC 2231-encoded (filename*=) rather than mangled.
func TestBuildMimeEnvelope_AttachmentFormatting(t *testing.T) {
	body := string(buildMimeEnvelope("f@x", "a@b", Message{
		To: "a@b", Subject: "s", Body: "hi",
		Attachments: []Attachment{
			{Filename: "plain.adi", ContentType: "", Body: []byte("x")},
			{Filename: "rapùç.adi", ContentType: "application/x-adif", Body: []byte("y")},
		},
	}))
	if !strings.Contains(body, "application/octet-stream") {
		t.Errorf("empty content type was not defaulted; got:\n%s", body)
	}
	if !strings.Contains(body, "filename*=") {
		t.Errorf("non-ASCII filename was not RFC 2231-encoded; got:\n%s", body)
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
	// mime.FormatMediaType emits a bare token (no quotes) for a tspecial-free
	// filename like "session.adi"; quotes/RFC-2231 only kick in when needed.
	if !strings.Contains(m.body, "filename=session.adi") {
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
