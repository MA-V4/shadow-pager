VERSION := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: run build test race vet fmt tidy

run:
	go run ./cmd/server

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/server ./cmd/server

# The same tests as CI, without the race detector, so this also works on Windows without a C compiler.
test:
	go test -count=1 ./...

# The race detector needs cgo and a C compiler such as gcc, and CI runs it on Linux for every push.
race:
	CGO_ENABLED=1 go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy
