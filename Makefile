.PHONY: build test lint cover clean fmt vet

BINARY := gnoma
BINDIR := ./bin
MODULE := somegit.dev/Owlibou/gnoma

build:
	go build -o $(BINDIR)/$(BINARY) ./cmd/gnoma

test:
	go test ./...

test-v:
	go test -v ./...

test-integration:
	go test -tags integration ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf $(BINDIR) coverage.out coverage.html

tidy:
	go mod tidy
