package ingest_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/ingest"
	"github.com/mathiasb/cobalt-dingo/internal/process"
)

var _ process.Extractor = (*ingest.Reader)(nil)

type fakeVision struct {
	calls int
	got   []byte
	inv   extract.Invoice
	err   error
}

func (f *fakeVision) Extract(_ context.Context, image []byte) (extract.Invoice, error) {
	f.calls++
	f.got = image
	return f.inv, f.err
}

type fakeText struct {
	calls int
	got   string
	inv   extract.Invoice
	err   error
}

func (f *fakeText) ExtractText(_ context.Context, text string) (extract.Invoice, error) {
	f.calls++
	f.got = text
	return f.inv, f.err
}

type fakeConv struct {
	calls int
	kind  ingest.Kind
	text  string
	err   error
}

func (f *fakeConv) Convert(_ context.Context, k ingest.Kind, _ []byte) (string, error) {
	f.calls++
	f.kind = k
	return f.text, f.err
}

func newReader() (*ingest.Reader, *fakeVision, *fakeText, *fakeConv) {
	v := &fakeVision{inv: extract.Invoice{Supplier: "from vision"}}
	tx := &fakeText{inv: extract.Invoice{Supplier: "from text"}}
	c := &fakeConv{text: "converted text"}
	return &ingest.Reader{Vision: v, Text: tx, Convert: c}, v, tx, c
}

func TestReader_AnImageGoesToTheVisionModelOnly(t *testing.T) {
	r, v, tx, c := newReader()
	png := []byte("\x89PNG\r\n\x1a\nrest")

	inv, err := r.Extract(context.Background(), png)

	require.NoError(t, err)
	assert.Equal(t, "from vision", inv.Supplier)
	assert.Equal(t, png, v.got)
	assert.Zero(t, tx.calls+c.calls)
}

func TestReader_APDFIsConvertedToTextAndReadByTheTextModel(t *testing.T) {
	r, v, tx, c := newReader()

	inv, err := r.Extract(context.Background(), fixture(t, "invoice-text-layer.pdf"))

	require.NoError(t, err)
	assert.Equal(t, "from text", inv.Supplier)
	assert.Equal(t, ingest.KindPDF, c.kind)
	assert.Equal(t, "converted text", tx.got)
	assert.Zero(t, v.calls)
}

func TestReader_ADocxIsConvertedToTextAndReadByTheTextModel(t *testing.T) {
	r, _, tx, c := newReader()

	inv, err := r.Extract(context.Background(), fixture(t, "invoice.docx"))

	require.NoError(t, err)
	assert.Equal(t, "from text", inv.Supplier)
	assert.Equal(t, ingest.KindDOCX, c.kind)
	assert.Equal(t, 1, tx.calls)
}

func TestReader_AnEInvoiceIsParsedWithoutAnyModelOrConverter(t *testing.T) {
	r, v, tx, c := newReader()

	inv, err := r.Extract(context.Background(), fixture(t, "ubl-invoice-mixed-vat.xml"))

	require.NoError(t, err)
	assert.Equal(t, "Östersunds Hotell AB", inv.Supplier)
	assert.Zero(t, v.calls+tx.calls+c.calls, "structured input needs no model")
}

func TestReader_AScannedPDFIsRefusedAsScannedNotGuessedAt(t *testing.T) {
	r, v, tx, c := newReader()
	c.err = ingest.ErrNoText

	_, err := r.Extract(context.Background(), fixture(t, "invoice-scan-no-text.pdf"))

	require.ErrorIs(t, err, ingest.ErrScanned)
	assert.Contains(t, err.Error(), "scan")
	assert.Zero(t, v.calls+tx.calls, "no model reads a document that has no text")
}

