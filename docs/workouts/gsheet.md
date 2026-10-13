# gsheet

Google Sheets client `internal/workouts/gsheet` and the CLI `cmd/gsheet` (`bin/gsheet` from `make build`). OAuth2 loopback flow with one refresh token per account, or a service account. `internal/workouts/block` is the only consumer.

```sh
gsheet auth setup <client_id> <client_secret>   # once; or GSHEET_CLIENT_ID / GSHEET_CLIENT_SECRET
gsheet auth login                               # prints a URL and code
gsheet auth list                                # * = default
gsheet auth default <email>
gsheet auth revoke <email>

gsheet open                                      # fzf a spreadsheet, every tab as local TSV in nvim
gsheet list
gsheet meta <sheet>                              # tabs: title, sheetId, grid
gsheet pull <sheet> 'Sheet1!A1:D'                # TSV to stdout
printf 'a\tb\n' | gsheet push <sheet> 'Sheet1!A1:B'
gsheet addtab <sheet> UpperB --header Exercise,Sets,Reps,Weight
gsheet rmtab <sheet> UpperB
gsheet checkbox <sheet> SessX E 2 40
```

Every sheet command takes `--account <email>`. `<sheet>` is the ID from `https://docs.google.com/spreadsheets/d/<ID>/edit`.

| What | Where |
|---|---|
| OAuth desktop client | `~/.config/gsheet/client.json` (Google Cloud project `gsheet-quota`, free) |
| Tokens | `~/.config/gsheet/tokens.json` |
| `open` files | `~/.local/share/gsheet/<name>/<tab>.tsv` (`$GSHEET_DIR`), mapping in `links.json`; `gsheet push` writes back |

`push` replaces the range's values. On a range open to the last row (`A1:Z` or a bare tab) it first inserts and deletes whole rows to match, so unchanged rows keep formatting and notes; bounded ranges are cleared and rewritten. Reads are `FORMATTED_VALUE`, writes `RAW`; cells with tabs or newlines are not supported. `list` needs the Drive scope: rerun `auth login` after an auth error.

```go
c := gsheet.New("SPREADSHEET_ID", gsheet.WithAccount("me@x.com"))
tabs, _ := c.Tabs(ctx)
grid, _ := c.Read(ctx, "UpperA", 4, 1000)
c.Write(ctx, gsheet.RangeAt("Sess X", 2, 5, 8), rows)
c.AddTab(ctx, "Upper B", "Exercise,Sets,Reps,Weight")
c.DelTab(ctx, "Upper B")
c.Checkbox(ctx, "Sess X", "E", 2, 40)
sps, _ := c.List(ctx)
```

## Test

`go test ./internal/workouts/gsheet/ ./cmd/gsheet/` runs against a fake HTTP server. Live, against a scratch spreadsheet shared as editor (creates and deletes its own tab):

```sh
GSHEET_LIVE=1 GSHEET_TEST_SHEET=<id> go test -run TestLiveSheets -v ./internal/workouts/gsheet/
infisical run -- env GSHEET_LIVE=1 GSHEET_TEST_SHEET=<id> go test -run TestLiveSheets -v ./internal/workouts/gsheet/
```

Auth is `GSHEET_SERVICE_PRINCIPAL_JSON` (wins) or the token store.
