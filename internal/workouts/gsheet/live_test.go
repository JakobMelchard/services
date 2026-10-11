package gsheet_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// TestLiveSheets exercises the full API against a real spreadsheet. Gated by
// env so the default `go test` stays network-free:
//
//	GSHEET_LIVE=1 GSHEET_TEST_SHEET=<id> go test -run TestLiveSheets -v
//
// Uses the real token store (~/.config/gsheet/tokens.json); auth is a
// prerequisite. Creates and deletes its own tab, so the sheet needs edit
// permission but no fixed structure.
func TestLiveSheets(t *testing.T) {
	if os.Getenv("GSHEET_LIVE") != "1" {
		t.Skip("GSHEET_LIVE!=1; set GSHEET_LIVE=1 and GSHEET_TEST_SHEET=<id> to run")
	}
	sheet := os.Getenv("GSHEET_TEST_SHEET")
	if sheet == "" {
		t.Fatal("GSHEET_TEST_SHEET is required")
	}
	c := gsheet.New(sheet)
	ctx := context.Background()

	tab := fmt.Sprintf("e2e %s", time.Now().Format("2006-01-02 15:04:05"))

	// create + header
	gid, err := c.AddTab(ctx, tab, "Exercise,Sets,Reps,Weight,Done")
	if err != nil {
		t.Fatalf("AddTab: %v", err)
	}
	defer func() {
		if err := c.DelTab(ctx, tab); err != nil {
			t.Errorf("DelTab: %v", err)
		}
	}()
	if gid == 0 {
		t.Error("AddTab returned gid 0")
	}

	// write rows at A2
	rows := [][]string{
		{"Deadlift", "1", "8-9", "100", "TRUE"},
		{"Deadlift", "2", "8-9", "100", "TRUE"},
		{"Leg Ext", "1", "10-12", "40", "FALSE"},
	}
	if err := c.Write(ctx, gsheet.RangeAt(tab, 2, 5, 3), rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// checkbox validation on E2:E4
	if err := c.Checkbox(ctx, tab, "E", 2, 4); err != nil {
		t.Fatalf("Checkbox: %v", err)
	}

	// read back
	grid, err := c.Read(ctx, tab, 5, 10)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(grid) != 4 {
		t.Fatalf("expected 4 rows (header + 3), got %d: %v", len(grid), grid)
	}
	if grid[0][0] != "Exercise" || grid[1][0] != "Deadlift" {
		t.Fatalf("unexpected grid: %v", grid)
	}
	if grid[1][4] != "TRUE" || grid[3][4] != "FALSE" {
		t.Fatalf("done column wrong: %v", grid)
	}

	// cell notes on B2:B4: set two, read three, clear one
	if err := c.SetNotes(ctx, tab, "B", 2, []string{"felt #easy", "", "last"}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
	notes, err := c.Notes(ctx, tab, "B", 2, 4)
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if want := []string{"felt #easy", "", "last", ""}; fmt.Sprint(notes) != fmt.Sprint(want) {
		t.Fatalf("Notes = %q, want %q", notes, want)
	}
	if err := c.SetNotes(ctx, tab, "B", 2, []string{""}); err != nil {
		t.Fatalf("SetNotes clear: %v", err)
	}
	if notes, _ = c.Notes(ctx, tab, "B", 2, 1); notes[0] != "" {
		t.Fatalf("note not cleared: %q", notes)
	}

	// row diff: drop row 2, the note on row 3 moves up with its row
	if err := c.SetNotes(ctx, tab, "A", 3, []string{"keep"}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
	next := [][]string{grid[0], grid[2], grid[3]}
	if err := c.EditRows(ctx, tab, 0, gsheet.DiffRows(grid, next)); err != nil {
		t.Fatalf("EditRows: %v", err)
	}
	if notes, _ = c.Notes(ctx, tab, "A", 2, 1); notes[0] != "keep" {
		t.Fatalf("note after EditRows = %q, want keep", notes)
	}
	if g, _ := c.Read(ctx, tab, 5, 10); fmt.Sprint(g) != fmt.Sprint(next) {
		t.Fatalf("after EditRows = %v, want %v", g, next)
	}

	// rename and back
	if err := c.RenameTab(ctx, tab, tab+" r"); err != nil {
		t.Fatalf("RenameTab: %v", err)
	}
	if g, err := c.Read(ctx, tab+" r", 1, 1); err != nil || g[0][0] != "Exercise" {
		t.Fatalf("renamed tab read: %v %v", g, err)
	}
	if err := c.RenameTab(ctx, tab+" r", tab); err != nil {
		t.Fatalf("RenameTab back: %v", err)
	}

	// tab listed
	tabs, err := c.Tabs(ctx)
	if err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	found := false
	for _, tt := range tabs {
		if tt.Title == tab {
			found = true
		}
	}
	if !found {
		t.Fatalf("tab %q not in %v", tab, tabs)
	}
}
