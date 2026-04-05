package audit_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- contract tests for FileLogger ---

func TestNewFileLogger_CreatesFileAndParentDirs(t *testing.T) {
	path := t.TempDir() + "/sub/dir/audit.log"

	lg, err := audit.NewFileLogger(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lg.Close() })

	_, err = os.Stat(path)
	assert.NoError(t, err, "log file should exist after NewFileLogger")
}

func TestFileLogger_LogWritesValidJSONLine(t *testing.T) {
	lg, path := newTestFileLogger(t)

	entry := audit.Entry{
		Op:         "GET",
		Endpoint:   "/invoices",
		Status:     200,
		DurationMs: 42,
		Agent:      "read-only-reporter",
	}
	require.NoError(t, lg.Log(entry))
	require.NoError(t, lg.Close())

	rows := readJSONLines(t, path)
	require.Len(t, rows, 1)
	assert.Equal(t, "GET", rows[0]["op"])
	assert.Equal(t, "/invoices", rows[0]["endpoint"])
	assert.Equal(t, float64(200), rows[0]["status"])
	assert.Equal(t, float64(42), rows[0]["duration_ms"])
	assert.Equal(t, "read-only-reporter", rows[0]["agent"])
}

func TestFileLogger_AutoSetsTimestampWhenZero(t *testing.T) {
	lg, path := newTestFileLogger(t)
	before := time.Now().UTC().Add(-time.Second)

	require.NoError(t, lg.Log(audit.Entry{Op: "GET", Endpoint: "/accounts"}))
	require.NoError(t, lg.Close())

	rows := readJSONLines(t, path)
	require.Len(t, rows, 1)

	raw, ok := rows[0]["ts"].(string)
	require.True(t, ok, "ts field should be a string")

	ts, err := time.Parse(time.RFC3339Nano, raw)
	require.NoError(t, err, "ts should be RFC3339Nano")
	assert.True(t, ts.After(before), "ts should be set to approximately now")
	assert.True(t, ts.Before(time.Now().UTC().Add(time.Second)))
}

func TestFileLogger_PreservesExplicitTimestamp(t *testing.T) {
	lg, path := newTestFileLogger(t)
	explicit := time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)

	require.NoError(t, lg.Log(audit.Entry{
		Timestamp: explicit,
		Op:        "POST",
		Endpoint:  "/vouchers",
	}))
	require.NoError(t, lg.Close())

	rows := readJSONLines(t, path)
	ts, _ := time.Parse(time.RFC3339Nano, rows[0]["ts"].(string))
	assert.True(t, ts.Equal(explicit), "explicit timestamp should not be overwritten")
}

func TestFileLogger_IsAppendOnly(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/audit.log"

	// First session: write one entry
	lg1, err := audit.NewFileLogger(path)
	require.NoError(t, err)
	require.NoError(t, lg1.Log(audit.Entry{Op: "GET", Endpoint: "/first"}))
	require.NoError(t, lg1.Close())

	// Second session: write another entry
	lg2, err := audit.NewFileLogger(path)
	require.NoError(t, err)
	require.NoError(t, lg2.Log(audit.Entry{Op: "POST", Endpoint: "/second"}))
	require.NoError(t, lg2.Close())

	rows := readJSONLines(t, path)
	require.Len(t, rows, 2, "log must be append-only – both entries must survive")
	assert.Equal(t, "/first", rows[0]["endpoint"])
	assert.Equal(t, "/second", rows[1]["endpoint"])
}

func TestFileLogger_ConcurrentWritesProduceValidOutput(t *testing.T) {
	lg, path := newTestFileLogger(t)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			assert.NoError(t, lg.Log(audit.Entry{
				Op:       "GET",
				Endpoint: fmt.Sprintf("/resource/%d", i),
			}))
		}(i)
	}
	wg.Wait()
	require.NoError(t, lg.Close())

	rows := readJSONLines(t, path)
	assert.Len(t, rows, n, "all concurrent entries must be written without corruption or loss")
}

func TestFileLogger_LogAfterCloseReturnsError(t *testing.T) {
	lg, _ := newTestFileLogger(t)
	require.NoError(t, lg.Close())

	err := lg.Log(audit.Entry{Op: "GET", Endpoint: "/test"})
	assert.Error(t, err, "Log after Close must return an error")
}

func TestFileLogger_ImplementsLoggerInterface(t *testing.T) {
	// Compile-time check: FileLogger satisfies the Logger interface.
	var _ audit.Logger = (*audit.FileLogger)(nil)
}

// --- contract tests for MemoryLogger (test helper) ---

func TestMemoryLogger_ImplementsLoggerInterface(t *testing.T) {
	var _ audit.Logger = (*audit.MemoryLogger)(nil)
}

func TestMemoryLogger_RecordsEntries(t *testing.T) {
	ml := &audit.MemoryLogger{}

	require.NoError(t, ml.Log(audit.Entry{Op: "GET", Endpoint: "/a"}))
	require.NoError(t, ml.Log(audit.Entry{Op: "POST", Endpoint: "/b"}))

	entries := ml.Entries()
	require.Len(t, entries, 2)
	assert.Equal(t, "/a", entries[0].Endpoint)
	assert.Equal(t, "/b", entries[1].Endpoint)
}

func TestMemoryLogger_AutoSetsTimestamp(t *testing.T) {
	ml := &audit.MemoryLogger{}
	before := time.Now().UTC().Add(-time.Second)

	require.NoError(t, ml.Log(audit.Entry{Op: "GET", Endpoint: "/test"}))

	entries := ml.Entries()
	require.Len(t, entries, 1)
	assert.True(t, entries[0].Timestamp.After(before))
}

func TestMemoryLogger_ResetClearsEntries(t *testing.T) {
	ml := &audit.MemoryLogger{}
	require.NoError(t, ml.Log(audit.Entry{Op: "GET", Endpoint: "/test"}))

	ml.Reset()
	assert.Empty(t, ml.Entries())
}

func TestMemoryLogger_EntriesReturnsCopy(t *testing.T) {
	ml := &audit.MemoryLogger{}
	require.NoError(t, ml.Log(audit.Entry{Op: "GET", Endpoint: "/test"}))

	entries := ml.Entries()
	entries[0].Endpoint = "mutated"

	assert.Equal(t, "/test", ml.Entries()[0].Endpoint, "Entries() must return a copy, not a reference to internal state")
}

// --- helpers ---

func newTestFileLogger(t *testing.T) (*audit.FileLogger, string) {
	t.Helper()
	path := t.TempDir() + "/audit.log"
	lg, err := audit.NewFileLogger(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lg.Close() })
	return lg, path
}

// readJSONLines reads a JSON Lines file and returns each line as a parsed map.
// Fails the test if any line is not valid JSON.
func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var rows []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		require.NoError(t, json.Unmarshal(line, &row), "each line must be valid JSON: %q", line)
		rows = append(rows, row)
	}
	require.NoError(t, scanner.Err())
	return rows
}
