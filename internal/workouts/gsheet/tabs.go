package gsheet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Tab describes one sheet in the spreadsheet.
type Tab struct {
	Title   string
	SheetID int
	Rows    int
	Cols    int
}

// Tabs lists all sheets in the spreadsheet.
func (c *Client) Tabs(ctx context.Context) ([]Tab, error) {
	b, err := c.request(ctx, "GET", API+"/"+url.PathEscape(c.SheetID)+"?fields=sheets.properties", nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Sheets []struct {
			Properties struct {
				Title string `json:"title"`
				ID    int    `json:"sheetId"`
				Grid  struct {
					Rows int `json:"rowCount"`
					Cols int `json:"columnCount"`
				} `json:"gridProperties"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	tabs := make([]Tab, 0, len(data.Sheets))
	for _, s := range data.Sheets {
		rows, cols := s.Properties.Grid.Rows, s.Properties.Grid.Cols
		if rows == 0 {
			rows = 1000
		}
		if cols == 0 {
			cols = 26
		}
		tabs = append(tabs, Tab{Title: s.Properties.Title, SheetID: s.Properties.ID, Rows: rows, Cols: cols})
	}
	return tabs, nil
}

func (c *Client) tabID(ctx context.Context, title string) (int, error) {
	tabs, err := c.Tabs(ctx)
	if err != nil {
		return 0, err
	}
	for _, t := range tabs {
		if t.Title == title {
			return t.SheetID, nil
		}
	}
	return 0, fmt.Errorf("no tab named %q", title)
}

// AddTab creates a tab; header (comma-separated) is written and bolded if set.
func (c *Client) AddTab(ctx context.Context, name, header string) (int, error) {
	b, err := c.request(ctx, "POST", API+"/"+url.PathEscape(c.SheetID)+":batchUpdate", map[string]any{
		"requests": []any{map[string]any{
			"addSheet": map[string]any{"properties": map[string]any{"title": name}},
		}},
	})
	if err != nil {
		return 0, err
	}
	var data struct {
		Replies []struct {
			AddSheet struct {
				Properties struct {
					ID int `json:"sheetId"`
				} `json:"properties"`
			} `json:"addSheet"`
		} `json:"replies"`
	}
	if err := json.Unmarshal(b, &data); err != nil || len(data.Replies) == 0 {
		return 0, fmt.Errorf("addSheet: unexpected reply %s", b)
	}
	gid := data.Replies[0].AddSheet.Properties.ID
	if header == "" {
		return gid, nil
	}
	cells := strings.Split(header, ",")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	if err := c.Write(ctx, RangeAt(name, 1, len(cells), 1), [][]string{cells}); err != nil {
		return gid, err
	}
	fmtReq := map[string]any{
		"repeatCell": map[string]any{
			"range": map[string]any{"sheetId": gid, "startRowIndex": 0, "endRowIndex": 1, "startColumnIndex": 0, "endColumnIndex": len(cells)},
			"cell": map[string]any{
				"userEnteredFormat": map[string]any{
					"backgroundColor": map[string]any{"red": 0.9, "green": 0.9, "blue": 0.9},
					"textFormat":      map[string]any{"bold": true},
				},
			},
			"fields": "userEnteredFormat(backgroundColor,textFormat)",
		},
	}
	return gid, c.batchUpdate(ctx, []any{fmtReq})
}

// RenameTab renames a tab, keeping its values, formatting and merges.
func (c *Client) RenameTab(ctx context.Context, old, name string) error {
	gid, err := c.tabID(ctx, old)
	if err != nil {
		return err
	}
	return c.batchUpdate(ctx, []any{map[string]any{
		"updateSheetProperties": map[string]any{
			"properties": map[string]any{"sheetId": gid, "title": name},
			"fields":     "title",
		},
	}})
}

// DelTab removes a tab by name.
func (c *Client) DelTab(ctx context.Context, name string) error {
	gid, err := c.tabID(ctx, name)
	if err != nil {
		return err
	}
	return c.batchUpdate(ctx, []any{map[string]any{"deleteSheet": map[string]any{"sheetId": gid}}})
}

// Checkbox adds BOOLEAN data validation (checkboxes) to a column range.
func (c *Client) Checkbox(ctx context.Context, tab, col string, start, end int) error {
	gid, err := c.tabID(ctx, tab)
	if err != nil {
		return err
	}
	colN := colNumber(col)
	req := map[string]any{
		"setDataValidation": map[string]any{
			"range": map[string]any{
				"sheetId": gid, "startRowIndex": start - 1, "endRowIndex": end,
				"startColumnIndex": colN - 1, "endColumnIndex": colN,
			},
			"rule": map[string]any{
				"condition":    map[string]any{"type": "BOOLEAN", "values": []any{}},
				"showCustomUi": true,
			},
		},
	}
	return c.batchUpdate(ctx, []any{req})
}

// colNumber turns a column letter ("A", "AB") into its 1-based number.
func colNumber(col string) int {
	n := 0
	for i := 0; i < len(col); i++ {
		n = n*26 + int(col[i]-'A'+1)
	}
	return n
}

// Notes reads the cell notes of rows cells of one column, starting at row
// start (1-based). The result always has rows entries; "" means no note.
func (c *Client) Notes(ctx context.Context, tab, col string, start, rows int) ([]string, error) {
	rng := fmt.Sprintf("%s!%s%d:%s%d", quote(tab), col, start, col, start+rows-1)
	u := API + "/" + url.PathEscape(c.SheetID) + "?ranges=" + url.QueryEscape(rng) +
		"&fields=" + url.QueryEscape("sheets.data.rowData.values.note")
	b, err := c.request(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Sheets []struct {
			Data []struct {
				RowData []struct {
					Values []struct {
						Note string `json:"note"`
					} `json:"values"`
				} `json:"rowData"`
			} `json:"data"`
		} `json:"sheets"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	out := make([]string, rows)
	if len(data.Sheets) > 0 && len(data.Sheets[0].Data) > 0 {
		for i, r := range data.Sheets[0].Data[0].RowData {
			if i < rows && len(r.Values) > 0 {
				out[i] = r.Values[0].Note
			}
		}
	}
	return out, nil
}

// SetNotes writes cell notes down one column from row start (1-based), one
// per entry; "" clears a cell's note. Cell values are left alone.
func (c *Client) SetNotes(ctx context.Context, tab, col string, start int, notes []string) error {
	if len(notes) == 0 {
		return nil
	}
	gid, err := c.tabID(ctx, tab)
	if err != nil {
		return err
	}
	rows := make([]any, len(notes))
	for i, n := range notes {
		rows[i] = map[string]any{"values": []any{map[string]any{"note": n}}}
	}
	return c.batchUpdate(ctx, []any{map[string]any{
		"updateCells": map[string]any{
			"start":  map[string]any{"sheetId": gid, "rowIndex": start - 1, "columnIndex": colNumber(col) - 1},
			"rows":   rows,
			"fields": "note",
		},
	}})
}
