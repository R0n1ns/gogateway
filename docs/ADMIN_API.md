# Admin API

По умолчанию admin server слушает `127.0.0.1:9090`. Не публикуйте его в интернет. Если `admin.api_key` непустой, все `/api/**` требуют `X-API-Key`; health, readiness, metrics и UI остаются отдельными operational endpoints.

## Заголовки изменения

- `X-API-Key` — ключ control plane;
- `If-Match` — ожидаемая revision для optimistic locking;
- `X-Actor` — имя оператора для audit, default `api`;
- `X-Change-Comment` — причина полной замены конфигурации.

## Endpoints

| Метод | Путь | Назначение |
|---|---|---|
| GET | `/healthz` | процесс работает |
| GET | `/readyz` | runtime имеет готовый upstream |
| GET | `/metrics` | Prometheus exposition |
| GET | `/` | embedded admin UI |
| GET | `/api/v1/config` | raw current config и `ETag` |
| PUT | `/api/v1/config` | валидировать и создать новую revision |
| GET | `/api/v1/policies` | активные policies |
| POST | `/api/v1/policies` | добавить policy и snapshot |
| DELETE | `/api/v1/policies/{id}` | удалить policy и создать snapshot |
| GET | `/api/v1/routes` | активные routes |
| POST | `/api/v1/routes` | добавить route и snapshot |
| DELETE | `/api/v1/routes/{id}` | удалить route и создать snapshot |
| GET | `/api/v1/versions?limit=50` | история snapshots |
| GET | `/api/v1/versions/{revision}` | конкретный snapshot |
| POST | `/api/v1/rollback/{revision}` | создать новую revision из старой |
| GET | `/api/v1/audit?limit=100` | administrative audit events |
| POST | `/api/v1/simulate` | policy dry-run |
| GET | `/api/v1/upstreams` | health/circuit/active state |

## Получение и обновление конфигурации

```powershell
curl.exe -i http://127.0.0.1:9090/api/v1/config `
  -H "X-API-Key: $env:GATEFORGE_ADMIN_API_KEY"
```

Ответ содержит `ETag: 3`. Используйте его при записи:

```powershell
curl.exe -X PUT http://127.0.0.1:9090/api/v1/config `
  -H "X-API-Key: $env:GATEFORGE_ADMIN_API_KEY" `
  -H 'If-Match: "3"' `
  -H "X-Actor: devops@example" `
  -H "X-Change-Comment: enable canary" `
  -H "Content-Type: application/json" `
  --data-binary "@configs/gateforge.json"
```

Устаревший `If-Match` возвращает `409`:

```json
{"code":"revision_conflict","message":"конфигурация была изменена другим оператором"}
```

## Rollback

Rollback не перемещает указатель назад молча. Он копирует выбранную конфигурацию в новую revision, поэтому история остаётся линейной и аудируемой:

```powershell
curl.exe -X POST http://127.0.0.1:9090/api/v1/rollback/2 `
  -H "X-API-Key: $env:GATEFORGE_ADMIN_API_KEY" `
  -H 'If-Match: "5"' `
  -H "X-Actor: on-call"
```

## Ошибки

Ошибки имеют стабильную JSON-форму `{"code":"...","message":"..."}`. Основные коды: `unauthorized` (401), `invalid_json` (400), `not_found` (404), `already_exists`/`revision_conflict` (409), `operation_failed` (422).

Public gateway использует собственные коды, например route not found, body too large, policy denied, rate limited и upstream unavailable; request ID возвращается в `X-Request-ID`.