package gsheet

import (
	"context"
	"strings"
)

// RowOp inserts or deletes the rows [Start, End) of a tab, 0-based.
type RowOp struct {
	Start, End int
	Insert     bool
}

// DiffRows returns the row inserts and deletes that line old up with new,
// so a following write keeps each unchanged row's formatting and notes on
// that row. Positions are in old's coordinates, ascending; rows edited in
// place need no op. Apply them with EditRows.
func DiffRows(old, new [][]string) []RowOp {
	key := func(r []string) string { return strings.TrimRight(strings.Join(r, "\t"), "\t") }
	a, b := make([]string, len(old)), make([]string, len(new))
	for i, r := range old {
		a[i] = key(r)
	}
	for i, r := range new {
		b[i] = key(r)
	}
	// Trim the common prefix and suffix, then LCS on the middle.
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	a, b = a[p:len(a)-s], b[p:len(b)-s]
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var ops []RowOp
	// gap turns unmatched old rows [i0, i) and new rows [j0, j) into ops:
	// paired rows are edits, only the surplus is inserted or deleted.
	gap := func(i0, i, j0, j int) {
		switch d, e := i-i0, j-j0; {
		case d > e:
			ops = append(ops, RowOp{Start: p + i0 + e, End: p + i})
		case e > d:
			ops = append(ops, RowOp{Start: p + i, End: p + i + e - d, Insert: true})
		}
	}
	i, j, i0, j0 := 0, 0, 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			gap(i0, i, j0, j)
			i, j = i+1, j+1
			i0, j0 = i, j
		case lcs[i+1][j] >= lcs[i][j+1]:
			i++
		default:
			j++
		}
	}
	gap(i0, n, j0, m)
	return ops
}

// EditRows applies ops from DiffRows to tab in one batch, last op first so
// earlier positions stay valid. offset shifts every op, e.g. by the range's
// start row minus one.
func (c *Client) EditRows(ctx context.Context, tab string, offset int, ops []RowOp) error {
	if len(ops) == 0 {
		return nil
	}
	gid, err := c.tabID(ctx, tab)
	if err != nil {
		return err
	}
	reqs := make([]any, 0, len(ops))
	for _, op := range ops {
		kind := "deleteDimension"
		if op.Insert {
			kind = "insertDimension"
		}
		rng := map[string]any{"sheetId": gid, "dimension": "ROWS", "startIndex": offset + op.Start, "endIndex": offset + op.End}
		reqs = append([]any{map[string]any{kind: map[string]any{"range": rng}}}, reqs...)
	}
	return c.batchUpdate(ctx, reqs)
}
