// Package email is the daemon's general-purpose mailer. Callers
// build a Message and call Service.Send; how the message gets to
// the recipient (SMTP submission with optional STARTTLS) is the
// service's problem.
//
// Designed for two consumers (today and future):
//
//   - SessionPanel "email this session's ADIF" button
//     (POST /v1/session/email handler).
//   - Future alert paths — forwarder backlog, refresher repeated
//     failures, daemon-health probes — call the same Send with
//     their own Subject / Body / Attachments.
//
// "External services degrade, not crash" applies: smtp.enabled=false is the
// kill switch — Send returns ErrMailerDisabled and callers fold that into a
// user-visible "email not configured" path rather than a 500. (When enabled,
// config validation requires host/from/port/timeout, so an enabled-but-blank
// block is rejected at startup, not silently disabled.) SMTP transport failures
// return wrapped errors the caller surfaces as a toast.
//
// Stdlib-only by design — net/smtp + crypto/tls + a hand-rolled
// MIME multipart envelope. The message shape (one text/plain body,
// any number of binary attachments) is small enough that pulling
// in gomail just to format MIME would be a dependency for nothing.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	stderrs "errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ServiceName is the DI bean ID for the mailer (used by internal/iocdi).
const ServiceName = "email"

// ErrMailerDisabled is returned by Send when the operator's SMTP
// block has Enabled=false. Callers check with errors.Is and surface
// "email not configured" to the operator.
var ErrMailerDisabled = stderrs.New("mailer disabled (smtp.enabled is false)")

// ErrInvalidMessage is returned for malformed callers (no recipient,
// no subject, etc.). Distinct from transport errors so handlers can
// map to 400 vs 5xx.
var ErrInvalidMessage = stderrs.New("invalid email message")

// Attachment is one file attached to an outbound message. ContentType
// is the MIME type (e.g. "application/x-adif"); Body is the raw bytes
// (the service base64-encodes for transport).
type Attachment struct {
	Filename    string
	ContentType string
	Body        []byte
}

// Message is what callers hand to Send. Single-recipient by design
// — the SessionPanel use case is "send this to my QSL manager," and
// future alert paths typically target one ops address. A future
// multi-recipient extension becomes To []string then if the need
// emerges; for now, simpler is right.
type Message struct {
	To      string
	Subject string
	// Kind is a fixed, PII-free classifier for the message (e.g. "session_email"), logged
	// instead of the operator-supplied Subject so smd.log never retains subject text (H4).
	Kind        string
	Body        string // plain text body
	Attachments []Attachment
}

// Domain returns the domain part of an email address for safe logging — never the local part
// or the full address. Returns "unknown" when the address has no parseable domain, so a
// redacted log or error can never fall back to raw PII (H4).
func Domain(addr string) string {
	at := strings.LastIndexByte(addr, '@')
	if at < 0 || at == len(addr)-1 {
		return "unknown"
	}
	if d := strings.TrimSpace(addr[at+1:]); d != "" {
		return d
	}
	return "unknown"
}

// Service is the singleton mailer. Constructed once at daemon
// startup with the operator's SMTP config; the same instance is
// shared by every consumer through the iocdi container.
//
// Thread-safe — every Send call opens its own SMTP connection
// (one-shot, connect-and-send-and-quit) so there's no shared mutable
// state between concurrent callers. The cost is one TCP+TLS
// handshake per message, which is fine at the volume this daemon
// produces (a handful of session emails per day).
type Service struct {
	cfg    types.SmtpConfig
	logger *logging.Service

	// tlsRoots overrides the root CA pool the STARTTLS handshake verifies the
	// server certificate against. nil (production) → the system roots. Tests set
	// it to a self-signed test cert's pool so the STARTTLS path can be exercised
	// end-to-end; server-name verification and the TLS 1.2 minimum still apply.
	tlsRoots *x509.CertPool
}

// New constructs a mailer Service. The cfg is read once and snapshotted
// — runtime config changes (operator edits SMTP block via PUT /v1/config)
// don't reach an existing Service instance. The daemon recreates the
// Service on relevant config changes if/when that lands; for now
// the operator restarts after editing SMTP creds.
func New(cfg types.SmtpConfig, logger *logging.Service) *Service {
	return &Service{cfg: cfg, logger: logger}
}

