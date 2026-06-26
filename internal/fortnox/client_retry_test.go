package fortnox

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// resetRateLimiter clears the shared sliding-window state so retry tests are
// deterministic regardless of what other tests in the package ran first.
func resetRateLimiter() {
	rateLimiter.mu.Lock()
	rateLimiter.count = 0
	rateLimiter.windowEnd = time.Time{}
	rateLimiter.mu.Unlock()
}

func TestDo_RetriesOn429ThenSucceeds(t *testing.T) {
	resetRateLimiter()
	old := retryBaseDelay
	retryBaseDelay = time.Millisecond
	defer func() { retryBaseDelay = old }()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", false)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls = %d, want 3 (two 429s + one success)", got)
	}
}

func TestDo_ResendsBodyOnRetry(t *testing.T) {
	resetRateLimiter()
	old := retryBaseDelay
	retryBaseDelay = time.Millisecond
	defer func() { retryBaseDelay = old }()

	var calls int32
	var lastBody atomic.Value
	lastBody.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody.Store(string(b))
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", false)
	req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader([]byte(`{"x":1}`)))
	resp, err := c.do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := lastBody.Load().(string); got != `{"x":1}` {
		t.Fatalf("retried request body = %q, want the original payload resent", got)
	}
}

func TestDo_HonorsRetryAfterHeader(t *testing.T) {
	resetRateLimiter()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", false)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	start := time.Now()
	resp, err := c.do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("elapsed = %v, want >= 1s (Retry-After honored)", elapsed)
	}
}

func TestDo_GivesUpAfterMaxRetries(t *testing.T) {
	resetRateLimiter()
	old := retryBaseDelay
	retryBaseDelay = time.Millisecond
	defer func() { retryBaseDelay = old }()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", false)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want final 429 surfaced after retries exhausted", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != maxRetries+1 {
		t.Fatalf("calls = %d, want %d (1 initial + %d retries)", got, maxRetries+1, maxRetries)
	}
}
