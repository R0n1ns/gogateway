# Требования к качеству GateForge

Область: один экземпляр GateForge, Linux amd64, PostgreSQL, HTTP/1.1 REST.
Требования определяют ожидаемое поведение независимо от реализации тестов.
Все строки критические, кроме PERF-01 (измерение без порога регрессии).
Указанные сценарии являются обязательствами; наличие строки не означает прохождение.

| ID | Ожидаемое поведение | Автоматический сценарий | Критерий прохождения |
|---|---|---|---|
| START-01 | Корректная поставка запускается | test_startup | readiness 200 до deadline |
| START-02 | Неверная конфигурация и занятый listener запрещают старт | test_invalid_startup; TestServerBindFailure | ненулевой exit code |
| HTTP-01 | Метод, путь, query, заголовки и тело сохраняются | test_echo | побайтовое сравнение тела и echo |
| ROUTE-01 | Выбор по Host и наиболее специфичному пути | test_routing | ответ выбранного backend, неизвестный маршрут 404 |
| POLICY-01 | deny запрещает, allow разрешает | test_policy | 403 и ноль backend-запросов для deny |
| AUTH-01 | Неверные API key, Basic и JWT запрещены | test_auth | 401 без backend; корректные credentials проходят |
| AUTH-02 | Admin API защищён ключом | test_admin_auth | 401, конфигурация не изменена |
| LIMIT-01 | Rate limit ограничивает поток | test_rate_limit | 429 сверх burst, backend получает только допущенные запросы |
| LIMIT-02 | Concurrency limit ограничивает одновременные запросы | test_concurrency_limit | 503 для лишнего запроса, слот освобождается |
| LIMIT-03 | Большое тело, включая chunked, запрещено | test_body_limit; TestChunkedBodyLimit | 413, backend не вызван |
| CONFIG-01 | CRUD маршрутов и политик меняет реальный трафик | test_admin_crud | создание/удаление видно через public API |
| CONFIG-02 | Устаревшая revision отклоняется | test_revision_conflict | 409, версия не изменена |
| CONFIG-03 | Неверное обновление не сохраняется | test_invalid_update | 422, storage и runtime остаются прежними |
| CONFIG-04 | Rollback создаёт новую revision | test_rollback | поведение старой конфигурации, revision возрастает |
| CONFIG-05 | PostgreSQL сохраняет конфигурацию после restart | test_restart | тот же документ и revision после перезапуска |
| CONFIG-06 | Параллельные изменения согласованы | test_parallel_updates; TestConcurrentPublication | один победитель CAS, storage и runtime совпадают |
| CONFIG-07 | Обновление под трафиком атомарно | test_update_under_traffic | каждый ответ соответствует целому старому/новому snapshot |
| FAIL-01 | Остановка backend обрабатывается | test_backend_stop | 502, после старта запрос проходит |
| FAIL-02 | Timeout ограничивает ожидание | test_timeout | 504 в установленном временном диапазоне |
| FAIL-03 | Retry ограничен и учитывает метод | test_retry | точное число попыток; POST не повторяется по умолчанию |
| FAIL-04 | Circuit открывается и восстанавливается | test_circuit | открытый circuit не вызывает backend, затем успешная проба |
| SECRET-01 | Секреты из ENV не сохраняются в истории/API | test_secrets | placeholders в БД/API, значений нет в логах |
| TRUST-01 | Недоверенный X-Forwarded-For не передаётся как достоверный | test_forwarded_for | поддельного IP нет у backend |
| STORE-01 | Ошибка persistence не меняет runtime | TestSaveFailureDoesNotPublish | прежняя revision и поведение |
| FUZZ-01 | Матчеры соответствуют своим предикатам | FuzzStringMatcherPrefix/Exact/Regex; FuzzRouteMatch | нет panic и нарушения свойств |
| GATE-01 | Неуспех, skip, cancel, отсутствие отчёта запрещают выпуск | test_quality_gate | решение reject и ненулевой exit |
| GATE-02 | Проверяется и выпускается одна сборка | test_quality_gate; verify_image | commit, image ID и архив совпадают |
| RELEASE-01 | Обновление и возврат совместимы с сохранённой БД | release-test | конфигурация читается и применяется обеими версиями |
| PERF-01 | Прямой backend и gateway измеряются одинаково | load-test | throughput, p95/p99, ошибки, CPU, память и параметры стенда сохранены |

Unit coverage: не менее 80% statements пакетов internal, без integration build tag.
Security: блокируются исправимые HIGH/CRITICAL, достижимые govulncheck findings
и серьёзные findings анализа кода. Инфраструктурная ошибка не является успехом.
Пропуски обязательных сценариев запрещены. Глобальный distributed reload не заявляется.
