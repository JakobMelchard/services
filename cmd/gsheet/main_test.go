package main

import (
	"flag"
	"slices"
	"testing"
)

func TestSplitRange(t *testing.T) {
	cases := []struct {
		rng, tab string
		start    int
		open     bool
	}{
		{"Sheet1", "Sheet1", 0, true},
		{"Sheet1!A1:Z", "Sheet1", 0, true},
		{"'Joe''s tab'!A5:C", "Joe's tab", 4, true},
		{"Sheet1!A1:Z10", "Sheet1", 0, false},
		{"Sheet1!B3", "Sheet1", 2, false},
		{"Sheet1!A:C", "Sheet1", 0, true},
	}
	for _, c := range cases {
		tab, start, open := splitRange(c.rng)
		if tab != c.tab || start != c.start || open != c.open {
			t.Errorf("splitRange(%q) = %q %d %v, want %q %d %v", c.rng, tab, start, open, c.tab, c.start, c.open)
		}
	}
}

func TestParse(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	account := fs.String("account", "", "")
	header := fs.String("header", "", "")
	pos := parse(fs, []string{"--account", "a@b", "sheet", "tab", "--header", "x,y"})
	if *account != "a@b" || *header != "x,y" || !slices.Equal(pos, []string{"sheet", "tab"}) {
		t.Errorf("parse = %q, account %q, header %q", pos, *account, *header)
	}
}
