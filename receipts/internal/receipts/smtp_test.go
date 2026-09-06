package receipts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildForwardMessage(t *testing.T) {
	m := Mail{
		MessageID: "<abc@example.com>",
		From:      "leverantor@example.com",
		Subject:   "Kvitto",
		Body:      "Original-Subject: Kvitto\r\n\r\nkvittotext",
	}

	out := string(buildForwardMessage("coo@d-ma.be", "kvitto+x@mynt.se", m))

	assert.Contains(t, out, "From: coo@d-ma.be")
	assert.Contains(t, out, "To: kvitto+x@mynt.se")
	assert.Contains(t, out, "Subject: Fwd: Kvitto")
	assert.Contains(t, out, "Content-Type: multipart/mixed")
	assert.Contains(t, out, "message/rfc822", "original is attached as a message")
	assert.Contains(t, out, m.Body, "original body must be embedded intact")
}
