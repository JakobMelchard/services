# workouts

<p>
  <img alt="Logging a day: sets with reps, weight, RPE, rest, done and notes" src="screenshots/log.png" width="260">
  <img alt="A planned day shown straight from the program" src="screenshots/planned.png" width="260">
  <img alt="Program view of a block tab" src="screenshots/program.png" width="260">
</p>

Training blocks on Google Sheets with a phone PWA for logging. Three
spreadsheets, every tab two columns (exercise, load):

```
program  (program)    one tab per block, named by its calendar weeks
  w37-39                the routine of one week, days as merged rows
  w41-46
active   (active)     the current block: the log
  Block                 program tab, started date
  w41 .. w46            the days trained that week, one row per set
archive  (archive)   finished blocks
  2026 w37 .. 2026 w39  the weeks as logged
```

The program is the template and the week tabs are the log. A day you have
not started is shown straight from the program, for that week's step, so
plan edits reach it. Starting a day (ticking a set done, editing a set, or
adding an exercise) writes the whole day into its week tab; from then on it
is yours to change and the program no longer touches it. Starting a block
(Program view, "Start this block") archives the week tabs of the last block
and records the new program tab. Plans are edited in the program sheet.

All Sheets I/O uses the [`gsheet`](gsheet.md) package (Go client plus the
`gsheet` CLI in `cmd/gsheet`) and loads use
[`workline`](https://github.com/JakobMelchard/workline), pinned in `go.mod`. Sheets access is a service account
(`-service-account key.json` or env `GSHEET_SERVICE_PRINCIPAL_JSON`) or the
`gsheet auth login` token store; the three spreadsheets must be shared with it
as editor.

## Program

A block tab is named `w<from>-<to>` (or `w<n>`) after the calendar weeks it
covers; other tabs are ignored. It holds one week:

| A | B |
|---|---|
| MONDAY (OHP) (merged) | |
| Push Press | `4 x 2` |
| OHP | `1 x 8|6|4|2+` |
| | |
| TUESDAY (REST) | |

A row with an empty Load starts a day; blank rows are ignored. Load is a
workline cell: `'` is minutes, `''` seconds, `SETS x` may be left out for one
set (`90-150'`), and `a|b|c` targets step through the block's weeks
(`8|6|4|2+` is 8 in the first week, 6 in the second, ...; past the last step
the last one repeats). Set ranges (`3-4 x 8`) are not workline. The Program
view shows every block tab with such rows marked; a block with any cannot
start.

## Week tabs

A week tab holds the days of that week that were started, one row per set,
laid out like the program: a day row, its sets, a blank row before the next
day. It is created when the week's first day starts.

| A | B |
|---|---|
| MONDAY (OHP) | |
| OHP | `1x8+` (cell note: `felt #easy`) |
| | |
| FRIDAY (SQUAT) | |
| Squat | `1x8+` |

`GET /weeks/{tab}/rows` answers the week as the app shows it: `{days, rows,
warning}`. `days` are the program's days in order (rest days included) plus
any day only in the week tab; `rows` are the started days' sets from the week
tab and the other days' sets from the program with `planned: true`. Any edit
to a planned set, `POST .../days/{day}/start`, or adding an exercise writes
that day to the week tab. `warning` says the program tab could not be read
(planned days are then missing).

Load is one workline set. The app shows its fields: reps (the target: `5`,
`8-12`, `5+`, `30s`, `20m` minutes, `2km`), weight (`100kg`, `80%`, `?+`),
RPE (`8`, `8+`) and rest (`2m`, `90s`), and writes them back as one
canonical cell (`6r` becomes `6`, `180s` rest becomes `3m`). A cell that is
not one valid set is kept as typed; the row carries an `invalid` reason and
the app offers the raw cell to fix it. A set's notes are the cell note on its
Load cell; `#words` in them are its tags. Done is kept only in the app (per
device, per block and week), not in the sheet.

Set edits send `{"ref": {"index", "day", "exercise", "set"}, "patch":
{...}}` with `load`, `reps`, `weight`, `rpe`, `rest` or `notes`. `index`
counts the view's sets, planned ones included, and `set` is the set's number
within its day and exercise. `POST .../days/{day}/exercises` takes
`{"exercise", "load"}` with a workline load (`3x10 20kg`) and adds one row
per set at the end of the day. If the tab changed underneath (someone edited the sheet), the
server answers 409 and nothing is written. Every edit rewrites the week's
rows and notes.

## Blocks

`POST /block` (form `program`, a block tab):

1. reads the program tab (fails, changing nothing, if any row is invalid)
2. copies each week tab of the active sheet, values and notes, to the archive
   as `<year> w<n>` (replacing a tab of that name left by an earlier failed
   attempt); weeks never started have no tab and leave nothing
3. deletes those week tabs from the active sheet
4. writes `Block` (`program`, tab / `started`, date)

Archive tabs are written before anything is deleted, so a failure midway
loses no logged sets; rerunning finishes the job. The empty `Sheet1` of a new
spreadsheet is removed once other tabs exist. Sheets reads are rate limited
(60 a minute per account); a week view costs 5, an edit about 8, and gsheet
retries quota errors with backoff. Edits and block starts run one at a time.
A request that is cancelled (client gone, server stopping) stops reading and
waiting at once; writes already begun are finished, within 5 s on shutdown.

## Running

The three spreadsheet IDs live in a JSON file, never in the repo or the service definition:

```json
{"program": "<id>", "active": "<id>", "archive": "<id>"}
```

```sh
make run SHEETS=<file> [ADDR=127.0.0.1:8093]
```

`make run` builds `bin/workouts`. The binary takes the same file with
`-sheets`, plus `-embed <published embed URL>` for the read-only Sheet view,
`-account`, `-service-account`, `-token` and `-token-file`.

The header shows the block and picks the week and day (it opens on this
calendar week and today's day, or the first day with an unticked set when the
week has no day for today). Each set has reps, weight, RPE and rest fields, a
done toggle, notes with `#tags`, copy and delete. Planned days are dimmed;
"Add exercise" adds sets to any day, rest days included. Program lists the
block tabs; open one to check it and start it.

Live test against three scratch spreadsheets (writes w41-42/w43 program tabs,
rewrites the active sheet, writes archive tabs):

```sh
GSHEET_LIVE=1 GSHEET_TEST_PROGRAM=<id> GSHEET_TEST_SHEET=<id> GSHEET_TEST_ARCHIVE=<id> \
  go test -run TestLiveBlock -v ./internal/workouts/block/
```

## Auth

The server acts with your Google credentials, so every non-GET route is
guarded by a shared token:

- `-token <t>` or env `WORKOUTS_TOKEN` (prefer the env var; flags show up in
  `ps`). Non-GET requests need `Authorization: Bearer <t>` or the login cookie,
  otherwise 401. GET routes stay open.
- Browser: a 401 sends the page to `/login`; enter the token once and the
  server sets an HttpOnly, SameSite=Strict cookie (30 days) holding a SHA-256
  of the token, Secure when the request came over https (directly or per
  `X-Forwarded-Proto` from the proxy in front). Five wrong tokens in a row
  lock `POST /login` for a minute.
- Without a token the server only starts on loopback (the default
  `127.0.0.1:8080`, or `localhost` / `[::1]`) and every route is open. Binding
  anything else without a token refuses to start.

## Errors

Env `SENTRY_DSN` (a Sentry project DSN, never committed) reports handler
panics to Sentry, environment `SENTRY_ENVIRONMENT` (default `production`), no tracing; the panic is still
rethrown so net/http logs it as before. Unset, nothing is sent.

## Endpoints

| Method | Path                              | Purpose                                     |
|--------|-----------------------------------|---------------------------------------------|
| GET    | `/`                               | App                                         |
| GET    | `/block`                          | JSON: `{block: {program, started, weeks}, programs}` |
| POST   | `/block`                          | Archive the block, start `program`          |
| GET    | `/programs/{tab}`                 | JSON: a block tab, days and rows with errors |
| GET    | `/weeks/{tab}/rows`               | JSON: the week view `{days, rows, warning}` |
| PATCH  | `/weeks/{tab}/rows`               | Edit one set: load, reps, weight, rpe, rest, notes |
| POST   | `/weeks/{tab}/rows/duplicate`     | Insert a copy of one set after it           |
| POST   | `/weeks/{tab}/rows/delete`        | Delete one set                              |
| POST   | `/weeks/{tab}/days/{day}/start`   | Start a planned day as prescribed           |
| POST   | `/weeks/{tab}/days/{day}/exercises` | Add an exercise: `{exercise, load}`       |
| GET    | `/login`                          | Token form (only with a token set)          |
| POST   | `/login`                          | Check token, set auth cookie                |
| GET    | `/static/manifest.webmanifest`, `/sw.js`, `/web/*` | PWA manifest, service worker, shared shell |

## Offline

The page is an installable PWA on the shared `web` shell. The service worker
caches the app shell; the page keeps the last `GET /block` and the last week's
rows, so it opens on the last known week without network.

Every set edit (edit, copy, delete) goes through the `web` queue in the page,
stored in localStorage, and is shown right away. When the server is unreachable
the edits stay queued, the header shows "N pending", and they are replayed in
order on the next `online` event or when the app becomes visible again. Each
replay sends the ref the edit was made against; a 409 (or any other refusal)
drops that edit, reloads the week and names the edit that was not applied.
Starting a block and editing the program need the network.
