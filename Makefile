.RECIPEPREFIX := >
.PHONY: build test test-race integration cover run validate policy-test fmt vet fuzz bench lint vuln docker-up docker-down

build:
>go build -o bin/gateforge ./cmd/gateforge

test:
>go test ./...

test-race:
>go test -race ./...

integration:
>go test -count=1 -run Integration ./internal/storage

cover:
>go test -coverprofile=coverage.out ./...
>go tool cover -func=coverage.out

run:
>go run ./cmd/gateforge run -config configs/gateforge.json

validate:
>go run ./cmd/gateforge validate -config configs/gateforge.json

policy-test:
>go run ./cmd/gateforge test -config configs/gateforge.json -cases configs/policy-tests.json

fmt:
>gofmt -w .

vet:
>go vet ./...

fuzz:
>go test ./internal/router -run=^$$ -fuzz=FuzzRouteMatch -fuzztime=10s
>go test ./internal/policy -run=^$$ -fuzz=FuzzStringMatcher -fuzztime=10s

bench:
>go test -run=^$$ -bench=. -benchmem ./internal/router ./internal/policy ./internal/ratelimit

lint:
>golangci-lint run

vuln:
>govulncheck ./...

docker-up:
>docker compose up --build

docker-down:
>docker compose down