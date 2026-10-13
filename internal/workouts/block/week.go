package block

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
	workline "github.com/JakobMelchard/workline/golang"
)

// A week tab has the program's two columns, Exercise and Load, with one row
// per set, all days of the week in order. A row with an empty Load starts a
// day and names it; a blank row separates days:
//
//	MONDAY (OHP)  |
//	OHP           | 1x6       <- cell note "felt #easy"
//	OHP           | 1x6
//	              |
//	FRIDAY (SQUAT)|
//	Squat         | 1x6
//
// Load is one workline set. Notes on a set are the cell note of its Load
// cell; their #words are the set's tags. Done is not stored in the sheet.
const (
	weekCols = 2
	loadCol  = "B"
	maxRows  = 2000
)

// ErrStale means the addressed row no longer holds what the caller saw,
// usually because the tab was edited in the Sheets UI meanwhile.
var ErrStale = errors.New("row changed since it was loaded; reload the week")

// ErrNoLoad means an edit cleared a set's load; delete the set instead.
var ErrNoLoad = errors.New("load required")

var tagRe = regexp.MustCompile(`#([\p{L}\p{N}_-]+)`)

// Row is one set of a week tab. Load is the cell; Reps, Weight, RPE and Rest
// are its workline fields (empty when Load is not one valid set). Set is the
// set's number within its day and exercise, derived from the row order.
type Row struct {
	Day      string   `json:"day"`
	Exercise string   `json:"exercise"`
	Set      string   `json:"set"`
	Load     string   `json:"load"`
	Reps     string   `json:"reps"`
	Weight   string   `json:"weight"`
	RPE      string   `json:"rpe"`
	Rest     string   `json:"rest"`
	Notes    string   `json:"notes"`
	Tags     []string `json:"tags,omitempty"`
	// Invalid holds the workline error when Load is not one valid set. The
	// cell is kept as typed; the sheet stays the source of truth.
	Invalid string `json:"invalid,omitempty"`
	// Planned marks a set of a day not started yet: it comes from the
	// program, not the week tab, and is written there once the day starts.
	Planned bool `json:"planned,omitempty"`
}

// Patch is a partial row update; nil fields are left alone. Load replaces
// the cell as typed; Reps/Weight/RPE/Rest rebuild it from the fields.
type Patch struct {
	Load   *string `json:"load"`
	Reps   *string `json:"reps"`
	Weight *string `json:"weight"`
	RPE    *string `json:"rpe"`
	Rest   *string `json:"rest"`
	Notes  *string `json:"notes"`
}

// Ref addresses a set (0-based among the week's sets, day rows not counted)
// and carries what the caller saw there, so edits never land on a set that
// moved or changed. Load is the Load cell as the server rendered it; nil
// (older clients) skips that check.
type Ref struct {
	Index    int     `json:"index"`
	Day      string  `json:"day"`
	Exercise string  `json:"exercise"`
	Set      string  `json:"set"`
	Load     *string `json:"load,omitempty"`
}

// setOf splits an expanded workline set into its fields.
func setOf(s workline.SetSpec) Set {
	join := func(m workline.Mods) string { return strings.Join(workline.SerializeMods(m), " ") }
	return Set{
		Reps:   s.Target.String(),
		Weight: join(workline.Mods{Weight: s.Mods.Weight, Percent: s.Mods.Percent}),
		RPE:    strings.TrimPrefix(join(workline.Mods{RPE: s.Mods.RPE}), "@"),
		Rest:   join(workline.Mods{Rest: s.Mods.Rest}),
	}
}

