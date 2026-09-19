# cdnnow-test-golang-14

Проект переписывает исходный Python-сервер и генератор нагрузки на Go.
C и Rust библиотеки сохранены как есть. Добавлены Prometheus-метрики,
режимы вычислений, pprof, Docker-сборка и мониторинг.

Исходное ТЗ лежит в `docs/TS.md`.

## Что было дано и что сделано

Дано:

- `docs/TS.md` - исходное ТЗ.
- `c_lib/` - C-библиотека с функцией `add`.
- `rust_lib/` - Rust-библиотека с функцией `sub`.
- `build.sh` - скрипт сборки библиотек.
- `calculator_server.py` - исходный HTTP-сервер на Python.
- `generator.py` - исходный генератор нагрузки на Python.

Сделано:

- Go-сервер `cmd/server`.
- Go-генератор `cmd/generator`.
- Go-healthcheck `cmd/healthcheck`.
- Внутренние пакеты `internal/`.
- Makefile для сборки, тестов и бенчмарков.
- Dockerfile с multi-stage сборкой.
- Docker Compose для Prometheus и Grafana.
- Тесты и бенчмарки.

Python-файлы и `build.sh` сохранены как исходный контекст, в проекте не используются.

## Соответствие ТЗ

- `POST /calc?num=X` - есть. Обрабатывается максимально быстро.
- `/metrics` - есть. Формат Prometheus.
- RPS за последние 60 секунд - есть. 60 значений по секундам.
- p95 и p99 для C - есть.
- p95 и p99 для Rust - есть.
- `generator` переписан на Go - есть.
- Дополнительно: 
    - режимы вычислений: `sync`, `async`, `parallel`
    - очередь (в асинхронных режимах)
    - pprof
    - healthcheck
    - Docker Compose
    - Grafana
    - в `lib.rs` исправлен баг (компилятор выкидывал нагрузочный цикл).

## Архитектура

- C-библиотека `libcalculator.so` экспортирует `add`.
- Rust-библиотека `libcalculator_rust.so` экспортирует `sub`.
- Пакет `internal/operators` загружает `.so` через `dlopen` и `dlsym`.
- Пакет `internal/calculators` реализует режимы вычислений.
- Пакет `internal/metrics` собирает RPS и гистограммы длительностей.
- Пакет `internal/api` содержит HTTP-роуты.
- `cmd/server` собирает все вместе.
- `cmd/generator` создает нагрузку.
- `cmd/healthcheck` проверяет `/ping`.

## Требования

- Go 1.26 или новее.
- GCC.
- Rust и Cargo.
- Linux. Код использует `dlopen` и `.so`.
- Docker и Docker Compose - опционально.

## Быстрый старт

Сборка:

```bash
make build
```

Запуск сервера:

```bash
./bin/server
```

Запуск генератора:

```bash
./bin/generator
```

Проверка:

```bash
curl http://localhost:8080/ping
curl -X POST 'http://localhost:8080/calc?num=42'
curl http://localhost:8080/metrics
```

Через Docker:

```bash
cd monitoring
docker compose up --build
```

После запуска:

