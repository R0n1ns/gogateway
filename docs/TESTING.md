# Тестирование

## Быстрая проверка

```powershell
go test ./...
go vet ./...
go build ./cmd/...
```

Перед коммитом с изменением конкурентного кода используйте race detector:

```powershell
go test -race ./...
```

## Уровни тестов

- unit tests рядом с `internal/*` проверяют defaults/validation, routing, matchers, auth, limiter, upstream algorithms, repositories, API и CLI;
- `tests/gateway_e2e_test.go` запускает два настоящих `httptest` backend и проверяет weighted routing через весь runtime;
- `postgres_integration_test.go` проверяет schema, Save и Get на настоящем PostgreSQL;
- policy cases проверяют пользовательские правила как отдельный versioned artifact;
- fuzz tests ищут panic и нарушения invariants на произвольных path/matcher input;
- benchmarks отслеживают hot path при 1000 routes/policies и parallel limiter access.

## Покрытие

```powershell
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go tool cover -html=coverage.out
```

Цель — покрывать риски, а не процент ради процента: validation errors, deny/auth branches, retries, circuit transitions, revision conflict, secret handling и конкурентный доступ важнее trivial getters.

## PostgreSQL integration

```powershell
docker run --rm --name gateforge-test-db `
  -e POSTGRES_USER=gateforge `
  -e POSTGRES_PASSWORD=gateforge `
  -e POSTGRES_DB=gateforge_test `
  -p 5432:5432 postgres:18-alpine

$env:GATEFORGE_TEST_DATABASE = "postgres://gateforge:gateforge@127.0.0.1:5432/gateforge_test?sslmode=disable"
go test -count=1 -run Integration ./internal/storage
```

Используйте отдельную disposable database: тест создаёт snapshots.

## Fuzz

```powershell
go test ./internal/router -run=^$ -fuzz=FuzzRouteMatch -fuzztime=30s
go test ./internal/policy -run=^$ -fuzz=FuzzStringMatcher -fuzztime=30s
```

CI выполняет короткий smoke, локально перед серьёзным изменением matcher рекомендуется 1–5 минут.

## Benchmark

```powershell
go test -run=^$ -bench=. -benchmem ./internal/router ./internal/policy ./internal/ratelimit
```

Сравнивайте результаты на одной машине и версии Go. Для формального сравнения можно сохранять outputs и использовать `benchstat`.

## Policy regression

```powershell
go run ./cmd/gateforge test -config configs/gateforge.json -cases configs/policy-tests.json
```

При исправлении production-инцидента сначала добавьте case, воспроизводящий ошибочное решение, затем меняйте policy/config/runtime.

## CI

`ci.yml` запускает format, vet, race+coverage, PostgreSQL integration, policy cases, fuzz smoke, build, golangci-lint и govulncheck. Отдельные workflows выполняют CodeQL и проверку container/SBOM.