// Line is the canonical one-set workline cell, e.g. "1x5 100kg @8 3m".
func (s Set) Line() string {
	parts := []string{"1x" + s.Reps}
	for _, p := range []string{s.Weight, prefixRPE(s.RPE), s.Rest} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

func prefixRPE(rpe string) string {
	if rpe = strings.TrimPrefix(strings.TrimSpace(rpe), "@"); rpe != "" {
		return "@" + rpe
	}
	return ""
}

// parseSet reads a cell as exactly one workline set.
func parseSet(load string) (Set, error) {
	if strings.TrimSpace(load) == "" {
		return Set{}, errors.New("load missing")
	}
	p := workline.Parse(load)
	if len(p.Errors) > 0 {
		return Set{}, errors.New(p.Errors[0].Reason)
	}
	if len(p.Lines) != 1 || p.Lines[0].Name != "" {
		return Set{}, errors.New("one set without a name expected")
	}
	sets := workline.Expand(p.Lines[0])
	if len(sets) != 1 {
		return Set{}, errors.New("one set expected, e.g. 1x5 100kg")
	}
	return setOf(sets[0]), nil
}

// ParseWeek reads a week grid and the Load cells' notes (notes[i] belongs
// to grid[i]) into sets.
func ParseWeek(grid [][]string, notes []string) []Row {
	var rows []Row
	day := ""
	for i, g := range grid {
		name, load := strings.TrimSpace(cell(g, 0)), strings.TrimSpace(cell(g, 1))
		if name == "" {
			continue
		}
		if load == "" {
			day = name
			continue
		}
		r := Row{Day: day, Exercise: name, Load: load}
		if i < len(notes) {
			r.Notes = notes[i]
		}
		annotate(&r)
		rows = append(rows, r)
	}
	return Renumber(rows)
}

// annotate derives the set fields and Invalid from Load, and Tags from Notes.
func annotate(r *Row) {
	r.Tags = tagsOf(r.Notes)
	r.Invalid = ""
	s, err := parseSet(r.Load)
	if err != nil {
		r.Invalid = err.Error()
	}
	r.Reps, r.Weight, r.RPE, r.Rest = s.Reps, s.Weight, s.RPE, s.Rest
}

// tagsOf returns the lowercased #words of notes, nil when there are none.
func tagsOf(notes string) []string {
	var tags []string
	for _, m := range tagRe.FindAllStringSubmatch(notes, -1) {
		tags = append(tags, strings.ToLower(m[1]))
	}
	return tags
}

// Apply updates the set at ref with p. A load that is one valid workline set
// is stored canonical; anything else as typed (Row.Invalid says why).
func Apply(rows []Row, ref Ref, p Patch) ([]Row, error) {
	if err := check(rows, ref); err != nil {
		return nil, err
	}
	r := rows[ref.Index]
	if p.Load != nil {
		if r.Load = strings.TrimSpace(*p.Load); r.Load == "" {
			return nil, ErrNoLoad
		}
	}
	if p.Reps != nil || p.Weight != nil || p.RPE != nil || p.Rest != nil {
		s := Set{r.Reps, r.Weight, r.RPE, r.Rest}
		for _, f := range []struct{ v, dst *string }{{p.Reps, &s.Reps}, {p.Weight, &s.Weight}, {p.RPE, &s.RPE}, {p.Rest, &s.Rest}} {
			if f.v != nil {
				*f.dst = strings.TrimSpace(*f.v)
			}
		}
		r.Load = s.Line()
	}
	if s, err := parseSet(r.Load); err == nil {
		r.Load = s.Line()
	}
	if p.Notes != nil {
		r.Notes = *p.Notes
	}
	annotate(&r)
	out := append([]Row(nil), rows...)
	out[ref.Index] = r
	return out, nil
}

// Duplicate inserts a copy of the set at ref right after it, without notes,
// and renumbers sets.
func Duplicate(rows []Row, ref Ref) ([]Row, error) {
	if err := check(rows, ref); err != nil {
		return nil, err
	}
	c := rows[ref.Index]
	c.Notes, c.Tags = "", nil
	out := append([]Row(nil), rows[:ref.Index+1]...)
	out = append(out, c)
	out = append(out, rows[ref.Index+1:]...)
	return Renumber(out), nil
}

// Delete removes the set at ref and renumbers sets.
func Delete(rows []Row, ref Ref) ([]Row, error) {
	if err := check(rows, ref); err != nil {
		return nil, err
	}
	out := append([]Row(nil), rows[:ref.Index]...)
	return Renumber(append(out, rows[ref.Index+1:]...)), nil
}

// Renumber sets Set to 1..n within each run of consecutive rows of one
// exercise on one day.
func Renumber(rows []Row) []Row {
	n := 0
	for i := range rows {
		if i > 0 && rows[i].Day == rows[i-1].Day && rows[i].Exercise == rows[i-1].Exercise {
			n++
		} else {
			n = 1
		}
		rows[i].Set = strconv.Itoa(n)
	}
	return rows
}

func check(rows []Row, ref Ref) error {
	if ref.Index < 0 || ref.Index >= len(rows) {
		return ErrStale
	}
	r := rows[ref.Index]
	if r.Day != ref.Day || r.Exercise != ref.Exercise || r.Set != ref.Set || ref.Load != nil && r.Load != *ref.Load {
		return ErrStale
	}
	return nil
}

// Grid is sets as week cells, a day row before each day's first set and a
// blank row between days, and the Load notes aligned with them. Both are
// padded to minRows so a shorter write clears what it replaces.
func Grid(rows []Row, minRows int) ([][]string, []string) {
	var cells [][]string
	var notes []string
	for i, r := range rows {
		if i == 0 || r.Day != rows[i-1].Day {
			if i > 0 {
				cells = append(cells, []string{"", ""})
				notes = append(notes, "")
			}
			cells = append(cells, []string{r.Day, ""})
			notes = append(notes, "")
		}
		cells = append(cells, []string{r.Exercise, r.Load})
		notes = append(notes, r.Notes)
	}
	for len(cells) < minRows {
		cells = append(cells, []string{"", ""})
		notes = append(notes, "")
	}
	return cells, notes
}

// readWeek reads a week tab's grid and its Load notes.
func readWeek(ctx context.Context, sc Sheet, tab string) ([][]string, []string, error) {
	grid, err := sc.Read(ctx, tab, weekCols, maxRows)
	if err != nil {
		return nil, nil, err
	}
	var notes []string
	if len(grid) > 0 {
		if notes, err = sc.Notes(ctx, tab, loadCol, 1, len(grid)); err != nil {
			return nil, nil, err
		}
	}
	return grid, notes, nil
}

// writeRows writes cells and notes to tab from row 1.
func writeRows(ctx context.Context, sc Sheet, tab string, cells [][]string, notes []string) error {
	if len(cells) == 0 {
		return nil
	}
	if err := sc.Write(ctx, gsheet.RangeAt(tab, 1, weekCols, len(cells)), cells); err != nil {
		return err
	}
	return sc.SetNotes(ctx, tab, loadCol, 1, notes)
}

// ReadSets reads the sets of any tab in week layout, e.g. an archived week.
func ReadSets(ctx context.Context, sc Sheet, tab string) ([]Row, error) {
	grid, notes, err := readWeek(ctx, sc, tab)
	if err != nil {
		return nil, err
	}
	return ParseWeek(grid, notes), nil
}
