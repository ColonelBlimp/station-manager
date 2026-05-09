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
// "External services degrade, not crash" applies: an empty Host in
// config disables the mailer, Send returns ErrMailerDisabled, and
// callers fold that into a user-visible "email not configured" path
// rather than a 500. SMTP transport failures return wrapped errors
// the caller surfaces as a toast.
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
	"encoding/base64"
	stderrs "errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ServiceName is the DI bean ID for the mailer (used by internal/iocdi).
const ServiceName = "email"

// ErrMailerDisabled is returned by Send when the SMTP block is
// unconfigured (Host empty). Callers check with errors.Is and surface
// "email not configured" to the operator.
var ErrMailerDisabled = stderrs.New("mailer disabled (smtp.host is empty)")

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
	To          string
	Subject     string
	Body        string // plain text body
	Attachments []Attachment
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
	return s != nil && s.cfg.Host != ""
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
// ErrMailerDisabled if Host is unset, ErrInvalidMessage for malformed
// input, or a wrapped transport error otherwise.
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
func (s *Service) Send(ctx context.Context, msg Message) error {
	const op errors.Op = "email.Send"

	if s == nil || s.cfg.Host == "" {
		return ErrMailerDisabled
	}
	if msg.To == "" {
		return errors.New(op).WithErr(ErrInvalidMessage).WithMsg("recipient is required")
	}
	if msg.Subject == "" {
		return errors.New(op).WithErr(ErrInvalidMessage).WithMsg("subject is required")
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

	if err := client.Mail(s.cfg.From); err != nil {
		return errors.New(op).WithErr(err).WithMsgf("mail from %s", s.cfg.From)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return errors.New(op).WithErr(err).WithMsgf("rcpt to %s", msg.To)
	}

	w, err := client.Data()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("smtp data")
	}
	if _, err := w.Write(buildMimeEnvelope(s.cfg.From, msg)); err != nil {
		_ = w.Close()
		return errors.New(op).WithErr(err).WithMsg("write data")
	}
	if err := w.Close(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("close data")
	}

	if s.logger != nil {
		s.logger.InfoWith().
			Str("to", msg.To).
			Str("subject", msg.Subject).
			Int("attachments", len(msg.Attachments)).
			Msg("email sent")
	}
	return nil
}

// buildMimeEnvelope formats msg as a multipart/mixed MIME message
// suitable for SMTP DATA. Single text/plain body part + one
// application/* attachment per Attachment (base64-encoded). Headers
// follow RFC 5322 / RFC 2045.
//
// Boundary is a random-ish string built from the current time —
// good enough for "doesn't collide with body content" at this scale,
// no need for a crypto/rand boundary.
func buildMimeEnvelope(from string, msg Message) []byte {
	var buf bytes.Buffer
	boundary := fmt.Sprintf("sm-mime-%d", time.Now().UnixNano())

	// RFC 5322 envelope headers.
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", msg.To)
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Attachments) == 0 {
		// Plain text only — no multipart wrapper needed.
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		buf.WriteString(msg.Body)
		return buf.Bytes()
	}

	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)

	// Body part.
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
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
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: %s; name=%q\r\n", ct, att.Filename)
		fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=%q\r\n", att.Filename)
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		writeBase64Wrapped(&buf, att.Body)
		buf.WriteString("\r\n")
	}

	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
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
