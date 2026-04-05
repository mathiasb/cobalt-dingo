// Package mail provides IMAP-based inbox monitoring.
// It works with any IMAP-compliant provider (Google Workspace, Fastmail,
// Proton Mail Bridge, etc.) – no vendor-specific APIs required.
package mail

import (
	"context"
	"fmt"
	"time"
)

// Config holds IMAP connection settings.
type Config struct {
	Host     string
	Port     int    // typically 993 (TLS)
	Username string
	Password string // or app-specific password
}

// Message is a simplified representation of an incoming e-mail.
type Message struct {
	Subject    string
	From       string
	Date       time.Time
	HasAttachment bool
	Attachments   []Attachment
}

// Attachment represents an e-mail attachment.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// KnownSender is a recognised supplier or institution whose mail is flagged.
type KnownSender struct {
	Name    string
	Domains []string // e.g. ["telenor.se", "telenor.com"]
}

// DefaultSenders lists all known senders for this business.
var DefaultSenders = []KnownSender{
	{Name: "Telenor", Domains: []string{"telenor.se", "telenor.com"}},
	{Name: "Telia", Domains: []string{"telia.se", "telia.com"}},
	{Name: "Bahnhof", Domains: []string{"bahnhof.se", "bahnhof.net"}},
	{Name: "BMW Financial Services", Domains: []string{"bmw-financialservices.com", "bmwgroup.com"}},
	{Name: "Söderberg & Partners", Domains: []string{"soderbergpartners.se"}},
	{Name: "Fortnox", Domains: []string{"fortnox.se"}},
	{Name: "Skatteverket", Domains: []string{"skatteverket.se"}},
	{Name: "Kivra", Domains: []string{"kivra.com", "kivra.se"}},
}

// Client connects to an IMAP mailbox and monitors for relevant messages.
type Client struct {
	cfg     Config
	senders []KnownSender
}

// New creates a new IMAP client.
func New(cfg Config, senders []KnownSender) (*Client, error) {
	if cfg.Host == "" || cfg.Username == "" {
		return nil, fmt.Errorf("mail: Host and Username are required")
	}
	if len(senders) == 0 {
		senders = DefaultSenders
	}
	return &Client{cfg: cfg, senders: senders}, nil
}

// CheckInbox connects to the IMAP server and returns messages from known senders
// received since the given date.
// TODO: implement using emersion/go-imap/v2.
func (c *Client) CheckInbox(_ context.Context, since time.Time) ([]Message, error) {
	_ = since
	return nil, fmt.Errorf("mail: IMAP integration not yet implemented")
}

// SaveAttachments writes PDF attachments from a message to the given directory.
// TODO: implement file writing with deduplication.
func (c *Client) SaveAttachments(_ Message, _ string) error {
	return fmt.Errorf("mail: SaveAttachments not yet implemented")
}
