# --- Собираем C и Rust библиотеки ---
FROM rust:1.98.1-slim-trixie AS lib-builder

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY c_lib ./c_lib
COPY rust_lib/src        ./rust_lib/src
COPY rust_lib/Cargo.toml ./rust_lib/

RUN ls && mkdir -p bin \
    && gcc -shared -fPIC -O2 -o ./bin/libcalculator.so c_lib/calculator.c \
    && (cd rust_lib && cargo build --release) \
    && cp rust_lib/target/release/libcalculator_rust.so ./bin


# --- Собираем Go-сервер ---
FROM golang:1.26.8-trixie AS go-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/healthcheck ./cmd/healthcheck
COPY cmd/server ./cmd/server
COPY internal   ./internal
RUN CGO_ENABLED=1 go build -o bin/server ./cmd/server \
    && CGO_ENABLED=0 go build -o bin/healthcheck ./cmd/healthcheck


# --- Собираем минимальный рабочий образ ---
FROM gcr.io/distroless/cc-debian13@sha256:4594d59540d1948417f6ca2829ddd9294493a7c68b7528f4dd459de7f203a750

WORKDIR /app

COPY --from=lib-builder /app/bin/ ./bin/
COPY --from=go-builder  /app/bin/ ./bin/

EXPOSE 8080
USER 65535:65535
ENTRYPOINT ["/app/bin/server"]
