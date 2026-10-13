// Package block models training blocks across three spreadsheets:
//
//   - program: one tab per block, named by its calendar weeks ("w41-46"),
//     holding the routine of one week (see program.go)
//   - active: the current block, a tab per calendar week ("w41".."w46") with
//     every set of that week (see week.go), plus a "Block" tab naming the
//     program tab and the start date
//   - archive: finished blocks, their weeks copied in as "<year> w41" tabs
//
// Every tab has two columns. Done lives in the app only.
package block

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

const (
	// infoTab holds block metadata on the active sheet: A1:B1 "program", tab
	// and A2:B2 "started", date. It also keeps the sheet from ever having no
	// tabs.
	infoTab  = "Block"
	maxWeeks = 26
)

// ErrNotWeek means the tab is not a week tab of the active block.
var ErrNotWeek = errors.New("not a week tab")

var weekRe = regexp.MustCompile(`^w(\d+)$`)

func weekNumber(tab string) (int, bool) {
	m := weekRe.FindStringSubmatch(tab)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, n > 0
}

// WeekTab names calendar week n: "w41".
func WeekTab(n int) string { return "w" + strconv.Itoa(n) }

// Sheet is the part of a spreadsheet the block code uses; *gsheet.Client
// implements it. sheetSuite in sheet_test.go is the contract any other
// implementation must pass.
type Sheet interface {
	Tabs(ctx context.Context) ([]gsheet.Tab, error)
	Read(ctx context.Context, title string, cols, rows int) ([][]string, error)
	Write(ctx context.Context, rng string, values [][]string) error
	AddTab(ctx context.Context, name, header string) (int, error)
	DelTab(ctx context.Context, name string) error
	Notes(ctx context.Context, tab, col string, start, rows int) ([]string, error)
	SetNotes(ctx context.Context, tab, col string, start int, notes []string) error
}

var _ Sheet = (*gsheet.Client)(nil)

// Store is the three spreadsheets of a block.
type Store struct {
	Program, Active, Archive Sheet

	// mu serializes Edit and Next: both read the active sheet, then rewrite
	// whole tabs from what they read.
	mu sync.Mutex
}

// Info is the active block: the program tab it came from, when it started,
// its weeks (from the program tab's name) and Current, the week tab of
// today's calendar week when the block covers it. Program is "" before the
// first block.
type Info struct {
	Program string   `json:"program"`
	Started string   `json:"started"`
	Weeks   []string `json:"weeks"`
	Current string   `json:"current,omitempty"`
}

// Current reads the active block.
func (s *Store) Current(ctx context.Context) (*Info, error) {
	info, _, err := s.state(ctx)
	return info, err
}

// state reads the active block and the active sheet's week tabs (in week
// order) with two requests; callers pass them on instead of asking again,
// since Sheets reads are rate limited.
func (s *Store) state(ctx context.Context) (*Info, []string, error) {
	info := &Info{Weeks: []string{}}
	tabs, err := s.Active.Tabs(ctx)
	if err != nil {
		return nil, nil, err
	}
	var weeks []string
	for _, t := range tabs {
		if _, ok := weekNumber(t.Title); ok {
			weeks = append(weeks, t.Title)
		}
	}
	sort.Slice(weeks, func(i, j int) bool {
		a, _ := weekNumber(weeks[i])
		b, _ := weekNumber(weeks[j])
		return a < b
	})
	for _, t := range tabs {
		if t.Title != infoTab {
			continue
		}
		grid, err := s.Active.Read(ctx, infoTab, 2, 2)
		if err != nil {
			return nil, nil, err
		}
		if len(grid) > 0 {
			info.Program = cell(grid[0], 1)
		}
		if len(grid) > 1 {
			info.Started = cell(grid[1], 1)
		}
	}
	if weeks, ok := ProgramWeeks(info.Program); ok {
		info.Weeks = weeks
		_, now := time.Now().ISOWeek()
		for _, w := range weeks {
			if w == WeekTab(now) {
				info.Current = w
			}
		}
	}
	return info, weeks, nil
}

