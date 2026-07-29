# Безопасность и границы проекта

## Что защищает GateForge

GateForge применяет явные политики доступа, CIDR/header/query/path filters, request-size/concurrency/rate limits, API key/Basic/JWT HS256 и скрывает внутреннюю топологию upstream. Это полезно для внутренних REST API и service-to-service gateway.

GateForge не является полноценным WAF. Он не анализирует произвольное тело запроса на SQL injection/XSS, не сканирует файлы антивирусом и не заменяет secure coding, identity provider, DDoS protection или managed edge WAF.

## Trust boundaries

1. Public server считается недоверенной границей.
2. `X-Forwarded-For` учитывается только от peer из `runtime.trusted_proxies`.
3. Admin server должен слушать loopback/private network и быть защищён API key и network ACL.
4. Upstream network должна быть недоступна клиенту напрямую, если gateway — точка enforcement.
5. PostgreSQL и OTLP подключаются по TLS вне локальной Compose-сети.

## Секреты

- храните `${ENV_NAME}` в JSON;
- передавайте значения через orchestrator/secret manager;
- не коммитьте `.env`, DSN, JWT secret или admin key;
- raw snapshots и Admin API сохраняют placeholder, runtime использует resolved copy;
- JWT HS256 secret должен содержать минимум 32 символа;
- меняйте demo credentials из `compose.yaml` перед реальным развёртыванием.

## HTTP и proxy

Runtime удаляет hop-by-hop headers и сам формирует forwarding context. Ограничивайте `max_body_bytes`, `max_concurrent`, timeouts и retries. Не включайте retry для неидемпотентных методов без idempotency keys.

TLS завершается внешним reverse proxy/load balancer или стандартным Go HTTP server после отдельного production-расширения. Собственную криптографию и TLS-реализацию проект намеренно не содержит.

## Authentication limitations

Control API использует один shared API key и не реализует RBAC. JWT module поддерживает только локальную проверку HS256, без JWKS rotation/revocation. Для production с пользователями подключите OIDC provider, asymmetric JWT verifier и RBAC/audit identity.

## Supply chain

CI выполняет govulncheck, CodeQL, container scan и создаёт SPDX SBOM. Release image собирается для amd64/arm64 по tag `v*` с build provenance. Для production дополнительно закрепляйте actions и base images по digest и включайте branch protection.

## Сообщение об уязвимости

Не публикуйте чувствительную информацию в обычном issue. Используйте GitHub Private Vulnerability Reporting/другой приватный канал владельца репозитория и приложите affected revision, сценарий воспроизведения, impact и предложенное исправление.

## Production checklist

- admin address не доступен из интернета;
- admin/API/JWT/DB secrets заменены и находятся вне Git;
- trusted proxies перечислены минимально;
- PostgreSQL и OTLP используют TLS;
- routes имеют body/concurrency/time limits;
- policies сначала проверены в shadow/simulator/CI;
- backup и rollback протестированы;
- метрики и alerts подключены;
- container запускается non-root и filesystem по возможности read-only.