// Enabled reports whether the mailer can actually send. Handlers
// can check this before building a Message to short-circuit with a
// clearer error, though Send itself is also safe to call (returns
// ErrMailerDisabled).
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// DefaultRecipient returns the operator's pre-configured recipient
// address, used by the SessionPanel to pre-fill the recipient input.
// Empty string means no default — operator types it every time.
func (s *Service) DefaultRecipient() string {
	if s == nil {
		return ""
	}
	return s.cfg.DefaultRecipient
}

// Send delivers msg via the configured SMTP server. Returns
// ErrMailerDisabled if smtp.enabled is false, ErrInvalidMessage for
// malformed input, or a wrapped transport error otherwise.
//
// Connect-and-send semantics: every call opens a fresh SMTP session
// (DIAL → STARTTLS → AUTH → MAIL FROM → RCPT TO → DATA → QUIT) and
// closes it. No connection pooling — at this daemon's volume the
// per-message handshake cost is negligible and the simplification
// is worth it.
//
// ctx bounds the entire round-trip via a deadline derived from
// cfg.TimeoutSec. SMTP servers vary widely in tolerance for slow
// clients; 30s is the configured default.
// Timeout reports the configured SMTP transaction budget — the same value
// Send arms its connection deadline with. Callers that must stay responsive
// PAST a send (the session-email handler extends its HTTP write deadline over
// it, codex 2026-08-08 P1) size against this rather than re-reading config,
// which can have changed since the Service was built. Zero when nil/blank —
// the caller's margin still applies.
func (s *Service) Timeout() time.Duration {
	if s == nil {
		return 0
	}
	return time.Duration(s.cfg.TimeoutSec) * time.Second
}

func (s *Service) Send(ctx context.Context, msg Message) error {
	const op errors.Op = "email.Send"

	if s == nil || !s.cfg.Enabled {
		return ErrMailerDisabled
	}

	// Validate at the package boundary, before any network I/O — internal/email
	// is a general-purpose mailer whose future callers (alert paths) won't repeat
	// the HTTP handler's checks, so address/header/attachment safety must live
	// here (review 2026-06-19 M2). fromAddr/toAddr are the parsed bare mailboxes
	// used for the SMTP envelope; they can't carry CR/LF (ParseAddress rejects
	// it), which closes header-injection on the envelope.
	fromAddr, toAddr, err := s.validateMessage(op, msg)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(time.Duration(s.cfg.TimeoutSec) * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))
	dialer := &net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("dial smtp %s", addr)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return errors.New(op).WithErr(err).WithMsg("set conn deadline")
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return errors.New(op).WithErr(err).WithMsg("smtp.NewClient")
	}
	defer func() {
		// Best-effort QUIT; ignore error on the close path because the
		// real outcome (DATA returned ok or not) has already been
		// captured above.
		_ = client.Quit()
	}()

	if s.cfg.StartTLS {
		tlsCfg := &tls.Config{
			ServerName: s.cfg.Host,
			MinVersion: tls.VersionTLS12,
			RootCAs:    s.tlsRoots, // nil → system roots (production)
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return errors.New(op).WithErr(err).WithMsg("starttls")
		}
	}

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return errors.New(op).WithErr(err).WithMsg("smtp auth")
		}
	}

	if err := client.Mail(fromAddr); err != nil {
		return errors.New(op).WithErr(err).WithMsgf("smtp MAIL FROM rejected (from_domain %s)", Domain(fromAddr))
	}
	if err := client.Rcpt(toAddr); err != nil {
		return errors.New(op).WithErr(err).WithMsgf("smtp RCPT TO rejected (to_domain %s)", Domain(toAddr))
	}

	w, err := client.Data()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("smtp data")
	}
	if _, err := w.Write(buildMimeEnvelope(fromAddr, toAddr, msg)); err != nil {
		_ = w.Close()
		return errors.New(op).WithErr(err).WithMsg("write data")
	}
	if err := w.Close(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("close data")
	}

	if s.logger != nil {
		// H4: log the recipient DOMAIN and a fixed message kind — never the raw recipient
		// or the operator-supplied subject (smd.log is 0644).
		s.logger.InfoWith().
			Str("to_domain", Domain(msg.To)).
			Str("kind", msg.Kind).
			Int("attachments", len(msg.Attachments)).
			Msg("email sent")
	}
	return nil
}