// Next archives the active block's week tabs (if any) and starts the block
// of program tab tab. No week tab is written yet: a day is written to its
// week tab when it starts (see View). A program with invalid rows changes
// nothing. Archive tabs are written before any week is removed from the
// active sheet. An existing archive tab is never replaced: the copy gets a
// " (2)" suffix, so a rerun after a partial failure leaves duplicates rather
// than losing an earlier block's week. Cancelling ctx stops the reads; once the first tab is
// written the rest follows regardless, so a client that goes away does not
// leave a block half archived.
func (s *Store) Next(ctx context.Context, tab string) (*Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := ReadProgram(ctx, s.Program, tab)
	if err != nil {
		return nil, err
	}
	if p.Errors > 0 {
		return nil, fmt.Errorf("%w: %s has %d rows that are not workline; fix them in the program sheet", ErrBadProgram, tab, p.Errors)
	}
	cur, logged, err := s.state(ctx)
	if err != nil {
		return nil, err
	}
	archived, err := s.Archive.Tabs(ctx)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, t := range archived {
		taken[t.Title] = true
	}
	ctx = context.WithoutCancel(ctx)
	for _, w := range logged {
		tab := ArchiveTab(cur.Started, w)
		for i := 2; taken[tab]; i++ {
			tab = fmt.Sprintf("%s (%d)", ArchiveTab(cur.Started, w), i)
		}
		if err := copyWeek(ctx, s.Active, s.Archive, w, tab); err != nil {
			return nil, fmt.Errorf("archive %s: %w", w, err)
		}
	}
	for _, w := range logged {
		if err := s.Active.DelTab(ctx, w); err != nil {
			return nil, fmt.Errorf("remove %s: %w", w, err)
		}
	}
	if len(logged) > 0 {
		if err := dropBlankDefaults(ctx, s.Archive); err != nil {
			return nil, err
		}
	}
	if err := writeTab(ctx, s.Active, infoTab, [][]string{
		{"program", tab},
		{"started", time.Now().Format("2006-01-02")},
	}); err != nil {
		return nil, err
	}
	if err := dropBlankDefaults(ctx, s.Active); err != nil {
		return nil, err
	}
	return s.Current(ctx)
}

// view reads week w: the week tab (if written yet) and the program. It also
// returns the week tab's row count, so a rewrite can clear what it replaces.
func (s *Store) view(ctx context.Context, w string) (*View, int, error) {
	info, tabs, err := s.state(ctx)
	if err != nil {
		return nil, 0, err
	}
	k := 0
	for i, x := range info.Weeks {
		if x == w {
			k = i + 1
		}
	}
	if k == 0 {
		return nil, 0, ErrNotWeek
	}
	var grid [][]string
	var notes []string
	for _, t := range tabs {
		if t == w {
			if grid, notes, err = readWeek(ctx, s.Active, w); err != nil {
				return nil, 0, err
			}
		}
	}
	logged := ParseWeek(grid, notes)
	var plan []Row
	var days []string
	warning := ""
	p, err := ReadProgram(ctx, s.Program, info.Program)
	switch {
	case errors.Is(err, ErrNotProgram):
		have := ""
		if tabs, e := Programs(ctx, s.Program); e != nil {
			log.Printf("program tabs: %v", e)
		} else if len(tabs) > 0 {
			have = " (it has " + strings.Join(tabs, ", ") + ")"
		}
		warning = fmt.Sprintf("program tab %s: %v%s; days not started are missing", info.Program, err, have)
	case err != nil:
		// err may carry the Sheets reply: it goes to the log, not the client.
		log.Printf("program tab %s: %v", info.Program, err)
		warning = fmt.Sprintf("program tab %s could not be read; days not started are missing", info.Program)
	default:
		plan = p.Week(k)
		for _, d := range p.Days {
			days = append(days, d.Name)
		}
	}
	v := Combine(logged, plan, days)
	v.Warning = warning
	return &v, len(grid), nil
}

