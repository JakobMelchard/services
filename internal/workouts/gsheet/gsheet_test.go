package gsheet

import "testing"

func TestColLetter(t *testing.T) {
	cases := map[int]string{1: "A", 26: "Z", 27: "AA", 52: "AZ", 53: "BA", 703: "AAA"}
	for in, want := range cases {
		if got := colLetter(in); got != want {
			t.Errorf("colLetter(%d) = %q, want %q", in, got, want)
		}
	}
}

// Without a home directory there is no token store: an error, not a
// ".config/gsheet" relative to wherever the process runs.
func TestConfigPathNeedsHome(t *testing.T) {
	t.Setenv("HOME", "")
	if p, err := New("x").configPath("tokens.json"); err == nil {
		t.Errorf("configPath = %q, want an error", p)
	}
	if p, err := New("x", WithConfigDir("/c")).configPath("tokens.json"); err != nil || p != "/c/tokens.json" {
		t.Errorf("configPath with a config dir = %q, %v", p, err)
	}
}

func TestNewExtractsSheetID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ABC", "ABC"},
		{"https://docs.google.com/spreadsheets/d/ABC/edit", "ABC"},
		{"https://docs.google.com/spreadsheets/d/ABC/edit#gid=0", "ABC"},
	}
	for _, c := range cases {
		if got := New(c.in).SheetID; got != c.want {
			t.Errorf("New(%q).SheetID = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRange(t *testing.T) {
	if got, want := Range("Sess X", 5, 10), "'Sess X'!A1:E10"; got != want {
		t.Errorf("Range = %q, want %q", got, want)
	}
	if got, want := RangeAt("Sess X", 2, 5, 8), "'Sess X'!A2:E9"; got != want {
		t.Errorf("RangeAt = %q, want %q", got, want)
	}
	if got, want := Range("Joe's", 1, 1), "'Joe''s'!A1:A1"; got != want {
		t.Errorf("Range = %q, want %q", got, want)
	}
}

func TestColumns(t *testing.T) {
	if got, want := Columns("Sess X", 5), "'Sess X'!A1:E"; got != want {
		t.Errorf("Columns = %q, want %q", got, want)
	}
	if got, want := Columns("Joe's", 1), "'Joe''s'!A1:A"; got != want {
		t.Errorf("Columns = %q, want %q", got, want)
	}
}

func TestCellString(t *testing.T) {
	cases := []struct {
		val  any
		want string
	}{
		{nil, ""},
		{true, "TRUE"},
		{false, "FALSE"},
		{float64(100), "100"},
		{float64(80.5), "80.5"},
		{"ohp", "ohp"},
	}
	for _, c := range cases {
		if got := cellString(c.val); got != c.want {
			t.Errorf("cellString(%v) = %q, want %q", c.val, got, c.want)
		}
	}
}

func TestColNumber(t *testing.T) {
	for col, want := range map[string]int{"A": 1, "B": 2, "Z": 26, "AA": 27, "AB": 28} {
		if got := colNumber(col); got != want {
			t.Errorf("colNumber(%q) = %d, want %d", col, got, want)
		}
	}
}
