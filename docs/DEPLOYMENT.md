# Развёртывание

## Локальный Compose

```powershell
docker compose up --build
```

Состав окружения:

- `gateforge` — public 8080 и admin, опубликованный только на `127.0.0.1:9090`;
- `backend-v1`, `backend-v2` — demo instances для canary 90/10;
- `postgres` — persistent volume и healthcheck.

Проверка:

```powershell
curl.exe http://127.0.0.1:8080/api/demo
curl.exe http://127.0.0.1:9090/readyz
curl.exe http://127.0.0.1:9090/api/v1/upstreams -H "X-API-Key: local-admin-key"
```

Остановка без удаления БД: `docker compose down`. Удаляйте volume только осознанно командой `docker compose down -v`: история конфигураций будет потеряна.

## Сборка image

```powershell
docker build --target gateforge -t gateforge:local .
```

Production stage основан на distroless и запускается non-root. Конфигурация монтируется read-only:

```powershell
docker run --rm `
  -p 8080:8080 `
  -p 127.0.0.1:9090:9090 `
  -e GATEFORGE_ADMIN_API_KEY="replace-me" `
  -v ${PWD}/configs/gateforge.json:/configs/gateforge.json:ro `
  gateforge:local
```

Если выбран PostgreSQL, добавьте `GATEFORGE_DATABASE_DSN` и обеспечьте сетевую доступность БД.

## Production topology

```text
Internet / internal clients
          |
  TLS Load Balancer
          |
  GateForge replicas ---- OTLP / Prometheus
          |
   Private backends

Control operators -> private admin endpoint -> PostgreSQL
```

TLS, DDoS protection и public certificate lifecycle лучше завершать на managed load balancer. Admin endpoint публикуйте через отдельную private listener/network policy.

## Масштабирование

Runtime stateless относительно конфигурации при общем PostgreSQL, но token buckets, concurrency и circuit/health state локальны каждому instance. Общий лимит 100 req/min на трёх репликах фактически может дать до 300 req/min. Для строгого глобального лимита потребуется внешний distributed limiter — он не входит в текущую версию.

Hot reload через Admin API обновляет только instance, который получил запрос. При нескольких репликах нужна orchestration-процедура: применить конфигурацию ко всем instances, проверить одинаковую revision/readiness и при ошибке выполнить rollback. Будущий отдельный control plane может автоматизировать fan-out.

## Release

Push тега `vX.Y.Z` запускает `.github/workflows/release.yml`, собирает linux/amd64 и linux/arm64 image в GHCR, добавляет SBOM/provenance. До тега:

1. рабочее дерево чистое;
2. format/vet/race/test/build проходят;
3. schema и policy cases проходят;
4. migrations проверены на пустой БД и копии production;
5. security scan не содержит необработанных critical findings;
6. CHANGE/release notes описывают config/API compatibility;
7. rollback сценарий проверен.

## Production checklist

См. также [SECURITY.md](SECURITY.md). Обязательно настройте timeouts, body/concurrency limits, trusted proxies, DB backups, alerts и graceful termination period больше `shutdown_timeout`.