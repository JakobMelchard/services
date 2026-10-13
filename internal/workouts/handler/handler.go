package handler

import (
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/JakobMelchard/services/internal/workouts/block"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// Handler serves the app against the three spreadsheets of a block (see
// package block): program, active and archive.
type Handler struct {
	store block.Store
	tmpl  *template.Template
	embed string
}

func New(program, active, archive *gsheet.Client, t *template.Template, embed string) *Handler {
	return &Handler{store: block.Store{Program: program, Active: active, Archive: archive}, tmpl: t, embed: embed}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if err := h.tmpl.ExecuteTemplate(w, "index.html", struct{ Embed string }{Embed: h.embed}); err != nil {
		log.Printf("index: %v", err)
	}
}

// maxBody caps a JSON request body; a row edit is a few hundred bytes.
const maxBody = 1 << 20

// decode reads the JSON body into v, answering 413 or 400 itself.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
	case err != nil:
		http.Error(w, "bad body: "+err.Error(), 400)
	}
	return err == nil
}

// fail answers 500 with a fixed text. err goes to the log only: it may
// carry the Sheets reply or a request URL with a spreadsheet ID.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	http.Error(w, "internal error; see the server log", 500)
}

type blockResp struct {
	Block    *block.Info `json:"block"`
	Programs []string    `json:"programs"`
}

// Block returns the active block and the program's block tabs.
func (h *Handler) Block(w http.ResponseWriter, r *http.Request) {
	info, err := h.store.Current(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	programs, err := block.Programs(r.Context(), h.store.Program)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, blockResp{Block: info, Programs: programs})
}

// NextBlock archives the active block and starts the one of program tab
// (form value program).
func (h *Handler) NextBlock(w http.ResponseWriter, r *http.Request) {
	tab := strings.TrimSpace(r.PostFormValue("program"))
	if tab == "" {
		http.Error(w, "program required", 400)
		return
	}
	info, err := h.store.Next(r.Context(), tab)
	switch {
	case errors.Is(err, block.ErrNotProgram):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, block.ErrBadProgram):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case err != nil:
		fail(w, r, err)
	default:
		writeJSON(w, info)
	}
}

// Program returns a block tab of the program with per-row workline errors.
func (h *Handler) Program(w http.ResponseWriter, r *http.Request) {
	tab, ok := tabParam(w, r)
	if !ok {
		return
	}
	p, err := block.ReadProgram(r.Context(), h.store.Program, tab)
	switch {
	case errors.Is(err, block.ErrNotProgram):
		http.Error(w, err.Error(), http.StatusNotFound)
	case err != nil:
		fail(w, r, err)
	default:
		writeJSON(w, p)
	}
}

// Rows returns a week's view: started days from the week tab, the rest as
// planned by the program.
func (h *Handler) Rows(w http.ResponseWriter, r *http.Request) {
	week, ok := tabParam(w, r)
	if !ok {
		return
	}
	v, err := h.store.Week(r.Context(), week)
	writeView(w, r, v, err)
}

type rowReq struct {
	Ref   block.Ref   `json:"ref"`
	Patch block.Patch `json:"patch"`
}

// EditRow applies a patch to one set.
func (h *Handler) EditRow(w http.ResponseWriter, r *http.Request) {
	h.rowOp(w, r, func(rows []block.Row, q rowReq) ([]block.Row, error) {
		return block.Apply(rows, q.Ref, q.Patch)
	})
}

// DuplicateRow inserts a copy of one set after it.
func (h *Handler) DuplicateRow(w http.ResponseWriter, r *http.Request) {
	h.rowOp(w, r, func(rows []block.Row, q rowReq) ([]block.Row, error) {
		return block.Duplicate(rows, q.Ref)
	})
}

// DeleteRow removes one set.
func (h *Handler) DeleteRow(w http.ResponseWriter, r *http.Request) {
	h.rowOp(w, r, func(rows []block.Row, q rowReq) ([]block.Row, error) {
		return block.Delete(rows, q.Ref)
	})
}

func (h *Handler) rowOp(w http.ResponseWriter, r *http.Request, op func([]block.Row, rowReq) ([]block.Row, error)) {
	week, ok := tabParam(w, r)
	if !ok {
		return
	}
	var q rowReq
	if !decode(w, r, &q) {
		return
	}
	v, err := h.store.Edit(r.Context(), week, q.Ref.Day, func(v block.View) ([]block.Row, error) { return op(v.Rows, q) })
	writeView(w, r, v, err)
}

// StartDay writes a planned day to the week tab as prescribed.
func (h *Handler) StartDay(w http.ResponseWriter, r *http.Request) {
	week, day, ok := weekDay(w, r)
	if !ok {
		return
	}
	v, err := h.store.Edit(r.Context(), week, day, func(v block.View) ([]block.Row, error) { return v.Rows, nil })
	writeView(w, r, v, err)
}

// AddExercise adds an exercise to a day: {"exercise": "Curl", "load": "3x10 20kg"}.
func (h *Handler) AddExercise(w http.ResponseWriter, r *http.Request) {
	week, day, ok := weekDay(w, r)
	if !ok {
		return
	}
	var q struct {
		Exercise string `json:"exercise"`
		Load     string `json:"load"`
	}
	if !decode(w, r, &q) {
		return
	}
	v, err := h.store.Edit(r.Context(), week, day, func(v block.View) ([]block.Row, error) {
		rows, err := block.AddExercise(v.Rows, v.Days, day, q.Exercise, q.Load)
		if err != nil {
			return nil, &badRequest{err}
		}
		return rows, nil
	})
	writeView(w, r, v, err)
}

// badRequest marks an error caused by the request body (400, not 500).
type badRequest struct{ error }

func weekDay(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	week, ok := tabParam(w, r)
	if !ok {
		return "", "", false
	}
	day := r.PathValue("day")
	if day == "" {
		http.Error(w, "bad day", 400)
		return "", "", false
	}
	return week, day, true
}

func writeView(w http.ResponseWriter, r *http.Request, v *block.View, err error) {
	var bad *badRequest
	switch {
	case errors.As(err, &bad):
		http.Error(w, bad.Error(), 400)
	case errors.Is(err, block.ErrNoLoad):
		http.Error(w, err.Error(), 400)
	case errors.Is(err, block.ErrStale):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, block.ErrNotWeek):
		http.Error(w, err.Error(), http.StatusNotFound)
	case err != nil:
		fail(w, r, err)
	default:
		writeJSON(w, v)
	}
}

func tabParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	tab := r.PathValue("tab")
	if tab == "" {
		http.Error(w, "bad tab", 400)
		return "", false
	}
	return tab, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}
