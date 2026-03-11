.PHONY: build run clean test dev dev-up dev-down help

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build      Build binary to ./bin/cgram-server"
	@echo "  run        Run without building (go run)"
	@echo "  dev        Start database and run app"
	@echo "  dev-up     Start development database"
	@echo "  dev-down   Stop development database"
	@echo "  test       Run tests"
	@echo "  clean      Remove ./bin directory"
	@echo "  help       Show this help"

build:
	go build -o ./bin/cgram-server ./cmd/server

run:
	go run ./cmd/server

dev: dev-up
	go run ./cmd/server

dev-up:
	docker compose -f docker-compose.dev.yml up -d

dev-down:
	docker compose -f docker-compose.dev.yml down

clean:
	rm -rf ./bin

test:
	go test ./...
