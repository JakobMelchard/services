package block_test

import (
	"context"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/JakobMelchard/services/internal/workouts/block"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// TestLiveBlock runs two blocks on real scratch spreadsheets. Opt-in so
// `go test` stays network-free:
//
//	GSHEET_LIVE=1 GSHEET_TEST_PROGRAM=<id> GSHEET_TEST_SHEET=<id> GSHEET_TEST_ARCHIVE=<id> \
//	  go test -run TestLiveBlock -v ./internal/workouts/block/
//
// All three must be scratch sheets: the test writes block tabs w41-42 and
// w43 to the program, rewrites the active sheet's week and Block tabs, and
// writes "<year> w41"/"<year> w42" to the archive. Auth as in gsheet.
func TestLiveBlock(t *testing.T) {
	if os.Getenv("GSHEET_LIVE") != "1" {
		t.Skip("GSHEET_LIVE!=1")
	}
	ids := map[string]string{}
	for _, k := range []string{"GSHEET_TEST_PROGRAM", "GSHEET_TEST_SHEET", "GSHEET_TEST_ARCHIVE"} {
		if ids[k] = os.Getenv(k); ids[k] == "" {
			t.Fatalf("%s is required", k)
		}
	}
	program, active, archive := gsheet.New(ids["GSHEET_TEST_PROGRAM"]), gsheet.New(ids["GSHEET_TEST_SHEET"]), gsheet.New(ids["GSHEET_TEST_ARCHIVE"])
	ctx := context.Background()

	for tab, rows := range map[string][][]string{
		"w41-42": {{"MONDAY (OHP)", ""}, {"OHP", "2 x 8|6 / 60kg"}, {"", ""}, {"SUNDAY (BIKE)", ""}, {"Bike", "90-150'"}},
		"w43":    {{"MONDAY", ""}, {"Squat", "1 x 5"}},
	} {
		program.DelTab(ctx, tab)
		if _, err := program.AddTab(ctx, tab, ""); err != nil {
			t.Fatal(err)
		}
		defer program.DelTab(ctx, tab)
		if err := program.Write(ctx, gsheet.RangeAt(tab, 1, 2, len(rows)), rows); err != nil {
			t.Fatal(err)
		}
	}

	// Scratch sheets: drop week and archive tabs earlier runs left behind.
	for _, sc := range []*gsheet.Client{active, archive} {
		tabs, err := sc.Tabs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, tb := range tabs {
			if weekish.MatchString(tb.Title) {
				sc.DelTab(ctx, tb.Title)
			}
		}
	}
	st := block.Store{Program: program, Active: active, Archive: archive}
	first, err := st.Next(ctx, "w41-42")
	if err != nil {
		t.Fatalf("first block: %v", err)
	}
	if !reflect.DeepEqual(first.Weeks, []string{"w41", "w42"}) || first.Program != "w41-42" {
		t.Fatalf("block = %+v", first)
	}
	if tabs, _ := active.Tabs(ctx); hasTab(tabs, "w41") || hasTab(tabs, "w42") {
		t.Fatalf("week tabs written before any day started: %v", tabs)
	}
	loads := func(v *block.View) (out []string) {
		for _, r := range v.Rows {
			s := r.Day + " " + r.Exercise + " " + r.Set + ": " + r.Load + " " + r.Notes
			if r.Planned {
				s += "(planned)"
			}
			out = append(out, s)
		}
		return
	}
	v, err := st.Week(ctx, "w42")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loads(v), []string{"MONDAY (OHP) OHP 1: 1x6 60kg (planned)", "MONDAY (OHP) OHP 2: 1x6 60kg (planned)", "SUNDAY (BIKE) Bike 1: 1x90-150m (planned)"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("w42 = %q, want %q", got, want)
	}

	// Editing a set of Monday starts Monday: only Monday lands in w41.
	notes := "grind #pr"
	if _, err := st.Edit(ctx, "w41", "MONDAY (OHP)", func(v block.View) ([]block.Row, error) {
		return block.Apply(v.Rows, block.Ref{Index: 1, Day: "MONDAY (OHP)", Exercise: "OHP", Set: "2"}, block.Patch{Weight: ptr("62.5kg"), Notes: &notes})
	}); err != nil {
		t.Fatal(err)
	}
	grid, err := active.Read(ctx, "w41", 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"MONDAY (OHP)"}, {"OHP", "1x8 60kg"}, {"OHP", "1x8 62.5kg"}}; !reflect.DeepEqual(grid, want) {
		t.Fatalf("w41 grid = %q, want %q", grid, want)
	}

	// A plan change still reaches Sunday (not started), not Monday.
	if err := program.Write(ctx, "w41-42!B2", [][]string{{"2 x 10 / 60kg"}}); err != nil {
		t.Fatal(err)
	}
	if err := program.Write(ctx, "w41-42!B5", [][]string{{"60'"}}); err != nil {
		t.Fatal(err)
	}
	v, err = st.Week(ctx, "w41")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loads(v), []string{"MONDAY (OHP) OHP 1: 1x8 60kg ", "MONDAY (OHP) OHP 2: 1x8 62.5kg " + notes, "SUNDAY (BIKE) Bike 1: 1x60m (planned)"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("w41 after plan change = %q, want %q", got, want)
	}

	// Add an exercise to Sunday: Sunday starts with the plan plus it.
	if _, err := st.Edit(ctx, "w41", "SUNDAY (BIKE)", func(v block.View) ([]block.Row, error) {
		return block.AddExercise(v.Rows, v.Days, "SUNDAY (BIKE)", "Hangboard", "2 x 10''")
	}); err != nil {
		t.Fatal(err)
	}
	v, _ = st.Week(ctx, "w41")
	if got := loads(v); len(got) != 5 || got[3] != "SUNDAY (BIKE) Hangboard 1: 1x10s " {
		t.Fatalf("w41 after add = %q", got)
	}

	second, err := st.Next(ctx, "w43")
	if err != nil {
		t.Fatalf("second block: %v", err)
	}
	if second.Program != "w43" || !reflect.DeepEqual(second.Weeks, []string{"w43"}) {
		t.Fatalf("second = %+v", second)
	}
	arch := block.ArchiveTab(first.Started, "w41")
	defer archive.DelTab(ctx, arch)
	logged, err := block.ReadSets(ctx, archive, arch)
	if err != nil {
		t.Fatalf("read %s: %v", arch, err)
	}
	if len(logged) != 5 || logged[1].Notes != notes {
		t.Errorf("%s = %+v, want the logged w41", arch, logged)
	}
	if tabs, _ := archive.Tabs(ctx); hasTab(tabs, block.ArchiveTab(first.Started, "w42")) {
		t.Errorf("w42 was never started but got archived")
	}
	if tabs, _ := active.Tabs(ctx); hasTab(tabs, "w41") {
		t.Errorf("w41 still on the active sheet")
	}
}

var weekish = regexp.MustCompile(`^(\d{4} )?[wW]\d+$`)

func hasTab(tabs []gsheet.Tab, name string) bool {
	for _, t := range tabs {
		if t.Title == name {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }
