package block

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
	workline "github.com/JakobMelchard/workline/golang"
)

// The program spreadsheet holds one tab per block, named by the calendar
// weeks it covers ("w41-46", or "w41" for one week). A tab is the routine of
// one week in two columns, Exercise and Load, as in:
//
//	MONDAY (OHP)          (merged row: a day starts)
//	Push Press   | 4 x 2
//	OHP          | 1 x 8|6|4|2+
//	                      (blank rows are ignored)
//	TUESDAY (REST)
//
// A row with an empty Load starts a day. Load is a workline cell; "a|b|c"
// targets step through the block's weeks. Other tabs are ignored.

var programRe = regexp.MustCompile(`^[wW](\d+)(?:-(\d+))?$`)

// ErrNotProgram means the program sheet has no such block tab.
var ErrNotProgram = errors.New("no such block in the program")

// ErrBadProgram means a block tab has rows that are not workline, so the
// block cannot start.
var ErrBadProgram = errors.New("program not valid")

// Set is one prescribed set as workline fields: Reps is the target ("5",
// "8-12", "5+", "30s", "20m", "2km"), Weight the load ("100kg", "80%", "?+"),
// RPE the number without "@" ("8", "8+"), Rest "2m" or "90s".
type Set struct {
	Reps, Weight, RPE, Rest string
}

// Program is a block tab of the program sheet.
type Program struct {
	Tab   string       `json:"tab"`
	Weeks []string     `json:"weeks"`
	Days  []ProgramDay `json:"days"`
	// Errors counts rows whose Load is not workline; such a block cannot start.
	Errors int `json:"errors"`
}

// ProgramDay is one day of a block's week.
type ProgramDay struct {
	Name string       `json:"name"`
	Rows []ProgramRow `json:"rows"`
}

// ProgramRow is one exercise row; Error says why Load is not workline.
type ProgramRow struct {
	Exercise string `json:"exercise"`
	Load     string `json:"load"`
	Error    string `json:"error,omitempty"`
}

// ProgramWeeks names the week tabs of a block tab: "w41-46" is w41..w46.
func ProgramWeeks(tab string) ([]string, bool) {
	m := programRe.FindStringSubmatch(strings.TrimSpace(tab))
	if m == nil {
		return nil, false
	}
	from, _ := strconv.Atoi(m[1])
	to := from
	if m[2] != "" {
		to, _ = strconv.Atoi(m[2])
	}
	if from < 1 || to < from || to-from >= maxWeeks {
		return nil, false
	}
	var weeks []string
	for w := from; w <= to; w++ {
		weeks = append(weeks, WeekTab(w))
	}
	return weeks, true
}

// ParseProgram reads a block tab grid. Rows before the first day go to a day
// named after nothing ("").
func ParseProgram(tab string, grid [][]string) Program {
	weeks, _ := ProgramWeeks(tab)
	p := Program{Tab: tab, Weeks: weeks, Days: []ProgramDay{}}
	for _, row := range grid {
		name, load := strings.TrimSpace(cell(row, 0)), strings.TrimSpace(cell(row, 1))
		if name == "" {
			continue
		}
		if load == "" {
			p.Days = append(p.Days, ProgramDay{Name: name, Rows: []ProgramRow{}})
			continue
		}
		if len(p.Days) == 0 {
			p.Days = append(p.Days, ProgramDay{Rows: []ProgramRow{}})
		}
		r := ProgramRow{Exercise: name, Load: load}
		if _, err := parseLoad(load); err != nil {
			r.Error = err.Error()
			p.Errors++
		}
		d := &p.Days[len(p.Days)-1]
		d.Rows = append(d.Rows, r)
	}
	return p
}

// parseLoad parses a program Load cell. The Exercise column names the
// exercise, so a workline name prefix ("Bench / 3x5") is rejected.
func parseLoad(raw string) ([]workline.Line, error) {
	p := workline.Parse(raw)
	if len(p.Errors) > 0 {
		e := p.Errors[0]
		return nil, fmt.Errorf("line %d %q: %s", e.Line+1, strings.TrimSpace(e.Raw), e.Reason)
	}
	for _, l := range p.Lines {
		if l.Name != "" {
			return nil, fmt.Errorf("line %d: name %q not allowed; the Exercise column names the exercise", l.Index+1, l.Name)
		}
	}
	return p.Lines, nil
}

// Week expands the program for week k (1-based) of the block: one row per
// set, days in order. Every line of a multi-line Load belongs to its row.
func (p Program) Week(k int) []Row {
	var rows []Row
	for _, d := range p.Days {
		for _, pr := range d.Rows {
			lines, err := parseLoad(pr.Load)
			if err != nil {
				continue
			}
			for _, l := range lines {
				for _, s := range workline.ExpandWeek(l, k) {
					r := Row{Day: d.Name, Exercise: pr.Exercise, Load: setOf(s).Line()}
					annotate(&r)
					rows = append(rows, r)
				}
			}
		}
	}
	return Renumber(rows)
}

// Programs lists the program's block tabs in tab order.
func Programs(ctx context.Context, program Sheet) ([]string, error) {
	tabs, err := program.Tabs(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, t := range tabs {
		if _, ok := ProgramWeeks(t.Title); ok {
			out = append(out, t.Title)
		}
	}
	return out, nil
}

// ReadProgram reads one block tab of the program sheet with one request.
func ReadProgram(ctx context.Context, program Sheet, tab string) (*Program, error) {
	if _, ok := ProgramWeeks(tab); !ok {
		return nil, ErrNotProgram
	}
	grid, err := program.Read(ctx, tab, 2, maxRows)
	if err != nil {
		// A missing tab is a 400 "Unable to parse range".
		var ae *gsheet.APIError
		if errors.As(err, &ae) && ae.Status == http.StatusBadRequest {
			return nil, ErrNotProgram
		}
		return nil, err
	}
	p := ParseProgram(tab, grid)
	return &p, nil
}

func cell(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}
