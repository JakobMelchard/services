package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/block"
	"github.com/JakobMelchard/services/internal/workouts/fakesheets"
	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// app serves the routes against a fakesheets.Sheets holding program tabs
// w10-11 (MON: two squat sets, TUE: a run) and w12, and a block started from
// w10-11 with no week tab yet. archive lists the archive's tabs.
func app(t *testing.T, token string) (h http.Handler, archive func() []string) {
	t.Helper()
	f := fakesheets.New()
	f.Add("program", "w10-11", [][]string{{"MON", ""}, {"Squat", "2 x 5 100kg"}, {"", ""}, {"TUE", ""}, {"Run", "30'"}})
	f.Add("program", "w12", [][]string{{"MON", ""}, {"Deadlift", "1 x 5 140kg"}})
	f.Add("active", "Block", [][]string{{"program", "w10-11"}, {"started", "2026-03-02"}})
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tok := fmt.Sprintf(`{"default":"a","accounts":{"a":{"access_token":"t","expires_at":%d}}}`, time.Now().Add(time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	opt := func(c *gsheet.Client) {
		c.HTTP = &http.Client{Transport: fakesheets.Rewrite(strings.TrimPrefix(srv.URL, "http://"))}
	}
	h, err := routes("", Sheets{Program: "program", Active: "active", Archive: "archive"}, token, gsheet.WithConfigDir(dir), opt)
	if err != nil {
		t.Fatal(err)
	}
	return h, func() []string {
		tabs, err := gsheet.New("archive", gsheet.WithConfigDir(dir), opt).Tabs(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tb := range tabs {
			names = append(names, tb.Title)
		}
		return names
	}
}

// call sends one request; a string body is a form, anything else JSON. cred
// is "Bearer <token>" for the Authorization header, anything else the login
// cookie's value.
func call(t *testing.T, h http.Handler, method, path string, body any, cred ...string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	switch b := body.(type) {
	case nil:
		r = httptest.NewRequest(method, path, nil)
	case string:
		r = httptest.NewRequest(method, path, strings.NewReader(b))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	default:
		j, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, strings.NewReader(string(j)))
	}
	for _, c := range cred {
		if strings.HasPrefix(c, "Bearer ") {
			r.Header.Set("Authorization", c)
		} else {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: c})
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// view decodes a week view reply, failing unless it is 200.
func view(t *testing.T, w *httptest.ResponseRecorder) block.View {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("got %d %q, want 200", w.Code, w.Body.String())
	}
	var v block.View
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func ref(r block.Row, i int) block.Ref {
	return block.Ref{Index: i, Day: r.Day, Exercise: r.Exercise, Set: r.Set}
}

func TestStaticRoutes(t *testing.T) {
	h, _ := app(t, "")
	for path, want := range map[string]string{
		"/":                            `<meta name="apple-mobile-web-app-capable" content="yes">`,
		"/sw.js":                       "WEB_SW",
		"/static/manifest.webmanifest": `"id": "/"`,
		"/static/ui.js":                "applyOp",
		"/static/icon-180.png":         "PNG",
		"/web/base.css":                "box-sizing",
		"/web/queue.js":                "createQueue",
	} {
		if w := call(t, h, "GET", path, nil); w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s: %d, want 200 with %s", path, w.Code, want)
		}
	}
	if w := call(t, h, "GET", "/login", nil); w.Code != 404 {
		t.Errorf("GET /login without a token: %d, want 404", w.Code)
	}
}

func TestBlockAndProgramRoutes(t *testing.T) {
	h, _ := app(t, "")
	w := call(t, h, "GET", "/block", nil)
	var b struct {
		Block    block.Info `json:"block"`
		Programs []string   `json:"programs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); w.Code != 200 || err != nil {
		t.Fatalf("GET /block: %d %q %v", w.Code, w.Body.String(), err)
	}
	if b.Block.Program != "w10-11" || !slices.Equal(b.Block.Weeks, []string{"w10", "w11"}) || !slices.Equal(b.Programs, []string{"w10-11", "w12"}) {
		t.Errorf("GET /block = %+v", b)
	}
	w = call(t, h, "GET", "/programs/w10-11", nil)
	var p block.Program
	if err := json.Unmarshal(w.Body.Bytes(), &p); w.Code != 200 || err != nil || len(p.Days) != 2 || p.Errors != 0 {
		t.Errorf("GET /programs/w10-11: %d %+v %v", w.Code, p, err)
	}
	if w := call(t, h, "GET", "/programs/nope", nil); w.Code != 404 {
		t.Errorf("GET /programs/nope: %d, want 404", w.Code)
	}
}

// TestWeekEdits walks the row routes the way the app does, reading the week
// back after every write: the edits must be in the (fake) sheet, not only in
// the reply.
func TestWeekEdits(t *testing.T) {
	h, _ := app(t, "")
	const rows = "/weeks/w10/rows"
	v := view(t, call(t, h, "GET", rows, nil))
	if !slices.Equal(v.Days, []string{"MON", "TUE"}) || len(v.Rows) != 3 || !v.Rows[0].Planned {
		t.Fatalf("fresh week = %+v, want MON and TUE planned", v)
	}

	notes, weight := "felt #easy", "105kg"
	view(t, call(t, h, "PATCH", rows, map[string]any{"ref": ref(v.Rows[1], 1), "patch": block.Patch{Notes: &notes, Weight: &weight}}))
	v = view(t, call(t, h, "GET", rows, nil))
	if r := v.Rows[1]; r.Planned || r.Notes != notes || r.Weight != weight || !slices.Equal(r.Tags, []string{"easy"}) {
		t.Fatalf("after edit, set 2 = %+v", r)
	}
	if !v.Rows[2].Planned {
		t.Errorf("TUE started by an edit of MON: %+v", v.Rows[2])
	}

	view(t, call(t, h, "POST", rows+"/duplicate", map[string]any{"ref": ref(v.Rows[1], 1)}))
	v = view(t, call(t, h, "GET", rows, nil))
	if len(v.Rows) != 4 || v.Rows[2].Weight != weight || v.Rows[2].Notes != "" || v.Rows[2].Set != "3" {
		t.Fatalf("after duplicate = %+v", v.Rows)
	}

	view(t, call(t, h, "POST", rows+"/delete", map[string]any{"ref": ref(v.Rows[0], 0)}))
	v = view(t, call(t, h, "GET", rows, nil))
	if len(v.Rows) != 3 || v.Rows[0].Weight != weight {
		t.Fatalf("after delete = %+v", v.Rows)
	}

	// The client saw a set that is no longer there: refused, nothing written,
	// and the day it named is not started.
	stale := block.Ref{Index: 0, Day: "TUE", Exercise: "Bench", Set: "1"}
	if w := call(t, h, "PATCH", rows, map[string]any{"ref": stale, "patch": block.Patch{Notes: &notes}}); w.Code != http.StatusConflict {
		t.Errorf("stale edit: %d %q, want 409", w.Code, w.Body.String())
	}
	if after := view(t, call(t, h, "GET", rows, nil)); !reflect.DeepEqual(after, v) {
		t.Fatalf("after stale edit = %+v, want %+v", after, v)
	}

	view(t, call(t, h, "POST", "/weeks/w10/days/TUE/start", nil))
	view(t, call(t, h, "POST", "/weeks/w10/days/TUE/exercises", map[string]string{"exercise": "Curl", "load": "2x10 20kg"}))
	v = view(t, call(t, h, "GET", rows, nil))
	var tue []string
	for _, r := range v.Rows {
		if r.Day == "TUE" {
			if r.Planned {
				t.Errorf("TUE still planned after start: %+v", r)
			}
			tue = append(tue, r.Exercise)
		}
	}
	if !slices.Equal(tue, []string{"Run", "Curl", "Curl"}) {
		t.Errorf("TUE = %q, want the run and two curl sets", tue)
	}

	if w := call(t, h, "POST", "/weeks/w10/days/TUE/exercises", map[string]string{"exercise": "Curl", "load": "lots"}); w.Code != 400 {
		t.Errorf("bad load: %d, want 400", w.Code)
	}
	if w := call(t, h, "GET", "/weeks/w40/rows", nil); w.Code != 404 {
		t.Errorf("week outside the block: %d, want 404", w.Code)
	}
}

// The mux has already unescaped path values; a day named with a % sign must
// not be unescaped a second time.
func TestDayWithPercent(t *testing.T) {
	h, _ := app(t, "")
	if w := call(t, h, "POST", "/weeks/w10/days/DELOAD%2080%25/start", nil); w.Code != 200 {
		t.Errorf("start DELOAD 80%%: %d %q, want 200", w.Code, w.Body.String())
	}
}

func TestNextBlockRoute(t *testing.T) {
	h, archive := app(t, "")
	view(t, call(t, h, "POST", "/weeks/w10/days/MON/start", nil))
	if w := call(t, h, "POST", "/block", "program=w12"); w.Code != 200 {
		t.Fatalf("POST /block: %d %q", w.Code, w.Body.String())
	}
	if got := archive(); !slices.Equal(got, []string{"2026 w10"}) {
		t.Errorf("archive tabs = %q, want the logged week", got)
	}
	var b struct{ Block block.Info }
	if err := json.Unmarshal(call(t, h, "GET", "/block", nil).Body.Bytes(), &b); err != nil || b.Block.Program != "w12" {
		t.Errorf("block after start = %+v, %v; want w12", b, err)
	}
	if w := call(t, h, "POST", "/block", "program=nope"); w.Code != 404 {
		t.Errorf("unknown program: %d, want 404", w.Code)
	}
}

// Data routes need the token on reads too: each read spends Sheets quota and
// shows the training log. The shell, the PWA files and login stay open.
func TestRoutesNeedToken(t *testing.T) {
	h, _ := app(t, "s3cret")
	if w := call(t, h, "POST", "/weeks/w10/days/MON/start", nil); w.Code != 401 {
		t.Errorf("write without token: %d, want 401", w.Code)
	}
	for _, path := range []string{"/block", "/programs/w10-11", "/weeks/w10/rows"} {
		if w := call(t, h, "GET", path, nil); w.Code != 401 {
			t.Errorf("GET %s without token: %d, want 401", path, w.Code)
		}
		if w := call(t, h, "GET", path, nil, "Bearer s3cret"); w.Code != 200 {
			t.Errorf("GET %s with token: %d %q, want 200", path, w.Code, w.Body.String())
		}
		if w := call(t, h, "GET", path, nil, cookieValue("s3cret")); w.Code != 200 {
			t.Errorf("GET %s with cookie: %d %q, want 200", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/", "/login", "/sw.js", "/static/ui.js", "/web/base.css", "/static/manifest.webmanifest"} {
		if w := call(t, h, "GET", path, nil); w.Code != 200 {
			t.Errorf("GET %s without token: %d, want 200", path, w.Code)
		}
	}
}
