package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/fakesheets"
	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// newHandler returns a Handler whose spreadsheets "program", "active" and
// "archive" are all answered by sheets.
func newHandler(t *testing.T, sheets http.HandlerFunc) *Handler {
	t.Helper()
	srv := httptest.NewServer(sheets)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tok := fmt.Sprintf(`{"default":"a","accounts":{"a":{"access_token":"t","expires_at":%d}}}`, time.Now().Add(time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	client := func(id string) *gsheet.Client {
		c := gsheet.New(id, gsheet.WithConfigDir(dir))
		c.HTTP = &http.Client{Transport: fakesheets.Rewrite(strings.TrimPrefix(srv.URL, "http://"))}
		return c
	}
	return New(client("program"), client("active"), client("archive"), nil, "")
}

// A request cancelled (client gone, or shutdown) while the Sheets client
// backs off after a 429 must not sit out the backoff.
func TestCancelDuringQuotaBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.AfterFunc(50*time.Millisecond, cancel)
		http.Error(w, "quota", http.StatusTooManyRequests)
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Block(httptest.NewRecorder(), httptest.NewRequest("GET", "/block", nil).WithContext(ctx))
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handler still backing off 500ms after its request was cancelled")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("%d Sheets calls, want 1 (no retry after the cancel)", n)
	}
}

func TestOversizedBody(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) })
	big := strings.Repeat("x", 2<<20)
	for name, tt := range map[string]struct {
		do   http.HandlerFunc
		body string
	}{
		"edit row":     {h.EditRow, `{"ref":{"index":0,"day":"MON"},"patch":{"notes":"` + big + `"}}`},
		"add exercise": {h.AddExercise, `{"exercise":"` + big + `","load":"1x5"}`},
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
		r.SetPathValue("tab", "w10")
		r.SetPathValue("day", "MON")
		w := httptest.NewRecorder()
		tt.do(w, r)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: got %d, want 413", name, w.Code)
		}
	}
}

// The Google reply (and with it sheet IDs or quota details) stays in the log.
func TestUpstreamErrorNotEchoed(t *testing.T) {
	const secret = "SECRET upstream detail"
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"`+secret+`"}}`, http.StatusForbidden)
	})
	for name, tt := range map[string]struct {
		do     http.HandlerFunc
		method string
		body   string
	}{
		"block":      {h.Block, "GET", ""},
		"next block": {h.NextBlock, "POST", "program=w10-11"},
		"program":    {h.Program, "GET", ""},
		"rows":       {h.Rows, "GET", ""},
		"start day":  {h.StartDay, "POST", ""},
	} {
		r := httptest.NewRequest(tt.method, "/", strings.NewReader(tt.body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("tab", "w10-11")
		r.SetPathValue("day", "MON")
		w := httptest.NewRecorder()
		tt.do(w, r)
		if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), secret) {
			t.Errorf("%s: got %d %q, want 500 without the upstream reply", name, w.Code, w.Body.String())
		}
	}
}

// A week still loads when only the program sheet fails; its warning names
// the tab, not the Google reply.
func TestProgramErrorNotInWarning(t *testing.T) {
	const secret = "SECRET upstream detail"
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/program/"):
			http.Error(w, secret, http.StatusForbidden)
		case strings.Contains(r.URL.Path, "/values/"):
			fmt.Fprint(w, `{"values":[["program","w10-11"],["started","2026-03-02"]]}`)
		default:
			fmt.Fprint(w, `{"sheets":[{"properties":{"title":"Block","sheetId":1}}]}`)
		}
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("tab", "w10")
	w := httptest.NewRecorder()
	h.Rows(w, r)
	if body := w.Body.String(); w.Code != 200 || !strings.Contains(body, "w10-11 could not be read") || strings.Contains(body, secret) {
		t.Errorf("got %d %q, want 200 with a warning without the upstream reply", w.Code, body)
	}
}

// A block whose program has rows that are not workline is the caller's to
// fix: it keeps its message and is not a 500.
func TestNextBlockBadProgram(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"values":[["MON"],["Squat","not workline"]]}`)
	})
	r := httptest.NewRequest("POST", "/block", strings.NewReader("program=w10-11"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.NextBlock(w, r)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "w10-11 has 1 rows") {
		t.Errorf("got %d %q, want 422 naming the tab", w.Code, w.Body.String())
	}
}
