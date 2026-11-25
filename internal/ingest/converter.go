package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ErrNoText is returned when a converter ran cleanly and found no text: a
// scanned PDF with no text layer, or an empty template. It is "this needs the
// vision path", never "this is not an invoice".
var ErrNoText = errors.New("the document has no extractable text (a scan or an empty file)")

// Converter turns a document into text. It is a port: markitdown on this host
// today, docmark (in-cluster, with the audited vision path) later.
type Converter interface {
	Convert(ctx context.Context, kind Kind, data []byte) (string, error)
}

// Markitdown runs the markitdown command line as a subprocess.
//
// The document goes in on stdin, so this code writes no temporary file for it.
// The child does not inherit the caller's environment (the demo holds its login
// and a 1Password session there), runs in its own process group so a timeout
// kills uvx and every worker it started, and has its output capped. Its stderr
// is discarded: it can echo the document and is never returned in an error.
//
// What this does not do: stop the child from making network calls or writing its
// own temporary files. markitdown converting a PDF or DOCX does neither, and the
// NetworkPolicy of the in-cluster docmark adapter is the real boundary there.
type Markitdown struct {
	// Command is the program and leading arguments; "-x .ext" is appended.
	Command []string
	// Timeout is the wall-clock limit for one conversion.
	Timeout time.Duration
	// MaxOutput is the most stdout, in bytes, that is accepted.
	MaxOutput int64
}

// NewMarkitdown returns the converter with the estate's documented command,
// without the extras that pull in cloud document-intelligence converters.
func NewMarkitdown() *Markitdown {
	return &Markitdown{
		Command:   []string{"uvx", "--quiet", "--from", "markitdown[docx,pdf]", "markitdown"},
		Timeout:   60 * time.Second,
		MaxOutput: 1 << 20,
	}
}

// childEnv is everything the converter gets: where to find programs and where
// uv keeps its cache. Nothing else crosses.
func childEnv() []string {
	env := []string{"LANG=C.UTF-8"}
	for _, k := range []string{"PATH", "HOME", "XDG_CACHE_HOME", "UV_CACHE_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Convert implements Converter for PDF and DOCX.
func (m *Markitdown) Convert(ctx context.Context, kind Kind, data []byte) (string, error) {
	var ext string
	switch kind {
	case KindPDF:
		ext = ".pdf"
	case KindDOCX:
		ext = ".docx"
	case KindImage, KindUBL, KindUnknown:
		return "", fmt.Errorf("markitdown does not convert a %s", kind)
	}
	if ext == "" {
		return "", fmt.Errorf("markitdown does not convert a %s", kind)
	}
	if len(m.Command) == 0 {
		return "", errors.New("no converter command configured")
	}

	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.Command[0], append(append([]string{}, m.Command[1:]...), "-x", ext)...)
	cmd.Env = childEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(data)
	out := &cappedWriter{max: m.MaxOutput, onOverflow: cancel}
	cmd.Stdout = out

	err := cmd.Run()
	switch {
	case out.overflowed:
		return "", fmt.Errorf("converter output is larger than %d bytes", m.MaxOutput)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "", fmt.Errorf("converter timed out after %s", m.Timeout)
	case ctx.Err() != nil:
		return "", fmt.Errorf("converter was cancelled: %w", ctx.Err())
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("converter failed (%s)", ee.ProcessState)
		}
		return "", fmt.Errorf("converter could not run: %w", err)
	}
	text := out.buf.String()
	if strings.TrimSpace(text) == "" {
		return "", ErrNoText
	}
	return text, nil
}

// cappedWriter keeps at most max bytes and, past that, cancels the command.
type cappedWriter struct {
	max        int64
	buf        bytes.Buffer
	overflowed bool
	onOverflow func()
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len()+len(p)) > w.max {
		w.overflowed = true
		w.onOverflow()
		return 0, errors.New("output limit reached")
	}
	return w.buf.Write(p)
}
