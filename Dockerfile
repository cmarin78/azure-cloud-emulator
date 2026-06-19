# --- Etapa de build ---
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Cacheable: solo se reinstalan dependencias si go.mod/go.sum cambian.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/azure-emulator ./cmd/azure-emulator

# --- Etapa final: imagen mínima, sin toolchain de Go ---
FROM alpine:3.20

RUN apk add --no-cache ca-certificates && \
    addgroup -S emulator && adduser -S emulator -G emulator

WORKDIR /app
COPY --from=builder /out/azure-emulator ./azure-emulator
COPY web/console ./web/console

RUN mkdir -p /data && chown -R emulator:emulator /app /data
USER emulator

ENV AZURE_EMULATOR_ADDR=:10000
ENV AZURE_EMULATOR_DB=/data/azure-emulator.db
ENV AZURE_EMULATOR_WEB=/app/web/c