package gsheet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	tok := fmt.Sprintf(`{"default":"a@b","accounts":{"a@b":{"access_token":"t","expires_at":%d}}}`, time.Now().Add(time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	return New("x", WithConfigDir(dir))
}

func TestRequestRetriesQuota(t *testing.T) {
	old := backoff
	backoff = 2 * time.Second
	defer func() { backoff = old }()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "quota", http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	b, err := testClient(t).request(t.Context(), "GET", srv.URL, nil)
	if err != nil || string(b) != "ok" || calls != 3 {
		t.Fatalf("got %q %v after %d calls, want ok after 3", b, err, calls)
	}
}

func TestRequestGivesUpOnQuota(t *testing.T) {
	old := backoff
	backoff = time.Second
	defer func() { backoff = old }()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "quota", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if _, err := testClient(t).request(t.Context(), "POST", srv.URL, map[string]any{"a": 1}); err == nil || calls != 2 {
		t.Fatalf("err %v after %d calls, want an error after 2", err, calls)
	}
}

// A context cancelled during the backoff ends the wait: no 16s default
// backoff to sit out, no second call.
func TestRequestCancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.AfterFunc(20*time.Millisecond, cancel)
		http.Error(w, "quota", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	start := time.Now()
	_, err := testClient(t).request(ctx, "GET", srv.URL, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond || calls.Load() != 1 {
		t.Fatalf("returned after %v and %d calls, want at once after 1", d, calls.Load())
	}
}
