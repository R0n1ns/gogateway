# PostgreSQL и версии конфигурации

## Repository abstraction

`internal/storage.Repository` определяет операции `Current`, `Get`, `List`, `Save`, `Audit`, `RecordAudit` и `Close`. Control service одинаково работает с memory и PostgreSQL реализациями.

Memory repository создаёт revision в процессе и нужен для тестов/локальной разработки. PostgreSQL repository хранит состояние между рестартами и использует драйвер pgx через `database/sql`.

## Схема

- `config_snapshots` — immutable JSON document, actor, comment, timestamp;
- `config_state` — singleton-указатель на current snapshot;
- `audit_events` — административные действия, actor, revision и details.

SQL-файлы находятся в `migrations/`. При подключении `PostgresRepository.Migrate` идемпотентно создаёт ту же текущую схему, поэтому чистая база готовится автоматически.

## Транзакция Save

1. Открывается serializable transaction.
2. Строка `config_state` блокируется `FOR UPDATE`.
3. Current revision сравнивается с `expected`.
4. При несовпадении возвращается `storage.ErrConflict`.
5. Создаётся snapshot с новой sequence revision.
6. Обновляется current pointer и добавляется `config.apply` audit event.
7. Транзакция фиксируется.

Документы snapshot не обновляются. Rollback читает старый snapshot и выполняет обычный Save, создавая новую revision.

## Подключение

```powershell
$env:GATEFORGE_DATABASE_DSN = "postgres://gateforge:password@127.0.0.1:5432/gateforge?sslmode=require"
go run ./cmd/gateforge run -config configs/postgres.json
```

В production используйте отдельного DB-пользователя, TLS, secret manager, backup и connection limit. DSN указывайте placeholder-ом `${GATEFORGE_DATABASE_DSN}`, чтобы он не попадал в snapshots.

## Backup и восстановление

Обычный `pg_dump` сохраняет историю и audit. Перед восстановлением остановите запись в control plane, восстановите все три таблицы и sequence `config_snapshots_id_seq`, затем запустите GateForge и проверьте `/readyz` и current revision.

## Интеграционный тест

```powershell
$env:GATEFORGE_TEST_DATABASE = "postgres://gateforge:gateforge@127.0.0.1:5432/gateforge_test?sslmode=disable"
go test -count=1 -run Integration ./internal/storage
```

Тест пропускается, если переменная не задана. CI поднимает изолированный PostgreSQL service автоматически.