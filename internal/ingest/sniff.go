// Package ingest turns an uploaded document of any supported format into the
// invoice fields the rest of the pipeline works on (slice #160 of bet #142).
//
// Structured sources are read deterministically first (e-invoice XML), text
// documents (PDF with a text layer, DOCX) are converted to text and read by a
// local text model, and only images go to the vision model. A document the
// pipeline cannot read is refused with a reason; it is never guessed at.
//
// Everything here is read-only and held in memory: uploads are not written to
// disk by this package, and the converter subprocess receives them on stdin.
package ingest

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
)

// Kind is what a document is, decided from its bytes.
type Kind int

const (
	KindUnknown Kind = iota
	KindImage
	KindPDF
	KindDOCX
	KindUBL
)

func (k Kind) String() string {
	switch k {
	case KindImage:
		return "image"
	case KindPDF:
		return "PDF"
	case KindDOCX:
		return "Word document"
	case KindUBL:
		return "e-invoice XML"
	case KindUnknown:
		return "unsupported"
	}
	return "unsupported"
}

// ublNamespaces are the document element namespaces of a UBL invoice and credit note.
var ublNamespaces = map[string]bool{
	"urn:oasis:names:specification:ubl:schema:xsd:Invoice-2":    true,
	"urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2": true,
}

// sniffXMLWindow bounds how much of a file is read to find the root element.
const sniffXMLWindow = 64 << 10

// Sniff decides what a document is from its bytes alone. The file name and the
// Content-Type are claims by the sender and are not inputs: a file that is
// renamed, or sent with the wrong type, is read as what it is.
func Sniff(data []byte) Kind {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")), bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return KindImage
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return KindPDF
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		if hasZipEntry(data, "word/document.xml") {
			return KindDOCX
		}
		return KindUnknown
	}
	if isUBL(data) {
		return KindUBL
	}
	return KindUnknown
}

func hasZipEntry(data []byte, name string) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

// isUBL reports whether the first element is a UBL Invoice or CreditNote. Only
// a bounded window is parsed, and Go's decoder does not expand external or
// custom entities, so a hostile file cannot make this read more than that.
func isUBL(data []byte) bool {
	if len(data) > sniffXMLWindow {
		data = data[:sniffXMLWindow]
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return false
			}
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return (se.Name.Local == "Invoice" || se.Name.Local == "CreditNote") && ublNamespaces[se.Name.Space]
		}
	}
}
