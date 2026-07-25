# GateForge

GateForge — Application Gateway with Policy Engine на Go.

Сервис принимает HTTP-запросы, выбирает подходящий upstream и применяет настроенные правила доступа, ограничения размера и частоты запросов. GateForge не является полноценным сигнатурным WAF и не выполняет автоматический поиск всех SQL injection или XSS.

## Запуск через Go

### Требования

- Go 1.26 или новее;
- запущенный backend, на который GateForge сможет перенаправлять запросы.

### Запуск

По умолчанию пример конфигурации направляет `/api/**` на `http://127.0.0.1:8081`.

```powershell
cd C:\Users\korob\OneDrive\Рабочий стол\проекты\gogateway
go test ./...
go run ./cmd/gateforge -config configs/gateforge.json
```

После запуска доступны:

- публичный gateway: `http://127.0.0.1:8080`;
- административная страница: `http://127.0.0.1:9090`;
- проверка состояния: `http://127.0.0.1:9090/healthz`;
- Prometheus-метрики: `http://127.0.0.1:9090/metrics`.

Пример запроса через gateway:

```powershell
curl http://127.0.0.1:8080/api/users
```

### Остановка

Нажмите `Ctrl+C` в терминале, где запущен GateForge. Public и admin серверы завершатся с учётом `runtime.shutdown_timeout`.

## Запуск через Docker

### Сборка

```powershell
docker build -t gateforge .
```

Перед запуском в контейнере измените `admin.address` на `:9090`, если административный сервер должен быть доступен с хоста. Адрес `127.0.0.1:9090` доступен только внутри контейнера.

```powershell
docker run --rm `
  -p 8080:8080 `
  -p 9090:9090 `
  -v ${PWD}/configs/gateforge.json:/configs/gateforge.json:ro `
  gateforge
```

## Настройка конфигурации

Основной пример находится в [`configs/gateforge.json`](configs/gateforge.json). Путь к другому файлу можно передать через аргумент `-config`:

```powershell
go run ./cmd/gateforge -config configs/local.json
```

`configs/local.json` добавлен в `.gitignore`, поэтому его удобно использовать для локальных адресов и API key.

### Общие параметры

| Поле | Назначение | Пример |
|---|---|---|
| `version` | Версия формата конфигурации. Для текущего формата используется `1` | `1` |
| `public.address` | Адрес публичного HTTP-сервера | `:8080` |
| `public.read_timeout` | Максимальное время чтения запроса | `10s` |
| `public.write_timeout` | Максимальное время записи ответа | `30s` |
| `public.idle_timeout` | Таймаут keep-alive соединения | `60s` |
| `admin.address` | Адрес control API и web-интерфейса | `127.0.0.1:9090` |
| `admin.api_key` | Ключ доступа к `/api/**`. Пустая строка отключает проверку | `change-me` |
| `runtime.shutdown_timeout` | Время на корректную остановку серверов | `10s` |
| `access_log.enabled` | Включает JSON access log | `true` |
| `access_log.format` | Формат журнала. В MVP используется `json` | `json` |
| `storage.driver` | В MVP используйте `memory`; `postgres` зарезервирован для развития control plane | `memory` |

Интервалы задаются строками Go duration: `500ms`, `10s`, `1m`, `2h`.

### Настройка маршрутов

Маршруты находятся в массиве `routes`. GateForge выбирает совпавший маршрут с самым длинным `path_prefix`.

```json
{
  "id": "api",
  "host": "api.example.local",
  "path_prefix": "/api",
  "upstreams": [
    "http://backend-v1:8080",
    "http://backend-v2:8080"
  ],
  "max_body_bytes": 1048576,
  "strip_prefix": false,
  "upstream_timeout": "15s",
  "add_headers": {
    "X-Gateway": "GateForge"
  },
  "remove_headers": [
    "X-Internal-Secret"
  ]
}
```

