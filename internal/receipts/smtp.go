package receipts

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPConfig holds credentials for the outbound relay used to forward mail.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // envelope + header From address
}

// buildForwardMessage wraps the original raw message as a message/rfc822
// attachment inside a multipart/mixed envelope. Attaching the original
// verbatim is the most reliable way to get inline content and attachments
// through to Mynt and Arkivplats intact.
func buildForwardMessage(from, to string, m Mail) []byte {
	header := strings.Join([]string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: Fwd: %s", m.Subject),
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="coo-agent-fwd"`,
		"",
		"--coo-agent-fwd",
		"Content-Type: text/plain; charset=utf-8",
		"",
		fmt.Sprintf("Vidarebefordrat av coo-agent. Originalavsändare: %s", m.From),
		"",
		"--coo-agent-fwd",
		"Content-Type: message/rfc822",
		"Content-Disposition: attachment",
		"",
	}, "\r\n")

	out := []byte(header + "\r\n")
	out = append(out, []byte(m.Body)...)
	out = append(out, []byte("\r\n--coo-agent-fwd--\r\n")...)
	return out
}

// deliver forwards a mail to its destination address via the SMTP relay.
// All current destination types ("smtp", "fortnox-receipts",
// "fortnox-invoices") are email forwards, so they share this path.
func (c *Collector) deliver(m Mail, dest *Destination) error {
	if dest.Address == "" {
		return fmt.Errorf("destination %q saknar adress", dest.Name)
	}
	if c.smtp.Host == "" {
		return fmt.Errorf("SMTP-relä är inte konfigurerat")
	}

	full := buildForwardMessage(c.smtp.From, dest.Address, m)

	addr := fmt.Sprintf("%s:%d", c.smtp.Host, c.smtp.Port)
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: c.smtp.Host})
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, c.smtp.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Auth(smtp.PlainAuth("", c.smtp.Username, c.smtp.Password, c.smtp.Host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(c.smtp.From); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(dest.Address); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(full); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	return w.Close()
}
