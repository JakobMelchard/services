package block

import (
	"errors"
	"reflect"
	"testing"
)

func grid() ([][]string, []string) {
	g := [][]string{
		{"Mon", ""},
		{"Squat", "1x5 100kg @8 3m"},
		{"Squat", "1x5 100kg @8 3m"},
		{"Plank", "1x30s+"},
		{"", ""},
		{"Thu", ""},
		{"Squat", "1x5 80%"},
	}
	notes := []string{"", "felt #easy, #Depth ok", "", "", "", "", ""}
	return g, notes
}

func ref(rows []Row, i int) Ref {
	return Ref{Index: i, Day: rows[i].Day, Exercise: rows[i].Exercise, Set: rows[i].Set}
}

func ptr[T any](v T) *T { return &v }

func sets(rs []Row) (s [][3]string) {
	for _, r := range rs {
		s = append(s, [3]string{r.Day, r.Exercise, r.Set})
	}
	return
}

func TestParseWeek(t *testing.T) {
	rows := ParseWeek(grid())
	want := [][3]string{{"Mon", "Squat", "1"}, {"Mon", "Squat", "2"}, {"Mon", "Plank", "1"}, {"Thu", "Squat", "1"}}
	if !reflect.DeepEqual(sets(rows), want) {
		t.Fatalf("sets = %v, want %v", sets(rows), want)
	}
	r := rows[0]
	if r.Reps != "5" || r.Weight != "100kg" || r.RPE != "8" || r.Rest != "3m" || r.Invalid != "" {
		t.Errorf("fields = %+v", r)
	}
	if want := []string{"easy", "depth"}; r.Notes != "felt #easy, #Depth ok" || !reflect.DeepEqual(r.Tags, want) {
		t.Errorf("notes %q tags %v", r.Notes, r.Tags)
	}
	if rows[3].Weight != "80%" {
		t.Errorf("Thu squat = %+v", rows[3])
	}
}

func TestParseWeekFlagsNonSets(t *testing.T) {
	rows := ParseWeek([][]string{{"Mon", ""}, {"Squat", "3x5 100kg"}, {"Row", "bodyweight"}}, nil)
	for _, r := range rows {
		if r.Invalid == "" || r.Reps != "" {
			t.Errorf("%+v not flagged", r)
		}
	}
}

func TestApplyFieldsRebuildLoad(t *testing.T) {
	rows := ParseWeek(grid())
	out, err := Apply(rows, ref(rows, 1), Patch{Reps: ptr(" 6r"), Weight: ptr("102.50kg"), RPE: ptr("@9"), Rest: ptr("180s"), Notes: ptr("grind #pr")})
	if err != nil {
		t.Fatal(err)
	}
	r := out[1]
	if r.Load != "1x6 102.5kg @9 3m" || r.Reps != "6" || r.RPE != "9" || r.Invalid != "" {
		t.Errorf("row = %+v", r)
	}
	if !reflect.DeepEqual(r.Tags, []string{"pr"}) {
		t.Errorf("tags = %v", r.Tags)
	}
	if rows[1].Reps != "5" {
		t.Error("Apply mutated its input")
	}
	// Clearing a field drops it from the load.
	out, _ = Apply(out, ref(out, 1), Patch{RPE: ptr("")})
	if out[1].Load != "1x6 102.5kg 3m" {
		t.Errorf("load = %q", out[1].Load)
	}
}

func TestApplyLoad(t *testing.T) {
	rows := ParseWeek(grid())
	out, _ := Apply(rows, ref(rows, 0), Patch{Load: ptr("1x5 @8 100kg 180s")})
	if out[0].Load != "1x5 100kg @8 3m" {
		t.Errorf("load not canonical: %q", out[0].Load)
	}
	out, _ = Apply(rows, ref(rows, 0), Patch{Weight: ptr("bodyweight")})
	if out[0].Load != "1x5 bodyweight @8 3m" || out[0].Invalid == "" {
		t.Errorf("row = %+v, want raw load kept with Invalid set", out[0])
	}
}

// An empty load would be written as a day header and regroup the following sets.
func TestApplyRejectsEmptyLoad(t *testing.T) {
	rows := ParseWeek(grid())
	if _, err := Apply(rows, ref(rows, 1), Patch{Load: ptr("  ")}); !errors.Is(err, ErrNoLoad) {
		t.Fatalf("err = %v, want ErrNoLoad", err)
	}
}

