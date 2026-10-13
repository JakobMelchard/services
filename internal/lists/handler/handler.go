// Package handler is the app: routes, the per-request model and the htmx
// markup. Every mutation re-renders the whole #app; the only partial swap is
// the sub form appended to the closest li.
package handler

import (
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/JakobMelchard/services/internal/lists/model"
	"github.com/JakobMelchard/services/internal/lists/store"
	"github.com/JakobMelchard/services/internal/lists/ui"
	"github.com/JakobMelchard/web"
)

type app struct {
	db  store.Dir
	tpl *template.Template
	now func() time.Time
	mu  sync.Mutex // one mutation at a time: read-modify-write of a list file
}

// Model is what the page and the #app fragment render.
type Model struct {
	Lists  []*model.Item
	List   *model.Item
	ListID string
	Items  []*model.Item // the selected list's entries, filtered and sorted
	Q      string
	Now    time.Time
	Focus  bool // autofocus the add field: after a swap, not on a full page load
}

type node struct {
	L   string
	X   *model.Item
	Now time.Time
	Sev int
}

type addForm struct {
	L, Parent string
	Focus     bool
}

// New serves the lists under dir. now is the clock, time.Now in production.
func New(dir string, now func() time.Time) (http.Handler, error) {
	tpl, err := template.New("").Funcs(template.FuncMap{
		"sorted": model.Sorted,
		"levels": func() []int { return []int{1, 2, 3} },
		"node":   func(l string, x *model.Item, now time.Time) node { return node{l, x, now, model.Sev(x, now)} },
		"add":    func(l, parent string, focus bool) addForm { return addForm{l, parent, focus} },
	}).ParseFS(ui.Templates, "templates/*.html")
	if err != nil {
		return nil, err
	}
	a := &app{db: store.Dir(dir), tpl: tpl, now: now}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.page)
	mux.HandleFunc("GET /app", a.fragment)
	mux.HandleFunc("POST /lists", a.newList)
	mux.HandleFunc("POST /lists/{l}/copy", guard(a.copyList))
	mux.HandleFunc("DELETE /lists/{l}", guard(a.delList))
	mux.HandleFunc("POST /lists/{l}/items", guard(a.mutate(a.addItem)))
	mux.HandleFunc("POST /lists/{l}/items/{id}", guard(a.mutate(a.patch)))
	mux.HandleFunc("POST /lists/{l}/items/{id}/done", guard(a.mutate(a.done)))
	mux.HandleFunc("POST /lists/{l}/items/{id}/copy", guard(a.mutate(a.copyItem)))
	mux.HandleFunc("DELETE /lists/{l}/items/{id}", guard(a.mutate(a.delItem)))
	mux.HandleFunc("GET /lists/{l}/items/{id}/add", guard(a.subForm))

	static, err := fs.Sub(ui.Static, "static")
	if err != nil {
		return nil, err
	}
	// One segment, so no directory listing and no path below static/.
	mux.HandleFunc("GET /static/{file}", func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, static, r.PathValue("file")) })
	dist, err := fs.Sub(web.FS, "dist")
	if err != nil {
		return nil, err
	}
	mux.HandleFunc("GET /web/{file}", func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, dist, r.PathValue("file")) })
	// The service worker must be served from / to control the whole app.
	mux.HandleFunc("GET /sw.js", rootFile(static, "sw.js", "text/javascript"))
	// Under /static so the manifest's relative icon paths resolve to the icons.
	manifest := web.Manifest(map[string]string{"name": "lists", "short_name": "lists", "description": "Todo and grocery lists as one nested tree", "start_url": "/", "scope": "/"})
	mux.HandleFunc("GET /static/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(manifest)
	})
	return mux, nil
}

// guard answers 404 for a {l} or {id} outside the id shape before the handler
// builds a file name from it.
func guard(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, v := range []string{r.PathValue("l"), r.PathValue("id")} {
			if v != "" && !model.ID.MatchString(v) {
				http.NotFound(w, r)
				return
			}
		}
		fn(w, r)
	}
}

func rootFile(fsys fs.FS, name, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := w.Write(b); err != nil {
			log.Printf("%s: %v", name, err)
		}
	}
}

// load builds the Model for list id (the form's `list` when empty), or the
// first list by name.
func (a *app) load(r *http.Request, id string) (Model, error) {
	lists, err := a.db.All()
	if err != nil {
		return Model{}, err
	}
	if id == "" {
		id = r.FormValue("list")
	}
	m := Model{Lists: lists, Q: r.FormValue("q"), Now: a.now(), Focus: true}
	for _, x := range lists {
		if x.ID == id {
			m.List = x
		}
	}
	if m.List == nil && len(lists) > 0 {
		m.List = lists[0]
	}
	if m.List != nil {
		m.ListID = m.List.ID
		if f := model.Filter(m.List, m.Q, m.Now); f != nil {
			m.Items = model.Sorted(f.Items, m.Now)
		}
	}
	return m, nil
}

func (a *app) render(w http.ResponseWriter, name string, m Model) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tpl.ExecuteTemplate(w, name, m); err != nil {
		log.Printf("%s: %v", name, err)
	}
}

