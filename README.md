# GateForge

GateForge — **Application Gateway with Policy Engine** на Go. Это не полноценный сигнатурный WAF: MVP маршрутизирует HTTP-трафик и применяет явно настроенные политики, не обещая обнаружение всех XSS/SQLi.

## Возможности

- reverse proxy и несколько upstream (round-robin);
- маршрутизация по host и наиболее специфичному path prefix;
- allow/deny по method, path, IP/CIDR, header, query, route и размеру body;
- режимы `block` и `shadow`;
- in-memory rate limit по IP или `header:Name`;
- лимит тела, добавление/удаление заголовков, strip prefix;
- JSON access log, request ID и Prometheus endpoint;
- горячая замена конфигурации и CRUD политик через control API;
- встроенная административная страница.

## Быстрый старт

Требуется Go 1.26+.

```bash
go test ./...
go run ./cmd/gateforge -config configs/gateforge.json
```

Public server слушает `:8080`, admin server — только `127.0.0.1:9090`. Тестовый upstream из примера должен слушать `127.0.0.1:8081`.

```bash
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/metrics
curl http://127.0.0.1:9090/api/v1/config
curl -X POST http://127.0.0.1:9090/api/v1/policies \
  -H 'Content-Type: application/json' \
  -d '{"id":"deny-delete","priority":5,"mode":"block","action":"deny","match":{"methods":["DELETE"]}}'
```

Если задан `admin.api_key`, для `/api/**` обязателен заголовок `X-API-Key`. Новая конфигурация проверяется целиком и публикуется атомарно; активные запросы продолжают использовать предыдущий snapshot.

## Pipeline

1. Создание request ID.
2. Выбор route по host/path.
3. Проверка размера body.
4. Policy engine: allow, deny, shadow deny, rate limit.
5. Round-robin выбор upstream и преобразование запроса.
6. Reverse proxy.
7. Access log, audit shadow-событий и метрики.

## Конфигурация

Полный рабочий пример находится в [`configs/gateforge.json`](configs/gateforge.json). Path prefix сравнивается по границе сегмента: `/api` не совпадает с `/apix`. Политики исполняются по возрастанию `priority`; первое block-решение завершает проверку, shadow-совпадения только журналируются.

Control API:

| Метод | URL | Назначение |
|---|---|---|
| GET | `/healthz` | liveness |
| GET | `/metrics` | Prometheus exposition |
| GET/PUT | `/api/v1/config` | получить/атомарно заменить конфигурацию |
| GET/POST | `/api/v1/policies` | список/создание политики |
| DELETE | `/api/v1/policies/{id}` | удаление политики |

## Разработка

```bash
make fmt
make vet
make test-race
make cover
make build
```

CI запускает форматирование, `go vet`, unit-тесты с race detector и сборку.

## Границы MVP

В проект намеренно не входят универсальный анализ тела, сигнатуры SQLi/XSS, антивирус, eBPF/nftables, собственный TLS, распределённая конфигурация и Kubernetes ingress controller. PostgreSQL показан как следующий persistence-слой control plane; текущий MVP хранит активный атомарный snapshot в памяти и получает исходную конфигурацию из файла/API.