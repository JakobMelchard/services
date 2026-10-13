# services

Go services on the tailnet: lists and workouts.

```sh
make build    # bin/lists, bin/workouts, bin/gsheet
make test
make vet lint
make screenshots
```

```sh
bin/lists -addr 127.0.0.1:8082 -data <dir>
make run SHEETS=<file>    # workouts; {"program": "<id>", "active": "<id>", "archive": "<id>"}
```

| Path | Contents |
|---|---|
| `cmd/<service>/` | Binaries |
| `internal/<service>/` | One service each, UI embedded in the binary |
| `internal/lists/data/` | Demo data and test fixtures |
| `scripts/<service>/` | Screenshot scripts |
| `docs/workouts/` | Sheet layout, API, [capacitor](docs/workouts/capacitor.md), [gsheet](docs/workouts/gsheet.md) |
| `capacitor/` | Android and iOS shells around the workouts UI |
| `deploy/mimi/` | LaunchDaemons for the reconciler |

lists keeps one JSON file per list at `<dir>/lists/<id>.json`. workouts needs cgo for the workline tree-sitter grammar.
