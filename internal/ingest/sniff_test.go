package ingest_test

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/ingest"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

func zipOf(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range names {
		f, err := w.Create(n)
		require.NoError(t, err)
		_, err = f.Write([]byte("<x/>"))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// Sniff takes bytes only. A file name or a browser's Content-Type is a claim by
// the sender and never reaches it, so a renamed file is decided by what it holds.
func TestSniff_DecidesByTheBytesAndNothingElse(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + "rest")
	jpeg := []byte("\xff\xd8\xff\xe0" + "rest")
	tests := []struct {
		name string
		data []byte
		want ingest.Kind
	}{
		{"png", png, ingest.KindImage},
		{"jpeg", jpeg, ingest.KindImage},
		{"pdf with a text layer", fixture(t, "invoice-text-layer.pdf"), ingest.KindPDF},
		{"pdf without text", fixture(t, "invoice-scan-no-text.pdf"), ingest.KindPDF},
		{"docx", fixture(t, "invoice.docx"), ingest.KindDOCX},
		{"ubl invoice", fixture(t, "ubl-invoice-mixed-vat.xml"), ingest.KindUBL},
		{"ubl credit note", fixture(t, "ubl-credit-note.xml"), ingest.KindUBL},
		{"empty", nil, ingest.KindUnknown},
		{"plain text", []byte("hello"), ingest.KindUnknown},
		{"html", []byte("<html><body>x</body></html>"), ingest.KindUnknown},
		{"xml that is not ubl", []byte(`<?xml version="1.0"?><note>hi</note>`), ingest.KindUnknown},
		{"xml with the right name in the wrong namespace", []byte(`<Invoice xmlns="urn:example:other"/>`), ingest.KindUnknown},
		{"zip that is not a docx", zipOf(t, "xl/workbook.xml"), ingest.KindUnknown},
		{"zip with a docx part name but no content types", zipOf(t, "word/document.xml"), ingest.KindDOCX},
		{"a pdf header after leading junk", append([]byte("junk"), fixture(t, "invoice-text-layer.pdf")...), ingest.KindUnknown},
		{"a truncated zip", fixture(t, "invoice.docx")[:200], ingest.KindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ingest.Sniff(tt.data))
		})
	}
}

func TestKind_NamesTheFormatForAPageAndALog(t *testing.T) {
	assert.Equal(t, "image", ingest.KindImage.String())
	assert.Equal(t, "PDF", ingest.KindPDF.String())
	assert.Equal(t, "Word document", ingest.KindDOCX.String())
	assert.Equal(t, "e-invoice XML", ingest.KindUBL.String())
	assert.Equal(t, "unsupported", ingest.KindUnknown.String())
}
