// Package fakesheets is an in-memory stand-in for the Google Sheets API, for
// tests and the cmd/demo screenshot server. Never used against Google.
package fakesheets

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
)

type tab struct {
	id    int
	title string
	rows  [][]string
	notes []string
}

// Sheets is an in-memory Sheets API: spreadsheets by ID, each a list of tabs
// with values and notes. It covers the calls block.Store makes, all of which
// address a tab from row 1.
type Sheets struct {
	mu    sync.Mutex
	books map[string][]*tab
	ids   int
}

// New returns a fake without spreadsheets.
func New() *Sheets { return &Sheets{books: map[string][]*tab{}} }

// Add appends tab title to spreadsheet book, with its rows and the notes of
// its Load (column B) cells, one per row.
func (f *Sheets) Add(book, title string, rows [][]string, notes ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.add(book, title, rows, notes)
}

func (f *Sheets) add(book, title string, rows [][]string, notes []string) {
	f.ids++
	f.books[book] = append(f.books[book], &tab{id: f.ids, title: title, rows: rows, notes: notes})
}

func (f *Sheets) find(book, rng string) *tab {
	title, _, _ := strings.Cut(rng, "!")
	title = strings.ReplaceAll(strings.Trim(title, "'"), "''", "'")
	for _, t := range f.books[book] {
		if t.title == title {
			return t
		}
	}
	return nil
}

// ServeHTTP answers a Sheets API v4 request.
func (f *Sheets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	book, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v4/spreadsheets/"), "/")
	book, batch := strings.CutSuffix(book, ":batchUpdate")
	reply := func(v any) { json.NewEncoder(w).Encode(v) }
	switch {
	case batch:
		var q struct {
			Requests []struct {
				AddSheet    *struct{ Properties struct{ Title string } }
				DeleteSheet *struct{ SheetID int }
				UpdateCells *struct {
					Start struct{ SheetID, RowIndex int }
					Rows  []struct{ Values []struct{ Note string } }
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil || len(q.Requests) != 1 {
			http.Error(w, "bad batch", http.StatusBadRequest)
			return
		}
		byID := func(id int) int {
			return slices.IndexFunc(f.books[book], func(t *tab) bool { return t.id == id })
		}
		switch q := q.Requests[0]; {
		case q.AddSheet != nil:
			if f.find(book, q.AddSheet.Properties.Title) != nil {
				http.Error(w, "a sheet with that name already exists", http.StatusBadRequest)
				return
			}
			f.add(book, q.AddSheet.Properties.Title, nil, nil)
			reply(map[string]any{"replies": []any{map[string]any{"addSheet": map[string]any{"properties": map[string]any{"sheetId": f.ids}}}}})
		case q.DeleteSheet != nil:
			i := byID(q.DeleteSheet.SheetID)
			if i < 0 {
				http.Error(w, "no such sheet", http.StatusBadRequest)
				return
			}
			f.books[book] = slices.Delete(f.books[book], i, i+1)
			reply(struct{}{})
		case q.UpdateCells != nil:
			i := byID(q.UpdateCells.Start.SheetID)
			if i < 0 {
				http.Error(w, "no such sheet", http.StatusBadRequest)
				return
			}
			t := f.books[book][i]
			for j, row := range q.UpdateCells.Rows {
				for len(t.notes) <= q.UpdateCells.Start.RowIndex+j {
					t.notes = append(t.notes, "")
				}
				t.notes[q.UpdateCells.Start.RowIndex+j] = row.Values[0].Note
			}
			reply(struct{}{})
		}
	case rest == "values:batchUpdate":
		var q struct {
			Data []struct {
				Range  string
				Values [][]string
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil || len(q.Data) != 1 {
			http.Error(w, "bad write", http.StatusBadRequest)
			return
		}
		t := f.find(book, q.Data[0].Range)
		if t == nil {
			http.Error(w, "Unable to parse range", http.StatusBadRequest)
			return
		}
		for i, row := range q.Data[0].Values {
			if i < len(t.rows) {
				t.rows[i] = row
			} else {
				t.rows = append(t.rows, row)
			}
		}
		reply(struct{}{})
	case strings.HasPrefix(rest, "values/"):
		t := f.find(book, strings.TrimPrefix(rest, "values/"))
		if t == nil {
			http.Error(w, "Unable to parse range", http.StatusBadRequest)
			return
		}
		// Like Sheets, leave out trailing blank rows.
		rows := t.rows
		for len(rows) > 0 && strings.Join(rows[len(rows)-1], "") == "" {
			rows = rows[:len(rows)-1]
		}
		reply(map[string]any{"values": rows})
	case r.URL.Query().Has("ranges"):
		var rd []any
		if t := f.find(book, r.URL.Query().Get("ranges")); t != nil {
			for _, n := range t.notes {
				rd = append(rd, map[string]any{"values": []any{map[string]any{"note": n}}})
			}
		}
		reply(map[string]any{"sheets": []any{map[string]any{"data": []any{map[string]any{"rowData": rd}}}}})
	default:
		sheets := []any{}
		for _, t := range f.books[book] {
			sheets = append(sheets, map[string]any{"properties": map[string]any{"title": t.title, "sheetId": t.id}})
		}
		reply(map[string]any{"sheets": sheets})
	}
}

// Rewrite is a RoundTripper that sends every request to host (host:port of
// the fake's server) over plain http.
type Rewrite string

func (host Rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = "http", string(host)
	return http.DefaultTransport.RoundTrip(r)
}
