# Архитектура GateForge

## Назначение

GateForge — Application Gateway with Policy Engine. Первая версия реализована как модульный монолит с двумя HTTP-серверами: public server обрабатывает клиентский трафик, admin server управляет конфигурацией и отдаёт operational endpoints.

## Компоненты

```text
                         GateForge process
+----------------------------------------------------------------+
| Public :8080                         Admin 127.0.0.1:9090        |
| +-------------------+               +------------------------+  |
| | Gateway Runtime   |<--snapshot----| Control Service        |  |
| | Router / Policy   |               | Admin API / UI         |  |
| | Limiter / Proxy   |               +-----------+------------+  |
| | Upstream Pools    |                           |               |
| +---------+---------+                   +-------v---------+     |
|           |                             | Repository      |     |
+-----------+-----------------------------+-------+---------+-----+
            |                                     |
      Backend v1/v2                         Memory/PostgreSQL
```

`DataPlane` и `ControlPlane` в `internal/admin/contracts.go` не дают HTTP handler зависеть от конкретного runtime или repository. В будущем эти интерфейсы позволяют вынести planes в отдельные процессы без переписывания API-контрактов.

## Pipeline запроса

1. Runtime загружает текущий immutable snapshot и увеличивает in-flight gauge.
2. W3C tracer читает или создаёт trace context, затем создаётся request ID.
3. Router выбирает наиболее специфичный маршрут по host и path prefix.
4. Проверяются route body/concurrency limits.
5. Policy Engine последовательно применяет правила по `priority`.
6. Rate limiter, auth, rewrite или terminal action изменяют решение.
7. Upstream pool выбирает здоровый backend согласно стратегии.
8. Proxy удаляет hop-by-hop headers, добавляет forwarding headers и выполняет retry при допустимых условиях.
9. Passive health и circuit breaker получают результат попытки.
10. Клиенту копируются безопасные response headers; access log, audit и метрики фиксируют результат.

## Конфигурационный snapshot

Публикация состоит из двух представлений:

- raw config хранит `${ENV_NAME}` и попадает в историю версий;
- resolved config раскрывает переменные окружения, валидируется, компилирует regex/CIDR/policies и становится runtime snapshot.

Новая версия сначала полностью строится. Только после успешной сборки указатель меняется атомарно; запрос видит либо старый, либо новый согласованный snapshot.

## Состояние

В памяти процесса находятся token buckets, счётчики concurrency, health/circuit state и compiled snapshot. PostgreSQL хранит версии конфигурации и audit events, но не распределяет runtime-state между репликами. Поэтому несколько экземпляров GateForge имеют независимые rate limits; это осознанное ограничение текущей версии.

## Отказоустойчивость

- active health периодически проверяет backend;
- passive health учитывает сетевые ошибки и заданные retry-statuses;
- circuit breaker временно исключает нестабильный backend;
- retry применяется только к настроенным безопасным HTTP-методам;
- graceful shutdown использует `runtime.shutdown_timeout`.