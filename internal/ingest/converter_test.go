package ingest_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
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
	c.Timeout = 3 * time.Second

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

// Passing the cap must stop the child at once, even when it would not exit by
// itself. Without the kill the call waits for the child's own end.
func TestMarkitdown_KillsAConverterAtOnceWhenOutputPassesTheCapEvenIfItWouldNotExit(t *testing.T) {
	c := conv(t, `head -c 20000 /dev/zero | tr '\0' 'x'; sleep 30`)
	c.MaxOutput = 4096
	c.Timeout = 25 * time.Second

	start := time.Now()
	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger")
	assert.Less(t, time.Since(start), 8*time.Second, "the child was waited out instead of killed")
}

// Review of #162: a 447 KB PDF took 3.1 GB of memory in the real converter. Wall
// time and output size were the only bounds. A memory limit is set on the child.
func TestMarkitdown_ChildCannotAddressMoreMemoryThanTheLimit(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	grab := func(mib int) string {
		return `exec python3 -c "x = bytearray(` + strconv.Itoa(mib) + ` * 1024 * 1024); print('allocated')"`
	}
	c := conv(t, grab(16))
	c.MaxMemory = 512 << 20

	out, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))
	require.NoError(t, err, "a small allocation must still work, or this test proves nothing")
	assert.Contains(t, out, "allocated")

	c = conv(t, grab(2048))
	c.MaxMemory = 512 << 20
	start := time.Now()
	_, err = c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err, "2 GiB must not be allocatable under a 512 MiB limit")
	assert.Less(t, time.Since(start), 10*time.Second)
}

func TestMarkitdown_ChildIsStoppedWhenItUsesMoreCPUTimeThanTheLimit(t *testing.T) {
	c := conv(t, `while :; do :; done`)
	c.MaxCPU = time.Second
	c.Timeout = 20 * time.Second

	start := time.Now()
	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "timed out", "the CPU limit stopped it, not the wall clock")
	assert.Less(t, time.Since(start), 10*time.Second)
}

// A limit too small for the child to start means the child does not run: the
// limits are applied before the converter is exec'd, not after.
func TestMarkitdown_ALimitTooSmallToStartTheChildMeansTheChildNeverRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	c := conv(t, `touch `+marker+`; cat`)
	c.MaxMemory = 1

	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the converter ran without its memory limit")
}

// Converter failures that a hostile or broken document causes are the document's
// fault; a converter that cannot start at all is ours.
func TestMarkitdown_ADocumentThatKillsTheConverterIsRefusedButAMissingConverterIsNot(t *testing.T) {
	short := conv(t, `sleep 30`)
	short.Timeout = 300 * time.Millisecond
	flood := conv(t, `yes`)
	flood.MaxOutput = 100
	for name, c := range map[string]*ingest.Markitdown{"exit code": conv(t, `exit 3`), "timeout": short, "output flood": flood} {
		_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))
		require.ErrorIs(t, err, ingest.ErrRefused, name)
	}

	missing := &ingest.Markitdown{Command: []string{"/nonexistent/markitdown"}, Timeout: time.Second, MaxOutput: 100}
	_, err := missing.Convert(context.Background(), ingest.KindPDF, []byte("x"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ingest.ErrRefused, "a missing converter is a server problem, not the document's")
}

// realMarkitdown returns the real converter from a virtualenv with markitdown
// installed (INGEST_MARKITDOWN, or ~/invoice-demo/markitdown-venv), or skips. uvx is
// not used: it is a launcher that starts python itself, and a memory limit on it
// misbehaves.
func realMarkitdown(t *testing.T) *ingest.Markitdown {
	t.Helper()
	if testing.Short() {
		t.Skip("short mode")
	}
	bin := os.Getenv("INGEST_MARKITDOWN")
	if bin == "" {
		home, _ := os.UserHomeDir()
		bin = filepath.Join(home, "invoice-demo", "markitdown-venv", "bin", "markitdown")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no markitdown virtualenv: set INGEST_MARKITDOWN or create ~/invoice-demo/markitdown-venv")
	}
	c := ingest.NewMarkitdown()
	c.Command = []string{bin}
	c.Timeout = 120 * time.Second
	return c
}

func TestMarkitdown_RealConverterReadsATextPDFAndADocxAndFindsNoTextInAScan(t *testing.T) {
	c := realMarkitdown(t)

	pdf, err := c.Convert(context.Background(), ingest.KindPDF, fixture(t, "invoice-text-layer.pdf"))
	require.NoError(t, err, "the default memory limit must not break a real invoice")
	assert.Contains(t, pdf, "Nordkloud AB")
	assert.Contains(t, pdf, "Att betala 4 349,08 SEK", "the total as printed, with its thousands separator")

	docx, err := c.Convert(context.Background(), ingest.KindDOCX, fixture(t, "invoice.docx"))
	require.NoError(t, err)
	assert.Contains(t, docx, "Norrlands Bilservice AB")

	_, err = c.Convert(context.Background(), ingest.KindPDF, fixture(t, "invoice-scan-no-text.pdf"))
	require.ErrorIs(t, err, ingest.ErrNoText)
}

// The attack from the review of #162: a small PDF whose content stream inflates to
// gigabytes. Under the default limit the real converter must fail fast.
func TestMarkitdown_RealConverterStopsAPDFWhoseContentStreamInflatesToGigabytes(t *testing.T) {
	c := realMarkitdown(t)
	bomb := inflatingPDF(t)
	require.Less(t, len(bomb), 1<<20, "the bomb is small as sent")

	start := time.Now()
	_, err := c.Convert(context.Background(), ingest.KindPDF, bomb)

	require.Error(t, err)
	assert.Less(t, time.Since(start), 40*time.Second)
}

func inflatingPDF(t *testing.T) []byte {
	t.Helper()
	var raw bytes.Buffer
	for i := 0; i < 8_000_000/32; i++ {
		raw.WriteString("BT /F1 12 Tf 10 10 Td (a) Tj ET\n")
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, err := zw.Write(raw.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	var b bytes.Buffer
	offs := []int{}
	obj := func(body string) {
		offs = append(offs, b.Len())
		b.WriteString(strconv.Itoa(len(offs)) + " 0 obj\n" + body + "\nendobj\n")
	}
	b.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>")
	obj("<< /Length " + strconv.Itoa(z.Len()) + " /Filter /FlateDecode >>\nstream\n" + z.String() + "\nendstream")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	x := b.Len()
	b.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	b.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n" + strconv.Itoa(x) + "\n%%EOF\n")
	return b.Bytes()
}

// With limits on, the command is started through the shell, which reports a
// missing converter as exit 127. That is still ours to fix, not the document's.
func TestMarkitdown_AMissingConverterBehindTheLimitWrapperIsStillNotTheDocumentsFault(t *testing.T) {
	c := &ingest.Markitdown{Command: []string{"/nonexistent/markitdown"}, Timeout: 5 * time.Second, MaxOutput: 100, MaxMemory: 512 << 20, MaxCPU: 5 * time.Second}

	_, err := c.Convert(context.Background(), ingest.KindPDF, []byte("x"))

	require.Error(t, err)
	assert.NotErrorIs(t, err, ingest.ErrRefused)
	assert.Contains(t, err.Error(), "could not run")
}
