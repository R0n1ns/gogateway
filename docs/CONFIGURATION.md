# Настройка конфигурации

## Загрузка и проверка

Файл передаётся через `-config`; по умолчанию используется `configs/gateforge.json`. Decoder запрещает неизвестные поля и несколько JSON-документов в одном файле. Перед запуском выполните:

```powershell
go run ./cmd/gateforge validate -config configs/gateforge.json
```

Для IDE подключите `$schema`:

```json
{
  "$schema": "./gateforge.schema.json",
  "version": 1
}
```

Duration записывается в формате Go: `250ms`, `10s`, `1m`, `2h`. Отрицательные интервалы запрещены.

## Корневые секции

| Секция | Назначение |
|---|---|
| `version` | версия формата, сейчас не меньше `1` |
| `revision` | версия snapshot; назначается repository |
| `public` | адрес и HTTP timeouts data plane |
| `admin` | адрес, timeouts и API key control plane |
| `runtime` | shutdown, trusted proxies, max config size |
| `storage` | `memory` или `postgres` |
| `observability` | service name, sampling и OTLP exporter |
| `routes` | virtual hosts, upstream и resilience |
| `policies` | условия и действия Policy Engine |
| `access_log` | включение JSON access log |

Безопасные defaults определены в `internal/config/defaults.go`: public timeouts 10/30/60s, admin 5/10/30s, shutdown 10s, max config 1 MiB, memory storage и service name `gateforge`.

## Секреты и переменные окружения

Подстановка `${NAME}` разрешена для admin API key, PostgreSQL DSN, upstream URL и auth credentials. Пример:

```json
{
  "admin": {"api_key": "${GATEFORGE_ADMIN_API_KEY}"},
  "storage": {"driver": "postgres", "dsn": "${GATEFORGE_DATABASE_DSN}"}
}
```

Необъявленная переменная становится пустой строкой и обычно вызывает validation error. Raw snapshot хранит placeholder, а не значение секрета. Не вставляйте секреты напрямую в tracked JSON.

## Public, admin и runtime

```json
{
  "public": {
    "address": ":8080",
    "read_timeout": "10s",
    "write_timeout": "30s",
    "idle_timeout": "60s"
  },
  "admin": {
    "address": "127.0.0.1:9090",
    "api_key": "${GATEFORGE_ADMIN_API_KEY}"
  },
  "runtime": {
    "shutdown_timeout": "10s",
    "trusted_proxies": ["10.0.0.0/8"],
    "max_config_bytes": 1048576
  }
}
```

`trusted_proxies` — CIDR адресов балансировщиков, которым разрешено задавать `X-Forwarded-For`. Если непосредственный peer не доверенный, gateway использует его `RemoteAddr` и игнорирует forwarded chain.

## Storage

```json
{
  "storage": {
    "driver": "postgres",
    "dsn": "${GATEFORGE_DATABASE_DSN}",
    "max_open_conns": 10,
    "max_idle_conns": 5
  }
}
```

`memory` подходит для локального запуска: история теряется после рестарта. `postgres` сохраняет snapshots/audit и автоматически создаёт необходимые таблицы при старте.

## Маршрут и upstream pool

```json
{
  "id": "api",
  "host": "api.example.test",
  "path_prefix": "/api",
  "upstream_pool": [
    {"id": "v1", "url": "http://backend-v1:8080", "weight": 90},
    {"id": "v2", "url": "http://backend-v2:8080", "weight": 10}
  ],
  "balance": {"strategy": "weighted_round_robin"},
  "max_body_bytes": 1048576,
  "max_concurrent": 100,
  "strip_prefix": false,
  "upstream_timeout": "15s",
  "add_headers": {"X-Gateway": "GateForge"},
  "remove_headers": ["X-Internal-Secret"],
  "add_response_headers": {"X-Served-By": "GateForge"}
}
```

Router выбирает совпавший маршрут с самым длинным `path_prefix`. Пустой `host` совпадает с любым host. Legacy-массив `upstreams` поддерживается, но `upstream_pool` предпочтительнее.

Стратегии `balance.strategy`:

- `round_robin` — равномерный цикл;
- `weighted_round_robin` — распределение по `weight`;
- `random` — случайный здоровый backend;
- `least_connections` — минимум активных запросов;
- `consistent_hash` — стабильный выбор по `hash_key` (`ip`, `header:Name` или `query:name`).

## Health, retry и circuit breaker

```json
{
  "health_check": {
    "enabled": true,
    "path": "/health",
    "interval": "5s",
    "timeout": "1s",
    "healthy_threshold": 1,
    "unhealthy_threshold": 2
  },
  "retry": {
    "attempts": 2,
    "backoff": "50ms",
    "statuses": [502, 503, 504],
    "methods": ["GET", "HEAD"]
  },
  "circuit_breaker": {
    "failure_threshold": 3,
    "open_timeout": "15s",
    "half_open_requests": 1
  }
}
```

`retry.attempts` ограничен диапазоном 1–10. Не добавляйте POST/PUT/PATCH без идемпотентности backend: повтор может продублировать изменение.

## Observability

```json
{
  "observability": {
    "service_name": "gateforge",
    "tracing_enabled": true,
    "sample_rate": 0.1,
    "otlp_endpoint": "https://otel-collector.example:4318",
    "otlp_insecure": false
  },
  "access_log": {"enabled": true, "format": "json"}
}
```

Пустой `otlp_endpoint` оставляет локальную W3C propagation без экспорта spans. `sample_rate` находится в диапазоне 0–1.

## Горячее обновление

Полную конфигурацию можно заменить через `PUT /api/v1/config`. Передайте текущую revision в `If-Match`, иначе параллельное изменение может дать `409`:

```powershell
curl.exe -X PUT http://127.0.0.1:9090/api/v1/config `
  -H "X-API-Key: $env:GATEFORGE_ADMIN_API_KEY" `
  -H 'If-Match: "3"' `
  -H "X-Actor: operator@example" `
  -H "X-Change-Comment: increase api limit" `
  -H "Content-Type: application/json" `
  --data-binary "@configs/gateforge.json"
```

Update становится видимым только после defaults, env resolution, полной validation и compilation. Файл на диске при этом не перезаписывается.