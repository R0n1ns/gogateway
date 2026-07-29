# Модули проекта

В Go нет классов: основными единицами являются пакеты, структуры, интерфейсы, функции и методы. Ниже описана ответственность каждого модуля и его публичный внутри проекта контракт.

| Пакет | Назначение | Основные элементы |
|---|---|---|
| `cmd/gateforge` | production entrypoint | вызывает `cli.Run` и возвращает exit code |
| `cmd/demo-backend` | backend для Compose/e2e | health endpoint и JSON-ответ с именем instance |
| `internal/config` | JSON-модель и invariants | `Config`, `Decode`, `Load`, `Defaults`, `Normalize`, `Validate`, `DeepCopy` |
| `internal/router` | выбор virtual host/route | compiled router и `Match` |
| `internal/policy` | компиляция и выполнение правил | `Compile`, `Engine.Evaluate`, auth validators |
| `internal/ratelimit` | token bucket/concurrency | `Limiter.Acquire`, `Release`, `Reset`, janitor |
| `internal/upstream` | балансировка и resilience | `Pool.Next`, `Done`, `Statuses`, health lifecycle |
| `internal/gateway` | data plane orchestration | `Runtime.New`, `ServeHTTP`, `Update`, `Ready`, `Close` |
| `internal/control` | use cases control plane | `Service.Apply`, `Rollback`, `Current`, `List`, `Audit` |
| `internal/storage` | persistence abstraction | `Repository`, `MemoryRepository`, `PostgresRepository` |
| `internal/admin` | HTTP API и embedded UI | `Handler`, `DataPlane`, `ControlPlane` |
| `internal/simulator` | сухой прогон запроса | `Simulator.Simulate` и объяснение совпадений |
| `internal/policytest` | policy cases для CI | `Runner.Run` и сравнение expected result |
| `internal/observability` | OpenTelemetry bootstrap | provider, OTLP exporter, shutdown |
| `internal/cli` | команды оператора | run/validate/format/simulate/test |

## Ключевые структуры и методы

### `config.Config`

Корневой документ. `Defaults` заполняет безопасные значения, `Normalize` приводит методы/host и legacy upstream к каноническому виду, `Validate` возвращает все найденные ошибки одним вызовом, `DeepCopy` исключает совместное владение slices/maps между snapshots.

### `gateway.Runtime`

Владеет atomic snapshot, limiter, metrics и logger. `ServeHTTP` выполняет request pipeline. `Update` сначала строит все зависимости, затем публикует новый snapshot. `Config` возвращает копию, поэтому вызывающий код не может изменить рабочее состояние.

### `policy.Engine`

При создании компилирует regex и CIDR. `Evaluate` обходит политики по priority и возвращает `Decision` с action, policy ID, subject, rewrite/redirect и explanation. Shadow-решение наблюдаемо, но не блокирует запрос.

### `ratelimit.Limiter`

`Acquire` атомарно пополняет и списывает token bucket и при необходимости резервирует concurrency slot. Возвращаемый `Result` содержит limit, remaining и reset time. Для успешного запроса вызывающий код обязан вызвать `Release`.

### `upstream.Pool`

`Next` выбирает только доступный backend. `Done` уменьшает active connections и обновляет passive health/circuit breaker. Background health checker имеет явный `Close`, поэтому hot reload не оставляет goroutine старого snapshot.

### `control.Service`

Оркестрирует validation → policy compilation → repository save → runtime update. `expected revision` обеспечивает optimistic locking. `Rollback` не меняет старую запись, а создаёт новую версию на основе выбранного snapshot.

### `storage.Repository`

Интерфейс скрывает memory/PostgreSQL. Операции принимают `context.Context`; `Save` сохраняет actor/comment и возвращает `ErrConflict` при устаревшей revision.

## Где находятся тесты

Unit-тесты лежат рядом с пакетами. Сквозные сценарии находятся в `tests/`, opt-in PostgreSQL test — в `internal/storage/postgres_integration_test.go`, fuzz/benchmark — в файлах `*_bench_test.go` и `fuzz_bench_test.go`.