func TestDuplicateAndDeleteRenumberPerDay(t *testing.T) {
	rows := ParseWeek(grid())
	dup, err := Duplicate(rows, ref(rows, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := [][3]string{{"Mon", "Squat", "1"}, {"Mon", "Squat", "2"}, {"Mon", "Squat", "3"}, {"Mon", "Plank", "1"}, {"Thu", "Squat", "1"}}
	if !reflect.DeepEqual(sets(dup), want) {
		t.Fatalf("after duplicate %v, want %v", sets(dup), want)
	}
	if dup[1].Notes != "" || dup[1].Load != rows[0].Load {
		t.Errorf("copy = %+v, want same load, no notes", dup[1])
	}
	// Mon Plank gone: Thu Squat must not continue Mon's numbering.
	del, err := Delete(dup, ref(dup, 3))
	if err != nil {
		t.Fatal(err)
	}
	want = [][3]string{{"Mon", "Squat", "1"}, {"Mon", "Squat", "2"}, {"Mon", "Squat", "3"}, {"Thu", "Squat", "1"}}
	if !reflect.DeepEqual(sets(del), want) {
		t.Fatalf("after delete %v, want %v", sets(del), want)
	}
}

func TestStaleRef(t *testing.T) {
	rows := ParseWeek(grid())
	for _, r := range []Ref{
		{Index: 9, Day: "Mon", Exercise: "Squat", Set: "1"},
		{Index: 2, Day: "Mon", Exercise: "Squat", Set: "1"},
		{Index: 0, Day: "Mon", Exercise: "Squat", Set: "2"},
		{Index: 3, Day: "Mon", Exercise: "Squat", Set: "1"},
	} {
		if _, err := Delete(rows, r); !errors.Is(err, ErrStale) {
			t.Errorf("Delete(%+v) err = %v, want ErrStale", r, err)
		}
	}
}

func TestStaleLoad(t *testing.T) {
	rows := ParseWeek(grid())
	planned := append([]Row(nil), rows...)
	planned[0].Planned = true
	for _, tc := range []struct {
		name  string
		rows  []Row
		load  *string
		stale bool
	}{
		{"nil skips the check", rows, nil, false},
		{"equal", rows, ptr(rows[0].Load), false},
		{"changed", rows, ptr("1x5 90kg @8 3m"), true},
		{"empty differs", rows, ptr(""), true},
		{"planned equal", planned, ptr(planned[0].Load), false},
		{"planned changed", planned, ptr("1x3"), true},
	} {
		r := ref(tc.rows, 0)
		r.Load = tc.load
		for name, op := range map[string]func() error{
			"Apply":     func() error { _, err := Apply(tc.rows, r, Patch{Notes: ptr("x")}); return err },
			"Duplicate": func() error { _, err := Duplicate(tc.rows, r); return err },
			"Delete":    func() error { _, err := Delete(tc.rows, r); return err },
		} {
			if err := op(); errors.Is(err, ErrStale) != tc.stale || err != nil && !tc.stale {
				t.Errorf("%s %s: err = %v, stale want %v", tc.name, name, err, tc.stale)
			}
		}
	}
}

func TestGridRoundTrips(t *testing.T) {
	g, n := grid()
	rows := ParseWeek(g, n)
	cells, notes := Grid(rows, 9)
	if len(cells) != 9 || len(notes) != 9 {
		t.Fatalf("not padded: %d cells, %d notes", len(cells), len(notes))
	}
	if !reflect.DeepEqual(cells[:7], g) || !reflect.DeepEqual(notes[:7], n) {
		t.Errorf("grid = %q\nnotes = %q", cells, notes)
	}
	if again := ParseWeek(cells, notes); !reflect.DeepEqual(again, rows) {
		t.Errorf("round trip = %+v, want %+v", again, rows)
	}
}

func TestParseWeekKeepsLoadWithoutExercise(t *testing.T) {
	g := [][]string{{"Mon", ""}, {"", "1x5 100kg"}, {"Squat", "1x5 80kg"}}
	notes := []string{"", "typed in Sheets", ""}
	rows := ParseWeek(g, notes)
	if len(rows) != 2 || rows[0].Load != "1x5 100kg" || rows[0].Notes != "typed in Sheets" {
		t.Fatalf("rows = %+v", rows)
	}
	got, gotNotes := Grid(rows, 0)
	if !reflect.DeepEqual(got, g) || !reflect.DeepEqual(gotNotes, notes) {
		t.Errorf("grid = %v %v, want %v %v", got, gotNotes, g, notes)
	}
}

func TestWeekNumber(t *testing.T) {
	for tab, want := range map[string]int{"w1": 1, "w41": 41, "w0": 0, "2026 w41": 0, "Week 1": 0, "W1": 0, "w41-46": 0} {
		if n, _ := weekNumber(tab); n != want {
			t.Errorf("weekNumber(%q) = %d, want %d", tab, n, want)
		}
	}
}
