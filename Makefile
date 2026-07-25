.PHONY: build test test-race cover run fmt vet

build:
	go build -o bin/gateforge ./cmd/gateforge

test:
	go test ./...

test-race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

run:
	go run ./cmd/gateforge -config configs/gateforge.json

fmt:
	gofmt -w .

vet:
	go vet ./...
