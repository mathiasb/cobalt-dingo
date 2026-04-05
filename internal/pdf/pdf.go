// Package pdf extracts invoice data from PDF files dropped into the
// watched inbox directory (~/.coo-agent/inbox/).
package pdf

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Invoice holds data extracted from an invoice PDF.
type Invoice struct {
	Supplier  string
	Amount    float64   // total incl. VAT
	VATAmount float64   // VAT portion
	NetAmount float64   // excl. VAT
	Date      time.Time
	Period    string // e.g. "mars 2025"
	Currency  string // defaults to SEK
	FilePath  string // source PDF path
}

// Parser extracts invoice data from PDF files.
type Parser struct {
	inboxDir string
}

// New creates a Parser that watches inboxDir for PDF files.
func New(inboxDir string) (*Parser, error) {
	if err := os.MkdirAll(inboxDir, 0o700); err != nil {
		return nil, fmt.Errorf("pdf: create inbox dir: %w", err)
	}
	return &Parser{inboxDir: inboxDir}, nil
}

// ParseFile extracts invoice data from a single PDF file.
// TODO: implement using pdfcpu or unipdf.
func (p *Parser) ParseFile(path string) (*Invoice, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("pdf: file not found: %w", err)
	}
	return nil, fmt.Errorf("pdf: parsing not yet implemented for %s", filepath.Base(path))
}

// ScanInbox returns all unprocessed PDF files in the inbox directory.
func (p *Parser) ScanInbox() ([]string, error) {
	entries, err := os.ReadDir(p.inboxDir)
	if err != nil {
		return nil, fmt.Errorf("pdf: read inbox dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".pdf" {
			files = append(files, filepath.Join(p.inboxDir, e.Name()))
		}
	}
	return files, nil
}
