package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
// The child does not inherit the caller's process variables (the demo holds its
// login and a 1Password session there), is started in its own process group that
// is killed on timeout, and has its output capped. Its stderr is discarded: it can
// echo the document and is never returned in an error.
//
// Bounds on what one document can cost, because wall time and output size alone
// were not enough (review of #162: a 447 KB PDF whose content stream inflates
// took 3.1 GB, and a 90 KB DOCX 1.5 GB): MaxMemory is the child's address space
// (RLIMIT_AS) and MaxCPU its CPU time (RLIMIT_CPU), both set through the shell's
// ulimit before the converter is exec'd. If a limit cannot be set the converter
// does not run.
//
// What this does not do: stop the child from making network calls or writing its
// own temporary files (markitdown converting a PDF or DOCX does neither, checked
// once on 2026-11-25), or kill a descendant that leaves the process group with
// setsid: the memory and CPU limits are inherited by descendants and are what
// bound those. The NetworkPolicy of the in-cluster docmark adapter is the real
// boundary for egress.
type Markitdown struct {
	// Command is the program and leading arguments; "-x .ext" is appended.
	Command []string
	// Timeout is the wall-clock limit for one conversion.
	Timeout time.Duration
	// MaxOutput is the most stdout, in bytes, that is accepted.
	MaxOutput int64
	// MaxMemory is the most address space the child may use, in bytes; zero is no limit.
	MaxMemory int64
	// MaxCPU is the most CPU time the child may use; zero is no limit.
	MaxCPU time.Duration
}

// NewMarkitdown returns the converter with the estate's documented limits. The
// command is `markitdown` on PATH: install it in a virtualenv (see the demo
// runbook) rather than running it through uvx, a launcher that starts python
// itself and does not behave under an address-space limit. Address space is
// counted in virtual memory, so the memory limit is generous: python and its
// libraries need most of 1.5 GiB of it to convert an ordinary invoice.
func NewMarkitdown() *Markitdown {
	return &Markitdown{
		Command:   []string{"markitdown"},
		Timeout:   60 * time.Second,
		MaxOutput: 1 << 20,
		MaxMemory: 2 << 30,
		MaxCPU:    45 * time.Second,
	}
}

// childVars is everything the converter gets: where to find programs and where
// uv keeps its cache. Nothing else crosses.
func childVars() []string {
	vars := []string{"LANG=C.UTF-8"}
	for _, k := range []string{"PATH", "HOME", "XDG_CACHE_HOME", "UV_CACHE_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			vars = append(vars, k+"="+v)
		}
	}
	return vars
}

// wrapperScript sets the limits and replaces the shell with the converter. If a
// limit cannot be set the shell exits 126, which Convert reads as a problem with
// the server, not the document, and the converter is never started.
const wrapperScript = `ulimit -v "$1" && ulimit -t "$2" || exit 126; shift 2; exec "$@"`

// limited wraps argv so the shell sets the limits and then replaces itself with
// the converter. With no limit configured argv is returned as it is.
func (m *Markitdown) limited(argv []string) []string {
	if m.MaxMemory <= 0 && m.MaxCPU <= 0 {
		return argv
	}
	mem, cpu := "unlimited", "unlimited"
	if m.MaxMemory > 0 {
		mem = strconv.FormatInt(m.MaxMemory/1024, 10) // ulimit -v is in KiB
	}
	if m.MaxCPU > 0 {
		cpu = strconv.FormatInt(int64((m.MaxCPU+time.Second-1)/time.Second), 10)
	}
	return append([]string{"sh", "-c", wrapperScript, "sh", mem, cpu}, argv...)
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

	argv := m.limited(append(append([]string{}, m.Command...), "-x", ext))
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = childVars()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(data)
	out := &cappedWriter{max: m.MaxOutput, onOverflow: cancel}
	cmd.Stdout = out

	err := cmd.Run()
	switch {
	case out.overflowed:
		return "", fmt.Errorf("%w: converter output is larger than %d bytes", ErrRefused, m.MaxOutput)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "", fmt.Errorf("%w: converter timed out after %s", ErrRefused, m.Timeout)
	case ctx.Err() != nil:
		return "", fmt.Errorf("converter was cancelled: %w", ctx.Err())
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// 126 and 127 are the shell saying the converter could not be started:
			// ours to fix, not the document's.
			if code := ee.ExitCode(); code == 126 || code == 127 {
				return "", fmt.Errorf("converter could not run (%s)", ee.ProcessState)
			}
			return "", fmt.Errorf("%w: converter failed (%s)", ErrRefused, ee.ProcessState)
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
