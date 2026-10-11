BIN = bin

.PHONY: build vet lint test screenshots clean

build:
	@mkdir -p $(BIN)
	go build -o $(BIN)/lists ./cmd/lists

vet:
	go vet ./...

# staticcheck at a fixed version; nothing added to go.mod.
lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...

test:
	go test ./...

# needs node and a Playwright with Chromium, see scripts/lists/screenshots.sh
screenshots: build
	scripts/lists/screenshots.sh

clean:
	rm -rf $(BIN) tmp
