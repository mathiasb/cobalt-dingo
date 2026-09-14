package fortnox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrInboxScopeMissing is returned when Fortnox refuses the inbox with 403.
//
// It is its own error because the remedy is expensive and non-obvious: scopes
// belong to the integration, not the connection, so granting `inbox` means
// re-authorizing every company already connected through the app. A bare 403
// would send someone looking at the token instead.
var ErrInboxScopeMissing = errors.New("fortnox refused the inbox: the integration is probably missing the `inbox` scope")

// InboxFile is a document sitting in the Fortnox Arkivplats.
//
// Fields are from the vendored OpenAPI spec, GET /3/inbox. Two absences matter
// more than anything present:
//
//   - There is NO timestamp. "How long has this been sitting unbooked" cannot
//     be answered from this endpoint, and a design assuming an upload date is
//     built on nothing.
//   - There is NO processed flag. Whether a file has been booked is answered by
//     /3/supplierinvoicefileconnections, joining on ArchiveFileId.
type InboxFile struct {
	URL           string `json:"@url"`
	ArchiveFileID string `json:"ArchiveFileId"`
	Comments      string `json:"Comments"`
	ID            string `json:"Id"`
	Name          string `json:"Name"`
	Path          string `json:"Path"`
	Size          int    `json:"Size"`
}

// InboxFolder is a folder in the Arkivplats, including the root.
//
// Email is the arkivplats address mail is forwarded to, and it is the reason
// this type is worth having: the receipt collector currently hardcodes those
// addresses, which is how a stale `inbox.lev.<org>` kept accepting mail that
// never arrived (#80). Fortnox knows the right answer.
type InboxFolder struct {
	URL     string        `json:"@url"`
	Email   string        `json:"Email"`
	ID      string        `json:"Id"`
	Name    string        `json:"Name"`
	Files   []InboxFile   `json:"Files"`
	Folders []InboxFolder `json:"Folders"`
}

type inboxResponse struct {
	Folder InboxFolder `json:"Folder"`
}

// GetInbox reads the Arkivplats root folder.
//
// Calls GET /3/inbox. Requires the `inbox` scope, which is separate from
// `archive` in Fortnox's scope list.
func (c *Client) GetInbox() (InboxFolder, error) {
	raw, err := c.Get(c.baseURL + "/3/inbox")
	if err != nil {
		// The scope failure is the one worth naming. Matching on the status
		// text rather than a code because Get wraps the status into the error;
		// a wrong guess here degrades to the generic error, never to silence.
		if strings.Contains(err.Error(), http.StatusText(http.StatusForbidden)) ||
			strings.Contains(err.Error(), "403") {
			return InboxFolder{}, fmt.Errorf("%w: %v", ErrInboxScopeMissing, err)
		}
		return InboxFolder{}, fmt.Errorf("get inbox: %w", err)
	}
	var envelope inboxResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return InboxFolder{}, fmt.Errorf("decode inbox: %w", err)
	}
	return envelope.Folder, nil
}
