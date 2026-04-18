package payments

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// MailerConfig bundles SMTP credentials + envelope addresses for the
// daily cash-receipt email. All fields required.
type MailerConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool // STARTTLS on the standard submission port
}

// Mailer sends a single email with one attachment. Scope is deliberately
// narrow — this is not a general-purpose SMTP client, it exists for the
// daily report. Uses stdlib net/smtp (PLAIN auth + STARTTLS) to avoid
// pulling in a heavier dependency for one outbound message per day.
type Mailer struct {
	cfg MailerConfig
}

// NewMailer constructs a production mailer.
func NewMailer(cfg MailerConfig) *Mailer {
	return &Mailer{cfg: cfg}
}

// Attachment is a single file attached to an email.
type Attachment struct {
	Filename    string
	ContentType string
	Body        []byte
}

// Send transmits a multipart email with the given recipients, subject,
// plaintext body, and optional attachment. Returns on success or the
// first error from dial/auth/transmit. Honours ctx cancellation via a
// dialer deadline — the SMTP exchange itself is not interruptible once
// started.
func (m *Mailer) Send(ctx context.Context, to []string, subject, textBody string, att *Attachment) error {
	if len(to) == 0 {
		return errors.New("mailer: no recipients")
	}
	if m.cfg.Host == "" || m.cfg.Port == 0 || m.cfg.From == "" {
		return errors.New("mailer: incomplete config")
	}

	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)

	// Honour context deadline on dial. After that, SMTP is not
	// interruptible by ctx — the built-in timeouts on the conn do the
	// rest.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(60 * time.Second)
	}
	d := net.Dialer{Deadline: deadline}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dialling smtp: %w", err)
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	if m.cfg.UseTLS {
		tlsCfg := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if m.cfg.Username != "" {
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt to %q: %w", rcpt, err)
		}
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := wc.Write(buildMIMEBody(m.cfg.From, to, subject, textBody, att)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("writing body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("closing body: %w", err)
	}
	return client.Quit()
}

// buildMIMEBody constructs a multipart/mixed MIME message with a
// text/plain body and one base64-encoded attachment. Kept minimal —
// this is the daily cash-receipt email, not a templating engine.
func buildMIMEBody(from string, to []string, subject, textBody string, att *Attachment) []byte {
	var buf bytes.Buffer
	boundary := fmt.Sprintf("----=_Part_%d", time.Now().UnixNano())

	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")

	if att == nil {
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
		buf.WriteString(textBody)
		return buf.Bytes()
	}

	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary)

	// Text part.
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
	fmt.Fprintf(&buf, "Content-Transfer-Encoding: 7bit\r\n\r\n")
	buf.WriteString(textBody)
	buf.WriteString("\r\n")

	// Attachment part.
	ct := att.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: %s; name=%q\r\n", ct, att.Filename)
	fmt.Fprintf(&buf, "Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=%q\r\n\r\n", att.Filename)

	encoded := base64.StdEncoding.EncodeToString(att.Body)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}

	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes()
}
