package block

import (
	"reflect"
	"testing"
)

func plan() ([]Row, []string) {
	p := ParseProgram("w41-42", [][]string{
		{"MON"}, {"OHP", "2 x 8|6+ / 60kg"}, {},
		{"TUE (REST)"}, {},
		{"FRI"}, {"Squat", "1 x 5"},
	})
	var days []string
	for _, d := range p.Days {
		days = append(days, d.Name)
	}
	return p.Week(2), days
}

func view(v View) (out []string) {
	for _, r := range v.Rows {
		s := r.Day + " " + r.Exercise + " " + r.Set + ": " + r.Load
		if r.Planned {
			s += " (planned)"
		}
		out = append(out, s)
	}
	return
}

func TestCombinePlannedAndLogged(t *testing.T) {
	p, days := plan()
	// Nothing logged: the whole week is planned, week 2's step.
	v := Combine(nil, p, days)
	if want := []string{"MON OHP 1: 1x6+ 60kg (planned)", "MON OHP 2: 1x6+ 60kg (planned)", "FRI Squat 1: 1x5 (planned)"}; !reflect.DeepEqual(view(v), want) {
		t.Fatalf("view = %q", view(v))
	}
	if want := []string{"MON", "TUE (REST)", "FRI"}; !reflect.DeepEqual(v.Days, want) {
		t.Errorf("days = %v", v.Days)
	}
	// MON logged (one set, heavier): it replaces the plan for MON only. A
	// day only in the week tab follows the program's days.
	logged := ParseWeek([][]string{{"MON"}, {"OHP", "1x5 62.5kg"}, {}, {"SAT"}, {"Curl", "1x10"}}, nil)
	v = Combine(logged, p, days)
	want := []string{"MON OHP 1: 1x5 62.5kg", "FRI Squat 1: 1x5 (planned)", "SAT Curl 1: 1x10"}
	if !reflect.DeepEqual(view(v), want) {
		t.Errorf("view = %q, want %q", view(v), want)
	}
	if want := []string{"MON", "TUE (REST)", "FRI", "SAT"}; !reflect.DeepEqual(v.Days, want) {
		t.Errorf("days = %v", v.Days)
	}
}

func TestStartAndLogged(t *testing.T) {
	p, days := plan()
	v := Combine(nil, p, days)
	rows := Start(v.Rows, "FRI")
	if got := view(View{Rows: Logged(rows)}); !reflect.DeepEqual(got, []string{"FRI Squat 1: 1x5"}) {
		t.Errorf("logged = %q", got)
	}
	if !v.Rows[2].Planned {
		t.Error("Start mutated its input")
	}
}

func TestEditPlannedDayKeepsItsPlan(t *testing.T) {
	p, days := plan()
	v := Combine(nil, p, days)
	ref := Ref{Index: 1, Day: "MON", Exercise: "OHP", Set: "2"}
	rows, err := Apply(v.Rows, ref, Patch{Weight: ptr("62.5kg")})
	if err != nil {
		t.Fatal(err)
	}
	got := view(View{Rows: Logged(Start(rows, "MON"))})
	if want := []string{"MON OHP 1: 1x6+ 60kg", "MON OHP 2: 1x6+ 62.5kg"}; !reflect.DeepEqual(got, want) {
		t.Errorf("logged = %q, want %q", got, want)
	}
}

func TestAddExercise(t *testing.T) {
	p, days := plan()
	v := Combine(nil, p, days)
	rows, err := AddExercise(v.Rows, v.Days, "MON", "Curl", "2x10 20kg")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"MON OHP 1: 1x6+ 60kg (planned)", "MON OHP 2: 1x6+ 60kg (planned)",
		"MON Curl 1: 1x10 20kg", "MON Curl 2: 1x10 20kg",
		"FRI Squat 1: 1x5 (planned)",
	}
	if got := view(View{Rows: rows}); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q", got)
	}
	// A rest day gets sets between the days around it.
	rows, err = AddExercise(v.Rows, v.Days, "TUE (REST)", "Bike", "45'")
	if err != nil {
		t.Fatal(err)
	}
	if got := view(View{Rows: rows})[2]; got != "TUE (REST) Bike 1: 1x45m" {
		t.Errorf("rest day row = %q in %q", got, view(View{Rows: rows}))
	}
	for _, bad := range [][3]string{{"MON", "", "3x5"}, {"MON", "Curl", "3-4 x 5"}, {"SUN", "Curl", "3x5"}, {"MON", "Curl", ""}} {
		if _, err := AddExercise(v.Rows, v.Days, bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("AddExercise(%q) accepted", bad)
		}
	}
}
