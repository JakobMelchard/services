# gsheet

Google Sheets sync: the Go client package `internal/workouts/gsheet` and the
`gsheet` CLI (`cmd/gsheet`, built to `bin/gsheet` by `make build`).

- `internal/workouts/gsheet/gsheet.go` is the Go client for the Sheets v4 API; `cmd/gsheet` the
  CLI. OAuth2 loopback flow, one refresh token per account, or a service
  account. No gcloud needed at runtime, no costs.

Bidirectional: edit the local TSV then push; pull to refresh from the sheet.

## Auth

OAuth login (`auth setup` / `auth login`: PKCE loopback + browser, `Client.Login`).
Token files:

- `~/.config/gsheet/client.json` — OAuth desktop client id/secret
- `~/.config/gsheet/tokens.json` — per-account access/refresh tokens

## Quick start

```
gsheet open          # fzf: pick spreadsheet -> every tab as local TSV, open in nvim
gsheet list          # list accessible spreadsheets
gsheet               # same as `open` (no args)
```

`open` pulls each tab's grid into `~/.local/share/gsheet/<name>/<tab>.tsv`
(`$GSHEET_DIR` overrides the root), records the mapping in `links.json`, and
opens the files in nvim tabs (in the current nvim if `$NVIM` is set). Push a
file back with `gsheet push`.

Requires the Drive API (`drive.readonly`) for listing spreadsheets, plus the
Sheets API. If `list` returns an auth error, re-run `gsheet auth login` to pick
up the new scope.

## OAuth client (once)

Create a free OAuth client ID:

1. Google Cloud console -> `gsheet-quota` project -> APIs & Services ->
   Credentials -> Create Credentials -> OAuth client ID -> type Desktop app.
2. Copy the client ID and secret.

```
gsheet auth setup <client_id> <client_secret>
```

or set the env vars `GSHEET_CLIENT_ID` / `GSHEET_CLIENT_SECRET` and run via
Infisical:

```
infisical secrets set GSHEET_CLIENT_ID=<client_id> GSHEET_CLIENT_SECRET=<client_secret>
infisical run -- gsheet auth login
```

The Sheets and Drive APIs are free; this project tracks quota only and is never
billed.

## Auth (per account)

```
gsheet auth login          # prints a URL + code, opens browser
gsheet auth list           # show accounts, * = default
gsheet auth default <email>  # pick default account
gsheet auth revoke <email>
```

## Go API

```go
import "github.com/JakobMelchard/services/internal/workouts/gsheet"

c := gsheet.New("SPREADSHEET_ID", gsheet.WithAccount("me@x.com"))
tabs, _ := c.Tabs(ctx)                                  // list tabs
grid, _ := c.Read(ctx, "UpperA", 4, 1000)                // read a tab
c.Write(ctx, gsheet.RangeAt("Sess X", 2, 5, 8), rows)   // write rows
c.AddTab(ctx, "Upper B", "Exercise,Sets,Reps,Weight")    // tab + bold header
c.DelTab(ctx, "Upper B")
c.Checkbox(ctx, "Sess X", "E", 2, 40)                     // checkboxes on a column
sps, _ := c.List(ctx)                                   // drive: list spreadsheets
```

Every call takes a `context.Context`; cancelling it ends the HTTP request
and the backoff between quota (429) retries.

`internal/workouts/block` is the only consumer.

## CLI

Build with `make build` (`bin/gsheet`) or `go install ./cmd/gsheet`.
Every sheet command takes `--account <email>`.

```sh
gsheet meta <sheet>                                    # tabs: title, sheetId, grid
gsheet pull <sheet> 'Sheet1!A1:D'                      # TSV to stdout
printf 'a\tb\n' | gsheet push <sheet> 'Sheet1!A1:B'    # TSV from stdin
gsheet addtab <sheet> UpperB --header Exercise,Sets,Reps,Weight
gsheet rmtab <sheet> UpperB
gsheet checkbox <sheet> SessX E 2 40
gsheet auth list
```

`push` replaces the range's values. On a range open to the last row
(`A1:Z`, or a bare tab name) it first inserts and deletes whole sheet rows
to match the new lines, so unchanged rows keep their formatting and notes.
Cells right of the range move with those rows. Bounded ranges (`A1:Z20`) are
only cleared and rewritten.

Get a sheet id from the URL: `https://docs.google.com/spreadsheets/d/<ID>/edit`.
Cell values use `FORMATTED_VALUE` on read and `RAW` on write. Cells with
literal tabs/newlines are not supported (TSV format).

## Test

- `go test ./internal/workouts/gsheet/ ./cmd/gsheet/` (part of `make test`) includes an
  end-to-end fake-HTTP test exercising token refresh, addSheet, write,
  checkbox, read and list (no real network).
- Live, against a real scratch spreadsheet shared with the credentials as
  editor (creates and deletes its own tab, needs no fixed structure):

```sh
GSHEET_LIVE=1 GSHEET_TEST_SHEET=<id> go test -run TestLiveSheets -v ./internal/workouts/gsheet/
```

Auth comes from `GSHEET_SERVICE_PRINCIPAL_JSON` (service-account key JSON,
wins over everything) or the `~/.config/gsheet` token store after
`gsheet auth login`. With the key in Infisical:
`infisical run -- env GSHEET_LIVE=1 GSHEET_TEST_SHEET=<id> go test -run TestLiveSheets -v ./internal/workouts/gsheet/`.
The workouts block test (`TestLiveBlock`, see the workouts README) uses the same
variables plus `GSHEET_TEST_PROGRAM` and `GSHEET_TEST_ARCHIVE`.