- сервер: `http://localhost:8080`
- pprof: `http://localhost:6060/debug/pprof/`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000`

## Сборка

Основные цели Makefile:

- `make build-libs` - собрать C и Rust библиотеки.
- `make build-server` - собрать Go-сервер.
- `make build-generator` - собрать Go-генератор.
- `make build` - собрать все.
- `make test` - запустить тесты.
- `make bench` - запустить бенчмарки.
- `make clean` - удалить `bin`, `tmp` и `.so`.

Dockerfile использует три стадии:

1. `rust-builder` - собирает C и Rust библиотеки.
2. `go-builder` - собирает Go-сервер и healthcheck.
3. `distroless/cc-debian13` - минимальный runtime.

## Конфигурация сервера

Флаги `cmd/server`:

- `-host` - адрес привязки. По умолчанию `0.0.0.0`.
- `-port` - порт. По умолчанию `8080`.
- `-c-lib` - путь к `libcalculator.so`.
- `-rust-lib` - путь к `libcalculator_rust.so`.
- `-interval` - период печати sum и sub. По умолчанию `5.0` секунд.
- `-calc-mode` - режим вычислений: `sync`, `async`, `parallel`.
- `-queue-size` - размер очереди для `async` и `parallel`. По умолчанию `1024`.
- `-pprof-addr` - адрес pprof. По умолчанию `localhost:6060`, пустая строка отключает pprof.

Режимы:

- `sync` - клиент ждет завершения вычислений.
- `async` - вычисления в отдельной горутине. Очередь. При переполнении 503.
- `parallel` - как `async`, но `add` и `sub` выполняются в отдельных горутинах.

## API

`POST /calc?num=X`

- 200 - `ok`.
- 400 - нет `num` или `num` не целое.
- 503 - перегрузка очереди.
- 500 - неизвестная ошибка.

`GET /ping`

- 200 - `ok`.

`GET /metrics`

- 200 - метрики в формате Prometheus.

## Метрики

Примеры метрик:

```text
calc_rps_1s{second="-1"} 123
calc_rps_1s{second="-2"} 120
calc_c_duration_ns{quantile="0.95"} 15000
calc_c_duration_ns{quantile="0.99"} 20000
calc_rust_duration_ns{quantile="0.95"} 15000
calc_rust_duration_ns{quantile="0.99"} 20000
```

Пояснения:

- `calc_rps_1s` - RPS за каждую секунду из последней минуты.
- `second` - смещение секунды. `0` - текущая (неполная) секунда.
- `calc_c_duration_ns` - длительность вызова C-функции `add` в наносекундах.
- `calc_rust_duration_ns` - длительность вызова Rust-функции `sub` в наносекундах.
- Гистограммы имеют окно 60 секунд. Ротация раз в секунду.
- В Grafana `second=0` исключается, потому что текущая секунда еще неполная.

## Нагрузочный генератор

Флаги `cmd/generator`:

- `-url` - endpoint. По умолчанию `http://localhost:8080/calc`.
- `-n` или `-threads` - число воркеров. По умолчанию `10`.
- `-interval` - пауза между запросами в секундах. `0` означает без паузы.
- `-timeout` - таймаут HTTP-запроса в секундах.
- `-no-keep-alive` - отключает keep-alive.
- `-percentiles` - считать и печатать перцентили.

Пример:

```bash
./bin/generator -url 'http://localhost:8080/calc' -n 50 -interval 0.001
```

По завершении работы генератор выводит общее количество `OK` и `Error`. С флагом `-percentiles` дополнительно выводит HDR-гистограмму времени выполнения запросов.

## Мониторинг

`monitoring/docker-compose.yml` поднимает:

- `server` - Go-сервер.
- `prometheus` - сбор метрик.
- `grafana` - визуализация.

Prometheus слушает `9090`. Grafana слушает `3000`.
Дашборд провижинится из `monitoring/grafana/dashboards/`.
Источник данных - `monitoring/grafana/provisioning/datasources/prometheus.yml`.

## Тесты и бенчмарки

Запуск тестов:

```bash
make test
```

Запуск бенчмарков:

```bash
make bench
```

Отдельные бенчмарки:

- `internal/operators` - `Add`, `Sub`, Go-контроль.
- `internal/calculators` - режимы калькулятора.
- `internal/api` - HTTP-обработчики.
- `cmd/hdr-test` - быстрый прогон HDR-гистограммы.

## Производительность и fair-play

Документы:

- `docs/fair-play.md` - выравнивание условий для C и Rust.
- `docs/ryzen-volatile-mystery.md` - почему на Ryzen `volatile` перестал тормозить.
- `docs/panics.md` - как выглядят паники и ошибки.
- `docs/build-link.md` - заметки про сборку и линковку.

Коротко: C и Rust приведены к одинаковым условиям.
`volatile` в C и `black_box` в Rust дают сравнимые числа.
На Ryzen memory renaming скрывает часть задержек памяти.

## Структура проекта

```text
cmd/
  generator/    - генератор нагрузки
  healthcheck/  - проверка /ping
  hdr-test/     - тест HDR-гистограммы
  server/       - основной сервер
internal/
  api/          - HTTP-роуты
  calculators/  - режимы вычислений
  metrics/      - агрегация метрик
  operators/    - загрузка C и Rust
  pools/        - пулы батчей
  queue/        - дек на кольцевом буфере
c_lib/          - C-библиотека
rust_lib/       - Rust-библиотека
monitoring/     - Prometheus и Grafana
docs/           - документация
scripts/        - вспомогательные скрипты
```

## Ограничения и TODO

- `CountRequests` вызывается до валидации `num`. Битые запросы попадают в RPS.
- Ошибки в `/calc` отдельно не метрикуются.
- `ParallelCalculator` может рассинхронизировать `sum` и `sub` при сбое.
  Сейчас ошибок нет, но поведение стоит помнить.
- `SyncCalculator` держит мьютекс во время вычислений.
- Проект рассчитан на Linux из-за `dlopen`.
- Python-версии и `build.sh` не поддерживаются. Оставлены как исходный контекст.
