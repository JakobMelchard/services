package handler

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JakobMelchard/services/internal/lists/model"
	"github.com/JakobMelchard/services/internal/lists/store"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

// fx serves the app on a copy of the fixtures in data/. tick moves the clock.
type fx struct {
	t    *testing.T
	h    http.Handler
	dir  string
	when time.Time
}

func mk(t *testing.T) *fx {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../data")); err != nil {
		t.Fatal(err)
	}
	f := &fx{t: t, dir: dir, when: now}
	h, err := New(dir, func() time.Time { return f.when })
	if err != nil {
		t.Fatal(err)
	}
	f.h = h
	return f
}

func (f *fx) tick(d time.Duration) { f.when = f.when.Add(d) }

// req sends a form; GET and DELETE carry nothing, others post it.
func (f *fx) req(method, path string, form ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	v := url.Values{}
	for i := 0; i+1 < len(form); i += 2 {
		v.Set(form[i], form[i+1])
	}
	var r *http.Request
	if method == "GET" || method == "DELETE" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func (f *fx) text(method, path string, form ...string) string {
	f.t.Helper()
	w := f.req(method, path, form...)
	if w.Code != 200 {
		f.t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	return w.Body.String()
}

func (f *fx) list(id string) *model.Item {
	f.t.Helper()
	x, err := store.Dir(f.dir).Get(id)
	if err != nil {
		f.t.Fatalf("list %s: %v", id, err)
	}
	return x
}

var idRe = regexp.MustCompile(`data-id="([^"]+)"`)

// ids are the data-id attributes in order: the section's list id first.
func ids(h string) []string {
	var out []string
	for _, m := range idRe.FindAllStringSubmatch(h, -1) {
		out = append(out, m[1])
	}
	return out
}

func texts(xs []*model.Item) []string {
	var out []string
	for _, x := range xs {
		out = append(out, x.Text)
	}
	return out
}

func TestLifecycle(t *testing.T) {
	f := mk(t)
	h := f.text("POST", "/lists", "text", "trip")
	l := ids(h)[0]
	if !strings.Contains(h, "<h2>trip</h2>") {
		t.Fatalf("new list: %s", h)
	}
	h = f.text("POST", "/lists/"+l+"/items", "text", "passport", "sev", "3", "note", "check expiry")
	a := ids(h)[1]
	f.tick(time.Millisecond)
	f.text("POST", "/lists/"+l+"/items", "parent", a, "text", "photo", "sev", "2")
	x := f.list(l)
	if p := x.Items[0]; len(x.Items) != 1 || p.Text != "passport" || p.Sev != 3 || p.Note != "check expiry" || !slices.Equal(texts(p.Items), []string{"photo"}) {
		t.Fatalf("after adds: %+v", x.Items)
	}
	b := x.Items[0].Items[0].ID
	f.text("POST", "/lists/"+l+"/items/"+b+"/done")
	if f.list(l).Items[0].Items[0].Done.IsZero() {
		t.Error("not done")
	}
	f.text("POST", "/lists/"+l+"/items/"+b+"/done")
	if !f.list(l).Items[0].Items[0].Done.IsZero() {
		t.Error("still done")
	}
	f.text("POST", "/lists/"+l+"/items/"+a+"/copy")
	y := f.list(l)
	if !slices.Equal(texts(y.Items), []string{"passport", "passport"}) || y.Items[0].ID == y.Items[1].ID || y.Items[1].Items[0].Text != "photo" {
		t.Fatalf("after copy: %+v", y.Items)
	}
	f.text("DELETE", "/lists/"+l+"/items/"+a)
	if n := len(f.list(l).Items); n != 1 {
		t.Fatalf("after delete: %d items", n)
	}
	// patch: each control posts its own field, the others must survive untouched
	p1 := y.Items[1].ID
	check := func(text string, sev int, note string) {
		t.Helper()
		if i := f.list(l).Items[0]; i.Text != text || i.Sev != sev || i.Note != note {
			t.Errorf("item = %q %d %q, want %q %d %q", i.Text, i.Sev, i.Note, text, sev, note)
		}
	}
	f.text("POST", "/lists/"+l+"/items/"+p1, "text", "passport!")
	check("passport!", 3, "check expiry")
	f.text("POST", "/lists/"+l+"/items/"+p1, "note", "renewed online")
	check("passport!", 3, "renewed online")
	f.text("POST", "/lists/"+l+"/items/"+p1, "sev", "2")
	check("passport!", 2, "renewed online")
	f.text("POST", "/lists/"+l+"/items/"+p1, "note", "  ") // emptying clears it
	check("passport!", 2, "")
	if w := f.req("POST", "/lists/"+l+"/items/"+p1, "text", " "); w.Code != 400 {
		t.Errorf("blank rename: %d", w.Code)
	}
	if h := f.text("POST", "/lists/"+l+"/copy"); !strings.Contains(h, "<h2>trip copy</h2>") {
		t.Errorf("list copy: %s", h)
	}
	if h := f.text("DELETE", "/lists/"+l); strings.Contains(h, "<h2>trip</h2>") {
		t.Errorf("list still shown after delete")
	}
	if _, err := store.Dir(f.dir).Get(l); !os.IsNotExist(err) {
		t.Errorf("file after delete: %v", err)
	}
}

func TestFixtures(t *testing.T) {
	f := mk(t)
	h := f.text("GET", "/app?list=todo")
	// t1 (sev 1, 42d) escalated to 3; t2/t3/t4 still 1, default is 1 now
	if got := ids(h)[1:]; !slices.Equal(got, []string{"t1", "t2", "t3", "t4"}) {
		t.Errorf("todo order = %q", got)
	}
	for _, s := range []string{`<li class="s3" data-id="t1"`, `<li class="s1" data-id="t4"`, `hx-include="#q"`} {
		if !strings.Contains(h, s) {
			t.Errorf("missing %s", s)
		}
	}
	for q, want := range map[string][]string{
		"/app?list=todo&q=sev:3":       {"t1"},
		"/app?list=groceries&q=-done":  {"g2", "g1", "g3", "g31"},
		"/app?list=groceries&q=bakery": {"g3"},
	} {
		if got := ids(f.text("GET", q))[1:]; !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", q, got, want)
		}
	}
	if h := f.text("GET", "/"); !strings.HasPrefix(h, "<!doctype html>") || !strings.Contains(h, `rel="manifest"`) {
		t.Errorf("page: %.80s", h)
	}
	if w := f.req("POST", "/lists/nope/items", "text", "x"); w.Code != 404 {
		t.Errorf("unknown list: %d", w.Code)
	}
	if w := f.req("POST", "/lists/todo/items", "text", " "); w.Code != 400 {
		t.Errorf("blank item: %d", w.Code)
	}
}

// snapshot is every file under dir with its content.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		m[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTraversalIdsTouchNoFile(t *testing.T) {
	f := mk(t)
	// a valid list one level above the data dir: lists/../../x.json resolves to it
	above := filepath.Join(filepath.Dir(f.dir), "x.json")
	if err := os.WriteFile(above, []byte(`{"id":"x","text":"x","at":"2026-09-21T12:00:00Z","items":[{"id":"c","text":"c","at":"2026-09-21T12:00:00Z","items":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(above) })
	before := snapshot(t, filepath.Dir(f.dir))
	l := "..%2F..%2Fx"
	for _, c := range [][2]string{
		{"GET", "/lists/" + l + "/items/c/add"}, {"GET", "/lists/todo/items/..%2Ft1/add"}, {"GET", "/lists/todo/items/T1/add"},
		{"POST", "/lists/" + l + "/copy"}, {"POST", "/lists/" + l + "/items"}, {"POST", "/lists/" + l + "/items/c"}, {"POST", "/lists/" + l + "/items/c/done"}, {"POST", "/lists/" + l + "/items/c/copy"},
		{"DELETE", "/lists/" + l + "/items/c"}, {"DELETE", "/lists/" + l}, {"DELETE", "/lists/todo%2F..%2Fgroceries"},
	} {
		if w := f.req(c[0], c[1], "text", "y"); w.Code != 404 {
			t.Errorf("%s %s: %d, want 404", c[0], c[1], w.Code)
		}
	}
	after := snapshot(t, filepath.Dir(f.dir))
	if _, ok := before[above]; !ok || !equalMaps(before, after) {
		t.Errorf("files changed: %d before, %d after", len(before), len(after))
	}
	// generated ids pass the guard
	if w := f.req("POST", "/lists/todo/items/t1/done"); w.Code != 200 {
		t.Errorf("t1 done: %d", w.Code)
	}
	if w := f.req("POST", "/lists/"+model.NewID(now)+"/items", "text", "y"); w.Code != 404 {
		t.Errorf("unknown generated id: %d", w.Code)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestBadFilesAreSkipped(t *testing.T) {
	for _, bad := range []string{"{", "", "null", "42", "{}", `{"id":"bad","text":"bad","at":"2026-09-21T12:00:00Z","items":[null]}`, `{"id":"bad","text":"bad","at":"2026-09-21T12:00:00Z","items":[{"id":"c","text":"c","items":[]}]}`, `{"id":"bad","text":"bad","at":"2026-09-21T12:00:00Z","items":{}}`} {
		f := mk(t)
		if err := os.WriteFile(filepath.Join(f.dir, "lists", "bad.json"), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, u := range []string{"/?list=bad", "/app?list=bad", "/app?list=bad&q=x", "/app?list=todo"} {
			w := f.req("GET", u)
			if w.Code != 200 || strings.Contains(w.Body.String(), `data-id="bad"`) {
				t.Errorf("%s with %q: %d, shown %v", u, bad, w.Code, strings.Contains(w.Body.String(), `data-id="bad"`))
			}
		}
		if got := ids(f.text("GET", "/app?list=todo"))[1:]; !slices.Equal(got, []string{"t1", "t2", "t3", "t4"}) {
			t.Errorf("todo with %q = %q", bad, got)
		}
	}
	// a Syncthing conflict copy is never read
	f := mk(t)
	b, _ := json.Marshal(&model.Item{ID: "ghost", Text: "ghost", At: now, Items: []*model.Item{}})
	if err := os.WriteFile(filepath.Join(f.dir, "lists", "todo.sync-conflict-20261001-120000-ABCDEF.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if h := f.text("GET", "/"); strings.Contains(h, "ghost") {
		t.Error("conflict copy shown")
	}
}

func TestOverlappingEditsKeepEveryOne(t *testing.T) {
	f := mk(t)
	n := len(f.list("todo").Items)
	var wg sync.WaitGroup
	for _, s := range []string{"a", "b", "c"} {
		wg.Go(func() { f.text("POST", "/lists/todo/items", "text", s) })
	}
	wg.Wait()
	got := texts(f.list("todo").Items[n:])
	slices.Sort(got)
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("after parallel adds: %q", got)
	}
	wg.Go(func() { f.text("POST", "/lists/todo/items/t1/done") })
	wg.Go(func() { f.text("DELETE", "/lists/todo/items/t2") })
	wg.Wait()
	y := f.list("todo")
	if c := model.Chain(y, "t1"); c == nil || c[len(c)-1].Done.IsZero() {
		t.Error("t1 not done")
	}
	if model.Chain(y, "t2") != nil {
		t.Error("t2 still there")
	}
}

func TestAutofocusAndEscaping(t *testing.T) {
	f := mk(t)
	if strings.Contains(f.text("GET", "/?list=todo"), "autofocus") {
		t.Error("full page load must not autofocus")
	}
	for _, c := range [][2]string{{"GET", "/app?list=todo"}, {"GET", "/lists/todo/items/t1/add"}} {
		if !strings.Contains(f.text(c[0], c[1]), `required autofocus>`) {
			t.Errorf("%s: no autofocus", c[1])
		}
	}
	if !strings.Contains(f.text("POST", "/lists/todo/items", "text", "x"), `required autofocus>`) {
		t.Error("swap: no autofocus")
	}
	h := f.text("POST", "/lists/todo/items", "text", "<script>", "note", "<b>")
	if strings.Contains(h, "<script>alert") || strings.Contains(h, `value="<`) || !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("unescaped input: %s", h)
	}
}
