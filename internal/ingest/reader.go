package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
)

var (
	// ErrUnsupported is a file that is none of the formats read here.
	ErrUnsupported = errors.New("unsupported file: send a PNG or JPEG image, a PDF with a text layer, a Word (.docx) document, or e-invoice XML (Peppol/UBL)")

	// ErrRefused marks a document refused on this package's own rules: malformed,
	// hostile, too large or empty. Its text can quote the document, so a caller
	// shows a fixed message and logs the detail.
	ErrRefused = errors.New("the file was refused")

	// ErrScanned is a PDF with no text layer. Reading it needs the vision path,
	// which this reader does not offer for PDFs.
	ErrScanned = errors.New("this PDF is a scan with no text layer; export it as a PNG or JPEG, or send the original e-invoice")
)

const (
	// MaxDocxExpanded bounds the total declared size of what a DOCX unpacks to.
	// A real invoice document is tens of kilobytes; a few megabytes of images at
	// most.
	MaxDocxExpanded = 16 << 20

	// MaxDocxPart bounds one XML part of a DOCX. It is text: a document.xml of a
	// few megabytes is not an invoice, and 31 MiB of it took the converter 1.5 GB
	// (review of #162). Images and other media are not limited by this.
	MaxDocxPart = 4 << 20

	// MaxDocxEntries bounds the number of parts in a DOCX.
	MaxDocxEntries = 1000
)

// VisionReader reads an invoice image.
type VisionReader interface {
	Extract(ctx context.Context, image []byte) (extract.Invoice, error)
}

// TextReader reads an invoice from converted text.
type TextReader interface {
	ExtractText(ctx context.Context, text string) (extract.Invoice, error)
}

// Reader reads any supported document. *extract.Client is both the Vision and
// the Text reader. It implements process.Extractor, so the rest of the pipeline
// does not change.
type Reader struct {
	Vision  VisionReader
	Text    TextReader
	Convert Converter
}

// Extract decides what the document is from its bytes and reads it the way that
// kind is read: images by the vision model, e-invoice XML by a parser, PDF and
// DOCX through the converter and the text model. A document that cannot be read
// is refused with a reason that names the format, never with a guess.
func (r *Reader) Extract(ctx context.Context, doc []byte) (extract.Invoice, error) {
	kind := Sniff(doc)
	switch kind {
	case KindImage:
		if r.Vision == nil {
			return extract.Invoice{}, errors.New("reading images is not configured")
		}
		return r.Vision.Extract(ctx, doc)
	case KindUBL:
		inv, err := ParseUBL(doc)
		if err != nil {
			return extract.Invoice{}, fmt.Errorf("%w: %v", ErrRefused, err)
		}
		return inv, nil
	case KindPDF, KindDOCX:
		return r.fromText(ctx, kind, doc)
	case KindUnknown:
		return extract.Invoice{}, ErrUnsupported
	}
	return extract.Invoice{}, ErrUnsupported
}

func (r *Reader) fromText(ctx context.Context, kind Kind, doc []byte) (extract.Invoice, error) {
	if r.Convert == nil || r.Text == nil {
		return extract.Invoice{}, fmt.Errorf("reading a %s is not configured", kind)
	}
	if kind == KindDOCX {
		if err := checkDocxSize(doc); err != nil {
			return extract.Invoice{}, fmt.Errorf("%w: %v", ErrRefused, err)
		}
	}
	text, err := r.Convert.Convert(ctx, kind, doc)
	switch {
	case errors.Is(err, ErrNoText) && kind == KindPDF:
		return extract.Invoice{}, ErrScanned
	case errors.Is(err, ErrNoText):
		return extract.Invoice{}, fmt.Errorf("%w: the %s has no text to read", ErrRefused, kind)
	case err != nil:
		return extract.Invoice{}, fmt.Errorf("convert %s: %w", kind, err)
	}
	inv, err := r.Text.ExtractText(ctx, text)
	if errors.Is(err, extract.ErrTextTooLong) {
		return extract.Invoice{}, fmt.Errorf("%w: %v", ErrRefused, err)
	}
	return inv, err
}

// checkDocxSize refuses a zip that is small as sent and large when read. The
// sizes are those the archive declares; the converter's own timeout and output
// cap are what bound a file whose header lies.
func checkDocxSize(doc []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(doc), int64(len(doc)))
	if err != nil {
		return fmt.Errorf("the document is not a readable Word file: %w", err)
	}
	if len(zr.File) > MaxDocxEntries {
		return fmt.Errorf("the document has more than %d entries", MaxDocxEntries)
	}
	var total uint64
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".xml") && f.UncompressedSize64 > MaxDocxPart {
			return fmt.Errorf("a part of the document is larger than %d bytes", MaxDocxPart)
		}
		total += f.UncompressedSize64
		if total > MaxDocxExpanded {
			return fmt.Errorf("the document expands to more than %d bytes", MaxDocxExpanded)
		}
	}
	return nil
}
