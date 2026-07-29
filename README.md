# GateForge

[![Проверка качества](https://github.com/R0n1ns/gogateway/actions/workflows/ci.yml/badge.svg)](https://github.com/R0n1ns/gogateway/actions/workflows/ci.yml)
[![CodeQL](https://github.com/R0n1ns/gogateway/actions/workflows/codeql.yml/badge.svg)](https://github.com/R0n1ns/gogateway/actions/workflows/codeql.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

GateForge — **Application Gateway with Policy Engine** на Go. Сервис принимает HTTP-запрос, выбирает маршрут и upstream, применяет политики доступа и ограничений, проксирует запрос и записывает метрики и аудит.

Это не полноценный сигнатурный WAF: проект не пытается самостоятельно обнаруживать все варианты SQL injection, XSS или вредоносные файлы. Его задача — управляемая маршрутизация и проверяемые gateway-политики для REST API.

## Возможности

- маршрутизация по `Host` и префиксу пути;
- round-robin, weighted round-robin, random, least connections и consistent hash;
- active/passive health checks, retry и circuit breaker;
- allow, deny, shadow, rate limit, rewrite, redirect и authenticate;
- фильтры method, path, IP/CIDR, header, query и размер body;
- API key, Basic Auth и JWT HS256;
- token bucket и ограничение параллельных запросов;
- горячее атомарное обновление и rollback конфигурации;
- memory/PostgreSQL control plane с optimistic locking и audit trail;
- Prometheus, JSON access log и OpenTelemetry OTLP;
- CLI, Policy Simulator, декларативные policy-тесты и web-панель.

## Быстрый запуск через Docker

Требуется Docker с поддержкой Compose.

```powershell
git clone https://github.com/R0n1ns/gogateway.git
cd gogateway
docker compose up --build
```

Compose поднимет GateForge, PostgreSQL и два demo-backend. После запуска доступны:

- gateway: `http://127.0.0.1:8080/api/demo`;
- admin UI: `http://127.0.0.1:9090`;
- health: `http://127.0.0.1:9090/healthz`;
- readiness: `http://127.0.0.1:9090/readyz`;
- metrics: `http://127.0.0.1:9090/metrics`.

Для запросов к `/api/v1/**` используйте ключ из `compose.yaml`:

```powershell
curl.exe http://127.0.0.1:9090/api/v1/upstreams -H "X-API-Key: local-admin-key"
```

Остановка:

```powershell
docker compose down
```

## Запуск из исходного кода

Требуется Go 1.26. Пример по умолчанию ожидает backend на `127.0.0.1:8081` и `127.0.0.1:8082`.

```powershell
go test ./...
go run ./cmd/gateforge run -config configs/gateforge.json
```

Основные команды CLI:

```powershell
go run ./cmd/gateforge validate -config configs/gateforge.json
go run ./cmd/gateforge format -config configs/gateforge.json
go run ./cmd/gateforge simulate -config configs/gateforge.json -request configs/simulate-request.json
go run ./cmd/gateforge test -config configs/gateforge.json -cases configs/policy-tests.json
```

## Конфигурация

GateForge использует строгий JSON: неизвестные поля приводят к ошибке. Полный пример находится в [`configs/gateforge.json`](configs/gateforge.json), JSON Schema — в [`configs/gateforge.schema.json`](configs/gateforge.schema.json).

Секреты передавайте через переменные окружения:

```json
{
  "admin": {"api_key": "${GATEFORGE_ADMIN_API_KEY}"},
  "storage": {
    "driver": "postgres",
    "dsn": "${GATEFORGE_DATABASE_DSN}"
  }
}
```

В snapshot и Admin API сохраняются ссылки `${NAME}`, а раскрытые значения используются только внутри runtime. Подробное описание всех секций, маршрутов, upstream и политик: [настройка конфигурации](docs/CONFIGURATION.md).

## Архитектура

```text
Client -> Public Server -> Router -> Limits -> Policy Engine
                                      |             |
                                      +------> Upstream Pool -> Backend

Operator -> Admin Server -> Control Service -> Memory / PostgreSQL
                              |
                              +-> atomic Runtime snapshot
```

Приложение остаётся модульным монолитом, но data plane и control plane разделены интерфейсами. Подробности: [архитектура](docs/ARCHITECTURE.md) и [модули](docs/MODULES.md).

## Документация

| Документ | Содержание |
|---|---|
| [Архитектура](docs/ARCHITECTURE.md) | Компоненты и request pipeline |
| [Модули](docs/MODULES.md) | Пакеты, структуры, функции и методы |
| [Конфигурация](docs/CONFIGURATION.md) | Поля JSON, defaults, secrets и reload |
| [Policy Engine](docs/POLICY_ENGINE.md) | Условия, действия, auth и simulator |
| [Admin API](docs/ADMIN_API.md) | Endpoints, ETag и примеры запросов |
| [База данных](docs/DATABASE.md) | Snapshot repository, миграции и rollback |
| [Наблюдаемость](docs/OBSERVABILITY.md) | Метрики, логи и tracing |
| [Безопасность](docs/SECURITY.md) | Trust boundaries и ограничения проекта |
| [Тестирование](docs/TESTING.md) | Unit, race, integration, fuzz и benchmark |
| [Развёртывание](docs/DEPLOYMENT.md) | Docker Compose и production checklist |
| [Разработка](docs/DEVELOPMENT.md) | Локальный workflow, CI и релизы |

## Проверка качества

```powershell
go fmt ./...
go vet ./...
go test -race ./...
go build ./cmd/...
```

CI дополнительно запускает PostgreSQL integration test, fuzz smoke, golangci-lint, govulncheck, CodeQL, сканирование image и создаёт SBOM. Публикация контейнера выполняется только для Git-тегов `v*`.

## Лицензия

Проект распространяется по лицензии [MIT](LICENSE).
