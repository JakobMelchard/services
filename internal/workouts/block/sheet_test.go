package block_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/JakobMelchard/services/internal/workouts/block"
	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// memSheet is an in-memory block.Sheet. Like fakeSheets it covers writes that
// address a tab from row 1, which is all Store makes.
type memSheet struct {
	tabs []*memTab
}

type memTab struct {
	title string
	rows  [][]string
	notes []string
}

func (m *memSheet) find(title string) (*memTab, error) {
	for _, t := range m.tabs {
		if t.title == title {
			return t, nil
		}
	}
	return nil, fmt.Errorf("no tab %q", title)
}

func (m *memSheet) Tabs(context.Context) ([]gsheet.Tab, error) {
	out := make([]gsheet.Tab, len(m.tabs))
	for i, t := range m.tabs {
		out[i] = gsheet.Tab{Title: t.title, SheetID: i + 1}
	}
	return out, nil
}

func (m *memSheet) Read(_ context.Context, title string, _, rows int) ([][]string, error) {
	t, err := m.find(title)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(t.rows[:min(rows, len(t.rows))])
	for len(out) > 0 && strings.Join(out[len(out)-1], "") == "" {
		out = out[:len(out)-1]
	}
	return out, nil
}

func (m *memSheet) Write(_ context.Context, rng string, values [][]string) error {
	title, _, _ := strings.Cut(rng, "!")
	t, err := m.find(strings.ReplaceAll(strings.Trim(title, "'"), "''", "'"))
	if err != nil {
		return err
	}
	for i, row := range values {
		if i < len(t.rows) {
			t.rows[i] = row
		} else {
			t.rows = append(t.rows, row)
		}
	}
	return nil
}

func (m *memSheet) AddTab(_ context.Context, name, _ string) (int, error) {
	if _, err := m.find(name); err == nil {
		return 0, errors.New("a sheet with that name already exists")
	}
	m.tabs = append(m.tabs, &memTab{title: name})
	return len(m.tabs), nil
}

func (m *memSheet) DelTab(_ context.Context, name string) error {
	i := slices.IndexFunc(m.tabs, func(t *memTab) bool { return t.title == name })
	if i < 0 {
		return fmt.Errorf("no tab %q", name)
	}
	m.tabs = slices.Delete(m.tabs, i, i+1)
	return nil
}

func (m *memSheet) Notes(_ context.Context, tab, _ string, start, rows int) ([]string, error) {
	t, err := m.find(tab)
	if err != nil {
		return nil, err
	}
	out := make([]string, rows)
	for i := range out {
		if j := start - 1 + i; j < len(t.notes) {
			out[i] = t.notes[j]
		}
	}
	return out, nil
}

func (m *memSheet) SetNotes(_ context.Context, tab, _ string, start int, notes []string) error {
	t, err := m.find(tab)
	if err != nil {
		return err
	}
	for i, n := range notes {
		for len(t.notes) <= start-1+i {
			t.notes = append(t.notes, "")
		}
		t.notes[start-1+i] = n
	}
	return nil
}

// sheetSuite is the contract of block.Sheet: what Store relies on, run
// against every implementation. s may already hold other tabs.
func sheetSuite(t *testing.T, s block.Sheet) {
	ctx := t.Context()
	const tab = "suite"
	has := func(title string) bool {
		tabs, err := s.Tabs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(tabs, func(x gsheet.Tab) bool { return x.Title == title })
	}

	t.Run("add and list", func(t *testing.T) {
		if _, err := s.AddTab(ctx, tab, ""); err != nil {
			t.Fatal(err)
		}
		if !has(tab) {
			t.Fatal("added tab is not listed")
		}
	})
	t.Run("add twice fails", func(t *testing.T) {
		if _, err := s.AddTab(ctx, tab, ""); err == nil {
			t.Fatal("a second tab of the same name was created")
		}
	})
	t.Run("write then read", func(t *testing.T) {
		want := [][]string{{"a", "1"}, {"b", "2"}, {"c", "3"}}
		if err := s.Write(ctx, gsheet.RangeAt(tab, 1, 2, len(want)), want); err != nil {
			t.Fatal(err)
		}
		got, err := s.Read(ctx, tab, 2, 50)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(got, want, slices.Equal) {
			t.Fatalf("read %v, want %v", got, want)
		}
	})
	t.Run("notes round trip", func(t *testing.T) {
		if err := s.SetNotes(ctx, tab, "A", 1, []string{"", "felt heavy", ""}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Notes(ctx, tab, "A", 1, 4)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"", "felt heavy", "", ""}; !slices.Equal(got, want) {
			t.Fatalf("notes %q, want %q (always the rows asked for, \"\" for none)", got, want)
		}
	})
	t.Run("read missing tab fails", func(t *testing.T) {
		if _, err := s.Read(ctx, "missing", 2, 5); err == nil {
			t.Fatal("no error for a missing tab")
		}
	})
	t.Run("delete", func(t *testing.T) {
		if err := s.DelTab(ctx, tab); err != nil {
			t.Fatal(err)
		}
		if has(tab) {
			t.Fatal("deleted tab is still listed")
		}
	})
}

func TestSheetMemory(t *testing.T) { sheetSuite(t, &memSheet{}) }

// The real client over the fake Sheets API.
func TestSheetClient(t *testing.T) { sheetSuite(t, fakeStore(t).Archive) }