// show renders #app on list id after a change; id empty picks the first list.
func (a *app) show(w http.ResponseWriter, r *http.Request, id string) {
	m, err := a.load(r, id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.render(w, "app", m)
}

func (a *app) page(w http.ResponseWriter, r *http.Request) {
	m, err := a.load(r, "")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	m.Focus = false
	a.render(w, "page", m)
}

func (a *app) fragment(w http.ResponseWriter, r *http.Request) { a.show(w, r, "") }

// level is the stored urgency: 1 is the default and stays unstored.
func level(s string) int {
	if s == "2" || s == "3" {
		return int(s[0] - '0')
	}
	return 0
}

// item is a new item from the posted text, note and sev.
func (a *app) item(r *http.Request) *model.Item {
	now := a.now()
	return &model.Item{ID: model.NewID(now), Text: strings.TrimSpace(r.FormValue("text")), Note: strings.TrimSpace(r.FormValue("note")), Sev: level(r.FormValue("sev")), At: now, SevAt: now, Items: []*model.Item{}}
}

func (a *app) newList(w http.ResponseWriter, r *http.Request) {
	x := a.item(r)
	if x.Text == "" {
		http.Error(w, "name required", 400)
		return
	}
	if err := a.db.Put(x); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.show(w, r, x.ID)
}

func (a *app) copyList(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	x, err := a.db.Get(r.PathValue("l"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	y := model.Clone(x, a.now())
	y.Text += " copy"
	if err := a.db.Put(y); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.show(w, r, y.ID)
}

func (a *app) delList(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.db.Del(r.PathValue("l")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.show(w, r, "")
}

// errBad marks a refused edit; its text is the 400 body.
type errBad string

func (e errBad) Error() string { return string(e) }

// mutate wraps a read-modify-write of list {l}: fn edits x in place and
// returns fs.ErrNotExist for an unknown item or an errBad to refuse. Edits run
// one at a time; another process writing the same file still races.
func (a *app) mutate(fn func(r *http.Request, x *model.Item) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		x, err := a.db.Get(r.PathValue("l"))
		if err == nil {
			err = r.ParseForm()
		}
		if err == nil {
			err = fn(r, x)
		}
		var bad errBad
		switch {
		case errors.As(err, &bad):
			http.Error(w, string(bad), 400)
			return
		case err != nil:
			http.NotFound(w, r)
			return
		}
		if err := a.db.Put(x); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		a.show(w, r, x.ID)
	}
}

// pair is the parent and the item {id}, both nil when unknown.
func pair(r *http.Request, x *model.Item) (p, t *model.Item) {
	c := model.Chain(x, r.PathValue("id"))
	if len(c) < 2 {
		return nil, nil
	}
	return c[len(c)-2], c[len(c)-1]
}

func (a *app) addItem(r *http.Request, x *model.Item) error {
	parent := r.FormValue("parent")
	if parent == "" {
		parent = x.ID
	}
	c := model.Chain(x, parent)
	if c == nil {
		return fs.ErrNotExist
	}
	y := a.item(r)
	if y.Text == "" {
		return errBad("text required")
	}
	p := c[len(c)-1]
	p.Items = append(p.Items, y)
	return nil
}

// patch touches only the fields actually posted: name, note and urgency are
// each edited in place by their own control, and there is no edit form to put
// the others back. Emptying the note clears it.
func (a *app) patch(r *http.Request, x *model.Item) error {
	_, t := pair(r, x)
	if t == nil {
		return fs.ErrNotExist
	}
	f := r.Form
	if f.Has("text") {
		s := strings.TrimSpace(f.Get("text"))
		if s == "" {
			return errBad("text required")
		}
		t.Text = s
	}
	if f.Has("note") {
		t.Note = strings.TrimSpace(f.Get("note"))
	}
	if f.Has("sev") {
		t.Sev, t.SevAt = level(f.Get("sev")), a.now()
	}
	return nil
}

func (a *app) done(r *http.Request, x *model.Item) error {
	_, t := pair(r, x)
	if t == nil {
		return fs.ErrNotExist
	}
	if t.Done.IsZero() {
		t.Done = a.now()
	} else {
		t.Done = time.Time{}
	}
	return nil
}

func (a *app) copyItem(r *http.Request, x *model.Item) error {
	p, t := pair(r, x)
	if t == nil {
		return fs.ErrNotExist
	}
	p.Items = slices.Insert(p.Items, slices.Index(p.Items, t)+1, model.Clone(t, a.now()))
	return nil
}

func (a *app) delItem(r *http.Request, x *model.Item) error {
	p, t := pair(r, x)
	if t == nil {
		return fs.ErrNotExist
	}
	i := slices.Index(p.Items, t)
	p.Items = slices.Delete(p.Items, i, i+1)
	return nil
}

func (a *app) subForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tpl.ExecuteTemplate(w, "add", addForm{r.PathValue("l"), r.PathValue("id"), true}); err != nil {
		log.Printf("add: %v", err)
	}
}
