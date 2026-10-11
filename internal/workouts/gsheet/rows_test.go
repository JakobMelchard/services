package gsheet

import (
	"fmt"
	"strings"
	"testing"
)

func grid(rows string) [][]string {
	var g [][]string
	for _, r := range strings.Split(rows, " ") {
		if r != "" {
			g = append(g, strings.Split(r, ","))
		}
	}
	return g
}

func TestDiffRows(t *testing.T) {
	cases := []struct {
		old, new string
		want     []RowOp
	}{
		{"a b c", "a b c", nil},
		{"a b c", "a x c", nil},                     // edit in place
		{"a b c", "a c", []RowOp{{1, 2, false}}},    // delete
		{"a b c", "a b x c", []RowOp{{2, 3, true}}}, // insert
		{"a b c d", "b d x", []RowOp{{0, 1, false}, {2, 3, false}, {4, 5, true}}},
		{"a b c", "a x y z c", []RowOp{{2, 4, true}}}, // edit + 2 inserts
		{"a b c d", "a x d", []RowOp{{2, 3, false}}},  // edit + delete
		{"", "a b", []RowOp{{0, 2, true}}},
		{"a b", "", []RowOp{{0, 2, false}}},
	}
	for _, c := range cases {
		if got := DiffRows(grid(c.old), grid(c.new)); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("DiffRows(%q, %q) = %v, want %v", c.old, c.new, got, c.want)
		}
	}
}

// TestDiffRowsApply replays the ops on old and checks unchanged rows land
// where new has them.
func TestDiffRowsApply(t *testing.T) {
	old, new := grid("h a b c d e"), grid("h b x c e f g")
	rows := append([][]string{}, old...)
	ops := DiffRows(old, new)
	for i := len(ops) - 1; i >= 0; i-- {
		op := ops[i]
		if op.Insert {
			rows = append(rows[:op.Start], append(make([][]string, op.End-op.Start), rows[op.Start:]...)...)
		} else {
			rows = append(rows[:op.Start], rows[op.End:]...)
		}
	}
	if len(rows) != len(new) {
		t.Fatalf("%d rows after ops, want %d", len(rows), len(new))
	}
	for i, r := range rows {
		if r != nil && fmt.Sprint(r) != fmt.Sprint(new[i]) {
			t.Errorf("row %d = %v, want %v", i, r, new[i])
		}
	}
}