func TestReader_ADocxWithNoTextIsNamedAsEmptyNotAsAScan(t *testing.T) {
	r, _, tx, c := newReader()
	c.err = ingest.ErrNoText

	_, err := r.Extract(context.Background(), fixture(t, "invoice.docx"))

	require.Error(t, err)
	assert.NotErrorIs(t, err, ingest.ErrScanned)
	assert.Contains(t, err.Error(), "no text")
	assert.Zero(t, tx.calls)
}

func TestReader_AnUnsupportedFileIsRefusedAndNothingIsRun(t *testing.T) {
	r, v, tx, c := newReader()

	_, err := r.Extract(context.Background(), []byte("MZ\x90\x00 an executable"))

	require.ErrorIs(t, err, ingest.ErrUnsupported)
	assert.Contains(t, err.Error(), "PDF")
	assert.Zero(t, v.calls+tx.calls+c.calls)
}

// A zip that is small on the wire and enormous when read is refused before the
// converter, and so before python, ever sees it.
func TestReader_ADocxThatExpandsPastTheLimitIsRefusedBeforeConversion(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("word/document.xml")
	require.NoError(t, err)
	_, err = f.Write(make([]byte, ingest.MaxDocxExpanded+1))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.Less(t, buf.Len(), 200<<10, "the bomb is small as sent")
	r, _, tx, c := newReader()

	_, err = r.Extract(context.Background(), buf.Bytes())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expands")
	assert.Zero(t, c.calls+tx.calls)
}

func TestReader_ADocxWithTooManyEntriesIsRefusedBeforeConversion(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	_, err := w.Create("word/document.xml")
	require.NoError(t, err)
	for i := 0; i < ingest.MaxDocxEntries+1; i++ {
		_, err = w.Create("x/" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	r, _, _, c := newReader()

	_, err = r.Extract(context.Background(), buf.Bytes())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "entries")
	assert.Zero(t, c.calls)
}

func TestReader_AConverterFailureIsReturnedWithoutTheDocumentText(t *testing.T) {
	r, _, tx, c := newReader()
	c.err = errors.New("converter failed (exit status 3)")

	_, err := r.Extract(context.Background(), fixture(t, "invoice-text-layer.pdf"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "converter failed")
	assert.Zero(t, tx.calls)
}

func TestReader_ATextModelFailureIsReturned(t *testing.T) {
	r, _, tx, _ := newReader()
	tx.err = errors.New("field total is missing")

	_, err := r.Extract(context.Background(), fixture(t, "invoice-text-layer.pdf"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "field total is missing")
}

func TestReader_AReaderWithoutAPartRefusesThatKindInsteadOfPanicking(t *testing.T) {
	r := &ingest.Reader{}

	for _, doc := range [][]byte{[]byte("\x89PNG\r\n\x1a\nrest"), fixture(t, "invoice-text-layer.pdf")} {
		_, err := r.Extract(context.Background(), doc)
		require.Error(t, err)
	}
}

// A document the reader refuses on its own rules (malformed, hostile, too large,
// empty) is one error class, so a caller can show a fixed message and log the detail.
func TestReader_EveryRefusalOnItsOwnRulesIsErrRefused(t *testing.T) {
	var bomb bytes.Buffer
	w := zip.NewWriter(&bomb)
	f, err := w.Create("word/document.xml")
	require.NoError(t, err)
	_, err = f.Write(make([]byte, ingest.MaxDocxExpanded+1))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	hostile := edit(t, fixture(t, "ubl-invoice-mixed-vat.xml"), `<Invoice `, `<!DOCTYPE Invoice [<!ENTITY a "b">]><Invoice `)
	r, _, _, c := newReader()
	c.err = ingest.ErrNoText

	for name, doc := range map[string][]byte{
		"a malformed e-invoice": hostile,
		"a docx that expands":   bomb.Bytes(),
		"a docx with no text":   fixture(t, "invoice.docx"),
	} {
		_, err := r.Extract(context.Background(), doc)
		assert.ErrorIs(t, err, ingest.ErrRefused, name)
	}
}
