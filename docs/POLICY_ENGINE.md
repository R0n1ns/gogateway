# Policy Engine

## Порядок выполнения

Политики сортируются по возрастанию `priority`. Пустые условия совпадают со всеми запросами. `deny`, `rate_limit`, `redirect` и `allow` завершают обход; `rewrite` и успешный `authenticate` позволяют перейти к следующему правилу. Поэтому authentication должна иметь меньший priority, чем rate limit с ключом `jwt_subject`.

`mode: block` применяет действие. `mode: shadow` только добавляет policy ID в decision, access log и метрику — удобный способ проверить правило перед включением.

## Условия

Все простые непустые поля одного `match` объединяются логическим AND:

- `methods`;
- `path_prefix` или расширенный `path`;
- `source_cidrs`;
- exact maps `headers` и `query`;
- `header_matches` и `query_matches`;
- `min_body_bytes`, `max_body_bytes`;
- `route_ids`.

Расширенный matcher содержит ровно один оператор:

```json
{"exact": "value"}
{"prefix": "Bearer "}
{"suffix": ".json"}
{"regex": "^v[0-9]+$"}
{"absent": true}
```

Regex и CIDR компилируются при публикации конфигурации, а не на каждый запрос.

## Логические группы

```json
{
  "all": [
    {"methods": ["POST"]},
    {"any": [
      {"source_cidrs": ["10.0.0.0/8"]},
      {"header_matches": [{"name": "X-Role", "match": {"exact": "admin"}}]}
    ]}
  ],
  "not": {"path": {"prefix": "/public"}}
}
```

`all` требует совпадения всех дочерних узлов, `any` — хотя бы одного, `not` инвертирует результат.

## Действия

| Action | Поведение |
|---|---|
| `allow` | завершает policy evaluation с разрешением |
| `deny` | возвращает 403 |
| `rate_limit` | применяет token bucket/concurrency и может вернуть 429 |
| `rewrite` | меняет path prefix и request headers, затем продолжает |
| `redirect` | возвращает настроенный 3xx и Location |
| `authenticate` | проверяет credential, сохраняет subject или возвращает 401 |

Route-level `add_response_headers` применяется к ответу upstream независимо от policy action.

## Rate limit

```json
{
  "id": "per-user-limit",
  "priority": 20,
  "mode": "block",
  "action": "rate_limit",
  "match": {"route_ids": ["api"]},
  "limit": {
    "algorithm": "token_bucket",
    "requests": 100,
    "window": "1m",
    "burst": 20,
    "key": "jwt_subject",
    "max_concurrent": 10,
    "idle_ttl": "10m"
  }
}
```

Ключи: `ip`, `jwt_subject`, `header:Header-Name`. Ответ содержит `RateLimit-Limit` и `RateLimit-Remaining`; отклонённый запрос также получает `RateLimit-Reset` и `Retry-After`. Limiter in-memory и не общий между репликами.

## Authentication

API key сравнивается constant-time с полем `auth.value`; имя заголовка задаёт `auth.header`:

```json
{"type": "api_key", "header": "X-API-Key", "value": "${SERVICE_API_KEY}"}
```

Basic Auth использует стандартный `Authorization: Basic ...`:

```json
{"type": "basic", "username": "${BASIC_USER}", "password": "${BASIC_PASSWORD}"}
```

JWT validator поддерживает только HS256 и проверяет signature, `exp`, `nbf`, опциональные `iss` и `aud`:

```json
{
  "type": "jwt_hs256",
  "header": "Authorization",
  "jwt_secret": "${JWT_SECRET}",
  "jwt_issuer": "example",
  "jwt_audience": "gateway-api"
}
```

Для OIDC/JWKS/RS256 нужен внешний verifier или отдельное расширение; текущий модуль не следует выдавать за полноценный identity provider.

## Simulator

Simulator выполняет routing и policy evaluation, но не отправляет запрос upstream:

```powershell
go run ./cmd/gateforge simulate `
  -config configs/gateforge.json `
  -request configs/simulate-request.json
```

Формат request: `method`, `host`, `path`, `source_ip`, `headers`, `query`, `body_bytes`, необязательный `route_id`. Результат содержит route, action, status, matched policy, shadow IDs и `explanation`.

## Декларативные policy-тесты

Файл `configs/policy-tests.json` хранит cases с `request` и `expected`. Команда возвращает ненулевой exit code при несовпадении и подходит для CI:

```powershell
go run ./cmd/gateforge test -config configs/gateforge.json -cases configs/policy-tests.json
```