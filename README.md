# services

Go services on the tailnet: lists, workouts

Scaffolded from [JakobMelchard/template](https://github.com/JakobMelchard/template) (`go`).
Templated files (CI caller, `.pre-commit-config.yaml`, devcontainer, org configs) come back through
`copier update`; Renovate opens those updates as PRs.

```sh
brew install prek && prek install    # hooks pinned in .pre-commit-config.yaml
go test ./...
go run .
```
