BIN = bin
# Sheet IDs file for make run, see README.
SHEETS ?= .data/sheets.json
ADDR ?= 127.0.0.1:8093

.PHONY: build run vet lint test screenshots clean

build:
	@mkdir -p $(BIN)
	go build -o $(BIN)/lists ./cmd/lists
	go build -o $(BIN)/workouts ./cmd/workouts
	go build -o $(BIN)/gsheet ./cmd/gsheet

run: build
	$(BIN)/workouts -addr '$(ADDR)' -sheets '$(SHEETS)'

vet:
	go vet ./...

# staticcheck at a fixed version, cmd/demo included; nothing added to go.mod.
lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 -tags demo ./...

test:
	go test -race ./...
	node --test "internal/workouts/ui/test/*.test.mjs"

# needs node and a Playwright with Chromium, see scripts/lists/screenshots.sh
screenshots: build
	scripts/lists/screenshots.sh
	scripts/workouts/screenshots.sh

clean:
	rm -rf $(BIN) tmp
