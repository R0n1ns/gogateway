"""Create identical evidence-based Markdown and Word readiness reports."""
import json
from pathlib import Path
import xml.etree.ElementTree as ET

from docx import Document
from docx.shared import Inches, Pt, RGBColor

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "artifacts/readiness-current"


def create():
    evidence = json.loads((EVIDENCE / "verification.json").read_text(encoding="utf-8"))
    rows = []
    checks = {r["name"]: r for r in evidence["checks"]}
    for name in ("fuzz", "build", "security"):
        stage_path = ROOT / "artifacts" / name / "stage.json"
        if stage_path.exists():
            stage = json.loads(stage_path.read_text(encoding="utf-8"))
            checks[name] = dict(status="passed" if stage.get("status") == "success" else "failed", duration_seconds=stage.get("duration_seconds"), commit=stage.get("commit"))
    integration_log = EVIDENCE / "integration.log"
    if integration_log.exists():
        text = integration_log.read_text(encoding="utf-8", errors="replace")
        events = []
        for line in text.splitlines():
            try: events.append(json.loads(line))
            except ValueError: pass
        passed = any(e.get("Action") == "pass" and not e.get("Test") for e in events)
        if passed: checks["integration"] = dict(status="passed")
    names = {"automation": "Quality Gate", "unit": "Go unit и race", "vet": "Go vet", "lint": "Lint", "integration": "PostgreSQL integration", "fuzz": "Fuzz", "build": "Сборка образа", "security": "Security", "e2e": "Внешние E2E", "release": "Обновление и возврат", "load": "Нагрузка"}
    for key, title in names.items():
        result = checks.get(key, {})
        status = {"passed": "Пройдено", "failed": "Ошибка", "infrastructure_error": "Ошибка инфраструктуры"}.get(result.get("status"), "Не подтверждено")
        source = "artifacts/"+key+"/stage.json" if key in ("fuzz", "build", "security") and result else "artifacts/readiness-current/"+key+".log" if result else "Нет нового результата"
        rows.append([title, status, str(result.get("duration_seconds") if result.get("duration_seconds") is not None else "—"), source])
    mandatory = set(names) - {"load"}
    accepted = all(checks.get(k, {}).get("status") == "passed" for k in mandatory)
    environment_path = EVIDENCE / "e2e/environment.json"
    build_manifest_path = ROOT / "artifacts/build/manifest.json"
    artifact_notes = []
    if environment_path.exists():
        environment = json.loads(environment_path.read_text(encoding="utf-8"))
        artifact_notes.append("Образ E2E: " + environment["image_id"] + ".")
        if build_manifest_path.exists():
            manifest = json.loads(build_manifest_path.read_text(encoding="utf-8"))
            artifact_notes.append("Новая сборка: " + manifest["image_id"] + "; commit " + manifest["commit"] + ".")
            if environment["image_id"] != manifest["image_id"]:
                accepted = False
                artifact_notes.append("E2E и новая сборка используют разные образы. Допуск новой сборки не подтверждён.")
    conclusion = "Готовность подтверждена в пределах выполненных испытаний." if accepted else "Полная готовность продукта к выпуску не подтверждена. Прототип может использоваться для демонстрации только с перечисленными ограничениями."
    counts = []
    for path, title in [(EVIDENCE/"automation.xml", "Quality Gate"), (EVIDENCE/"e2e/junit.xml", "E2E")]:
        if path.exists():
            suites = list(ET.parse(path).getroot().iter("testsuite"))
            values = {k: sum(int(s.get(k, 0)) for s in suites) for k in ("tests", "failures", "errors", "skipped")}
            counts.append(f"{title}: всего {values['tests']}, failures {values['failures']}, errors {values['errors']}, пропусков {values['skipped']}.")
    go_tests = {}
    unit_path = EVIDENCE / "unit.log"
    if unit_path.exists():
        for line in unit_path.read_text(encoding="utf-8", errors="replace").splitlines():
            try: event = json.loads(line)
            except ValueError: continue
            if event.get("Test") and event.get("Action") in ("pass", "fail", "skip"):
                go_tests[(event.get("Package"), event["Test"])] = event["Action"]
        if go_tests:
            counts.append(f"Go test events: {len(go_tests)} завершённых тестов и подтестов, pass {list(go_tests.values()).count('pass')}, fail {list(go_tests.values()).count('fail')}, skip {list(go_tests.values()).count('skip')}. Итог команды оценивается отдельно.")
    total = covered = 0
    coverage = EVIDENCE / "coverage.out"
    if coverage.exists():
        for line in coverage.read_text().splitlines()[1:]:
            if "/internal/" not in line: continue
            _, statements, executions = line.rsplit(" ", 2)
            total += int(statements)
            if int(executions): covered += int(statements)
        if total:
            counts.append(f"Покрытие internal по полученному профилю: {100*covered/total:.2f}%. Порог 80%. Профиль считается окончательным только при успешном завершении unit-команды.")
    sections = [
        ("Объект и методика", ["GateForge — HTTP-шлюз с маршрутизацией, политиками доступа и административным API. Проверяются существующие возможности одного экземпляра и хранение конфигурации в PostgreSQL. Новые функции в рамках подготовки отчёта не добавлялись.", f"Исходный commit запуска: {evidence['commit']}. Начало проверки UTC: {evidence['started_at']}. Команды, коды завершения и длительности сохранены в artifacts/readiness-current/verification.json.", "Локальный стенд использует Windows и Docker Desktop с Linux-контейнерами. Сценарии проверяют HTTP-ответы и последствия: число backend-запросов, состояние конфигурации и revision. Публикация релиза в ходе проверки не выполнялась.", *artifact_notes]),
        ("Результаты тестов", counts or ["Итоговые тестовые отчёты отсутствуют."]),
        ("Проверяемые требования", ["Маршрутизация и передача метода, query, заголовков и тела; allow/deny и аутентификация; rate/concurrency/body limits; CRUD маршрутов и политик; конфликты revision, неверные обновления, rollback и сохранение после restart.", "Проверки отказов включают остановку backend, timeout, границы retry, circuit breaker и обновление конфигурации под трафиком. Отдельно проверяются секреты и недоверенные forwarding headers. Полная матрица требований: docs/QUALITY_REQUIREMENTS.md."]),
        ("Исправленные дефекты", ["В истории разработки регрессионными тестами подтверждены: успешный exit code при ошибке listener, ответ 502 вместо 413 для большого chunked-тела, передача поддельной цепочки X-Forwarded-For и рассогласование revision storage/runtime при параллельных обновлениях. Исправления сохранены в коммитах 121ae32, bd86f31 и abdc8d9.", "Стенд дополнен проверкой PostgreSQL по TCP, обновлением адресов после restart, ограниченными ожиданиями и сохранением причин ошибок запуска. Исторические прогоны не заменяют результаты текущей версии."]),
        ("Ограничения и допуск", [conclusion, "Отсутствие отчёта, прерванная команда или ошибка инфраструктуры не являются успешным тестом. Настройка обязательной проверки ветки и полный GitHub Actions запуск не подтверждаются локальными тестами.", "Сравнение конвейеров A/B/C и независимая оценка ИИ-случаев относятся к исследовательской части. Этот отчёт не доказывает её завершение или превосходство нового конвейера. Исторический успешный upgrade/rollback не считается подтверждением текущего образа.", "Для окончательного допуска необходимы успешные результаты всех обязательных строк таблицы и совпадение commit, образа и отчётов. Нагрузочные показатели без нового измерения не приводятся. Готовность к production-эксплуатации не заявляется."]),
        ("Воспроизведение", ["Установить зависимости: python -m pip install -r tests/requirements.txt. Проверить доступность Docker Desktop и собранных gateforge:local, gateforge-test-backend:local.", "Запустить python scripts/verify_readiness.py. Для отдельных испытаний: python scripts/e2e.py test; python scripts/release_test.py --image gateforge:local. PostgreSQL integration требует GATEFORGE_TEST_DATABASE и make integration.", "Данный отчёт сформирован из полученных машинных результатов. После исправлений требуется новый запуск и повторное формирование обоих документов."]),
    ]
    vulnerability_log = ROOT / "artifacts/security/govulncheck.log"
    if vulnerability_log.exists() and checks.get("security", {}).get("status") == "failed":
        security_text = vulnerability_log.read_text(encoding="utf-8", errors="replace")
        import re
        identifiers = re.findall(r"Vulnerability #\d+: (GO-[\d-]+)", security_text)
        if identifiers:
            sections.insert(3, ("Результаты проверки безопасности", [
                f"Локальный govulncheck завершился отказом и перечислил {len(identifiers)} достижимых уязвимостей. Источник: artifacts/security/govulncheck.log. Это блокирует положительное решение о выпуске.",
                "Проверка исходного кода использовала локальный Go 1.26.5, тогда как собранный образ использует закреплённый Go 1.26.9. Число находок локального анализа нельзя автоматически переносить на контейнер. Для окончательного решения нужны повторный анализ на единой версии toolchain и завершённое сканирование образа.",
                "Среди находок в зависимостях: golang.org/x/text, golang.org/x/net, OpenTelemetry и gRPC. Журнал содержит исправленные версии. Обновление зависимостей и Go требует отдельного повторного регрессионного прогона; факт отсутствия таких уязвимостей здесь не заявляется.",
                "Идентификаторы находок: " + ", ".join(identifiers) + ".",
            ]))
    title = "Отчёт о готовности продукта GateForge для курсовой работы"
    md = ["# "+title, "", conclusion, "", "| Проверка | Результат | Секунды | Источник |", "|---|---|---:|---|"]
    md += ["| "+" | ".join(row)+" |" for row in rows]
    for heading, paragraphs in sections:
        md += ["", "## "+heading, "", "\n\n".join(paragraphs)]
    (ROOT/"docs/READINESS_REPORT.md").write_text("\n".join(md)+"\n", encoding="utf-8")
    doc = Document()
    section = doc.sections[0]
    section.page_width, section.page_height = Inches(8.5), Inches(11)
    section.top_margin = section.bottom_margin = Inches(.7)
    style = doc.styles["Normal"]
    style.font.name, style.font.size = "Calibri", Pt(10.5)
    style.paragraph_format.space_after = Pt(5)
    style.paragraph_format.line_spacing = 1
    title_style = doc.styles["Title"]
    title_style.font.size = Pt(20)
    title_style.font.color.rgb = RGBColor(0, 0, 0)
    for element in doc.styles.element.iter():
        for child in list(element):
            if child.tag.endswith("}pBdr"): element.remove(child)
    doc.add_paragraph(title, "Title")
    doc.add_paragraph(conclusion)
    table = doc.add_table(rows=1, cols=3)
    table.style = "Light Shading Accent 1"
    for cell, text in zip(table.rows[0].cells, ["Проверка", "Результат", "Секунды"]): cell.text = text
    for row in rows:
        for cell, text in zip(table.add_row().cells, row[:3]): cell.text = text
    for heading, paragraphs in sections:
        doc.add_heading(heading, level=1)
        for paragraph in paragraphs: doc.add_paragraph(paragraph)
    for element in doc.element.iter():
        for child in list(element):
            if child.tag.endswith("}pBdr"): element.remove(child)
    output = ROOT/"reports/GateForge_Readiness_Report.docx"
    output.parent.mkdir(exist_ok=True)
    doc.save(output)
    import zipfile
    with zipfile.ZipFile(ROOT/"reports/Readiness_Evidence.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for directory in (EVIDENCE, ROOT/"artifacts/fuzz", ROOT/"artifacts/build", ROOT/"artifacts/security"):
            if not directory.exists(): continue
            for path in directory.rglob("*"):
                if path.is_file() and path.suffix in (".log", ".json", ".jsonl", ".xml", ".out") and "render" not in path.parts and not any(p.startswith("pytest-") for p in path.parts):
                    archive.write(path, str(path.relative_to(ROOT)))


if __name__ == "__main__":
    create()