// Week reads week w of the active block.
func (s *Store) Week(ctx context.Context, w string) (*View, error) {
	v, _, err := s.view(ctx, w)
	return v, err
}

// Edit applies op to week w's view and writes the started days to the week
// tab, creating it on first use. day is the day op acts on; it counts as
// started afterwards. Every row is rewritten, notes included, rather than
// shifting rows with gsheet.EditRows. Cancelling ctx stops the reads; the writes run
// to the end regardless, since a tab with its values written and its notes
// not has the notes on the wrong rows.
func (s *Store) Edit(ctx context.Context, w, day string, op func(View) ([]Row, error)) (*View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, n, err := s.view(ctx, w)
	if err != nil {
		return nil, err
	}
	rows, err := op(*v)
	if err != nil {
		return nil, err
	}
	rows = Start(rows, day)
	ctx = context.WithoutCancel(ctx)
	if n == 0 {
		if err := writeTab(ctx, s.Active, w, nil); err != nil {
			return nil, err
		}
	}
	cells, notes := Grid(Logged(rows), n)
	if err := writeRows(ctx, s.Active, w, cells, notes); err != nil {
		return nil, err
	}
	v.Rows = rows
	return v, nil
}

// ArchiveTab names week w of a block started in year: "2026 w41".
func ArchiveTab(started, w string) string {
	year := time.Now().Year()
	if t, err := time.Parse("2006-01-02", started); err == nil {
		year = t.Year()
	}
	return fmt.Sprintf("%d %s", year, w)
}

// copyWeek copies a week tab, values and notes, to a new tab on another sheet.
func copyWeek(ctx context.Context, from, to Sheet, w, tab string) error {
	grid, notes, err := readWeek(ctx, from, w)
	if err != nil {
		return err
	}
	return writeWeek(ctx, to, tab, grid, notes)
}

// writeWeek (re)creates a week tab with rows and Load notes.
func writeWeek(ctx context.Context, sc Sheet, tab string, rows [][]string, notes []string) error {
	if err := writeTab(ctx, sc, tab, nil); err != nil {
		return err
	}
	return writeRows(ctx, sc, tab, rows, notes)
}

// writeTab replaces tab (deleting any existing one) with rows from row 1.
func writeTab(ctx context.Context, sc Sheet, tab string, rows [][]string) (err error) {
	tabs, err := sc.Tabs(ctx)
	if err != nil {
		return err
	}
	for _, t := range tabs {
		if t.Title == tab {
			if len(tabs) == 1 {
				// A spreadsheet needs one tab; park a placeholder first.
				old := tab + " (old)"
				if _, err := sc.AddTab(ctx, old, ""); err != nil {
					return err
				}
				defer func() {
					if derr := sc.DelTab(ctx, old); derr != nil && err == nil {
						err = fmt.Errorf("remove placeholder %s: %w", old, derr)
					}
				}()
			}
			if err := sc.DelTab(ctx, tab); err != nil {
				return err
			}
		}
	}
	if _, err := sc.AddTab(ctx, tab, ""); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return sc.Write(ctx, gsheet.RangeAt(tab, 1, len(rows[0]), len(rows)), rows)
}

var defaultTabRe = regexp.MustCompile(`^(Sheet|Tabelle|Blatt)\d+$`)

// dropBlankDefaults removes the empty "Sheet1" a new spreadsheet comes with,
// once the sheet has other tabs. Tabs with any value are kept.
func dropBlankDefaults(ctx context.Context, sc Sheet) error {
	tabs, err := sc.Tabs(ctx)
	if err != nil || len(tabs) < 2 {
		return err
	}
	for _, t := range tabs {
		if !defaultTabRe.MatchString(t.Title) {
			continue
		}
		grid, err := sc.Read(ctx, t.Title, t.Cols, t.Rows)
		if err != nil {
			return err
		}
		if len(grid) == 0 {
			if err := sc.DelTab(ctx, t.Title); err != nil {
				return err
			}
		}
	}
	return nil
}
