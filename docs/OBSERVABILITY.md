# Наблюдаемость

## Operational endpoints

- `/healthz` отвечает, если HTTP-процесс работает;
- `/readyz` возвращает 200, когда runtime готов обслуживать трафик, иначе 503;
- `/metrics` отдаёт Prometheus text exposition;
- `/api/v1/upstreams` показывает health, circuit и active connections.

Health не следует использовать как readiness: первый подходит для restart probe, второй — для исключения instance из балансировки.

## Prometheus metrics

| Метрика | Тип | Labels/значение |
|---|---|---|
| `gateforge_requests_in_flight` | gauge | запросы внутри runtime |
| `gateforge_config_revision` | gauge | активная revision |
| `gateforge_requests_total` | counter | route, decision, status |
| `gateforge_policy_matches_total` | counter | policy, mode |
| `gateforge_upstream_requests_total` | counter | route, upstream, status |
| `gateforge_request_duration_seconds` | histogram | полная latency gateway |
| `gateforge_upstream_duration_seconds` | histogram | latency upstream попыток |

Labels экранируются и ограничиваются 128 символами. Route/policy/upstream IDs берутся из валидированной конфигурации, чтобы не создавать unbounded cardinality из пользовательских URL или header values.

Пример scrape config:

```yaml
scrape_configs:
  - job_name: gateforge
    static_configs:
      - targets: [gateforge:9090]
```

## Access log

При `access_log.enabled=true` каждый запрос пишет structured record со следующими полями: `request_id`, `trace_id`, `method`, `path`, `route`, `decision`, `status`, `duration_ms`, `source_ip`. Shadow matches пишутся отдельными warning events.

Не добавляйте в логи Authorization, cookies, API keys, полный query или request body. Сейчас runtime этого не делает.

## Tracing

GateForge поддерживает W3C `traceparent` и передаёт контекст upstream. При заполненном `observability.otlp_endpoint` создаётся OpenTelemetry OTLP/HTTP batch exporter. Sampling parent-based и управляется `sample_rate`.

```json
{
  "observability": {
    "service_name": "gateforge-edge",
    "tracing_enabled": true,
    "sample_rate": 0.1,
    "otlp_endpoint": "http://otel-collector:4318",
    "otlp_insecure": true
  }
}
```

`otlp_insecure=true` допустим только внутри доверенной сети. На shutdown provider flush-ит batch в пределах общего shutdown context.

## Базовые alerts

Рекомендуемые сигналы:

- рост доли 5xx в `gateforge_requests_total`;
- рост 502/503/504 по конкретному upstream;
- p95/p99 request duration;
- `/readyz` недоступен;
- circuit долго остаётся open;
- config revision неожиданно изменилась или перестала совпадать между instances.