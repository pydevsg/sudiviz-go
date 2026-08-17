VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/pydevsg/sudiviz-go/internal/version.Version=$(VERSION)
GOFLAGS := -trimpath

.PHONY: build test lint release docker install tidy vet

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/sudiviz ./cmd/sudiviz
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/sudiviz-mcp ./cmd/sudiviz-mcp

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

install:
	CGO_ENABLED=0 go install $(GOFLAGS) -ldflags="$(LDFLAGS)" github.com/pydevsg/sudiviz-go/cmd/sudiviz@latest

release:
	goreleaser release --clean

docker:
	docker build -t sudiviz:$(VERSION) .

cross:
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o dist/sudiviz-linux-amd64 ./cmd/sudiviz
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o dist/sudiviz-linux-arm64 ./cmd/sudiviz
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o dist/sudiviz-darwin-amd64 ./cmd/sudiviz
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o dist/sudiviz-darwin-arm64 ./cmd/sudiviz
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o dist/sudiviz-windows-amd64.exe ./cmd/sudiviz
