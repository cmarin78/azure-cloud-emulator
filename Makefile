.PHONY: all build run run-tls test test-race test-cover vet verify clean docker-build docker-run help

BINARY := bin/azure-emulator
PORT := 10000

all: build

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/azure-emulator

run:
	go run ./cmd/azure-emulator -addr :$(PORT)

run-tls:
	go run ./cmd/azure-emulator -addr :$(PORT) -tls

test:
	go test ./...

test-race:
	go test -v -race ./...

test-cover:
	go test -v -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1

vet:
	go vet ./...

verify:
	go mod verify
	go mod tidy
	git diff --exit-code go.mod go.sum

clean:
	rm -rf bin/ coverage.out .azure-emulator-data/

docker-build:
	docker build -t azure-cloud-emulator:latest .

docker-run:
	docker run -p $(PORT):10000 -v azure-emulator-data:/data azure-cloud-emulator:latest

help:
	@echo "Available make targets:"
	@echo "  build        - Build local binary in bin/"
	@echo "  run          - Run emulator locally on port $(PORT)"
	@echo "  run-tls      - Run emulator locally with self-signed TLS"
	@echo "  test         - Run unit tests"
	@echo "  test-race    - Run unit tests with race detector"
	@echo "  test-cover   - Run tests with coverage profile and print summary"
	@echo "  vet          - Run go vet"
	@echo "  verify       - Run go mod verify and check tidy diff"
	@echo "  clean        - Remove local build artifacts and coverage files"
	@echo "  docker-build - Build Docker container image"
	@echo "  docker-run   - Run emulator in Docker container"
