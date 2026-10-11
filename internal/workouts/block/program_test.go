package block

import (
	"reflect"
	"strings"
	"testing"
)

// program is laid out like a block tab of the real program sheet: merged day
// rows (value in A only), blank separators, no header.
func program() [][]string {
	return [][]string{
		{"MONDAY (OHP)"},
		{"Push Press", "4 x 2"},
		{"OHP", "1 x 8|6|4|2+ "},
		{"Squat", "4 x 6-10+"},
		{},
		{"TUESDAY (REST)"},
		{},
		{"SUNDAY (BIKE)"},
		{"Bike", "90-150'"},
		{"Hangboard", "5 x 5-10''"},
	}
}

func TestProgramWeeks(t *testing.T) {
	for tab, want := range map[string][]string{
		"w41-46":     {"w41", "w42", "w43", "w44", "w45", "w46"},
		"W7":         {"w7"},
		"w46-41":     nil,
		"Sheet1":     nil,
		"w41-46 old": nil,
	} {
		got, ok := ProgramWeeks(tab)
		if !reflect.DeepEqual(got, want) || ok != (want != nil) {
			t.Errorf("ProgramWeeks(%q) = %v %v, want %v", tab, got, ok, want)
		}
	}
}

func TestParseProgram(t *testing.T) {
	p := ParseProgram("w41-46", program())
	if p.Errors != 0 || len(p.Weeks) != 6 {
		t.Fatalf("program = %+v", p)
	}
	var days []string
	for _, d := range p.Days {
		days = append(days, d.Name)
	}
	if want := []string{"MONDAY (OHP)", "TUESDAY (REST)", "SUNDAY (BIKE)"}; !reflect.DeepEqual(days, want) {
		t.Errorf("days = %v", days)
	}
}

func TestProgramWeekSteps(t *testing.T) {
	p := ParseProgram("w41-46", program())
	loads := func(k int) (out []string) {
		for _, r := range p.Week(k) {
			if r.Invalid != "" {
				t.Errorf("week %d %s set %s invalid: %s", k, r.Exercise, r.Set, r.Invalid)
			}
			if r.Exercise == "OHP" || r.Exercise == "Bike" || r.Exercise == "Hangboard" && r.Set == "1" {
				out = append(out, r.Day+" "+r.Exercise+" "+r.Set+": "+r.Load)
			}
		}
		return
	}
	if got, want := loads(1), []string{"MONDAY (OHP) OHP 1: 1x8+", "SUNDAY (BIKE) Bike 1: 1x90-150m", "SUNDAY (BIKE) Hangboard 1: 1x5-10s"}; !reflect.DeepEqual(got, want) {
		t.Errorf("week 1 = %q", got)
	}
	if got := loads(4)[0]; got != "MONDAY (OHP) OHP 1: 1x2+" {
		t.Errorf("week 4 OHP = %q", got)
	}
	if got := loads(6)[0]; got != "MONDAY (OHP) OHP 1: 1x2+" {
		t.Errorf("week 6 OHP = %q, want the last step repeated", got)
	}
	if n := len(p.Week(1)); n != 4+1+4+1+5 {
		t.Errorf("week 1 has %d sets", n)
	}
}

func TestParseProgramFlagsRows(t *testing.T) {
	p := ParseProgram("w31-35", [][]string{
		{"MONDAY"},
		{"Chin-Up", "3-4 x 8-9"},
		{"Run", "30-45' (4-6 x 15'' 90%)"},
		{"Dips", "Dips / 3x8"},
		{"Row", "3 x 10-12"},
	})
	if p.Errors != 3 {
		t.Fatalf("errors = %d, want 3: %+v", p.Errors, p.Days)
	}
	if r := p.Days[0].Rows[2]; !strings.Contains(r.Error, "name") {
		t.Errorf("name prefix not flagged: %+v", r)
	}
	if r := p.Days[0].Rows[3]; r.Error != "" {
		t.Errorf("valid row flagged: %+v", r)
	}
}
