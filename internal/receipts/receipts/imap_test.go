package receipts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawMultipartWithAttachment builds a minimal MIME message carrying one text
// part and one base64-encoded PDF attachment. CRLF endings are required by
// RFC 2822, so \n is normalised to \r\n.
func rawMultipartWithAttachment() []byte {
	msg := strings.Join([]string{
		"From: leverantor@example.com",
		"To: me@example.com",
		"Subject: Kvitto",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="BOUND"`,
		"",
		"--BOUND",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Tack for ditt kop.",
		"--BOUND",
		`Content-Type: application/pdf; name="receipt.pdf"`,
		`Content-Disposition: attachment; filename="receipt.pdf"`,
		"Content-Transfer-Encoding: base64",
		"",
		"JVBERi0xLjQK", // "%PDF-1.4\n"
		"--BOUND--",
		"",
	}, "\n")
	return []byte(strings.ReplaceAll(msg, "\n", "\r\n"))
}

func TestExtractAttachments(t *testing.T) {
	atts, err := extractAttachments(rawMultipartWithAttachment())
	require.NoError(t, err)
	require.Len(t, atts, 1, "should find the attachment, not the text part")

	a := atts[0]
	assert.Equal(t, "receipt.pdf", a.Filename)
	assert.Contains(t, a.ContentType, "application/pdf")
	assert.Equal(t, "%PDF-1.4\n", string(a.Data), "base64 body must be decoded")
}

func TestExtractAttachments_noAttachment(t *testing.T) {
	plain := strings.ReplaceAll(strings.Join([]string{
		"From: a@example.com",
		"Subject: hej",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"ingen bilaga har",
		"",
	}, "\n"), "\n", "\r\n")

	atts, err := extractAttachments([]byte(plain))
	require.NoError(t, err)
	assert.Empty(t, atts)
}