| Поле | Назначение |
|---|---|
| `id` | Уникальный идентификатор маршрута |
| `host` | Требуемый Host. Пустая строка совпадает с любым host |
| `path_prefix` | Префикс URL. `/api` совпадает с `/api/users`, но не с `/apix` |
| `upstreams` | Список backend-адресов. Запросы распределяются round-robin |
| `max_body_bytes` | Максимальный размер request body. `0` отключает лимит маршрута |
| `strip_prefix` | Удаляет `path_prefix` перед отправкой запроса в backend |
| `upstream_timeout` | Таймаут ожидания заголовков ответа от backend |
| `add_headers` | Заголовки, добавляемые или заменяемые перед proxy |
| `remove_headers` | Заголовки, удаляемые перед proxy |

### Настройка политик

Политики находятся в массиве `policies` и проверяются по возрастанию `priority`.

```json
{
  "id": "deny-sensitive-request",
  "priority": 10,
  "mode": "block",
  "action": "deny",
  "match": {
    "methods": ["GET", "POST"],
    "path_prefix": "/admin",
    "source_cidrs": ["10.0.0.0/8"],
    "headers": {
      "X-Environment": "internal"
    },
    "query": {
      "enabled": "true"
    },
    "route_ids": ["api"],
    "min_body_bytes": 0,
    "max_body_bytes": 1048576
  }
}
```

Все непустые условия внутри `match` должны совпасть одновременно.

| Поле | Назначение |
|---|---|
| `methods` | Разрешённые HTTP-методы в верхнем регистре |
| `path_prefix` | Префикс пути запроса |
| `source_cidrs` | Список IPv4/IPv6 CIDR источника |
| `headers` | Точные значения HTTP-заголовков |
| `query` | Точные значения query-параметров |
| `route_ids` | Маршруты, к которым применяется правило |
| `min_body_bytes` | Минимальный заявленный размер тела |
| `max_body_bytes` | Максимальный заявленный размер тела для совпадения |

Действия политики:

- `allow` — разрешить запрос;
- `deny` — вернуть `403 Forbidden`;
- `rate_limit` — применить настроенный лимит.

Режимы применения:

- `block` — решение влияет на запрос;
- `shadow` — совпадение записывается в журнал и метрики, но запрос не блокируется.

### Настройка rate limit

```json
{
  "id": "api-rate-limit",
  "priority": 20,
  "mode": "block",
  "action": "rate_limit",
  "match": {
    "route_ids": ["api"]
  },
  "limit": {
    "requests": 100,
    "window": "1m",
    "burst": 20,
    "key": "ip"
  }
}
```

`requests` задаёт базовое число запросов за окно, `burst` — дополнительный запас. `key: "ip"` группирует запросы по IP. Значение вида `header:X-API-Key` создаёт отдельный лимит для каждого значения указанного заголовка. Состояние limiter хранится в памяти и сбрасывается после перезапуска или обновления конфигурации.

## Обновление конфигурации без перезапуска

Получить текущую конфигурацию:

```powershell
curl http://127.0.0.1:9090/api/v1/config
```

Заменить конфигурацию целиком:

```powershell
curl.exe -X PUT http://127.0.0.1:9090/api/v1/config `
  -H "Content-Type: application/json" `
  -H "X-API-Key: change-me" `
  --data-binary "@configs/gateforge.json"
```

Также доступны:

| Метод | URL | Назначение |
|---|---|---|
| `GET` | `/api/v1/policies` | Получить политики |
| `POST` | `/api/v1/policies` | Добавить политику |
| `DELETE` | `/api/v1/policies/{id}` | Удалить политику |

Изменения через API публикуются атомарно, но не записываются обратно в JSON-файл. После перезапуска загрузится конфигурация с диска.

## Проверка проекта

```powershell
go fmt ./...
go vet ./...
go test -race ./...
go build ./cmd/gateforge
```
