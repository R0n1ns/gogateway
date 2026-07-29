# Разработка и Git workflow

## Подготовка

```powershell
git clone https://github.com/R0n1ns/gogateway.git
cd gogateway
go mod download
go test ./...
```

Полезные команды собраны в `Makefile`: `build`, `test`, `test-race`, `cover`, `validate`, `policy-test`, `fuzz`, `bench`, `docker-up`, `docker-down`.

## Стиль кода

- пакеты имеют одну понятную ответственность;
- зависимости направлены к интерфейсам, HTTP handler не знает concrete repository;
- `context.Context` передаётся первым аргументом для I/O;
- ошибки оборачиваются с контекстом и проверяются через `errors.Is/As`;
- background goroutine имеет lifecycle/`Close`;
- shared state защищено mutex/atomic и проверяется `-race`;
- комментарии к нетривиальным функциям, методам и структурам пишутся по-русски и объясняют причину/invariant;
- exported API внутри проекта сохраняется небольшим; преждевременные abstraction и глобальное состояние не добавляются.

## Порядок изменения

1. Создайте небольшую ветку.
2. Добавьте failing test или policy case.
3. Реализуйте минимальный законченный блок.
4. Выполните `gofmt`, targeted tests, затем `go test ./...`.
5. Для concurrency/network/storage выполните `go test -race ./...`.
6. Обновите config schema и docs, если изменён контракт.
7. Создайте тематический коммит с русским заголовком и коротким описанием причины.

Пример:

```powershell
git add internal/policy configs/gateforge.schema.json docs/POLICY_ENGINE.md
git commit -m "Добавить проверку нового условия политики" `
  -m "Скомпилировать условие при публикации snapshot и покрыть корректные и ошибочные варианты unit-тестами."
```

## Как пушить по одному коммиту

В этом репозитории remote называется `master`, ветка тоже `master`. Сначала посмотрите очередь:

```powershell
git fetch master
git log --reverse --oneline master/master..master
```

Затем отправляйте каждый SHA по порядку, от старого к новому:

```powershell
git push master <SHA_ПЕРВОГО>:refs/heads/master
git push master <SHA_ВТОРОГО>:refs/heads/master
```

Каждый push должен быть fast-forward. Не используйте `--force`. Для обычного remote `origin` замените первое `master` в командах на `origin`.

## Добавление поля конфигурации

Измените последовательно:

1. `internal/config/types.go`;
2. defaults/normalize/validation/deep copy;
3. `configs/gateforge.schema.json`;
4. runtime consumer;
5. unit tests и CLI validation;
6. `configs/gateforge.json` и `docs/CONFIGURATION.md`.

Secrets разрешайте к env expansion только явно; raw snapshots не должны содержать resolved value.

## Добавление Admin endpoint

Сначала расширьте `DataPlane`/`ControlPlane`, затем handler route, size limit/auth/audit, stable error contract и handler tests. Operational endpoint без `/api/` должен быть намеренно публичным; иначе он обязан проходить API-key middleware.

## CI и release

Pull request проходит quality, CodeQL и container security workflows. Release создаётся только annotated tag после checklist из [DEPLOYMENT.md](DEPLOYMENT.md). Локальные коммиты в рамках разработки не пушатся автоматически.