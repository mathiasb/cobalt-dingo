package ingest_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/ingest"
)

// script writes an executable shell script and returns the command that runs it.
func script(t *testing.T, body string) []string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "conv.sh")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700))
	return []string{p}
}

func conv(t *testing.T, body string) *ingest.Markitdown {
	t.Helper()
	return &ingest.Markitdown{Command: script(t, body), Timeout: 5 * time.Second, MaxOutput: 1 << 16}
}

func TestMarkitdown_PassesTheDocumentOnStdinAndReturnsStdout(t *testing.T) {
	c := conv(t, `cat`)

	out, err := c.Convert(context.Background(), ingest.KindPDF, []byte("Faktura 42\nTotal 100 SEK"))

	require.NoError(t, err)
	assert.Equal(t, "Faktura 42\nTotal 100 SEK", out)
}

func TestMarkitdown_TellsTheConverterTheFormatByExtensionNotByName(t *testing.T) {
	c := conv(t, `echo "$@"`)

	pdf, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))
	require.NoError(t, err)
	docx, err := c.Convert(context.Background(), ingest.KindDOCX, []byte("x"))
	require.NoError(t, err)

	assert.Equal(t, "-x .pdf", strings.TrimSpace(pdf))
	assert.Equal(t, "-x .docx", strings.TrimSpace(docx))
}

func TestMarkitdown_RefusesAKindItDoesNotConvert(t *testing.T) {
	c := conv(t, `cat`)

	for _, k := range []ingest.Kind{ingest.KindImage, ingest.KindUBL, ingest.KindUnknown} {
		_, err := c.Convert(context.Background(), k, []byte("x"))
		require.Error(t, err, k.String())
	}
}

// The demo process holds its own login and the 1Password session in its
// environment. A converter that runs untrusted documents must not see them.
func TestMarkitdown_ChildDoesNotInheritTheCallersEnvironment(t *testing.T) {
	t.Setenv("DEMO_PASS", "sentinel-password-value")
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "sentinel-op-token")
	t.Setenv("DATABASE_URL", "postgres://sentinel")
	c := conv(t, `env`)

	out, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.NoError(t, err)
	assert.NotContains(t, out, "sentinel")
	assert.NotContains(t, out, "DEMO_PASS")
	assert.Contains(t, out, "PATH=", "the converter still finds its tools")
}

func TestMarkitdown_KillsAConverterThatRunsPastTheTimeout(t *testing.T) {
	c := conv(t, `sleep 30`)
	c.Timeout = 300 * time.Millisecond

	start := time.Now()
	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "time")
	assert.Less(t, time.Since(start), 5*time.Second)
}

// uvx starts python, which starts workers. Killing only the first process would
// leave the real converter running with the document in memory.
func TestMarkitdown_TimeoutKillsTheWholeProcessGroupNotJustTheFirstProcess(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	c := conv(t, `sleep 30 &
echo $! > `+pidFile+`
wait`)
	c.Timeout = 400 * time.Millisecond

	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))
	require.Error(t, err)

	raw, rerr := os.ReadFile(pidFile)
	require.NoError(t, rerr)
	pid, perr := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, perr)
	assert.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil }, 3*time.Second, 50*time.Millisecond,
		"the grandchild process is still running after the timeout")
}

func TestMarkitdown_StopsAConverterThatFloodsStdout(t *testing.T) {
	c := conv(t, `yes "FLOOD"`)
	c.MaxOutput = 4096

	start := time.Now()
	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger")
	assert.Less(t, time.Since(start), 5*time.Second, "the flood was cut short, not waited out")
}

func TestMarkitdown_OutputExactlyAtTheCapIsAccepted(t *testing.T) {
	c := conv(t, `printf 'abcd'`)
	c.MaxOutput = 4

	out, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.NoError(t, err)
	assert.Equal(t, "abcd", out)
}

func TestMarkitdown_ExitCodeFailureNamesNoDocumentText(t *testing.T) {
	c := conv(t, `echo "Traceback: invoice for SECRET-CUSTOMER total 999" >&2; exit 3`)

	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET-CUSTOMER", "the converter's stderr can echo the document and is not returned")
}

// A converter that exits 0 with nothing is a scanned PDF or an empty template,
// and the pipeline must say "no text", never "no invoice".
func TestMarkitdown_EmptyOrWhitespaceOutputIsErrNoText(t *testing.T) {
	for _, body := range []string{`true`, `printf '  \n\n  '`} {
		_, err := conv(t, body).Convert(context.Background(), ingest.KindPDF, []byte("x"))
		require.ErrorIs(t, err, ingest.ErrNoText, body)
	}
}

func TestMarkitdown_ACancelledContextStopsTheConverter(t *testing.T) {
	c := conv(t, `sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()

	start := time.Now()
	_, err := c.Convert(ctx, ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestMarkitdown_ACommandThatDoesNotExistIsAnErrorNotAPanic(t *testing.T) {
	c := &ingest.Markitdown{Command: []string{"/nonexistent/markitdown"}, Timeout: time.Second, MaxOutput: 100}

	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
}

// The real converter, on the real fixtures. Skipped where uvx is not installed.
func TestMarkitdown_RealConverterReadsATextPDFAndADocxAndFindsNoTextInAScan(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := exec.LookPath("uvx"); err != nil {
		t.Skip("uvx not installed")
	}
	c := ingest.NewMarkitdown()
	c.Timeout = 120 * time.Second

	pdf, err := c.Convert(context.Background(), ingest.KindPDF, fixture(t, "invoice-text-layer.pdf"))
	require.NoError(t, err)
	assert.Contains(t, pdf, "Nordkloud AB")
	assert.Contains(t, pdf, "Att betala 4 349,08 SEK", "the total as printed, with its thousands separator")

	docx, err := c.Convert(context.Background(), ingest.KindDOCX, fixture(t, "invoice.docx"))
	require.NoError(t, err)
	assert.Contains(t, docx, "Norrlands Bilservice AB")

	_, err = c.Convert(context.Background(), ingest.KindPDF, fixture(t, "invoice-scan-no-text.pdf"))
	require.ErrorIs(t, err, ingest.ErrNoText)
}