// validateMessage enforces the mailer's input contract before any network I/O
// and returns the parsed bare from/to mailboxes for the SMTP envelope. cfg.From
// and msg.To must each be exactly one RFC 5322 mailbox; the subject is required
// and must be control-character-free (header-injection guard); attachment
// filenames must be control-free and any declared content type must be a valid
// media type. Every failure wraps ErrInvalidMessage so callers map it to 400,
// not a transport 5xx (review 2026-06-19 M2).
func (s *Service) validateMessage(op errors.Op, msg Message) (fromAddr, toAddr string, err error) {
	from, perr := mail.ParseAddress(s.cfg.From)
	if perr != nil {
		return "", "", errors.New(op).WithErr(ErrInvalidMessage).WithMsgf("invalid smtp.from %q", s.cfg.From)
	}
	if msg.To == "" {
		return "", "", errors.New(op).WithErr(ErrInvalidMessage).WithMsg("recipient is required")
	}
	to, perr := mail.ParseAddress(msg.To)
	if perr != nil {
		return "", "", errors.New(op).WithErr(ErrInvalidMessage).WithMsgf("invalid recipient %q", msg.To)
	}
	if msg.Subject == "" {
		return "", "", errors.New(op).WithErr(ErrInvalidMessage).WithMsg("subject is required")
	}
	if hasControlChars(msg.Subject) {
		return "", "", errors.New(op).WithErr(ErrInvalidMessage).WithMsg("subject contains control characters")
	}
	for _, att := range msg.Attachments {
		if hasControlChars(att.Filename) {
			return "", "", errors.New(op).WithErr(ErrInvalidMessage).
				WithMsgf("attachment filename %q contains control characters", att.Filename)
		}
		if att.ContentType != "" {
			if _, _, mperr := mime.ParseMediaType(att.ContentType); mperr != nil {
				return "", "", errors.New(op).WithErr(ErrInvalidMessage).
					WithMsgf("invalid attachment content type %q", att.ContentType)
			}
		}
	}
	return from.Address, to.Address, nil
}

// hasControlChars reports whether s contains any C0 control character or DEL —
// notably CR/LF, the SMTP/MIME header-injection vectors.
func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// buildMimeEnvelope formats msg as a multipart/mixed MIME message
// suitable for SMTP DATA. Single text/plain body part + one
// application/* attachment per Attachment (base64-encoded). Headers
// follow RFC 5322 / RFC 2045. from/to are the validated bare mailboxes
// (validateMessage ran first), so they carry no CR/LF.
//
// Boundary is a random-ish string built from the current time —
// good enough for "doesn't collide with body content" at this scale,
// no need for a crypto/rand boundary.
func buildMimeEnvelope(from, to string, msg Message) []byte {
	var buf bytes.Buffer
	boundary := fmt.Sprintf("sm-mime-%d", time.Now().UnixNano())

	// RFC 5322 envelope headers.
	_, _ = fmt.Fprintf(&buf, "From: %s\r\n", from)
	_, _ = fmt.Fprintf(&buf, "To: %s\r\n", to)
	_, _ = fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	_, _ = fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Attachments) == 0 {
		// Plain text only — no multipart wrapper needed.
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		buf.WriteString(msg.Body)
		return buf.Bytes()
	}

	_, _ = fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)

	// Body part.
	_, _ = fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	buf.WriteString(msg.Body)
	if !strings.HasSuffix(msg.Body, "\r\n") {
		buf.WriteString("\r\n")
	}

	// Attachment parts. base64 with 76-column line wraps per RFC 2045.
	for _, att := range msg.Attachments {
		ct := att.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		// FormatMediaType emits RFC 2045/2231-correct parameters — quoting or
		// RFC 2231-encoding the filename as needed (notably non-ASCII names) —
		// rather than the previous naive %q, which can't represent those.
		ctHeader := mime.FormatMediaType(ct, map[string]string{"name": att.Filename})
		if ctHeader == "" {
			ctHeader = ct // defensive: ct is validated upstream, so unreachable
		}
		disp := mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename})
		_, _ = fmt.Fprintf(&buf, "--%s\r\n", boundary)
		_, _ = fmt.Fprintf(&buf, "Content-Type: %s\r\n", ctHeader)
		_, _ = fmt.Fprintf(&buf, "Content-Disposition: %s\r\n", disp)
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		writeBase64Wrapped(&buf, att.Body)
		buf.WriteString("\r\n")
	}

	_, _ = fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes()
}

// writeBase64Wrapped encodes data as base64 and writes it to w with
// CRLF after every 76 characters (RFC 2045 §6.8 requirement).
func writeBase64Wrapped(w *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	const lineLen = 76
	for i := 0; i < len(encoded); i += lineLen {
		end := i + lineLen
		if end > len(encoded) {
			end = len(encoded)
		}
		w.WriteString(encoded[i:end])
		w.WriteString("\r\n")
	}
}
