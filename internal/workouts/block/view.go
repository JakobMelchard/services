package block

import (
	"errors"
	"fmt"
	"strings"

	workline "github.com/JakobMelchard/workline/golang"
)

// A week is shown as its view: every day of the program's week in order, a
// started day with the sets logged in the week tab, a day not started yet
// with the sets the program prescribes for this week (Planned). Days only in
// the week tab (added in the sheet) follow the program's days. Editing a
// set of a planned day, or starting it, writes the whole day to the week
// tab; from then on the program no longer changes it.

// View is one week as the app shows it. Warning is set when the program tab
// could not be read, so planned days are missing.
type View struct {
	Days    []string `json:"days"`
	Rows    []Row    `json:"rows"`
	Warning string   `json:"warning,omitempty"`
}

// Combine builds a week's view from its logged rows (the week tab), the
// program's rows for the week and the program's days in order.
func Combine(logged, plan []Row, days []string) View {
	started := map[string]bool{}
	for _, r := range logged {
		started[r.Day] = true
	}
	v := View{Days: []string{}, Rows: []Row{}}
	seen := map[string]bool{}
	add := func(d string) {
		if seen[d] {
			return
		}
		seen[d] = true
		v.Days = append(v.Days, d)
		src := plan
		if started[d] {
			src = logged
		}
		for _, r := range src {
			if r.Day == d {
				r.Planned = !started[d]
				v.Rows = append(v.Rows, r)
			}
		}
	}
	for _, d := range days {
		add(d)
	}
	for _, r := range logged {
		add(r.Day)
	}
	Renumber(v.Rows)
	return v
}

// Start marks day as started: its rows are no longer planned.
func Start(rows []Row, day string) []Row {
	out := append([]Row(nil), rows...)
	for i := range out {
		if out[i].Day == day {
			out[i].Planned = false
		}
	}
	return out
}

// Logged is the rows of started days, as the week tab holds them.
func Logged(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if !r.Planned {
			out = append(out, r)
		}
	}
	return out
}

// AddExercise appends exercise to day, one row per set of load (a workline
// cell, e.g. "3x10 20kg"), after the day's last set. A day without sets
// (a rest day) gets them where the day falls in days.
func AddExercise(rows []Row, days []string, day, exercise, load string) ([]Row, error) {
	exercise = strings.TrimSpace(exercise)
	if exercise == "" {
		return nil, errors.New("exercise required")
	}
	known := false
	for _, d := range days {
		known = known || d == day
	}
	if !known {
		return nil, fmt.Errorf("no day %q in this week", day)
	}
	lines, err := parseLoad(load)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errors.New("load required, e.g. 3x10 20kg")
	}
	var sets []Row
	for _, l := range lines {
		for _, s := range workline.Expand(l) {
			r := Row{Day: day, Exercise: exercise, Load: setOf(s).Line()}
			annotate(&r)
			sets = append(sets, r)
		}
	}
	at := insertAt(rows, days, day)
	out := append([]Row(nil), rows[:at]...)
	out = append(out, sets...)
	out = append(out, rows[at:]...)
	return Renumber(out), nil
}

// insertAt is the index after day's last row, or, for a day without rows,
// after the last row of the days before it.
func insertAt(rows []Row, days []string, day string) int {
	before := map[string]bool{}
	for _, d := range days {
		if d == day {
			break
		}
		before[d] = true
	}
	at := 0
	for i, r := range rows {
		if r.Day == day || before[r.Day] {
			at = i + 1
		}
	}
	return at
}
